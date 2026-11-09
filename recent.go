package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// recentConv is a Claude Code conversation found on disk for a directory.
type recentConv struct {
	ID, Title, First string
	Alias            string // claude alias whose config dir holds it
	Cwd              string // where it was started, when known (resume by ID); else the tab's dir
	Modified         time.Time
}

var projectKeyRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// claudeProjectDir is where a Claude config dir keeps the transcripts of conversations started in dir.
func claudeProjectDir(configDir, dir string) string {
	return filepath.Join(configDir, "projects", projectKeyRe.ReplaceAllString(dir, "-"))
}

// recentConversations returns the latest conversations started in dir under any claude alias
// (each alias may have its own config dir), newest first.
func recentConversations(dir string, limit int, exclude map[string]bool, cfg Config) []recentConv {
	aliases := cfg.claudeAliases()
	type fileAt struct {
		path, alias string
		mod         time.Time
	}
	var fs []fileAt
	seenDir := map[string]bool{}
	for _, a := range aliases {
		if a.isCodex() {
			continue
		}
		// Config dirs often share one projects folder through a symlink: list each real folder once.
		pd := claudeProjectDir(a.configDir(), dir)
		real, err := filepath.EvalSymlinks(pd)
		if err != nil || seenDir[real] {
			continue
		}
		seenDir[real] = true
		files, _ := filepath.Glob(filepath.Join(pd, "*.jsonl"))
		for _, f := range files {
			if st, err := os.Stat(f); err == nil {
				fs = append(fs, fileAt{f, a.Name, st.ModTime()})
			}
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mod.After(fs[j].mod) })
	var out []recentConv
	for _, f := range fs {
		if len(out) >= limit {
			break
		}
		id := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
		if exclude[id] {
			continue
		}
		title, first := readTranscriptSummary(f.path)
		if first == "" {
			continue // never got a real prompt
		}
		out = append(out, recentConv{ID: id, Title: title, First: first, Alias: guessAlias(cfg, id, f.path), Modified: f.mod})
	}
	return out
}

// transcriptSummaryReads counts transcript summary reads, for tests that guard against
// rereading transcripts.
var transcriptSummaryReads atomic.Int64

// readTranscriptSummary returns the session's latest custom title and its first real prompt.
func readTranscriptSummary(path string) (title, first string) {
	title, first, _ = readTranscriptSummaryFrom(path, 0, "", "")
	return title, first
}

// readTranscriptSummaryFrom continues a summary from byte offset off, where an earlier read of
// the same append-only transcript ended with title and first. It returns the updated summary and
// the offset just past the last complete line, from which the next read resumes; a line still
// being written is left for that next read.
func readTranscriptSummaryFrom(path string, off int64, title, first string) (string, string, int64) {
	transcriptSummaryReads.Add(1)
	fh, err := os.Open(path)
	if err != nil {
		return title, first, off
	}
	defer fh.Close()
	if _, err := fh.Seek(off, io.SeekStart); err != nil {
		return title, first, off
	}
	r := bufio.NewReaderSize(fh, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil { // EOF: a trailing line without newline is incomplete
			return title, first, off
		}
		off += int64(len(line))
		switch {
		case bytesContains(line, `"type":"custom-title"`):
			var e struct {
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(line, &e) == nil && e.CustomTitle != "" {
				title = e.CustomTitle
			}
		case first == "" && bytesContains(line, `"type":"user"`):
			first = firstPrompt(line)
		}
	}
}

func bytesContains(b []byte, s string) bool { return bytes.Contains(b, []byte(s)) }

// firstPrompt extracts typed text from a user entry, ignoring meta entries, tool results and
// command / reminder envelopes.
func firstPrompt(line []byte) string {
	var e struct {
		IsMeta      bool `json:"isMeta"`
		IsSidechain bool `json:"isSidechain"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.IsMeta || e.IsSidechain {
		return ""
	}
	var text string
	if json.Unmarshal(e.Message.Content, &text) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type == "text" {
				text = p.Text
				break
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

var monthsES = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

func whenES(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "ahora"
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %d h", int(d.Hours()))
	case d < 48*time.Hour:
		return "ayer"
	}
	return fmt.Sprintf("%d %s", t.Day(), monthsES[t.Month()-1])
}
