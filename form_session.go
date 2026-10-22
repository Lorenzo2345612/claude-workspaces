package main

import (
	"path/filepath"
	"strings"
)

const (
	actCancel = iota
	actSave
	actSaveRestart
)

// sessionForm creates a Claude session or edits one: alias (picked from the aliases used
// before, then editable), this session's own claude arguments, and a preview of the command.
type sessionForm struct {
	heading    string
	editing    bool
	isShell    bool
	canRestart bool
	restartMsg string // why restarting is not offered, when it is not
	globalArgs []string

	alias, args   []rune
	claudeAliases []ClaudeAlias // choices for how to launch claude
	aliasIdx      int
	query         []rune // what was typed in the alias field; ↑↓ browse its matches
	history       []aliasUse
	matches       []aliasUse
	sel           int // highlighted match, -1 none
	focus         int // 0 name · 4 claude alias · 1 args · 2 primary button · 3 restart button
	preview       func(name string, args []string, claudeAlias string) string
	// onAliasChange returns a note shown when the selected claude alias differs from the
	// session's current one (editing only).
	onAliasChange func(name string) string
	sendContinue  bool // checkbox shown when the alias changes: send the continue message on restart
	hits          []hit
	errMsg        string
}

func (f *sessionForm) refresh() {
	f.matches = suggestAliases(string(f.query), f.history, 5)
	f.sel = -1
}

func (f *sessionForm) order() []int {
	var o []int
	switch {
	case f.isShell:
		return []int{0, 2}
	case f.editing && f.canRestart:
		o = []int{0, 4, 1, 2, 3}
	default:
		o = []int{0, 4, 1, 2}
	}
	if f.aliasNote() != "" { // the continue checkbox sits below the arguments, before the buttons
		o = append([]int{0, 4, 1, 6}, o[3:]...)
	}
	return o
}

// aliasNote is the checkbox label while the selected alias differs from the session's own.
func (f *sessionForm) aliasNote() string {
	if f.onAliasChange == nil || f.isShell {
		return ""
	}
	return f.onAliasChange(f.selectedAlias())
}

func (f *sessionForm) move(d int) {
	o := f.order()
	i := 0
	for j, v := range o {
		if v == f.focus {
			i = j
		}
	}
	f.focus = o[(i+d+len(o))%len(o)]
}

