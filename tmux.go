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

func (t Tmux) createWindow(w windowSpec) (string, error) {
	var args []string
	if w.first {
		args = []string{"new-session", "-d", "-s", w.session}
	} else {
		args = []string{"new-window", "-d", "-t", w.session + ":"}
	}
	args = append(args, "-P", "-F", "#{window_id}", "-n", w.name, "-c", w.slot.Cwd,
		"-e", "CW_WS="+t.WS, "-e", "CW_SLOT="+w.slot.ID)
	for _, kv := range w.env {
		args = append(args, "-e", kv)
	}
	args = append(args, w.argv...)
	id, err := t.Run(args...)
	if err != nil {
		return "", err
	}
	_, err = t.Run(
		"set", "-w", "-t", id, "@cw_slot", w.slot.ID, ";",
		"set", "-w", "-t", id, "@cw_kind", w.slot.Kind, ";",
		"set", "-w", "-t", id, "@cw_alias", w.slot.Alias, ";",
		"set", "-w", "-t", id, "@cw_cwd", w.slot.Cwd, ";",
		"set", "-w", "-t", id, "@cw_sid", w.slot.SessionID, ";",
		"set", "-w", "-t", id, "@cw_status", w.display, ";",
		"set", "-w", "-t", id, "@cw_args", argsToOption(w.slot.Args), ";",
		"set", "-w", "-t", id, "@cw_claude_alias", w.slot.ClaudeAlias,
	)
	return id, err
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

// capture reads the running layout back into a Workspace.
func (t Tmux) capture() (*Workspace, error) {
	sessions, err := t.Lines("list-sessions", "-F",
		strings.Join([]string{"#{session_name}", "#{@cw_title}", "#{@cw_dir}"}, sep))
	if err != nil {
		return nil, err
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i][0] < sessions[j][0] })

	windows, err := t.Lines("list-windows", "-a", "-F", strings.Join([]string{
		"#{session_name}", "#{window_index}", "#{window_active}", "#{window_name}",
		"#{@cw_slot}", "#{@cw_kind}", "#{@cw_alias}", "#{@cw_cwd}", "#{@cw_sid}", "#{@cw_status}",
		"#{pane_current_path}", "#{@cw_args}", "#{@cw_claude_alias}",
	}, sep))
	if err != nil {
		return nil, err
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i][0] != windows[j][0] {
			return windows[i][0] < windows[j][0]
		}
		a, _ := strconv.Atoi(windows[i][1])
		b, _ := strconv.Atoi(windows[j][1])
		return a < b
	})

	active, _ := t.Run("show", "-gqv", "@cw_active")
	w := &Workspace{Name: t.WS}
	for _, s := range sessions {
		tab := Tab{Title: s[1], Dir: s[2]}
		if tab.Title == "" {
			tab.Title = s[0]
		}
		if tab.Dir == "" {
			tab.Dir = homeDir()
		}
		for _, win := range windows {
			if win[0] != s[0] {
				continue
			}
			slot := Slot{ID: win[4], Kind: win[5], Alias: win[6], Cwd: win[7], SessionID: win[8], Status: win[9],
				Args: argsFromOption(win[11]), ClaudeAlias: win[12]}
			if slot.Kind == kindHome {
				continue
			}
			if slot.Kind == "" { // a window opened with plain tmux keys: keep it as a shell
				slot.Kind = kindShell
			}
			if slot.ID == "" {
				slot.ID = newUUID()
			}
			if slot.Kind == kindShell {
				slot.Alias = win[3]
				slot.Cwd = win[10]
				slot.SessionID, slot.Status = "", ""
			} else {
				slot.Alias = win[3] // the window name is the alias the user sees and may rename
				if slot.Status == statusSleeping || slot.Status == "" {
					slot.Status = statusIdle
				}
			}
			if slot.Cwd == "" {
				slot.Cwd = tab.Dir
			}
			if win[2] == "1" {
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
