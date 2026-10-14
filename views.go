package main

import (
	"fmt"
	"strings"
)

// arrangeView applies a saved layout to a view (falling back to an even grid) and shows the
// per-session title bars when the view holds more than one session.
func (t Tmux) arrangeView(window, layout string) {
	n, _ := t.Run("display", "-p", "-t", window, "#{window_panes}")
	if n == "1" || n == "" {
		_, _ = t.Run("set", "-w", "-t", window, "pane-border-status", "off")
		return
	}
	if layout == "" || t.Command("select-layout", "-t", window, layout).Run() != nil {
		_, _ = t.Run("select-layout", "-t", window, "tiled")
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
	layout := ""
	if len(args) > 3 {
		layout = args[3]
	}
	t.arrangeView(dstWin, layout)
	if layout == "" {
		// keep the drop position, just even the sizes out
		_, _ = t.Run("select-layout", "-t", dstWin, "-E")
	}
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
