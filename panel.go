package main

import (
	"errors"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A session with live children is a coordinator: its pane gets @cw_coord=1, which colours its
// subtab in the bar (and the app's), and in terminal workspaces its subtab gets a panel at the
// right (cw _panel) listing its children with their state; a click, or ↑↓ and Enter, goes to
// a child's subtab. The panel is not a session: capture skips it, it is never saved, and the
// periodic sync recreates or removes it as children come and go.

const (
	kindPanel  = "panel"
	panelWidth = 30
	colCoord   = "colour141"
)

type paneRow struct {
	pane, slot, parent, kind, coord, window, session, panelFor, alias, status, dead, width string
}

func listPaneRows(t Tmux) ([]paneRow, error) {
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{pane_id}", "#{@cw_slot}", "#{@cw_parent}", "#{@cw_kind}", "#{@cw_coord}", "#{window_id}",
		"#{session_name}", "#{@cw_panel_for}", "#{@cw_alias}", "#{@cw_status}", "#{pane_dead}", "#{pane_width}",
	}, sep))
	if err != nil {
		return nil, err
	}
	var out []paneRow
	for _, r := range rows {
		if len(r) != 12 {
			continue
		}
		out = append(out, paneRow{r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8], r[9], r[10], r[11]})
	}
	return out, nil
}

// syncCoordinators marks the coordinators of a workspace and, in terminal workspaces, keeps one
// children panel next to each of them.
func syncCoordinators(t Tmux) {
	rows, err := listPaneRows(t)
	if err != nil {
		return
	}
	live := map[string]bool{}
	for _, r := range rows {
		if r.slot != "" && r.kind != kindPanel && r.dead != "1" {
			live[r.slot] = true
		}
	}
	coords := map[string]bool{}
	for _, r := range rows {
		if r.slot != "" && r.parent != "" && live[r.slot] && live[r.parent] && r.parent != r.slot {
			coords[r.parent] = true
		}
	}
	terminal := !appMode(t)
	panels := map[string]paneRow{}
	for _, r := range rows {
		if r.kind == kindPanel {
			panels[r.panelFor] = r
		}
	}
	changed := map[string]bool{}
	for _, r := range rows {
		if r.slot == "" || r.kind == kindPanel {
			continue
		}
		want := coords[r.slot]
		if want != (r.coord == "1") {
			if want {
				_, _ = t.Run("set", "-p", "-t", r.pane, "@cw_coord", "1")
			} else {
				_, _ = t.Run("set", "-p", "-u", "-t", r.pane, "@cw_coord")
			}
			changed[r.session] = true
		}
		if !terminal {
			continue
		}
		p, has := panels[r.slot]
		delete(panels, r.slot)
		switch {
		case want && has && p.window != r.window: // the coordinator moved to another view
			_, _ = t.Run("kill-pane", "-t", p.pane)
			_ = openPanel(t, r)
		case want && !has:
			_ = openPanel(t, r)
		case !want && has:
			_, _ = t.Run("kill-pane", "-t", p.pane)
			_, _ = t.Run("set", "-w", "-u", "-t", r.window, "@cw_has_panel")
		case want && has && p.width != strconv.Itoa(panelWidth):
			_, _ = t.Run("resize-pane", "-t", p.pane, "-x", strconv.Itoa(panelWidth))
		}
	}
	for _, p := range panels { // panels whose coordinator is gone
		_, _ = t.Run("kill-pane", "-t", p.pane)
		_, _ = t.Run("set", "-w", "-u", "-t", p.window, "@cw_has_panel")
	}
	for session := range changed {
		refreshTabLabel(t, session)
	}
}

// openPanel splits the coordinator's view and runs the children panel at its right (the app
// keeps its own sidebar at the left).
func openPanel(t Tmux, coord paneRow) error {
	args := append([]string{"split-window", "-d", "-h", "-l", strconv.Itoa(panelWidth), "-t", coord.pane,
		"-P", "-F", "#{pane_id}"}, panelArgv(t.WS, coord.slot)...)
	pane, err := t.Run(args...)
	if err != nil {
		return err
	}
	_, err = t.Run("set", "-p", "-t", pane, "@cw_kind", kindPanel, ";",
		"set", "-p", "-t", pane, "@cw_panel_for", coord.slot, ";",
		"set", "-p", "-t", pane, "@cw_alias", "hijas", ";",
		"set", "-w", "-t", coord.window, "@cw_has_panel", "1")
	return err
}

// panelArgv is the panel's command (tests replace it).
var panelArgv = func(ws, slot string) []string { return []string{selfBin(), "_panel", ws, slot} }

type panelChild struct {
	pane, slot, alias, status, session, tab string
}

