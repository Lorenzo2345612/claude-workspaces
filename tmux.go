package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// sep separates fields in tmux -F output; it cannot appear in a title, alias or path we accept.
const sep = "|~|"

// Tmux talks to the private tmux server of one workspace (socket "cw-<name>"),
// so it never touches a tmux the user runs on their own.
type Tmux struct {
	WS string
}

func (t Tmux) Socket() string { return "cw-" + t.WS }

func (t Tmux) baseArgs() []string {
	return []string{"-u", "-L", t.Socket(), "-f", tmuxConfFile(t.WS)}
}

func (t Tmux) Command(args ...string) *exec.Cmd {
	return exec.Command("tmux", append(t.baseArgs(), args...)...)
}

// Run executes one or more tmux commands (separate commands with a ";" argument) and returns stdout.
func (t Tmux) Run(args ...string) (string, error) {
	var out, errb bytes.Buffer
	c := t.Command(args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

func (t Tmux) Running() bool {
	return t.Command("has-session").Run() == nil
}

func (t Tmux) Lines(args ...string) ([][]string, error) {
	out, err := t.Run(args...)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			rows = append(rows, strings.Split(l, sep))
		}
	}
	return rows, nil
}

// nextTabSession returns the next tmux session name ("t01", "t02", ...). Sessions are named by
// creation order so tmux's name-sorted loops keep tabs in order; the visible title is @cw_title.
func (t Tmux) nextTabSession() string {
	max := 0
	if rows, err := t.Lines("list-sessions", "-F", "#{session_name}"); err == nil {
		for _, r := range rows {
			if n, err := strconv.Atoi(strings.TrimPrefix(r[0], "t")); err == nil && n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("t%02d", max+1)
}

type windowSpec struct {
	session string
	first   bool // creates the tmux session (the tab) along with its first window
	name    string
	slot    Slot
	argv    []string // empty: the default shell
	display string   // status shown in the bar (may differ from slot.Status, e.g. "sleeping")
	env     []string // KEY=VALUE for the window's process (the claude alias's variables)
}

// createWindow opens a slot in a new window (and the tab's session when first is true) and
// returns the window id.
func (t Tmux) createWindow(w windowSpec) (string, error) {
	var args []string
	if w.first {
		args = []string{"new-session", "-d", "-s", w.session}
	} else {
		args = []string{"new-window", "-d", "-t", w.session + ":"}
	}
	args = append(args, "-P", "-F", "#{window_id}"+sep+"#{pane_id}", "-n", w.name)
	ids, err := t.Run(append(args, t.processArgs(w)...)...)
	if err != nil {
		return "", err
	}
	f := strings.Split(ids, sep)
	return f[0], t.setSlotOptions(f[1], w)
}

// createPane opens a slot as a new pane of an existing view, next to target (a pane or window).
func (t Tmux) createPane(w windowSpec, target string) (string, error) {
	args := []string{"split-window", "-d", "-h", "-t", target, "-P", "-F", "#{pane_id}"}
	pane, err := t.Run(append(args, t.processArgs(w)...)...)
	if err != nil {
		return "", err
	}
	return pane, t.setSlotOptions(pane, w)
}

func (t Tmux) processArgs(w windowSpec) []string {
	args := []string{"-c", w.slot.Cwd, "-e", "CW_WS=" + t.WS, "-e", "CW_SLOT=" + w.slot.ID}
	for _, kv := range w.env {
		args = append(args, "-e", kv)
	}
	return append(args, w.argv...)
}

// setSlotOptions records a slot on its pane: every session of a view is a pane, and pane options
// are what the bar, the hooks and the save read.
func (t Tmux) setSlotOptions(pane string, w windowSpec) error {
	_, err := t.Run(
		"set", "-p", "-t", pane, "@cw_slot", w.slot.ID, ";",
		"set", "-p", "-t", pane, "@cw_kind", w.slot.Kind, ";",
		"set", "-p", "-t", pane, "@cw_alias", w.name, ";",
		"set", "-p", "-t", pane, "@cw_cwd", w.slot.Cwd, ";",
		"set", "-p", "-t", pane, "@cw_sid", w.slot.SessionID, ";",
		"set", "-p", "-t", pane, "@cw_status", w.display, ";",
		"set", "-p", "-t", pane, "@cw_args", argsToOption(w.slot.Args), ";",
		"set", "-p", "-t", pane, "@cw_claude_alias", w.slot.ClaudeAlias, ";",
		"set", "-p", "-t", pane, "@cw_suspended", boolOpt(w.slot.Suspended),
	)
	return err
}

func (t Tmux) setTabOptions(session string, tab Tab) error {
	_, err := t.Run(
		"set", "-t", session, "@cw_title", tab.Title, ";",
		"set", "-t", session, "@cw_dir", tab.Dir,
	)
	if err == nil {
		refreshTabLabel(t, session)
	}
	return err
}

// capture reads the running layout back into a Workspace: tabs, their views (tmux windows) in
// order, and the sessions (panes) of each view with the view's layout.
func (t Tmux) capture() (*Workspace, error) {
	sessions, err := t.Lines("list-sessions", "-F",
		strings.Join([]string{"#{session_name}", "#{@cw_title}", "#{@cw_dir}"}, sep))
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i][0] < sessions[j][0] })

	panes, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{session_name}", "#{window_index}", "#{window_active}", "#{pane_index}", "#{pane_active}",
		"#{@cw_slot}", "#{@cw_kind}", "#{@cw_alias}", "#{@cw_cwd}", "#{@cw_sid}", "#{@cw_status}",
		"#{pane_current_path}", "#{@cw_args}", "#{@cw_claude_alias}", "#{window_layout}", "#{window_panes}", "#{@cw_suspended}",
	}, sep))
	if err != nil {
		return nil, err
	}
	num := func(s string) int { n, _ := strconv.Atoi(s); return n }
	sort.Slice(panes, func(i, j int) bool {
		a, b := panes[i], panes[j]
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		if a[1] != b[1] {
			return num(a[1]) < num(b[1])
		}
		return num(a[3]) < num(b[3])
	})

	active, _ := t.Run("show", "-gqv", "@cw_active")
	w := &Workspace{Name: t.WS}
	if title, _ := t.Run("show", "-gqv", "@cw_ws_title"); title != t.WS {
		w.Title = title
	}
	w.Dir, _ = t.Run("show", "-gqv", optWSDir)
	for _, s := range sessions {
		tab := Tab{Title: s[1], Dir: s[2]}
		if tab.Title == "" {
			tab.Title = s[0]
		}
		if tab.Dir == "" {
			tab.Dir = homeDir()
		}
		view, lastWindow := -1, ""
		for _, p := range panes {
			if p[0] != s[0] || p[6] == kindHome {
				continue
			}
			if p[1] != lastWindow {
				view++
				lastWindow = p[1]
				layout := ""
				if num(p[15]) > 1 {
					layout = p[14]
				}
				tab.Layouts = append(tab.Layouts, layout)
			}
			slot := Slot{ID: p[5], Kind: p[6], Alias: p[7], Cwd: p[8], SessionID: p[9], Status: p[10],
				Args: argsFromOption(p[12]), ClaudeAlias: p[13], View: view, Suspended: p[16] == "1"}
			if slot.Kind == "" { // a pane opened with plain tmux keys: keep it as a shell
				slot.Kind = kindShell
			}
			if slot.ID == "" {
				slot.ID = newUUID()
			}
			if slot.Alias == "" {
				slot.Alias = "shell"
			}
			if slot.Kind == kindShell {
				slot.Cwd = p[11]
				slot.SessionID, slot.Status = "", ""
			} else if slot.Status == statusSleeping || slot.Status == "" {
				slot.Status = statusIdle
			}
			if slot.Cwd == "" {
				slot.Cwd = tab.Dir
			}
			if p[2] == "1" && p[4] == "1" {
				tab.ActiveSlot = len(tab.Slots)
			}
			tab.Slots = append(tab.Slots, slot)
		}
		if s[0] == active {
			w.ActiveTab = len(w.Tabs)
		}
		w.Tabs = append(w.Tabs, tab)
	}
	return w, nil
}

// runningWorkspaces lists the cw-* tmux servers that answer on this machine.
func runningWorkspaces() []string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	entries, _ := os.ReadDir(fmt.Sprintf("%s/tmux-%d", dir, os.Getuid()))
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutPrefix(e.Name(), "cw-"); ok && validWorkspaceName(n) == nil && (Tmux{WS: n}).Running() {
			names = append(names, n)
		}
	}
	return names
}