func (f *sessionForm) draw() {
	var s screen
	field := func(row int, label string, v []rune, focused bool) {
		style := sgrField
		if focused {
			style = sgrFocus
		}
		shown := string(v)
		if r := []rune(shown); len(r) > fieldWidth-2 {
			shown = "…" + string(r[len(r)-(fieldWidth-3):])
		}
		s.at(row, 4, label)
		s.at(row, 17, style+" "+fit(shown, fieldWidth-1)+sgrReset)
	}
	s.at(1, 4, sgrMuted+f.heading+sgrReset)
	if !f.editing && !f.isShell {
		for i, agent := range []string{"Claude", "Codex"} {
			selected := len(f.claudeAliases) > 0 && f.claudeAliases[f.aliasIdx].isCodex() == (i == 1)
			s.btn(2, 17+i*13, button(agent, selected, false), "agent:"+strings.ToLower(agent))
		}
	}
	field(3, "Nombre", f.alias, f.focus == 0)
	s.zone(3, 4, fieldWidth+13, "f0")
	row := 4
	if f.focus == 0 {
		for i, m := range f.matches {
			line := " " + fit(m.Alias, 22) + fit(m.Tab, 12) + fit(whenES(m.At), 11) + " "
			s.zone(row, 17, fieldWidth, "match:"+itoa(i))
			if i == f.sel {
				s.at(row, 17, sgrSel+line+sgrReset)
			} else {
				s.at(row, 17, sgrMuted+line+sgrReset)
			}
			row++
		}
		if len(f.matches) == 0 && len(f.history) > 0 {
			s.at(row, 17, sgrDim+" ningún alias usado coincide"+sgrReset)
			row++
		}
	}
	row = max(row, 6) + 1
	argsRow := row
	if !f.isShell && len(f.claudeAliases) > 0 {
		a := f.claudeAliases[f.aliasIdx]
		style := sgrField
		if f.focus == 4 {
			style = sgrSel
		}
		s.at(row, 4, "Agente")
		s.at(row, 17, style+" ‹ "+fit(a.Name, fieldWidth-14)+" › "+sgrReset+sgrDim+
			"  "+itoa(f.aliasIdx+1)+"/"+itoa(len(f.claudeAliases))+sgrReset)
		s.zone(row, 17, 3, "aprev")
		s.zone(row, 20, fieldWidth-14, "anext")
		s.zone(row, 20+fieldWidth-14, 3, "anext")
		s.at(row+1, 17, sgrDim+" "+fit(a.summary(), fieldWidth-1)+sgrReset)
		row += 3
		argsRow = row
	}
	if !f.isShell {
		field(row, "Argumentos", f.args, f.focus == 1)
		s.zone(row, 4, fieldWidth+13, "f1")
		row++
		if len(f.globalArgs) > 0 {
			s.at(row, 17, sgrDim+" globales: "+fit(joinArgs(f.globalArgs), fieldWidth-12)+sgrReset)
		} else {
			s.at(row, 17, sgrDim+" ej. --model opus --permission-mode plan"+sgrReset)
		}
		row += 2
		cmd := f.preview(cleanLabel(string(f.alias)), splitArgs(string(f.args)), f.selectedAlias())
		s.at(row, 4, sgrMuted+"Comando"+sgrReset)
		for line, chunk := range wrapWords(cmd, fieldWidth+1, 3) {
			s.at(row+line, 17, sgrDim+chunk+sgrReset)
		}
		row += 4
	} else {
		row++
	}
	note := f.aliasNote()
	if f.errMsg != "" {
		s.at(row-1, 4, sgrRed+f.errMsg+sgrReset)
	} else if note != "" {
		box := "[ ]"
		if f.sendContinue {
			box = "[x]"
		}
		style := sgrBusy
		if f.focus == 6 {
			style = sgrSel
		}
		s.btn(row-1, 4, style+" "+box+" "+fit(note, fieldWidth+8)+" "+sgrReset, "check")
	} else if f.editing && !f.isShell && !f.canRestart && f.restartMsg != "" {
		s.at(row-1, 4, sgrDim+f.restartMsg+sgrReset)
	}
	primary := "Crear"
	if f.editing {
		primary = "Guardar"
	}
	btns := []btnDef{{label: "Cancelar", id: "cancel"}, {label: primary, id: "save", primary: true, focused: f.focus == 2}}
	if f.editing && f.canRestart {
		btns = append(btns, btnDef{label: "Guardar y reiniciar", id: "restart", focused: f.focus == 3})
	}
	s.place(row, 4, []lineItem{buttonLine(0, btns...)})
	s.closeX("cancel")
	f.hits = s.hits
	switch f.focus {
	case 0:
		s.at(3, 18+min(len(f.alias), fieldWidth-2), "\033[?25h")
	case 1:
		s.at(argsRow, 18+min(len(f.args), fieldWidth-2), "\033[?25h")
	default:
		s.b.WriteString("\033[?25l")
	}
	s.flush(true)
}

