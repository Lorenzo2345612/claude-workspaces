package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type dirSuggestion struct {
	path   string
	recent bool
}

// recentDirs lists the base directories of every tab in every saved workspace, plus the tabs of
// the running workspace, most recently saved first.
func recentDirs(t Tmux) []string {
	type dated struct {
		dir string
		at  int64
	}
	var all []dated
	for _, name := range savedWorkspaces() {
		w, err := loadWorkspace(name)
		if err != nil {
			continue
		}
		for _, tab := range w.Tabs {
			all = append(all, dated{tab.Dir, w.SavedAt.Unix()})
		}
	}
	if rows, err := t.Lines("list-sessions", "-F", "#{@cw_dir}"); err == nil {
		for _, r := range rows {
			all = append(all, dated{r[0], 1 << 62})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at > all[j].at })
	seen := map[string]bool{}
	var out []string
	for _, d := range all {
		if d.dir != "" && !seen[d.dir] && isDir(d.dir) {
			seen[d.dir] = true
			out = append(out, d.dir)
		}
	}
	return out
}

// workDirs lists the directories one level under ~/work (or ~ when there is no ~/work).
func workDirs() []string {
	root := filepath.Join(homeDir(), "work")
	if !isDir(root) {
		root = homeDir()
	}
	return subdirs(root)
}

func subdirs(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// suggestDirs filters candidates for what the user typed. A query that looks like a path
// ("~/…", "/…", "./…") completes against the filesystem; anything else matches as a substring.
func suggestDirs(query string, recents, work []string, limit int) []dirSuggestion {
	q := strings.TrimSpace(query)
	var out []dirSuggestion
	seen := map[string]bool{}
	add := func(p string, recent bool) {
		if !seen[p] && len(out) < limit {
			seen[p] = true
			out = append(out, dirSuggestion{p, recent})
		}
	}
	if strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || strings.HasPrefix(q, ".") {
		abs, err := expandPath(q)
		if err == nil {
			parent, prefix := abs, ""
			if !strings.HasSuffix(q, "/") {
				parent, prefix = filepath.Dir(abs), filepath.Base(abs)
			}
			if isDir(abs) && !strings.HasSuffix(q, "/") {
				add(abs, false)
			}
			for _, d := range subdirs(parent) {
				if strings.HasPrefix(strings.ToLower(filepath.Base(d)), strings.ToLower(prefix)) {
					add(d, false)
				}
			}
		}
		return out
	}
	match := func(p string) bool {
		return q == "" || strings.Contains(strings.ToLower(shortPath(p)), strings.ToLower(q))
	}
	for _, d := range recents {
		if match(d) {
			add(d, true)
		}
	}
	for _, d := range work {
		if match(d) {
			add(d, false)
		}
	}
	return out
}
