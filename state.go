package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	kindClaude = "claude"
	kindShell  = "shell"

	statusIdle     = "idle"
	statusBusy     = "busy"
	statusWaiting  = "waiting"  // blocked on a permission prompt — still mid-task
	statusSleeping = "sleeping" // lazy-start placeholder; persisted as idle
)

// Workspace is the saved state of one tmux server: its tabs and every session inside them.
type Workspace struct {
	Name string `json:"name"`
	// Title is the name the user sees; Name stays the workspace's id (tmux socket, file name),
	// because a running tmux server cannot be renamed.
	Title     string    `json:"title,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
	ActiveTab int       `json:"active_tab"`
	Tabs      []Tab     `json:"tabs"`
	// Dir is the workspace's default folder: where new tabs start.
	Dir string `json:"dir,omitempty"`
}

// Tab groups the sessions of one task under a title and a base directory.
type Tab struct {
	Title      string `json:"title"`
	Dir        string `json:"dir"`
	ActiveSlot int    `json:"active_slot"`
	Slots      []Slot `json:"slots"`
	// Layouts holds, per view, the tmux layout of a view that shows several sessions ("" for one).
	Layouts []string `json:"layouts,omitempty"`
}

// Slot is one tmux window: a Claude session or a plain shell.
// ID is stable for the life of the window; SessionID is Claude's current conversation and can
// change under /clear or /resume — the hooks keep it up to date.
type Slot struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Alias     string `json:"alias"`
	Cwd       string `json:"cwd"`
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status,omitempty"`
	// Args are this session's own extra claude arguments, added after the global claude_args.
	Args []string `json:"args,omitempty"`
	// ClaudeAlias is the configured claude alias (env + args) the session runs with.
	ClaudeAlias string `json:"claude_alias,omitempty"`
	// View groups the sessions shown together: slots with the same View share one tmux window.
	View int `json:"view,omitempty"`
	// Suspended sessions keep their place and conversation but run no Claude process until
	// they are resumed (Enter in the session, or Reanudar).
	Suspended bool `json:"suspended,omitempty"`
}

func (s Slot) wasWorking() bool {
	return s.Kind == kindClaude && (s.Status == statusBusy || s.Status == statusWaiting)
}

func loadWorkspace(ws string) (*Workspace, error) {
	data, err := os.ReadFile(workspaceFile(ws))
	if err != nil {
		return nil, err
	}
	var w Workspace
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// saveWorkspace writes the state and keeps the previous version as <name>.json.bak when it changed.
func saveWorkspace(w *Workspace) error {
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := workspaceFile(w.Name)
	if prev, err := os.ReadFile(path); err == nil && !sameIgnoringSavedAt(prev, data) {
		_ = writeFileAtomic(path+".bak", prev)
	}
	return writeFileAtomic(path, data)
}

func sameIgnoringSavedAt(a, b []byte) bool {
	strip := func(d []byte) []byte {
		var lines [][]byte
		for _, l := range bytes.Split(d, []byte("\n")) {
			if !bytes.Contains(l, []byte(`"saved_at"`)) {
				lines = append(lines, l)
			}
		}
		return bytes.Join(lines, []byte("\n"))
	}
	return bytes.Equal(strip(a), strip(b))
}

func savedWorkspaces() []string {
	entries, _ := os.ReadDir(workspacesDir())
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".json") && !e.IsDir() {
			names = append(names, strings.TrimSuffix(n, ".json"))
		}
	}
	sort.Strings(names)
	return names
}

// findTranscript looks for the session's transcript, first in preferDir (the config dir of the
// claude alias it will run with), then in the config dirs of every other alias. A session that
// never received a prompt has none, and `claude --resume` would fail on it.
func findTranscript(sessionID, preferDir string, otherDirs []string) (path string, inPreferred bool) {
	if sessionID == "" || strings.ContainsAny(sessionID, "/*?[") {
		return "", false
	}
	for i, dir := range append([]string{preferDir}, otherDirs...) {
		matches, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sessionID+".jsonl"))
		if len(matches) > 0 {
			return matches[0], i == 0
		}
	}
	return "", false
}

// displayTitle is the name shown for a workspace: its title, or its id when it has none.
func (w *Workspace) displayTitle() string {
	if w != nil && w.Title != "" {
		return w.Title
	}
	if w != nil {
		return w.Name
	}
	return ""
}

// resolveWorkspace turns what the user typed — a workspace id or the title it was renamed to —
// into the workspace id.
func resolveWorkspace(nameOrTitle string) string {
	if _, err := os.Stat(workspaceFile(nameOrTitle)); err == nil || (Tmux{WS: nameOrTitle}).Running() {
		return nameOrTitle
	}
	for _, n := range savedWorkspaces() {
		if w, err := loadWorkspace(n); err == nil && strings.EqualFold(w.Title, nameOrTitle) {
			return n
		}
	}
	return nameOrTitle
}