func (f *sessionForm) run() int {
	f.query = f.alias
	f.refresh()
	setRaw(true)
	defer func() { setRaw(false); stdoutWrite("\033[?25h") }()
	f.draw()
	for {
		keys, err := readKeys()
		if err != nil {
			return actCancel
		}
		for _, k := range keys {
			f.errMsg = ""
			if k.kind == keyClick {
				switch id := hitAt(f.hits, k.x, k.y); {
				case id == "cancel":
					return actCancel
				case id == "save" || id == "restart":
					if cleanLabel(string(f.alias)) == "" {
						f.errMsg, f.focus = "Falta el nombre.", 0
						continue
					}
					if id == "restart" {
						return actSaveRestart
					}
					return actSave
				case strings.HasPrefix(id, "agent:"):
					f.selectAgent(strings.TrimPrefix(id, "agent:"))
				case id == "f0":
					f.focus = 0
				case id == "f1":
					f.focus = 1
				case id == "check":
					f.focus, f.sendContinue = 6, !f.sendContinue
				case strings.HasPrefix(id, "match:"):
					i := int(id[6] - '0')
					f.alias = []rune(f.matches[i].Alias)
					f.sel = i
				case id == "aprev" || id == "anext":
					d := 1
					if id == "aprev" {
						d = -1
					}
					f.focus = 4
					f.aliasIdx = (f.aliasIdx + d + len(f.claudeAliases)) % len(f.claudeAliases)
				}
				continue
			}
			var text *[]rune
			switch f.focus {
			case 0:
				text = &f.alias
			case 1:
				text = &f.args
			}
			switch {
			case k.kind == keyEsc:
				return actCancel
			case f.focus == 0 && (k.kind == keyUp || k.kind == keyDown):
				if len(f.matches) > 0 {
					if k.kind == keyDown {
						f.sel = (f.sel + 1) % len(f.matches)
					} else if f.sel <= 0 {
						f.sel = len(f.matches) - 1
					} else {
						f.sel--
					}
					f.alias = []rune(f.matches[f.sel].Alias)
				}
			case f.focus == 6 && (k.kind == keyEnter || (k.kind == keyRune && k.r == ' ')):
				f.sendContinue = !f.sendContinue
			case f.focus == 4 && (k.kind == keyLeft || k.kind == keyRight) && len(f.claudeAliases) > 0:
				d := 1
				if k.kind == keyLeft {
					d = -1
				}
				f.aliasIdx = (f.aliasIdx + d + len(f.claudeAliases)) % len(f.claudeAliases)
			case (f.focus == 2 || f.focus == 3) && (k.kind == keyLeft || k.kind == keyRight):
				if f.editing && f.canRestart {
					f.focus = 5 - f.focus
				}
			case k.kind == keyEnter && (f.focus == 2 || f.focus == 3 || f.focus == 1 || f.isShell):
				if cleanLabel(string(f.alias)) == "" {
					f.errMsg, f.focus = "Falta el nombre.", 0
					continue
				}
				if f.focus == 3 {
					return actSaveRestart
				}
				return actSave
			case k.kind == keyEnter || k.kind == keyTab || (k.kind == keyDown && f.focus != 0):
				f.move(1)
			case k.kind == keyBackTab || (k.kind == keyUp && f.focus != 0):
				f.move(-1)
			case k.kind == keyBackspace && text != nil:
				if len(*text) > 0 {
					*text = (*text)[:len(*text)-1]
				}
				if f.focus == 0 {
					f.query = f.alias
					f.refresh()
				}
			case k.kind == keyCtrlU && text != nil:
				*text = nil
				if f.focus == 0 {
					f.query = nil
					f.refresh()
				}
			case k.kind == keyRune && text != nil:
				*text = append(*text, k.r)
				if f.focus == 0 {
					f.query = f.alias
					f.refresh()
				}
			}
		}
		f.draw()
	}
}

// commandPreview renders the command a Claude slot would run, with the hook settings elided.
func (l *Launcher) commandPreview(s Slot, resuming bool) string {
	lc := *l
	lc.Cfg.LazyStart = false
	s.Status = statusIdle
	if s.SessionID == "" {
		s.SessionID = "<id-nuevo>"
	}
	argv, _ := lc.claudeArgv(&s, resuming)
	for len(argv) > 0 && filepath.Base(argv[0]) == "env" { // drop the `env -u …` prefix
		i := 1
		for i+1 < len(argv) && argv[i] == "-u" {
			i += 2
		}
		argv = argv[i:]
	}
	if len(argv) == 3 && argv[1] == "-ic" { // run through the user's shell: show the command line
		return strings.Replace(argv[2], shQuote(lc.hookSettings()), "'{…hooks de cw…}'", 1)
	}
	for i, a := range argv {
		if i > 0 && argv[i-1] == "--settings" {
			argv[i] = "{…hooks de cw…}"
		}
		if a == lc.ClaudeBin {
			argv[i] = "claude"
		}
	}
	return strings.TrimSpace(joinArgs(argv))
}

// wrapWords breaks text at spaces into at most maxLines lines of width w; the last line ends
// in "…" when text is left over. A word longer than w is cut.
func wrapWords(text string, w, maxLines int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		for len([]rune(word)) > w {
			if cur != "" {
				lines, cur = append(lines, cur), ""
			}
			lines = append(lines, string([]rune(word)[:w]))
			word = string([]rune(word)[w:])
		}
		switch {
		case cur == "":
			cur = word
		case len([]rune(cur))+1+len([]rune(word)) <= w:
			cur += " " + word
		default:
			lines, cur = append(lines, cur), word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = fit(lines[maxLines-1], w-1) + "…"
	}
	return lines
}

func (f *sessionForm) selectedAlias() string {
	if len(f.claudeAliases) == 0 {
		return ""
	}
	return f.claudeAliases[f.aliasIdx].Name
}

// selectAlias preselects a claude alias by name (the session's own, or the default).
func (f *sessionForm) selectAlias(name string) {
	for i, a := range f.claudeAliases {
		if a.Name == name {
			f.aliasIdx = i
		}
	}
}

func (f *sessionForm) selectAgent(agent string) {
	for i, a := range f.claudeAliases {
		if a.isCodex() == (agent == "codex") {
			f.aliasIdx = i
			return
		}
	}
	f.errMsg = "Configura un alias de " + agent
}
