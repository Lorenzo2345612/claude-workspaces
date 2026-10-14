package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// cmdLauncher is what a bare `cw` shows: create a workspace in this directory, or resume one
// of the saved / running workspaces.
func cmdLauncher() error {
	if os.Getenv("CW_WS") != "" {
		return fmt.Errorf("ya estás dentro de un workspace; desconéctate primero (Alt+m → Desconectar)")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	l := &launcher{cwd: cwd}
	choice, err := l.run()
	if err != nil || choice == "" {
		return err
	}
	if choice == "\x00new" {
		return cmdNewHere()
	}
	return cmdStart(choice, ".", false)
}

type launcher struct {
	cwd      string
	entries  []entry
	sel      int // 0 = new workspace, 1.. = entries
	deleting bool
	msg      string
	hits     []hit
}

func (l *launcher) load() {
	l.entries = listEntries()
	// running first, then most recently saved
	sortEntries(l.entries)
	l.sel = min(l.sel, len(l.entries))
}

func sortEntries(es []entry) {
	at := func(e entry) time.Time {
		if e.saved == nil {
			return time.Time{}
		}
		return e.saved.SavedAt
	}
	for i := 1; i < len(es); i++ {
		for j := i; j > 0; j-- {
			a, b := es[j-1], es[j]
			if (b.running && !a.running) || (a.running == b.running && at(b).After(at(a))) {
				es[j-1], es[j] = b, a
			}
		}
	}
}

func (l *launcher) run() (string, error) {
	l.load()
	os.Stdout.WriteString("\033[?1049h")
	setRaw(true)
	defer func() {
		setRaw(false)
		os.Stdout.WriteString("\033[?25h\033[?1049l")
	}()
	l.draw()
	for {
		keys, err := readKeys()
		if err != nil {
			return "", nil
		}
		for _, k := range keys {
			action := ""
			if k.kind == keyClick {
				action = hitAt(l.hits, k.x, k.y)
			}
			if l.deleting {
				if action == "delyes" || (k.kind == keyRune && (k.r == 's' || k.r == 'y')) {
					name := l.entries[l.sel-1].name
					if err := cmdRemoveQuiet(name); err != nil {
						l.msg = err.Error()
					} else {
						l.msg = "Borrado " + name + "."
					}
					l.load()
				}
				if action != "" || k.kind != keyClick {
					l.deleting = false
				}
				continue
			}
			l.msg = ""
			n := len(l.entries) + 1
			switch {
			case action == "quit" || k.kind == keyEsc || (k.kind == keyRune && k.r == 'q'):
				return "", nil
			case action == "new" || (k.kind == keyRune && k.r == 'n'):
				return "\x00new", nil
			case strings.HasPrefix(action, "ws:"):
				i, _ := strconv.Atoi(action[3:])
				return l.entries[i].name, nil
			case action == "del" || (k.kind == keyRune && (k.r == 'x' || k.r == 'd')):
				switch {
				case l.sel == 0:
					l.msg = "Elige primero un workspace de la lista."
				case l.entries[l.sel-1].running:
					l.msg = "Está corriendo: apágalo antes de borrarlo."
				default:
					l.deleting = true
				}
			case k.kind == keyUp || k.kind == keyWheelUp:
				l.sel = (l.sel - 1 + n) % n
			case k.kind == keyDown || k.kind == keyTab || k.kind == keyWheelDown:
				l.sel = (l.sel + 1) % n
			case k.kind == keyRune && k.r >= '1' && k.r <= '9':
				if i := int(k.r - '0'); i <= len(l.entries) {
					return l.entries[i-1].name, nil
				}
			case action == "open" || k.kind == keyEnter:
				if l.sel == 0 {
					return "\x00new", nil
				}
				return l.entries[l.sel-1].name, nil
			}
		}
		l.draw()
	}
}

func (l *launcher) draw() {
	w, h := termSize()
	const blockW = 86
	left := max((w-blockW)/2, 0) + 1
	var lines []lineItem
	add := func(li lineItem) { lines = append(lines, li) }

	add(plainLine(sgrAccent + sgrBold + "≡ cw" + sgrReset + " " + sgrDim + version + sgrReset + "   " + sgrMuted + "workspaces de Claude Code" + sgrReset))
	add(plainLine(""))
	card := sgrDim
	if l.sel == 0 {
		card = sgrAccent
	}
	add(clickLine(card+"╭────────────────────────────────────────────╮"+sgrReset, "new"))
	add(clickLine(card+"│"+sgrReset+"  "+sgrBold+"+ Nuevo workspace aquí"+sgrReset+"                    "+card+"│"+sgrReset, "new"))
	add(clickLine(card+"│"+sgrReset+"  "+sgrMuted+fit(shortPath(l.cwd), 42)+sgrReset+card+"│"+sgrReset, "new"))
	add(clickLine(card+"╰────────────────────────────────────────────╯"+sgrReset, "new"))
	add(plainLine(""))
	if len(l.entries) == 0 {
		add(plainLine(sgrDim + "Todavía no hay workspaces guardados." + sgrReset))
	} else {
		add(plainLine(sgrMuted + "RETOMAR UN WORKSPACE" + sgrReset + "   " + sgrDim + "clic para abrir" + sgrReset))
		for i, e := range l.entries {
			add(clickLine(l.entryLine(i, e), "ws:"+strconv.Itoa(i)))
		}
	}
	add(plainLine(""))
	switch {
	case l.deleting:
		add(plainLine(sgrRed + "¿Borrar el estado guardado de " + l.entries[l.sel-1].name + "?" + sgrReset +
			sgrDim + "  Las conversaciones de Claude no se borran." + sgrReset))
		add(buttonLine(0, btnDef{label: "Borrar", id: "delyes", style: sgrDanger}, btnDef{label: "Cancelar", id: "delno"}))
	default:
		if l.msg != "" {
			add(plainLine(sgrMuted + l.msg + sgrReset))
		} else {
			add(plainLine(""))
		}
		btns := []btnDef{{label: "+ Nuevo aquí", id: "new", primary: l.sel == 0}}
		if l.sel > 0 {
			btns = append([]btnDef{{label: "Abrir " + l.entries[l.sel-1].name, id: "open", primary: true}},
				btnDef{label: "+ Nuevo aquí", id: "new"}, btnDef{label: "Borrar guardado", id: "del"})
		}
		add(buttonLine(0, append(btns, btnDef{label: "Salir", id: "quit"})...))
	}

	var s screen
	s.place(max((h-len(lines))/2, 0)+1, left, lines)
	s.b.WriteString("\033[?25l")
	l.hits = s.hits
	s.flush(true)
}

func (l *launcher) entryLine(i int, e entry) string {
	state := sgrDim + fit("apagado", 10) + sgrReset
	if e.running {
		state = sgrGreen + fit("● abierto", 10) + sgrReset
	}
	tabs, sessions, working := 0, 0, 0
	when, titles := "", ""
	if e.saved != nil {
		tabs = len(e.saved.Tabs)
		var ts []string
		for _, t := range e.saved.Tabs {
			ts = append(ts, t.Title)
			sessions += len(t.Slots)
			for _, s := range t.Slots {
				if s.wasWorking() {
					working++
				}
			}
		}
		titles = strings.Join(ts, " · ")
		when = whenES(e.saved.SavedAt)
	}
	counts := fmt.Sprintf("%d pestañas, %d sesiones", tabs, sessions)
	work := ""
	if working > 0 && !e.running {
		work = " " + sgrBusy + "●" + strconv.Itoa(working) + sgrReset
	}
	plain := fmt.Sprintf(" %d  %s ", i+1, fit(e.name, 18))
	rest := fmt.Sprintf(" %s  %s  %s ", fit(counts, 22), fit(when, 11), fit(titles, 24))
	if l.sel == i+1 {
		stateText := "apagado"
		if e.running {
			stateText = "● abierto"
		}
		return sgrSel + plain + fit(stateText, 10) + rest + sgrReset + work
	}
	return plain + state + sgrMuted + rest + sgrReset + work
}

// termSize asks the terminal for its size (rows/cols) through stty.
func termSize() (int, int) {
	out, err := sttyOutput("size")
	if err == nil {
		if f := strings.Fields(out); len(f) == 2 {
			h, _ := strconv.Atoi(f[0])
			w, _ := strconv.Atoi(f[1])
			if w > 0 && h > 0 {
				return w, h
			}
		}
	}
	return 100, 30
}

func cmdRemoveQuiet(ws string) error {
	if (Tmux{WS: ws}).Running() {
		return fmt.Errorf("el workspace %q está corriendo", ws)
	}
	if err := os.Remove(workspaceFile(ws)); err != nil {
		return err
	}
	_ = os.Remove(workspaceFile(ws) + ".bak")
	return nil
}
