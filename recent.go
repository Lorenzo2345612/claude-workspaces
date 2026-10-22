package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// readTranscriptSummary returns the session's latest custom title and its first real prompt.
func readTranscriptSummary(path string) (title, first string) {
	fh, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer fh.Close()
	r := bufio.NewReaderSize(fh, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
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
		if err == io.EOF || err != nil {
			return title, first
		}
	}
}

func bytesContains(b []byte, s string) bool { return strings.Contains(string(b), s) }

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
