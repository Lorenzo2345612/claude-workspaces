package main

import (
	"strconv"
	"strings"
)

type sessionRow struct {
	session, windowID, paneID string
	tab                       string
	slot                      Slot
}

func listSessionRows(t Tmux) []sessionRow {
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{session_name}", "#{window_id}", "#{pane_id}", "#{@cw_title}", "#{@cw_alias}",
		"#{@cw_kind}", "#{@cw_status}", "#{@cw_sid}", "#{@cw_cwd}", "#{@cw_slot}", "#{@cw_args}", "#{@cw_claude_alias}",
	}, sep))
	if err != nil {
		return nil
	}
	var out []sessionRow
	for _, r := range rows {
		if len(r) != 12 || r[5] == kindHome {
			continue
		}
		kind := r[5]
		if kind == "" {
			kind = kindShell
		}
		name := r[4]
		if name == "" {
			name = "shell"
		}
		out = append(out, sessionRow{session: r[0], windowID: r[1], paneID: r[2], tab: r[3],
			slot: Slot{Alias: name, Kind: kind, Status: r[6], SessionID: r[7], Cwd: r[8], ID: r[9], Args: argsFromOption(r[10]), ClaudeAlias: r[11]}})
	}
	return out
}

// uiSessions lists every session of the workspace; Enter edits one, i jumps to it.
func uiSessions(ws string) error {
	t := Tmux{WS: ws}
	client, _, err := currentClient(t)
	if err != nil {
		return err
	}
	sel := 0
	confirm := "" // "x" closing the selected session, "w" closing its tab
	for {
		rows := listSessionRows(t)
		if len(rows) == 0 {
			pause("No hay sesiones en este workspace.")
			return nil
		}
		sel = min(sel, len(rows)-1)
		hits := drawSessionList(rows, sel, confirm)
		setRaw(true)
		keys, err := readKeys()
		setRaw(false)
		if err != nil {
			return nil
		}
		for _, k := range keys {
			action := ""
			if k.kind == keyClick {
				action = hitAt(hits, k.x, k.y)
				if strings.HasPrefix(action, "row:") {
					sel, _ = strconv.Atoi(action[4:])
					continue
				}
			}
			if confirm != "" {
				if action == "yes" || (k.kind == keyRune && (k.r == 's' || k.r == 'y')) {
					var err error
					if confirm == "x" {
						err = closeWindow(ws, rows[sel].paneID)
					} else {
						err = closeTab(ws, rows[sel].session, client)
					}
					if err != nil {
						pause("Error: " + err.Error())
					}
				}
				if action != "" || k.kind != keyClick {
					confirm = ""
				}
				break
			}
			switch {
			case action == "closes" || (k.kind == keyRune && k.r == 'x'):
				confirm = "x"
			case action == "closet" || (k.kind == keyRune && k.r == 'w'):
				confirm = "w"
			case action == "quit" || k.kind == keyEsc || (k.kind == keyRune && k.r == 'q'):
				return nil
			case k.kind == keyUp || k.kind == keyWheelUp:
				sel = (sel - 1 + len(rows)) % len(rows)
			case k.kind == keyDown || k.kind == keyWheelDown:
				sel = (sel + 1) % len(rows)
			case action == "go" || (k.kind == keyRune && k.r == 'i'):
				_, _ = t.Run("switch-client", "-c", client, "-t", rows[sel].session, ";", "select-window", "-t", rows[sel].windowID, ";", "select-pane", "-t", rows[sel].paneID)
				return nil
			case action == "edit" || k.kind == keyEnter:
				if err := editSession(ws, rows[sel]); err != nil {
					pause("Error: " + err.Error())
				}
			}
		}
	}
}

