package main

import (
	"fmt"
	"os"
	"path/filepath"
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

// cmdSlotMenu shows the right-click menu of a session pill: _slotmenu <ws> <client> <window> <x> <y>.
// It is built here because it lists the other sessions to show side by side with this one.
func cmdSlotMenu(args []string) error {
	if len(args) != 5 {
		return fmt.Errorf("uso: _slotmenu <ws> <cliente> <vista> <x> <y>")
	}
	ws, client, win, x, y := args[0], args[1], args[2], args[3], args[4]
	t := Tmux{WS: ws}
	migrateLegacyOptions(t)
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	session, _ := t.Run("display", "-p", "-t", win, "#{session_name}")
	info, _ := t.Run("display", "-p", "-t", win, "#{window_panes}"+sep+"#{@cw_kind}"+sep+"#{@cw_alias}")
	f := strings.Split(info, sep)
	q := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	shell := func(a ...string) string {
		return fmt.Sprintf(`run-shell "%s"`, q(joinArgs(append([]string{bin}, a...))))
	}
	popup := func(title string, w, h int, a ...string) string {
		return fmt.Sprintf(`display-popup -E -w %d -h %d -T " %s " "%s"`, w, h, title, q(joinArgs(append([]string{bin}, a...))))
	}
	title := "sesión"
	if len(f) == 3 && f[2] != "" {
		title = f[2]
	}
	_ = client
	menu := []string{"display-menu", "-x", x, "-y", y, "-T", "#[align=centre] " + q(title) + " ", "--"}
	add := func(label, cmd string) { menu = append(menu, label, "", cmd) }
	add("← Mover a la izquierda", fmt.Sprintf("select-window -t %s ; swap-window -t -1", win))
	add("→ Mover a la derecha", fmt.Sprintf("select-window -t %s ; swap-window -t +1", win))
	menu = append(menu, "", "", "")
	others := 0
	for _, v := range listViews(t) {
		if v.window == win || v.session != session {
			continue
		}
		others++
		add("⧉ Ver junto a "+q(shorten(strings.Join(v.names, " · "), 30)), shell("_join", ws, v.window, win))
	}
	if others == 0 {
		menu = append(menu, "-⧉ No hay otras sesiones en esta pestaña", "", "")
	}
	add("⧉ Combinar con sesiones de otras pestañas…",
		fmt.Sprintf("select-window -t %s ; %s", win, popup("Combinar sesiones en esta vista", 78, 24, "_ui", "combine", ws)))
	if len(f) == 3 && f[0] != "1" {
		add("⇱ Separar la sesión activa", shell("_splitout", ws, win+"."))
	}
	menu = append(menu, "", "", "")
	add("▸ Renombrar", fmt.Sprintf("select-window -t %s ; %s", win, popup("Renombrar sesión", 70, 10, "_ui", "rename", ws, "slot")))
	add("▸ Editar sesión", fmt.Sprintf("select-window -t %s ; %s", win, popup("Editar sesión", 72, 24, "_ui", "editcurrent", ws)))
	if len(f) == 3 && f[1] == kindClaude {
		add("✦ Cambiar alias de claude", fmt.Sprintf("select-window -t %s ; %s", win, popup("Cambiar alias de claude", 72, 24, "_ui", "editcurrent", ws)))
	}
	add("✕ Cerrar", popup("Cerrar sesión", 66, 13, "_ui", "confirm", ws, "close", win))
	// The menu is written as a tmux command for the click's own binding to source: a menu opened
	// from outside the click would close as soon as the mouse button is released.
	return writeFileAtomic(slotMenuFile(ws), []byte(tmuxQuoteCommand(menu)+"\n"))
}

func slotMenuFile(ws string) string { return filepath.Join(runDir(), ws+".slotmenu.conf") }

// tmuxQuoteCommand renders arguments as one tmux configuration line, each in double quotes.
func tmuxQuoteCommand(args []string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = `"` + r.Replace(a) + `"`
	}
	return strings.Join(parts, " ")
}
