package main

import (
	"strings"
)

const (
	startClaude = iota
	startShell
	startEmpty
)

// newTabForm is the "Nueva pestaña" dialog: name, directory with suggestions, how to start, alias.
type newTabForm struct {
	name, dir, alias []rune
	start            int
	focus            int // 0 name · 1 dir · 2 start · 3 alias · 4 create
	sel              int // selected suggestion, -1 none
	suggestions      []dirSuggestion
	recents, work    []string
	errMsg           string
	claudeAliases    []ClaudeAlias
	aliasIdx         int
}

const fieldWidth = 46

func (f *newTabForm) fields() []int {
	if f.start == startClaude {
		return []int{0, 1, 2, 5, 3, 4}
	}
	return []int{0, 1, 2, 4}
}

func (f *newTabForm) move(delta int) {
	fs := f.fields()
	i := 0
	for j, v := range fs {
		if v == f.focus {
			i = j
		}
	}
	i = (i + delta + len(fs)) % len(fs)
	f.focus = fs[i]
	if f.focus == 1 {
		f.refresh()
	}
}

func (f *newTabForm) refresh() {
	f.suggestions = suggestDirs(string(f.dir), f.recents, f.work, 4)
	f.sel = 0
	if len(f.suggestions) == 0 {
		f.sel = -1
	}
}

func (f *newTabForm) text() *[]rune {
	switch f.focus {
	case 0:
		return &f.name
	case 1:
		return &f.dir
	case 3:
		return &f.alias
	}
	return nil
}

func (f *newTabForm) draw() {
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
	field(2, "Nombre", f.name, f.focus == 0)
	field(3, "Directorio", f.dir, f.focus == 1)
	row := 4
	if f.focus == 1 {
		for i, sg := range f.suggestions {
			tag := ""
			if sg.recent {
				tag = "reciente"
			}
			line := " " + fit(shortPath(sg.path), fieldWidth-11) + fit(tag, 9) + " "
			if i == f.sel {
				s.at(row, 17, sgrSel+line+sgrReset)
			} else {
				s.at(row, 17, sgrMuted+line+sgrReset)
			}
			row++
		}
	}
	row = max(row, 5) + 1
	s.at(row, 4, "Empezar con")
	col := 17
	for i, opt := range []string{" ✦ Claude ", " ❯ Terminal ", " ○ Vacía "} {
		style := sgrField
		if i == f.start {
			style = sgrSel
			if f.focus != 2 {
				style = "\033[48;5;24m\033[38;5;255m"
			}
		}
		s.at(row, col, style+opt+sgrReset)
		col += len([]rune(opt)) + 2
	}
	row++
	if f.start == startClaude && len(f.claudeAliases) > 0 {
		style := sgrField
		if f.focus == 5 {
			style = sgrSel
		}
		a := f.claudeAliases[f.aliasIdx]
		s.at(row, 4, "Claude")
		s.at(row, 17, style+" ‹ "+fit(a.Name, 20)+" › "+sgrReset+sgrDim+" "+fit(a.summary(), fieldWidth-26)+sgrReset)
		row++
	}
	aliasRow := row
	if f.start == startClaude {
		field(row, "Nombre", f.alias, f.focus == 3)
		if len(f.alias) == 0 && f.focus != 3 {
			s.at(row, 18, sgrField+sgrDim+string(f.name)+sgrReset)
		}
		row++
	}
	row++
	if f.errMsg != "" {
		s.at(row, 4, sgrRed+f.errMsg+sgrReset)
	}
	row++
	hint := "Tab siguiente · ↑↓ elegir · Esc cancelar"
	if f.focus == 1 {
		hint = "Tab completar · ↑↓ elegir · Enter aceptar · Esc"
	} else if f.focus == 2 {
		hint = "←→ o c / t / v elegir · Tab siguiente · Esc"
	} else if f.focus == 5 {
		hint = "←→ elegir alias de claude · Tab siguiente"
	}
	s.at(row, 3, sgrMuted+hint+sgrReset)
	create := sgrField + " Crear " + sgrReset
	if f.focus == 4 {
		create = sgrSel + " Crear " + sgrReset
	}
	s.at(row, 17+fieldWidth-7, create)

	// put the cursor at the end of the focused text field
	switch f.focus {
	case 0:
		s.at(2, 18+min(len(f.name), fieldWidth-2), "\033[?25h")
	case 1:
		s.at(3, 18+min(len(f.dir), fieldWidth-2), "\033[?25h")
	case 3:
		s.at(aliasRow, 18+min(len(f.alias), fieldWidth-2), "\033[?25h")
	default:
		s.b.WriteString("\033[?25l")
	}
	s.flush(true)
}

