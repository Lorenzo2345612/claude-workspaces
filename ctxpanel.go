package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
//
// tmux ignores clicks outside a popup, so the panel takes the whole client — no border — and
// draws a dimmed copy of the screen behind its box: a click anywhere outside the box closes it.
// Dialogs (rename, alias, confirmations) need the screen to themselves, so the panel leaves
// their request in a file and closes; they open here in a popup of their own, and the panel
// comes back when they close.
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
	if err := os.MkdirAll(runDir(), 0o755); err != nil {
		return err
	}
	res := filepath.Join(runDir(), "ctx-"+newUUID()[:8]+".req")
	defer os.Remove(res)
	for i := 0; i < 100; i++ {
		_ = os.Remove(res)
		if _, err := t.Run("display-popup", "-c", client, "-B", "-E", "-x", "0", "-y", "0", "-w", "100%", "-h", "100%",
			joinArgs([]string{bin, "_ui", "ctx", ws, kind, target, res})); err != nil {
			return err
		}
		data, _ := os.ReadFile(res)
		req := strings.TrimSpace(string(data))
		if req == "" {
			return nil // closed: Esc, ✕, Cerrar or a click outside
		}
		title, w, h := ctxDialogPopup(req)
		if _, err := t.Run(popupCmd(client, title, w, h, bin, "_ui", "ctxdialog", ws, kind, target, req)...); err != nil {
			return err
		}
	}
	return nil
}

// ctxDialogs are the panel's actions that open a dialog, with the popup each one needs.
var ctxDialogs = map[string]struct {
	title string
	w, h  int
}{
	"tab-rename": {"Renombrar pestaña", 70, 10},
	"tab-alias":  {"Alias de claude de la pestaña", 70, 16},
	"tab-close":  {"Cerrar pestaña", 66, 13},
	"tab-dir":    {"Carpeta de la pestaña", 76, 10},
	"combine":    {"Combinar sesiones en esta vista", 78, 24},
	"view-alias": {"Alias de claude de la vista", 70, 16},
	"view-close": {"Cerrar vista", 66, 13},
	"rename":     {"Renombrar sesión", 70, 10},
	"edit":       {"Editar sesión", 72, 24},
	"close":      {"Cerrar sesión", 66, 13},
}

func ctxDialogPopup(req string) (string, int, int) {
	id, _, _ := strings.Cut(req, ":")
	if d, ok := ctxDialogs[id]; ok {
		return d.title, d.w, d.h
	}
	return "Acciones", 72, 20
}

// uiCtxDialog runs one dialog asked for by the panel: _ui ctxdialog <ws> <kind> <target> <request>.
func uiCtxDialog(ws, kind, target, req string) error {
	err := ctxRun(Tmux{WS: ws}, ws, kind, target, req)
	if err == errCancelled {
		return nil
	}
	return err
}

