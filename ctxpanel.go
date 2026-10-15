package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The right-click menus of tabs, views and sessions are a floating panel of cw instead of a tmux
// menu: a tmux menu always closes once an item is chosen, and this one stays open — an action
// runs, the list is rebuilt from the new state, and dialogs (rename, alias, confirmations) open
// inside the same panel before returning to it.

type ctxItem struct {
	label  string
	id     string // "" for headings and blank lines
	header bool
}

// cmdCtxPopup opens the panel for the client that clicked: _ctxpopup <ws> <client> tab|slot <target>.
// The bar's bindings call it through run-shell because display-popup does not expand formats.
func cmdCtxPopup(args []string) error {
	if len(args) != 4 {
		return fmt.Errorf("uso: _ctxpopup <ws> <cliente> tab|slot <destino>")
	}
	ws, client, kind, target := args[0], args[1], args[2], args[3]
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	t := Tmux{WS: ws}
	title := "Acciones"
	if kind == "tab" {
		if v, _ := t.Run("display", "-p", "-t", target, "#{@cw_title}"); v != "" {
			title = v
		}
	}
	_, err = t.Run(popupCmd(client, title, 78, 26, bin, "_ui", "ctx", ws, kind, target)...)
	return err
}

// uiContext runs the panel: kind "tab" targets a tmux session id ($N), "slot" a window (@N, a
// view) or a pane (%N, one session of a view).
func uiContext(ws, kind, target string) error {
	t := Tmux{WS: ws}
	migrateLegacyOptions(t)
	sel := 0
	msg := ""
	for {
		title, items, ok := ctxItems(t, kind, target)
		if !ok {
			return nil // the tab or session no longer exists (closed from this panel)
		}
		var choices []int
		for i, it := range items {
			if it.id != "" {
				choices = append(choices, i)
			}
		}
		sel = min(sel, len(choices)-1)
		var s screen
		s.at(2, 4, sgrAccent+sgrBold+fit(title, 66)+sgrReset)
		row := 4
		for i, it := range items {
			switch {
			case it.header:
				s.at(row, 4, sgrMuted+it.label+sgrReset)
			case it.id == "":
			default:
				line := "  " + fit(it.label, 62) + " "
				if len(choices) > 0 && choices[sel] == i {
					line = sgrSel + line + sgrReset
				}
				s.btn(row, 3, line, "item:"+strconv.Itoa(i))
			}
			row++
		}
		if msg != "" {
			s.at(row, 4, sgrRed+msg+sgrReset)
		}
		s.place(row+1, 4, []lineItem{buttonLine(0, btnDef{label: "Cerrar", id: "close", primary: true})})
		s.b.WriteString("\033[?25l")
		hits := s.hits
		s.flush(true)

		setRaw(true)
		keys, err := readKeys()
		setRaw(false)
		if err != nil {
			return nil
		}
		chosen := ""
		for _, k := range keys {
			switch {
			case k.kind == keyEsc || (k.kind == keyRune && k.r == 'q'):
				return nil
			case k.kind == keyClick:
				id := hitAt(hits, k.x, k.y)
				if id == "close" {
					return nil
				}
				if strings.HasPrefix(id, "item:") {
					i, _ := strconv.Atoi(id[5:])
					chosen = items[i].id
					for j, c := range choices {
						if c == i {
							sel = j
						}
					}
				}
			case (k.kind == keyUp || k.kind == keyWheelUp) && len(choices) > 0:
				sel = (sel - 1 + len(choices)) % len(choices)
			case (k.kind == keyDown || k.kind == keyWheelDown || k.kind == keyTab) && len(choices) > 0:
				sel = (sel + 1) % len(choices)
			case k.kind == keyEnter && len(choices) > 0:
				chosen = items[choices[sel]].id
			}
		}
		msg = ""
		if chosen != "" {
			if err := ctxRun(t, ws, kind, target, chosen); err != nil && err != errCancelled {
				msg = err.Error()
			}
		}
	}
}

