package main

import (
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