// uiContext runs the panel: kind "tab" targets a tmux session id ($N), "slot" a window (@N, a
// view) or a pane (%N, one session of a view). A chosen dialog is written to resFile.
func uiContext(ws, kind, target, resFile string) error {
	t := Tmux{WS: ws}
	migrateLegacyOptions(t)
	client, _, _ := currentClient(t)
	sel, top := 0, 0
	msg := ""
	for {
		title, items, ok := ctxItems(t, kind, target)
		if !ok {
			return nil // the tab or session no longer exists
		}
		var choices []int
		for i, it := range items {
			if it.id != "" {
				choices = append(choices, i)
			}
		}
		sel = max(0, min(sel, len(choices)-1))
		cols, rows := termSize()
		var s screen
		drawBackdrop(&s, t, client, cols, rows)

		// The box: title, a blank line, the items and a line for messages, inside a border whose
		// top-right corner holds the ✕.
		const inner = 70
		boxW := min(inner+4, cols-2)
		visible := max(1, min(len(items), rows-7))
		boxH := visible + 5
		left := max(1, (cols-boxW)/2+1)
		topRow := max(1, (rows-boxH)/2+1)
		if len(choices) > 0 { // keep the selection in view
			cur := choices[sel]
			if cur < top {
				top = cur
			}
			if cur >= top+visible {
				top = cur - visible + 1
			}
		}
		top = max(0, min(top, len(items)-visible))
		drawBox(&s, topRow, left, boxW, boxH, " Acciones ")
		s.zone(topRow, left, boxW, "box") // clicks on the box itself never close it
		for r := 1; r < boxH; r++ {
			s.zone(topRow+r, left, boxW, "box")
		}
		s.btn(topRow, left+boxW-6, sgrAccent+"╴"+sgrReset+sgrBold+" ✕ "+sgrReset+sgrAccent+"╶"+sgrReset, "close")
		x := left + 3
		s.at(topRow+1, x, sgrAccent+sgrBold+fit(title, boxW-8)+sgrReset)
		row := topRow + 3
		for i := top; i < top+visible && i < len(items); i++ {
			it := items[i]
			switch {
			case it.header:
				s.at(row, x, sgrMuted+fit(it.label, boxW-8)+sgrReset)
			case it.id == "":
			default:
				line := " " + fit(it.label, boxW-10) + " "
				if len(choices) > 0 && choices[sel] == i {
					line = sgrSel + line + sgrReset
				}
				s.btn(row, x-1, line, "item:"+strconv.Itoa(i))
			}
			row++
		}
		if top > 0 {
			s.at(topRow+2, left+boxW-6, sgrDim+"▲"+sgrReset)
		}
		if top+visible < len(items) {
			s.at(topRow+boxH-1, left+boxW-10, sgrDim+"▼"+sgrReset)
		}
		if msg != "" {
			s.at(topRow+boxH-2, x, sgrRed+fit(msg, boxW-8)+sgrReset)
		}
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
			case k.kind == keyClick || k.kind == keyRightClick:
				id := hitAt(hits, k.x, k.y)
				switch {
				case id == "" || id == "close":
					return nil // a click outside the box closes the panel
				case strings.HasPrefix(id, "item:") && k.kind == keyClick:
					i, _ := strconv.Atoi(id[5:])
					chosen = items[i].id
					for j, c := range choices {
						if c == i {
							sel = j
						}
					}
				}
			case k.kind == keyWheelUp && len(choices) > 0:
				top = max(0, top-1)
			case k.kind == keyWheelDown && len(choices) > 0:
				top = min(max(0, len(items)-visible), top+1)
			case k.kind == keyUp && len(choices) > 0:
				sel = (sel - 1 + len(choices)) % len(choices)
			case (k.kind == keyDown || k.kind == keyTab) && len(choices) > 0:
				sel = (sel + 1) % len(choices)
			case k.kind == keyEnter && len(choices) > 0:
				chosen = items[choices[sel]].id
			}
		}
		msg = ""
		if chosen == "" {
			continue
		}
		if id, _, _ := strings.Cut(chosen, ":"); ctxDialogs[id].w > 0 {
			return os.WriteFile(resFile, []byte(chosen), 0o600)
		}
		if err := ctxRun(t, ws, kind, target, chosen); err != nil && err != errCancelled {
			msg = err.Error()
		}
		saveNow(ws)
	}
}

// drawBox draws a rounded border in the accent colour and blanks its inside.
func drawBox(s *screen, top, left, w, h int, title string) {
	s.at(top, left, sgrAccent+"╭─"+title+strings.Repeat("─", max(0, w-3-visibleLen(title)))+"╮"+sgrReset)
	for r := 1; r < h-1; r++ {
		s.at(top+r, left, sgrAccent+"│"+sgrReset+strings.Repeat(" ", w-2)+sgrAccent+"│"+sgrReset)
	}
	s.at(top+h-1, left, sgrAccent+"╰"+strings.Repeat("─", w-2)+"╯"+sgrReset)
}

var tmuxStyleRe = regexp.MustCompile(`#\[[^\]]*\]`)