// ctxItems builds the panel's list from the current state; ok is false when the target is gone.
func ctxItems(t Tmux, kind, target string) (title string, items []ctxItem, ok bool) {
	add := func(label, id string) { items = append(items, ctxItem{label: label, id: id}) }
	head := func(label string) {
		if len(items) > 0 {
			items = append(items, ctxItem{})
		}
		items = append(items, ctxItem{label: label, header: true})
	}
	if kind == "tab" {
		name, err := t.Run("display", "-p", "-t", target, "#{@cw_title}")
		if err != nil {
			return "", nil, false
		}
		head("PESTAÑA")
		add("← Mover a la izquierda", "tab-left")
		add("→ Mover a la derecha", "tab-right")
		add("▸ Renombrar pestaña", "tab-rename")
		add("✦ Cambiar alias de claude de la pestaña…", "tab-alias")
		add("✕ Cerrar pestaña", "tab-close")
		return "Pestaña " + name, items, true
	}

	win, err := t.Run("display", "-p", "-t", target, "#{window_id}")
	if err != nil {
		return "", nil, false
	}
	session, _ := t.Run("display", "-p", "-t", win, "#{session_name}")
	panes, _ := t.Lines("list-panes", "-t", win, "-F", "#{pane_id}"+sep+"#{@cw_alias}"+sep+"#{@cw_kind}")
	onlyPane := ""
	if strings.HasPrefix(target, "%") {
		onlyPane = target
	}
	var names []string
	for _, p := range panes {
		if len(p) == 3 && p[2] != kindHome {
			n := p[1]
			if n == "" {
				n = "shell"
			}
			names = append(names, n)
		}
	}
	title = strings.Join(names, " · ")
	if onlyPane == "" {
		head("VISTA")
		add("← Mover a la izquierda", "view-left")
		add("→ Mover a la derecha", "view-right")
		for _, v := range listViews(t) {
			if v.window != win && v.session == session {
				add("⧉ Ver junto a "+shorten(strings.Join(v.names, " · "), 40), "join:"+v.window)
			}
		}
		add("⧉ Combinar con sesiones de otras pestañas…", "combine")
		if len(names) > 1 {
			add("▥ Distribución: lado a lado", "layout:lado")
			add("▤ Distribución: arriba y abajo", "layout:apilado")
			add("▦ Distribución: cuadrícula", "layout:cuadricula")
			add("✦ Cambiar alias de claude de toda la vista…", "view-alias")
			add("✕ Cerrar la vista", "view-close")
		}
	}
	for _, p := range panes {
		if len(p) != 3 || p[2] == kindHome || (onlyPane != "" && p[0] != onlyPane) {
			continue
		}
		name := p[1]
		if name == "" {
			name = "shell"
		}
		if len(names) > 1 || onlyPane != "" {
			head(strings.ToUpper(shorten(name, 40)))
		} else {
			head("SESIÓN")
		}
		add("◉ Ir a esta sesión", "goto:"+p[0])
		add("▸ Renombrar", "rename:"+p[0])
		add("▸ Editar sesión", "edit:"+p[0])
		if p[2] == kindClaude {
			add("✦ Cambiar alias de claude", "edit:"+p[0])
		}
		if len(names) > 1 {
			add("⇱ Separar de la vista", "split:"+p[0])
		}
		add("✕ Cerrar", "close:"+p[0])
	}
	return title, items, true
}

// ctxRun performs one action of the panel.
func ctxRun(t Tmux, ws, kind, target, id string) error {
	client, _, _ := currentClient(t)
	win, _ := t.Run("display", "-p", "-t", target, "#{window_id}")
	focusView := func() {
		_, _ = t.Run("switch-client", "-c", client, "-t", win, ";", "select-window", "-t", win)
	}
	arg := ""
	if i := strings.Index(id, ":"); i >= 0 {
		id, arg = id[:i], id[i+1:]
	}
	switch id {
	case "tab-left", "tab-right":
		name, err := t.Run("display", "-p", "-t", target, "#{session_name}")
		if err != nil {
			return err
		}
		d := "-1"
		if id == "tab-right" {
			d = "+1"
		}
		return cmdTabMove([]string{ws, name, d})
	case "tab-rename":
		session, err := t.Run("display", "-p", "-t", target, "#{session_name}")
		if err != nil {
			return err
		}
		title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		if v, ok := askText("Nuevo nombre de la pestaña", "Nombre", title, ""); ok && cleanLabel(v) != "" {
			return t.setTabOptions(session, Tab{Title: cleanLabel(v), Dir: tabDir(t, session)})
		}
	case "tab-alias":
		_, _ = t.Run("switch-client", "-c", client, "-t", target)
		return uiBulkAlias(ws, "tab")
	case "tab-close":
		session, err := t.Run("display", "-p", "-t", target, "#{session_name}")
		if err != nil {
			return err
		}
		return uiConfirm(ws, "closetab", session)
	case "view-left", "view-right":
		idx, err := t.Run("display", "-p", "-t", win, "#{window_index}")
		if err != nil {
			return err
		}
		session, _ := t.Run("display", "-p", "-t", win, "#{session_name}")
		n, _ := strconv.Atoi(idx)
		if id == "view-left" {
			n--
		} else {
			n++
		}
		// keeps the window id, so this panel still targets it after the move
		_, _ = t.Run("swap-window", "-d", "-s", win, "-t", session+":"+strconv.Itoa(n))
	case "join":
		return cmdJoin([]string{ws, arg, win})
	case "combine":
		focusView()
		return uiCombine(ws)
	case "layout":
		return cmdLayout([]string{ws, win, arg})
	case "view-alias":
		focusView()
		return uiBulkAlias(ws, "view")
	case "view-close":
		return uiConfirm(ws, "close", win)
	case "goto":
		_, err := t.Run("switch-client", "-c", client, "-t", arg, ";", "select-window", "-t", arg, ";", "select-pane", "-t", arg)
		return err
	case "rename":
		return renameSlot(t, arg)
	case "edit":
		return uiEditPane(ws, arg)
	case "split":
		return cmdSplitOut([]string{ws, arg})
	case "close":
		return uiConfirm(ws, "close", arg)
	}
	return nil
}
