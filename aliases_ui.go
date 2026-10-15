package main

import (
	"strconv"
	"strings"
)

// textForm is a plain multi-field text dialog (Tab moves, Enter on the last field accepts).
type textForm struct {
	heading string
	labels  []string
	values  [][]rune
	hints   []string
	focus   int
	errMsg  string
	hits    []hit
	check   func(values []string) string // returns an error message, "" when valid
}

func (f *textForm) draw() {
	var s screen
	s.at(1, 4, sgrMuted+f.heading+sgrReset)
	for i, label := range f.labels {
		row := 3 + i*3
		style := sgrField
		if i == f.focus {
			style = sgrFocus
		}
		v := string(f.values[i])
		if r := []rune(v); len(r) > fieldWidth-2 {
			v = "…" + string(r[len(r)-(fieldWidth-3):])
		}
		s.at(row, 4, label)
		s.at(row, 17, style+" "+fit(v, fieldWidth-1)+sgrReset)
		if i < len(f.hints) && f.hints[i] != "" {
			s.at(row+1, 17, sgrDim+" "+f.hints[i]+sgrReset)
		}
	}
	bottom := 3 + len(f.labels)*3
	if f.errMsg != "" {
		s.at(bottom, 4, sgrRed+f.errMsg+sgrReset)
	}
	for i := range f.labels {
		s.zone(3+i*3, 4, fieldWidth+13, "f:"+itoa(i))
	}
	s.place(bottom+1, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"}, btnDef{label: "Guardar", id: "save", primary: true})})
	s.closeX("cancel")
	f.hits = s.hits
	s.at(3+f.focus*3, 18+min(len(f.values[f.focus]), fieldWidth-2), "\033[?25h")
	s.flush(true)
}

func (f *textForm) run() bool {
	setRaw(true)
	defer setRaw(false)
	f.draw()
	for {
		keys, err := readKeys()
		if err != nil {
			return false
		}
		for _, k := range keys {
			f.errMsg = ""
			v := &f.values[f.focus]
			if k.kind == keyClick {
				switch id := hitAt(f.hits, k.x, k.y); {
				case id == "cancel":
					return false
				case id == "save":
					k = key{kind: keyEnter}
				case strings.HasPrefix(id, "f:"):
					f.focus = int(id[2] - '0')
					continue
				default:
					continue
				}
			}
			switch k.kind {
			case keyEsc:
				return false
			case keyTab, keyDown:
				f.focus = (f.focus + 1) % len(f.labels)
			case keyBackTab, keyUp:
				f.focus = (f.focus - 1 + len(f.labels)) % len(f.labels)
			case keyEnter:
				vals := make([]string, len(f.values))
				for i, x := range f.values {
					vals[i] = strings.TrimSpace(string(x))
				}
				if msg := f.check(vals); msg != "" {
					f.errMsg = msg
				} else {
					return true
				}
			case keyBackspace:
				if len(*v) > 0 {
					*v = (*v)[:len(*v)-1]
				}
			case keyCtrlU:
				*v = nil
			case keyRune:
				*v = append(*v, k.r)
			}
		}
		f.draw()
	}
}

// uiClaudeAliases manages the claude aliases: add, edit, delete, set the default, import.
func uiClaudeAliases() error {
	sel := 0
	msg := ""
	confirmDelete := false
	for {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		list := cfg.claudeAliases()
		sel = min(sel, len(list)-1)
		_, h := termSize()
		visible := max(h-8, 3)
		offset := 0
		if sel >= visible {
			offset = sel - visible + 1
		}
		var s screen
		s.at(1, 4, sgrMuted+"ALIAS DE CLAUDE"+sgrReset+sgrDim+"   ★ = el que se propone al crear una sesión"+sgrReset)
		for i, a := range list {
			if i < offset || i >= offset+visible {
				continue
			}
			star := "  "
			if a.Name == cfg.defaultAlias() {
				star = "★ "
			}
			line := " " + star + fit(a.Name, 20) + " " + fit(a.summary(), 56) + " "
			if i == sel {
				line = sgrSel + line + sgrReset
			}
			s.btn(3+i-offset, 3, line, "row:"+itoa(i))
		}
		foot := 4 + min(len(list), visible)
		switch {
		case confirmDelete:
			s.at(foot, 4, sgrRed+"¿Borrar el alias "+list[sel].Name+"?"+sgrReset)
			s.place(foot+1, 4, []lineItem{buttonLine(0, btnDef{label: "Borrar", id: "yes", style: sgrDanger}, btnDef{label: "Cancelar", id: "no"})})
		default:
			if msg != "" {
				s.at(foot, 4, sgrMuted+msg+sgrReset)
			}
			s.place(foot+1, 4, []lineItem{buttonLine(0,
				btnDef{label: "+ Agregar", id: "add", primary: true},
				btnDef{label: "Editar", id: "edit"},
				btnDef{label: "Borrar", id: "del"},
				btnDef{label: "★ Por defecto", id: "default"},
				btnDef{label: "Importar del shell", id: "import"})})
		}
		s.closeX("back")
		s.b.WriteString("\033[?25l")
		hits := s.hits
		s.flush(true)

		setRaw(true)
		keys, err := readKeys()
		setRaw(false)
		if err != nil {
			return nil
		}
		msg = ""
		for _, k := range keys {
			// clicks become the equivalent key presses
			if k.kind == keyClick {
				id := hitAt(hits, k.x, k.y)
				switch {
				case strings.HasPrefix(id, "row:"):
					sel, _ = strconv.Atoi(id[4:])
					continue
				case id == "":
					continue
				}
				k = key{kind: keyRune, r: map[string]rune{"yes": 's', "no": 'n', "add": 'a', "del": 'x',
					"default": 'd', "import": 'i', "back": 'q'}[id]}
				if id == "edit" {
					k = key{kind: keyEnter}
				}
			}
			if confirmDelete {
				confirmDelete = false
				if k.kind == keyRune && (k.r == 's' || k.r == 'y') {
					name := list[sel].Name
					var kept []ClaudeAlias
					for _, a := range cfg.ClaudeAliases {
						if a.Name != name {
							kept = append(kept, a)
						}
					}
					cfg.ClaudeAliases = kept
					if cfg.DefaultClaudeAlias == name {
						cfg.DefaultClaudeAlias = ""
					}
					if err := saveConfig(cfg); err != nil {
						return err
					}
					msg = "Borrado " + name + ". Las sesiones que lo usaban arrancarán con " + builtinAlias + "."
				}
				continue
			}
			switch {
			case k.kind == keyEsc || (k.kind == keyRune && k.r == 'q'):
				return nil
			case k.kind == keyUp:
				sel = (sel - 1 + len(list)) % len(list)
			case k.kind == keyDown:
				sel = (sel + 1) % len(list)
			case k.kind == keyRune && k.r == 'a':
				if a, ok := editClaudeAlias(cfg, ClaudeAlias{}, true); ok {
					cfg.ClaudeAliases = append(cfg.ClaudeAliases, a)
					if err := saveConfig(cfg); err != nil {
						return err
					}
					sel = len(cfg.claudeAliases()) - 1
				}
			case k.kind == keyEnter:
				old := list[sel]
				if old.FromShell { // keep it linked to the shell: edit the stored (name-only) entry
					old = ClaudeAlias{Name: old.Name}
				}
				if a, ok := editClaudeAlias(cfg, old, false); ok {
					replaced := false
					for i := range cfg.ClaudeAliases {
						if cfg.ClaudeAliases[i].Name == old.Name {
							cfg.ClaudeAliases[i], replaced = a, true
						}
					}
					if !replaced { // editing the built-in "claude" stores it explicitly
						cfg.ClaudeAliases = append([]ClaudeAlias{a}, cfg.ClaudeAliases...)
					}
					if cfg.DefaultClaudeAlias == old.Name {
						cfg.DefaultClaudeAlias = a.Name
					}
					if err := saveConfig(cfg); err != nil {
						return err
					}
				}
			case k.kind == keyRune && k.r == 'x':
				if list[sel].Name == builtinAlias {
					msg = "El alias " + builtinAlias + " siempre existe; puedes editarlo pero no borrarlo."
				} else {
					confirmDelete = true
				}
			case k.kind == keyRune && k.r == 'd':
				cfg.DefaultClaudeAlias = list[sel].Name
				if err := saveConfig(cfg); err != nil {
					return err
				}
			case k.kind == keyRune && k.r == 'i':
				have := map[string]bool{}
				for _, a := range cfg.claudeAliases() {
					have[a.Name] = true
				}
				added := 0
				for _, a := range importShellAliases() {
					if !have[a.Name] {
						cfg.ClaudeAliases = append(cfg.ClaudeAliases, a)
						added++
					}
				}
				if err := saveConfig(cfg); err != nil {
					return err
				}
				msg = "Importados " + itoa(added) + " alias de tus archivos del shell."
			}
		}
	}
}

func editClaudeAlias(cfg Config, a ClaudeAlias, isNew bool) (ClaudeAlias, bool) {
	heading := "Editar alias de claude " + a.Name
	if isNew {
		heading = "Nuevo alias de claude"
	}
	f := &textForm{
		heading: heading,
		labels:  []string{"Nombre", "Variables", "Argumentos"},
		values:  [][]rune{[]rune(a.Name), []rune(joinArgs(a.Env)), []rune(joinArgs(a.Args))},
		hints: []string{"vacío lo demás = usa el alias del shell con este nombre",
			"ej. CLAUDE_CONFIG_DIR=~/.claude-2 CLAUDE_YOLO=1",
			"ej. --model opus --dangerously-skip-permissions"},
		check: func(v []string) string {
			name := cleanLabel(v[0])
			if name == "" || strings.ContainsAny(name, " /") {
				return "El nombre no puede estar vacío ni tener espacios."
			}
			for _, other := range cfg.claudeAliases() {
				if other.Name == name && (isNew || name != a.Name) {
					return "Ya existe un alias " + name + "."
				}
			}
			for _, kv := range splitArgs(v[1]) {
				if !envWordRe.MatchString(kv) {
					return "Variables: cada una como NOMBRE=valor (" + kv + ")."
				}
			}
			return ""
		},
	}
	if !f.run() {
		return a, false
	}
	out := ClaudeAlias{Name: cleanLabel(string(f.values[0])), Args: splitArgs(string(f.values[2]))}
	for _, kv := range splitArgs(string(f.values[1])) {
		k, v, _ := strings.Cut(kv, "=")
		out.Env = append(out.Env, k+"="+expandHome(v))
	}
	return out, true
}
