package main

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// refreshTabLabel recomputes a tab's label with its activity counters (●N working, ?N waiting
// for a permission) and the underline drawn beneath it, both the same display width.
func refreshTabLabel(t Tmux, session string) {
	title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
	if title == "" {
		title = session
	}
	busy, waiting := 0, 0
	if rows, err := t.Lines("list-windows", "-t", session, "-F", "#{@cw_kind}"+sep+"#{@cw_status}"); err == nil {
		for _, r := range rows {
			if len(r) == 2 && r[0] == kindClaude {
				switch r[1] {
				case statusBusy:
					busy++
				case statusWaiting:
					waiting++
				}
			}
		}
	}
	label, width := title, len([]rune(title))
	if busy > 0 {
		n := strconv.Itoa(busy)
		label += " #[fg=" + colBusy + "]●" + n
		width += 2 + len(n)
	}
	if waiting > 0 {
		n := strconv.Itoa(waiting)
		label += " #[fg=" + colWait + "]?" + n
		width += 2 + len(n)
	}
	width += 2 // the underline also spans the tab's " ✕"
	_, _ = t.Run(
		"set", "-t", session, "@cw_tab_label", label, ";",
		"set", "-t", session, "@cw_tab_on", strings.Repeat("━", width), ";",
		"set", "-t", session, "@cw_tab_off", strings.Repeat("─", width),
	)
	_, _ = t.Run("refresh-client", "-S") // fails harmlessly while no client is attached
}

func refreshAllLabels(t Tmux) {
	if rows, err := t.Lines("list-sessions", "-F", "#{session_name}"); err == nil {
		for _, r := range rows {
			refreshTabLabel(t, r[0])
		}
	}
}

// cmdTabMove moves a tab (tmux session) one place left or right, or onto the position of another
// tab: _tabmove <ws> <session> -1|+1|<target-session>. Tabs are ordered by session name, so a
// move swaps the names of the two sessions; attached clients follow their session.
func cmdTabMove(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("uso: _tabmove <ws> <sesión> -1|+1|<sesión destino>")
	}
	t := Tmux{WS: args[0]}
	rows, err := t.Lines("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return err
	}
	var names []string
	for _, r := range rows {
		names = append(names, r[0])
	}
	sort.Strings(names)
	from := slices.Index(names, args[1])
	if from < 0 {
		return nil
	}
	to := slices.Index(names, args[2])
	switch args[2] {
	case "-1":
		to = from - 1
	case "+1":
		to = from + 1
	}
	if to < 0 || to >= len(names) || to == from {
		return nil
	}
	// Walk the tab to its new place one swap at a time, so the tabs in between keep their order.
	step := 1
	if to < from {
		step = -1
	}
	active, _ := t.Run("show", "-gqv", "@cw_active")
	for i := from; i != to; i += step {
		a, b := names[i], names[i+step]
		if _, err := t.Run("rename-session", "-t", "="+a, "cw-swap", ";",
			"rename-session", "-t", "="+b, a, ";", "rename-session", "-t", "=cw-swap", b); err != nil {
			return err
		}
		switch active {
		case a:
			active = b
		case b:
			active = a
		}
	}
	_, _ = t.Run("set", "-g", "@cw_active", active, ";", "refresh-client", "-S")
	return nil
}

// cmdWinMove moves the current session pill of a tab onto the place of another one (drag in the
// bar): _winmove <ws> <tab-session> <target-window-id>.
func cmdWinMove(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("uso: _winmove <ws> <pestaña> <ventana destino>")
	}
	t := Tmux{WS: args[0]}
	cur, err := t.Run("display", "-p", "-t", "="+args[1]+":", "#{window_id}")
	if err != nil || cur == args[2] {
		return err
	}
	_, err = t.Run("swap-window", "-s", cur, "-t", args[2], ";", "select-window", "-t", cur)
	return err
}