func drawSessionList(rows []sessionRow, sel int, confirm string) []hit {
	var s screen
	s.at(2, 4, sgrMuted+fit("PESTAÑA", 16)+fit("NOMBRE", 24)+fit("CLAUDE", 17)+"ESTADO"+sgrReset)
	for i, r := range rows {
		kind, state := "❯ terminal", ""
		if r.slot.Kind == kindClaude {
			kind = "✦ " + r.slot.ClaudeAlias
			if r.slot.ClaudeAlias == "" {
				kind = "✦ " + builtinAlias
			}
			state = map[string]string{statusBusy: "● trabajando", statusWaiting: "? pide permiso",
				statusSleeping: "z dormida"}[r.slot.Status]
			if state == "" {
				state = "○ lista"
			}
		}
		line := " " + fit(r.tab, 15) + " " + fit(r.slot.Alias, 23) + " " + fit(kind, 16) + " " + fit(state, 15) + " "
		if i == sel {
			s.btn(3+i, 3, sgrSel+line+sgrReset, "row:"+strconv.Itoa(i))
		} else {
			s.btn(3+i, 3, line, "row:"+strconv.Itoa(i))
		}
	}
	foot := 4 + len(rows)
	switch confirm {
	case "x":
		s.at(foot, 4, sgrRed+"¿Cerrar la sesión "+rows[sel].slot.Alias+"?"+sgrReset)
		s.place(foot+1, 4, []lineItem{buttonLine(0, btnDef{label: "Cerrar sesión", id: "yes", style: sgrDanger}, btnDef{label: "Cancelar", id: "no"})})
	case "w":
		s.at(foot, 4, sgrRed+"¿Cerrar la pestaña "+rows[sel].tab+" con todas sus sesiones?"+sgrReset)
		s.place(foot+1, 4, []lineItem{buttonLine(0, btnDef{label: "Cerrar pestaña", id: "yes", style: sgrDanger}, btnDef{label: "Cancelar", id: "no"})})
	default:
		s.at(foot, 4, sgrDim+"Selecciona una sesión y elige qué hacer:"+sgrReset)
		s.place(foot+1, 4, []lineItem{buttonLine(0,
			btnDef{label: "Editar", id: "edit", primary: true},
			btnDef{label: "Ir a la sesión", id: "go"},
			btnDef{label: "Cerrar sesión", id: "closes"},
			btnDef{label: "Cerrar pestaña", id: "closet"},
			btnDef{label: "Salir", id: "quit"})})
	}
	s.b.WriteString("\033[?25l")
	hits := s.hits
	s.flush(true)
	return hits
}

// editSession changes a session's alias and its own claude arguments. The alias renames the
// window at once; Claude takes the new name (and new arguments) when the session restarts —
// right away with "Guardar y reiniciar", which is offered only while the session is not working.
func editSession(ws string, r sessionRow) error {
	t := Tmux{WS: ws}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	f := &sessionForm{
		heading:       "Editar " + r.slot.Alias + " · pestaña " + r.tab,
		editing:       true,
		isShell:       r.slot.Kind != kindClaude,
		history:       aliasHistory(),
		globalArgs:    cfg.ClaudeArgs,
		claudeAliases: cfg.claudeAliases(),
		alias:         []rune(r.slot.Alias),
		args:          []rune(joinArgs(r.slot.Args)),
		preview: func(name string, args []string, alias string) string {
			s := r.slot
			s.Alias, s.Args, s.ClaudeAlias = name, args, alias
			return l.commandPreview(s, true)
		},
	}
	f.selectAlias(cfg.claudeAlias(r.slot.ClaudeAlias).Name)
	switch r.slot.Status {
	case statusBusy, statusWaiting:
		f.restartMsg = "Está trabajando: cambiar el alias de claude la reinicia y le manda el mensaje de continuación."
	default:
		f.canRestart = r.slot.Kind == kindClaude
	}
	oldAlias := cfg.claudeAlias(r.slot.ClaudeAlias).Name
	f.onAliasChange = func(name string) string {
		if name != oldAlias {
			return "Al reiniciar con " + name + ", mandar «" + shorten(cfg.ContinueMessage, 18) + "»"
		}
		return ""
	}
	f.sendContinue = true
	action := f.run()
	if action == actCancel {
		return nil
	}
	alias := cleanLabel(string(f.alias))
	args := splitArgs(string(f.args))
	if err := setSlotName(t, r.paneID, alias); err != nil {
		return err
	}
	if _, err := t.Run("set", "-p", "-t", r.paneID, "@cw_args", argsToOption(args), ";",
		"set", "-p", "-t", r.paneID, "@cw_claude_alias", f.selectedAlias()); err != nil {
		return err
	}
	if r.slot.Kind == kindClaude {
		recordAlias(alias, r.tab)
	}
	if r.slot.Kind == kindClaude && f.selectedAlias() != oldAlias {
		// Switching the claude alias: restart under the new alias and let it continue its work.
		return respawnSlot(ws, r.paneID, f.sendContinue)
	}
	if action == actSaveRestart {
		return respawnSlot(ws, r.paneID, false)
	}
	return nil
}

// uiEditCurrent opens the editor on the session the user is looking at.
func uiEditCurrent(ws string) error {
	t := Tmux{WS: ws}
	client, _, err := currentClient(t)
	if err != nil {
		return err
	}
	pane, err := t.Run("display", "-p", "-c", client, "#{pane_id}")
	if err != nil {
		return err
	}
	for _, r := range listSessionRows(t) {
		if r.paneID == pane {
			return editSession(ws, r)
		}
	}
	pause("Esta pestaña no tiene una sesión seleccionada.")
	return nil
}

// shorten truncates s to n runes with "…", without padding.
func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