// panelChildren lists the live children of a coordinator, by tab and name.
func panelChildren(t Tmux, coord string) (children []panelChild, coordAlive bool) {
	rows, err := listPaneRows(t)
	if err != nil {
		return nil, false
	}
	titles := map[string]string{}
	if ss, err := t.Lines("list-sessions", "-F", "#{session_name}"+sep+"#{@cw_title}"); err == nil {
		for _, s := range ss {
			if len(s) == 2 {
				titles[s[0]] = s[1]
			}
		}
	}
	for _, r := range rows {
		if r.slot == coord && r.kind != kindPanel {
			coordAlive = true
		}
		if r.parent == coord && r.slot != "" && r.kind != kindPanel && r.dead != "1" {
			children = append(children, panelChild{r.pane, r.slot, r.alias, r.status, r.session, titles[r.session]})
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].session != children[j].session {
			return children[i].session < children[j].session
		}
		return children[i].alias < children[j].alias
	})
	return children, coordAlive
}

func panelState(status string) (sym, sgr, word string) {
	switch status {
	case statusBusy:
		return "●", "\033[38;5;214m", "trabajando"
	case statusWaiting:
		return "?", "\033[38;5;203m", "pregunta"
	case statusSleeping:
		return "z", "\033[38;5;245m", "suspendida"
	}
	return "○", "\033[38;5;114m", "lista"
}

// goToChild shows a child's subtab to the client looking at the coordinator.
func goToChild(t Tmux, c panelChild) {
	if client, _, err := currentClient(t); err == nil {
		_, _ = t.Run("switch-client", "-c", client, "-t", c.session)
	}
	_, _ = t.Run("select-window", "-t", c.pane, ";", "select-pane", "-t", c.pane)
}

// cmdPanel: _panel <ws> <coordinator slot> — the children panel of a terminal workspace.
func cmdPanel(args []string) error {
	if len(args) != 2 {
		return errors.New("uso: cw _panel <ws> <sesión coordinadora>")
	}
	t, coord := Tmux{WS: args[0]}, args[1]
	os.Stdout.WriteString("\033[?1049h\033[?25l")
	setRaw(true)
	defer func() {
		setRaw(false)
		os.Stdout.WriteString("\033[?25h\033[?1049l")
	}()
	keys := make(chan []key)
	go func() {
		for {
			k, err := readKeys()
			if err != nil {
				close(keys)
				return
			}
			keys <- k
		}
	}()
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	sel, gone := 0, 0
	const top = 3 // rows above the first child: title, rule, blank
	for {
		children, alive := panelChildren(t, coord)
		if !alive || len(children) == 0 {
			if gone++; gone >= 3 { // the sync removes the panel too; leave on our own as well
				return nil
			}
		} else {
			gone = 0
		}
		sel = min(max(0, sel), max(0, len(children)-1))
		cols, height := termSize()
		var b strings.Builder
		b.WriteString("\033[H")
		line := func(s string) { b.WriteString(s + "\033[K\r\n") }
		line("\033[38;5;141m\033[1m ◆ Hijas (" + strconv.Itoa(len(children)) + ")" + sgrReset)
		line("\033[38;5;240m" + strings.Repeat("─", max(1, cols)) + sgrReset)
		line("")
		shown := 0
		for i, c := range children {
			if top+shown >= height-2 {
				break
			}
			sym, sgr, word := panelState(c.status)
			name := fit(" "+c.alias, max(1, cols-3))
			row := sgr + " " + sym + sgrReset + name
			if i == sel {
				row = sgrSel + " " + sym + fit(" "+c.alias, max(1, cols-3)) + sgrReset
			}
			line(row)
			line("\033[38;5;245m   " + fit(word+" · "+c.tab, max(1, cols-3)) + sgrReset)
			shown += 2
		}
		for i := top + shown; i < height-1; i++ {
			line("")
		}
		b.WriteString("\033[38;5;240m" + fit(" clic o ↑↓ Enter: ir", cols) + sgrReset + "\033[K")
		os.Stdout.WriteString(b.String())
		select {
		case <-tick.C:
		case <-resized:
		case ks, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range ks {
				switch {
				case k.kind == keyUp || k.kind == keyWheelUp || k.kind == keyRune && k.r == 'k':
					sel--
				case k.kind == keyDown || k.kind == keyWheelDown || k.kind == keyRune && k.r == 'j':
					sel++
				case k.kind == keyEnter && sel < len(children):
					goToChild(t, children[sel])
				case k.kind == keyClick:
					if i := (k.y - 1 - top) / 2; k.y-1 >= top && i < len(children) {
						sel = i
						goToChild(t, children[i])
					}
				}
			}
		}
	}
}
