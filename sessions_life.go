package main

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"strings"
)

// Suspending, resuming and reopening sessions, and the workspace's default folder — shared by
// the terminal version and the app.

const (
	optWSDir  = "@cw_ws_dir" // the workspace's default folder for new tabs
	optClosed = "@cw_closed" // the sessions closed through cw, newest first (Ctrl+Shift+T)
	maxClosed = 20
)

func baseName(dir string) string {
	if dir == "" {
		return "inicio"
	}
	return filepath.Base(dir)
}

func boolOpt(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// paneSlot reads the session recorded on a pane.
func paneSlot(t Tmux, pane string) (Slot, error) {
	out, err := t.Run("display", "-p", "-t", pane, strings.Join([]string{
		"#{@cw_slot}", "#{@cw_kind}", "#{@cw_alias}", "#{@cw_cwd}", "#{@cw_sid}", "#{@cw_args}", "#{@cw_claude_alias}",
		"#{@cw_status}", "#{@cw_suspended}", "#{pane_current_path}"}, sep))
	if err != nil {
		return Slot{}, err
	}
	f := strings.Split(out, sep)
	if len(f) != 10 || f[0] == "" {
		return Slot{}, errors.New("panel sin datos de cw")
	}
	s := Slot{ID: f[0], Kind: f[1], Alias: f[2], Cwd: f[3], SessionID: f[4], Args: argsFromOption(f[5]),
		ClaudeAlias: f[6], Status: f[7], Suspended: f[8] == "1"}
	if s.Kind == kindShell && f[9] != "" {
		s.Cwd = f[9]
	}
	return s, nil
}

// suspendPane stops the Claude process of a session and leaves it waiting, with its place and
// its conversation, until it is resumed. Terminals and sessions already asleep are left alone.
func suspendPane(ws, pane string) error {
	t := Tmux{WS: ws}
	s, err := paneSlot(t, pane)
	if err != nil {
		return err
	}
	if s.Kind != kindClaude || s.Status == statusSleeping {
		return nil
	}
	if _, err := t.Run("set", "-p", "-t", pane, "@cw_suspended", "1"); err != nil {
		return err
	}
	if err := respawnSlotMode(ws, pane, false, true); err != nil {
		return err
	}
	if session, err := t.Run("display", "-p", "-t", pane, "#{session_name}"); err == nil {
		refreshTabLabel(t, session)
	}
	return nil
}

// wakePane resumes a suspended (or lazily started) session: its placeholder starts Claude on Enter.
func wakePane(ws, pane string) error {
	t := Tmux{WS: ws}
	if st, _ := t.Run("display", "-p", "-t", pane, "#{@cw_status}"); st != statusSleeping {
		return nil
	}
	_, err := t.Run("send-keys", "-t", pane, "Enter")
	return err
}

// closedSlot is a session closed through cw, kept so it can be reopened.
type closedSlot struct {
	Slot Slot   `json:"slot"`
	Tab  string `json:"tab"` // the tmux session of its tab (terminal) or the app's tab id
}

// pushClosed remembers a session that is about to close.
func pushClosed(t Tmux, s Slot, tab string) {
	if s.ID == "" || s.Kind == kindHome {
		return
	}
	var list []closedSlot
	if v, _ := t.Run("show", "-gqv", optClosed); v != "" {
		_ = json.Unmarshal([]byte(v), &list)
	}
	s.Status, s.Suspended, s.View = statusIdle, false, 0
	list = append([]closedSlot{{Slot: s, Tab: tab}}, list...)
	if len(list) > maxClosed {
		list = list[:maxClosed]
	}
	if data, err := json.Marshal(list); err == nil {
		_, _ = t.Run("set", "-g", optClosed, string(data))
	}
}

// popClosed takes the most recently closed session; ok is false when there is none.
func popClosed(t Tmux) (closedSlot, bool) {
	var list []closedSlot
	if v, _ := t.Run("show", "-gqv", optClosed); v != "" {
		_ = json.Unmarshal([]byte(v), &list)
	}
	if len(list) == 0 {
		return closedSlot{}, false
	}
	data, _ := json.Marshal(list[1:])
	_, _ = t.Run("set", "-g", optClosed, string(data))
	c := list[0]
	c.Slot.ID = newUUID() // a new window: the old one is gone
	if !isDir(c.Slot.Cwd) {
		c.Slot.Cwd = homeDir()
	}
	return c, true
}

// workspaceDir is the default folder for new tabs: the workspace's own, else where cw was opened.
func workspaceDir(t Tmux) string {
	for _, opt := range []string{optWSDir, optLaunchAt} {
		if d, _ := t.Run("show", "-gqv", opt); d != "" && isDir(d) {
			return d
		}
	}
	return homeDir()
}

// checkDir expands and validates a folder typed by the user.
func checkDir(p string) (string, error) {
	abs, err := expandPath(p)
	if err != nil || !isDir(abs) {
		return "", errors.New("la carpeta " + p + " no existe")
	}
	return abs, nil
}

// cmdLife suspends or resumes sessions: _life <ws> suspend|wake <pane|session>. A tmux session
// (a tab) acts on every Claude session in it.
func cmdLife(args []string) error {
	if len(args) != 3 || (args[1] != "suspend" && args[1] != "wake" && args[1] != "toggle") {
		return errors.New("uso: _life <ws> suspend|wake|toggle <panel|pestaña>")
	}
	ws, what, target := args[0], args[1], args[2]
	t := Tmux{WS: ws}
	var panes []string
	if strings.HasPrefix(target, "%") {
		panes = []string{target}
	} else {
		rows, err := t.Lines("list-panes", "-s", "-t", target, "-F", "#{pane_id}")
		if err != nil {
			return err
		}
		for _, r := range rows {
			panes = append(panes, r[0])
		}
	}
	if what == "toggle" {
		// Suspend when any Claude session there is running; resume when all of them sleep.
		what = "wake"
		for _, p := range panes {
			if s, err := paneSlot(t, p); err == nil && s.Kind == kindClaude && s.Status != statusSleeping {
				what = "suspend"
			}
		}
	}
	for _, p := range panes {
		var err error
		if what == "suspend" {
			err = suspendPane(ws, p)
		} else {
			err = wakePane(ws, p)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// cmdReopen reopens the session closed last, in its tab when it still exists: _reopen <ws> <client>.
func cmdReopen(args []string) error {
	if len(args) != 2 {
		return errors.New("uso: _reopen <ws> <cliente>")
	}
	ws, client := args[0], args[1]
	t := Tmux{WS: ws}
	c, ok := popClosed(t)
	if !ok {
		_, _ = t.Run("display-message", "-c", client, "No hay sesiones cerradas para reabrir")
		return nil
	}
	session := c.Tab
	if session == "" || t.Command("has-session", "-t", "="+session).Run() != nil {
		session, _ = t.Run("display", "-p", "-c", client, "#{client_session}")
	}
	resuming := c.Slot.Kind == kindClaude && c.Slot.SessionID != ""
	if err := openInTab(ws, session, c.Slot, resuming); err != nil {
		return err
	}
	_, _ = t.Run("switch-client", "-c", client, "-t", session)
	return nil
}

// uiTabDir changes the folder of a tab — where its new sessions start: _ui tabdir <ws> [session].
func uiTabDir(ws, session string) error {
	t := Tmux{WS: ws}
	if session == "" {
		var err error
		if _, session, err = currentClient(t); err != nil {
			return err
		}
	}
	title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
	v, ok := askText("Carpeta de la pestaña "+title+" (las sesiones nuevas empiezan ahí)", "Carpeta", shortPath(tabDir(t, session)), "")
	if !ok {
		return nil
	}
	dir, err := checkDir(v)
	if err != nil {
		return err
	}
	return t.setTabOptions(session, Tab{Title: title, Dir: dir})
}

// uiWorkspaceDir changes the workspace's default folder — where new tabs start: _ui wsdir <ws>.
func uiWorkspaceDir(ws string) error {
	t := Tmux{WS: ws}
	v, ok := askText("Carpeta por defecto del workspace (las pestañas nuevas empiezan ahí)", "Carpeta", shortPath(workspaceDir(t)), "")
	if !ok {
		return nil
	}
	dir, err := checkDir(v)
	if err != nil {
		return err
	}
	_, err = t.Run("set", "-g", optWSDir, dir)
	return err
}
