package main

import (
	"fmt"
	"strings"
)

// arrangeView applies a layout to a view ("" keeps the current one; a saved layout that no longer
// fits falls back to columns) and shows the per-session title bars when the view holds more
// than one session.
func (t Tmux) arrangeView(window, layout string) {
	n, _ := t.Run("display", "-p", "-t", window, "#{window_panes}")
	if n == "1" || n == "" {
		_, _ = t.Run("set", "-w", "-t", window, "pane-border-status", "off")
		return
	}
	switch {
	case layout == "":
		// a session left the view: share the space out again, keeping the arrangement
		_, _ = t.Run("select-layout", "-t", window, "-E")
	case t.Command("select-layout", "-t", window, layout).Run() != nil:
		_, _ = t.Run("select-layout", "-t", window, "even-horizontal")
	}
	_, _ = t.Run("set", "-w", "-t", window, "pane-border-status", "top")
}

// cmdJoin moves every session of one view into another view, next to a pane:
// _join <ws> <source-window> <target-pane-or-window> [layout]. Used by drag and drop (dropping a
// session of the bar onto the terminal) and by the "Combinar vista" dialog.
func cmdJoin(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("uso: _join <ws> <vista origen> <panel destino> [distribución]")
	}
	t := Tmux{WS: args[0]}
	migrateLegacyOptions(t)
	src, dst := args[1], args[2]
	srcWin, err := t.Run("display", "-p", "-t", src, "#{window_id}")
	if err != nil {
		return err
	}
	dstWin, err := t.Run("display", "-p", "-t", dst, "#{window_id}")
	if err != nil || srcWin == dstWin {
		return err
	}
	if kind, _ := t.Run("display", "-p", "-t", dst, "#{@cw_kind}"); kind == kindHome {
		return nil // an empty tab's start screen is not a session to combine with
	}
	panes, err := t.Lines("list-panes", "-t", srcWin, "-F", "#{pane_id}"+sep+"#{@cw_kind}")
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p[1] == kindHome {
			continue
		}
		if _, err := t.Run("join-pane", "-d", "-h", "-s", p[0], "-t", dst); err != nil {
			return err
		}
	}
	// Combined sessions show side by side, in columns, unless a layout was asked for.
	layout := "even-horizontal"
	if len(args) > 3 {
		layout = args[3]
	}
	t.arrangeView(dstWin, layout)
	_, _ = t.Run("select-window", "-t", dstWin)
	if session, err := t.Run("display", "-p", "-t", dstWin, "#{session_name}"); err == nil {
		refreshTabLabel(t, session)
	}
	return nil
}

// cmdSplitOut moves one session out of a combined view into its own view: _splitout <ws> <pane>.
func cmdSplitOut(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("uso: _splitout <ws> <panel>")
	}
	t := Tmux{WS: args[0]}
	win, err := t.Run("display", "-p", "-t", args[1], "#{window_id}")
	if err != nil {
		return err
	}
	name, _ := t.Run("display", "-p", "-t", args[1], "#{@cw_alias}")
	newWin, err := t.Run("break-pane", "-d", "-s", args[1], "-P", "-F", "#{window_id}")
	if err != nil {
		return err
	}
	_, _ = t.Run("rename-window", "-t", newWin, strings.TrimSpace(name))
	t.arrangeView(win, "")
	t.arrangeView(newWin, "")
	return nil
}

// cmdLayout changes how a view arranges its sessions: _layout <ws> <window> lado|apilado|cuadricula.
func cmdLayout(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("uso: _layout <ws> <vista> lado|apilado|cuadricula")
	}
	name := map[string]string{"lado": "even-horizontal", "apilado": "even-vertical", "cuadricula": "tiled"}[args[2]]
	if name == "" {
		return fmt.Errorf("distribución desconocida %q", args[2])
	}
	_, err := Tmux{WS: args[0]}.Run("select-layout", "-t", args[1], name)
	return err
}

var slotKeys = []string{"@cw_slot", "@cw_kind", "@cw_alias", "@cw_cwd", "@cw_sid", "@cw_status", "@cw_args", "@cw_claude_alias"}

// migrateLegacyOptions moves session data that versions before 0.4 kept on windows onto the
// pane: combining views moves panes between windows, and window options would stay behind.
func migrateLegacyOptions(t Tmux) {
	rows, err := t.Lines("list-panes", "-a", "-F", "#{window_id}"+sep+"#{pane_id}"+sep+"#{window_panes}")
	if err != nil {
		return
	}
	for _, r := range rows {
		if len(r) != 3 || r[2] != "1" {
			continue
		}
		win, pane := r[0], r[1]
		if v, _ := t.Run("show", "-w", "-t", win, "-qv", "@cw_slot"); v == "" {
			continue
		}
		if v, _ := t.Run("show", "-p", "-t", pane, "-qv", "@cw_slot"); v != "" {
			continue
		}
		var cmds []string
		for _, k := range slotKeys {
			v, _ := t.Run("show", "-w", "-t", win, "-qv", k)
			cmds = append(cmds, "set", "-p", "-t", pane, k, v, ";", "set", "-w", "-u", "-t", win, k, ";")
		}
		_, _ = t.Run(cmds[:len(cmds)-1]...)
	}
}