// drawBackdrop paints a dimmed copy of what the client shows — its status lines and the
// sessions of the current view — behind the panel.
func drawBackdrop(s *screen, t Tmux, client string, cols, rows int) {
	info, err := t.Run("display", "-p", "-c", client, "#{status}"+sep+"#{status-position}"+sep+"#{window_id}")
	if err != nil {
		return
	}
	f := strings.Split(info, sep)
	if len(f) != 3 {
		return
	}
	lines := 0
	switch f[0] {
	case "off", "0", "":
	case "on":
		lines = 1
	default:
		lines, _ = strconv.Atoi(f[0])
	}
	statusTop := 1
	paneTop := 1
	if f[1] == "top" {
		paneTop = 1 + lines
	} else {
		statusTop = rows - lines + 1
	}
	dim := "\033[38;5;238m"
	for i := 0; i < lines; i++ {
		out, err := t.Run("display", "-p", "-c", client, "#{E:status-format["+itoa(i)+"]}")
		if err != nil {
			continue
		}
		leftPart, rightPart, _ := strings.Cut(out, "#[align=right]")
		leftPart = tmuxStyleRe.ReplaceAllString(leftPart, "")
		rightPart = tmuxStyleRe.ReplaceAllString(rightPart, "")
		s.at(statusTop+i, 1, dim+fit(leftPart, cols)+sgrReset)
		if rightPart != "" {
			s.at(statusTop+i, max(1, cols-visibleLen(rightPart)+1), dim+rightPart+sgrReset)
		}
	}
	panes, err := t.Lines("list-panes", "-t", f[2], "-F", strings.Join([]string{"#{pane_id}", "#{pane_left}", "#{pane_top}", "#{pane_width}", "#{pane_height}"}, sep))
	if err != nil {
		return
	}
	for _, p := range panes {
		if len(p) != 5 {
			continue
		}
		x, _ := strconv.Atoi(p[1])
		y, _ := strconv.Atoi(p[2])
		w, _ := strconv.Atoi(p[3])
		h, _ := strconv.Atoi(p[4])
		content, err := t.Run("capture-pane", "-p", "-t", p[0])
		if err != nil {
			continue
		}
		for i, line := range strings.Split(content, "\n") {
			if i >= h || paneTop+y+i > rows {
				break
			}
			s.at(paneTop+y+i, x+1, dim+fit(line, w)+sgrReset)
		}
		if x > 0 {
			for i := -1; i < h && paneTop+y+i <= rows; i++ {
				s.at(paneTop+y+i, x, dim+"│"+sgrReset)
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
		add("▸ Cambiar la carpeta de la pestaña…", "tab-dir")
		running, asleep := 0, 0
		if rows, err := t.Lines("list-panes", "-s", "-t", target, "-F", "#{@cw_kind}"+sep+"#{@cw_status}"); err == nil {
			for _, r := range rows {
				if len(r) == 2 && r[0] == kindClaude {
					if r[1] == statusSleeping {
						asleep++
					} else {
						running++
					}
				}
			}
		}
		if running > 0 {
			add("⏾ Suspender la pestaña (sus "+itoa(running)+" de Claude dejan de correr)", "tab-life")
		} else if asleep > 0 {
			add("▶ Reanudar la pestaña", "tab-life")
		}
		add("✦ Cambiar alias de claude de la pestaña…", "tab-alias")
		add("✕ Cerrar pestaña", "tab-close")
		return "Pestaña " + name, items, true
	}

	win, err := t.Run("display", "-p", "-t", target, "#{window_id}")
	if err != nil {
		return "", nil, false
	}
	session, _ := t.Run("display", "-p", "-t", win, "#{session_name}")
	panes, _ := t.Lines("list-panes", "-t", win, "-F", "#{pane_id}"+sep+"#{@cw_alias}"+sep+"#{@cw_kind}"+sep+"#{@cw_status}")
	onlyPane := ""
	if strings.HasPrefix(target, "%") {
		onlyPane = target
	}
	var names []string
	for _, p := range panes {
		if len(p) == 4 && p[2] != kindHome {
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
		if len(p) != 4 || p[2] == kindHome || (onlyPane != "" && p[0] != onlyPane) {
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
			if p[3] == statusSleeping {
				add("▶ Reanudar", "life:"+p[0])
			} else {
				add("⏾ Suspender (Claude deja de correr; la conversación queda)", "life:"+p[0])
			}
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
	case "tab-dir":
		session, err := t.Run("display", "-p", "-t", target, "#{session_name}")
		if err != nil {
			return err
		}
		return uiTabDir(ws, session)
	case "tab-life":
		session, err := t.Run("display", "-p", "-t", target, "#{session_name}")
		if err != nil {
			return err
		}
		return cmdLife([]string{ws, "toggle", session})
	case "life":
		return cmdLife([]string{ws, "toggle", arg})
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
