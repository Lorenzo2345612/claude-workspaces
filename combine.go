package main

import (
	"strings"
)

// viewInfo is one view (tmux window) and the names of the sessions it shows.
type viewInfo struct {
	window, session, tab string
	names                []string
}

func listViews(t Tmux) []viewInfo {
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{window_id}", "#{session_name}", "#{@cw_title}", "#{@cw_alias}", "#{@cw_kind}"}, sep))
	if err != nil {
		return nil
	}
	var out []viewInfo
	index := map[string]int{}
	for _, r := range rows {
		if len(r) != 5 || notSession(r[4]) {
			continue
		}
		i, ok := index[r[0]]
		if !ok {
			i = len(out)
			index[r[0]] = i
			out = append(out, viewInfo{window: r[0], session: r[1], tab: r[2]})
		}
		name := r[3]
		if name == "" {
			name = "shell"
		}
		out[i].names = append(out[i].names, name)
	}
	return out
}

// uiCombine adds sessions from other views to the view the user is looking at, and chooses how
// the view arranges them.
func uiCombine(ws string) error {
	t := Tmux{WS: ws}
	migrateLegacyOptions(t)
	client, _, err := currentClient(t)
	if err != nil {
		return err
	}
	current, err := t.Run("display", "-p", "-c", client, "#{window_id}")
	if err != nil {
		return err
	}
	if kind, _ := t.Run("display", "-p", "-c", client, "#{@cw_kind}"); kind == kindHome {
		pause("Esta pestaña todavía no tiene sesiones: crea una primero.")
		return nil
	} else if kind == kindDoc {
		pause("Un documento no se combina con sesiones: vuelve a una sesión primero.")
		return nil
	}
	var here viewInfo
	var others []viewInfo
	for _, v := range listViews(t) {
		if v.window == current {
			here = v
		} else {
			others = append(others, v)
		}
	}
	if len(others) == 0 {
		pause("No hay otras sesiones para combinar con esta vista.")
		return nil
	}
	checked := make([]bool, len(others))
	layouts := []struct{ id, label string }{{"lado", "▥ Lado a lado"}, {"apilado", "▤ Arriba y abajo"}, {"cuadricula", "▦ Cuadrícula"}}
	layout, sel := 0, 0
	var hits []hit
	draw := func() {
		var s screen
		s.at(2, 4, sgrMuted+"ESTA VISTA"+sgrReset+"   "+sgrBold+strings.Join(here.names, " · ")+sgrReset)
		s.at(4, 4, sgrMuted+"AGREGAR A ESTA VISTA"+sgrReset+sgrDim+"   marca las sesiones que quieres ver aquí"+sgrReset)
		// The list scrolls so the layout and the buttons under it always fit in the popup.
		_, h := termSize()
		visible := max(2, h-11)
		offset := 0
		if sel >= visible {
			offset = sel - visible + 1
		}
		for i, v := range others {
			if i < offset || i >= offset+visible {
				continue
			}
			box := "[ ]"
			if checked[i] {
				box = "[x]"
			}
			line := " " + box + " " + fit(v.tab, 14) + " " + fit(strings.Join(v.names, " · "), 44) + " "
			if i == sel {
				line = sgrSel + line + sgrReset
			}
			s.btn(5+i-offset, 3, line, "view:"+itoa(i))
		}
		if offset+visible < len(others) {
			s.at(5+visible, 70, sgrDim+"▼ más"+sgrReset)
		}
		row := 6 + min(len(others), visible)
		s.at(row, 4, sgrMuted+"DISTRIBUCIÓN"+sgrReset)
		var btns []btnDef
		for i, l := range layouts {
			btns = append(btns, btnDef{label: l.label, id: "layout:" + itoa(i), primary: i == layout})
		}
		s.place(row+1, 4, []lineItem{buttonLine(0, btns...)})
		n := 0
		for _, c := range checked {
			if c {
				n++
			}
		}
		label := "Combinar"
		if n > 0 {
			label = "Combinar " + itoa(n)
		}
		s.place(row+3, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"}, btnDef{label: label, id: "ok", primary: true})})
		s.closeX("cancel")
		s.b.WriteString("\033[?25l")
		hits = s.hits
		s.flush(true)
	}
	setRaw(true)
	draw()
	for {
		keys, err := readKeys()
		if err != nil {
			setRaw(false)
			return nil
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(hits, k.x, k.y)
			}
			switch {
			case id == "cancel" || k.kind == keyEsc:
				setRaw(false)
				return nil
			case strings.HasPrefix(id, "view:"):
				sel = atoiDefault(id[5:], 0)
				checked[sel] = !checked[sel]
			case strings.HasPrefix(id, "layout:"):
				layout = atoiDefault(id[7:], 0)
			case k.kind == keyUp:
				sel = (sel - 1 + len(others)) % len(others)
			case k.kind == keyDown:
				sel = (sel + 1) % len(others)
			case k.kind == keyRune && k.r == ' ':
				checked[sel] = !checked[sel]
			case k.kind == keyLeft:
				layout = (layout - 1 + len(layouts)) % len(layouts)
			case k.kind == keyRight || k.kind == keyTab:
				layout = (layout + 1) % len(layouts)
			case id == "ok" || k.kind == keyEnter:
				setRaw(false)
				for i, v := range others {
					if checked[i] {
						if err := cmdJoin([]string{ws, v.window, current}); err != nil {
							return err
						}
					}
				}
				return cmdLayout([]string{ws, current, layouts[layout].id})
			}
		}
		draw()
	}
}