// run drives the form; it returns false when the user cancels.
func (f *newTabForm) run() bool {
	setRaw(true)
	defer func() { setRaw(false); stdoutWrite("\033[?25h") }()
	f.draw()
	for {
		keys, err := readKeys()
		if err != nil {
			return false
		}
		for _, k := range keys {
			f.errMsg = ""
			switch {
			case k.kind == keyEsc:
				return false
			case f.focus == 1 && (k.kind == keyUp || k.kind == keyDown):
				if len(f.suggestions) > 0 {
					d := 1
					if k.kind == keyUp {
						d = -1
					}
					f.sel = (f.sel + d + len(f.suggestions)) % len(f.suggestions)
				}
			case f.focus == 1 && k.kind == keyTab && f.sel >= 0:
				f.dir = []rune(shortPath(f.suggestions[f.sel].path) + "/")
				f.refresh()
			case f.focus == 1 && k.kind == keyEnter:
				if f.sel >= 0 {
					f.dir = []rune(shortPath(f.suggestions[f.sel].path))
				}
				f.move(1)
			case f.focus == 5 && (k.kind == keyLeft || k.kind == keyRight) && len(f.claudeAliases) > 0:
				d := 1
				if k.kind == keyLeft {
					d = -1
				}
				f.aliasIdx = (f.aliasIdx + d + len(f.claudeAliases)) % len(f.claudeAliases)
			case f.focus == 2 && (k.kind == keyLeft || k.kind == keyRight):
				d := 1
				if k.kind == keyLeft {
					d = -1
				}
				f.start = (f.start + d + 3) % 3
			case f.focus == 2 && k.kind == keyRune && strings.ContainsRune("ctvCTV", k.r):
				f.start = map[rune]int{'c': startClaude, 't': startShell, 'v': startEmpty}[k.r|0x20]
			case k.kind == keyTab || k.kind == keyDown:
				f.move(1)
			case k.kind == keyBackTab || k.kind == keyUp:
				f.move(-1)
			case k.kind == keyEnter && (f.focus == 4 || f.focus == 3):
				if f.validate() {
					return true
				}
			case k.kind == keyEnter:
				f.move(1)
			case k.kind == keyBackspace:
				if p := f.text(); p != nil && len(*p) > 0 {
					*p = (*p)[:len(*p)-1]
				}
				if f.focus == 1 {
					f.refresh()
				}
			case k.kind == keyCtrlU:
				if p := f.text(); p != nil {
					*p = nil
				}
				if f.focus == 1 {
					f.refresh()
				}
			case k.kind == keyRune:
				if p := f.text(); p != nil {
					*p = append(*p, k.r)
				}
				if f.focus == 1 {
					f.refresh()
				}
			}
		}
		f.draw()
	}
}

func (f *newTabForm) validate() bool {
	if cleanLabel(string(f.name)) == "" {
		f.errMsg, f.focus = "Falta el nombre de la pestaña.", 0
		return false
	}
	abs, err := expandPath(string(f.dir))
	if err != nil || !isDir(abs) {
		f.errMsg, f.focus = "Ese directorio no existe.", 1
		f.refresh()
		return false
	}
	return true
}

func stdoutWrite(s string) { var sc screen; sc.b.WriteString(s); sc.flush(false) }
