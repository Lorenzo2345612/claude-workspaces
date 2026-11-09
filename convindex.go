package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// convSummary is what the resume picker shows of a transcript. Reading one means scanning the
// whole file (a custom title can be set at any point), and transcripts add up to gigabytes, so
// summaries are kept in an index on disk and a transcript is reread only when its size or
// modification time changes.
type convSummary struct {
	Size  int64     `json:"size"`
	Mod   time.Time `json:"mod"`
	Read  int64     `json:"read"` // offset past the last complete line read: transcripts only grow
	Title string    `json:"title,omitempty"`
	First string    `json:"first,omitempty"`
	Cwd   string    `json:"cwd,omitempty"`
}

// maxIndexedPrompt is how much of the first prompt the index keeps, in characters.
const maxIndexedPrompt = 500

func convIndexFile() string { return filepath.Join(dataDir(), "conversations-index.json") }

func loadConvIndex() map[string]convSummary {
	idx := map[string]convSummary{}
	if raw, err := os.ReadFile(convIndexFile()); err == nil {
		_ = json.Unmarshal(raw, &idx)
	}
	return idx
}

// saveConvIndex writes the index atomically: several cw processes may scan at the same time.
func saveConvIndex(idx map[string]convSummary) {
	raw, err := json.Marshal(idx)
	if err != nil {
		return
	}
	path := convIndexFile()
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".conversations-index-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// scanConversations lists every resumable Claude conversation of every configured account,
// newest first, with its summary. Only transcripts that changed since the last scan are read.
func scanConversations(cfg Config) []foundConv {
	type file struct {
		path string
		st   os.FileInfo
	}
	var files []file
	seenDir, seenID := map[string]bool{}, map[string]bool{}
	for _, a := range cfg.claudeAliases() {
		if a.isCodex() {
			continue
		}
		projects := filepath.Join(a.configDir(), "projects")
		real, err := filepath.EvalSymlinks(projects)
		if err != nil || seenDir[real] {
			continue
		}
		seenDir[real] = true
		matches, _ := filepath.Glob(filepath.Join(projects, "*", "*.jsonl"))
		for _, path := range matches {
			id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if seenID[id] {
				continue
			}
			st, err := os.Stat(path)
			if err != nil {
				continue
			}
			seenID[id] = true
			files = append(files, file{path, st})
		}
	}

	old := loadConvIndex()
	idx := make(map[string]convSummary, len(files))
	var stale []file
	for _, f := range files {
		s, ok := old[f.path]
		switch {
		case ok && s.Size == f.st.Size() && s.Mod.Equal(f.st.ModTime()):
			idx[f.path] = s
		case ok && f.st.Size() >= s.Read:
			idx[f.path] = s // grew: resume from where the last read stopped
			stale = append(stale, f)
		default:
			stale = append(stale, f)
		}
	}
	if len(stale) > 0 {
		var mu sync.Mutex
		work := make(chan file)
		var wg sync.WaitGroup
		for range min(len(stale), max(1, runtime.NumCPU())) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := range work {
					mu.Lock()
					s := idx[f.path] // zero for a new transcript: read it from the start
					mu.Unlock()
					title, first, read := readTranscriptSummaryFrom(f.path, s.Read, s.Title, s.First)
					if r := []rune(first); len(r) > maxIndexedPrompt {
						first = string(r[:maxIndexedPrompt]) // pasted prompts can be huge; the picker shows a line
					}
					if first != "" && s.Cwd == "" {
						s.Cwd = transcriptCwd(f.path)
					}
					s.Size, s.Mod, s.Read, s.Title, s.First = f.st.Size(), f.st.ModTime(), read, title, first
					mu.Lock()
					idx[f.path] = s
					mu.Unlock()
				}
			}()
		}
		for _, f := range stale {
			work <- f
		}
		close(work)
		wg.Wait()
	}
	if len(stale) > 0 || len(idx) != len(old) {
		saveConvIndex(idx)
	}

	out := make([]foundConv, 0, len(files))
	for _, f := range files {
		s := idx[f.path]
		if s.First == "" {
			continue // never got a real prompt
		}
		id := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
		out = append(out, foundConv{
			conv:   recentConv{ID: id, Title: s.Title, First: s.First, Modified: s.Mod},
			cwd:    s.Cwd,
			path:   f.path,
			search: strings.ToLower(strings.Join([]string{id, s.Title, s.First, s.Cwd}, "\n")),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].conv.Modified.Equal(out[j].conv.Modified) {
			return out[i].conv.ID < out[j].conv.ID
		}
		return out[i].conv.Modified.After(out[j].conv.Modified)
	})
	return out
}

// filterConversations keeps the (at most 100) conversations of a scan that match the query by
// ID, title, first prompt or directory. It touches no transcript beyond the alias guess of the
// rows it returns, which g memoizes, so it is cheap enough to run on every keystroke.
func filterConversations(all []foundConv, query string, g *aliasGuesser) []foundConv {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []foundConv
	for _, f := range all {
		if q != "" && !strings.Contains(f.search, q) {
			continue
		}
		f.conv.Alias = g.guess(f.conv.ID, f.path)
		out = append(out, f)
		if len(out) == 100 {
			break
		}
	}
	return out
}
