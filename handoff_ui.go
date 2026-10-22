package main

import "strings"

// The destination must be selected explicitly, even when only one alias is available.
func pickHandoffAlias(cfg Config, agent string) (string, bool) {
	var choices []ClaudeAlias
	for _, a := range cfg.claudeAliases() {
		if a.isCodex() == (agent == "codex") {
			choices = append(choices, a)
		}
	}
	selected := -1
	focusButton := false
	setRaw(true)
	defer func() { setRaw(false); stdoutWrite("\033[?25h") }()
	for {
		var s screen
		s.at(1, 4, sgrBold+"Retomar con "+agent+sgrReset)
		s.at(3, 4, "Elige el alias de destino (reemplaza esta misma sesión)")
		row := 5
		for i, a := range choices {
			style := sgrField
			if i == selected {
				style = sgrSel
			}
			s.btn(row, 4, style+" "+fit(a.Name, 24)+" "+sgrReset, "alias:"+itoa(i))
			s.at(row, 32, sgrDim+fit(a.summary(), 38)+sgrReset)
			row++
		}
		if len(choices) == 0 {
			s.at(row, 4, "Configura un alias para este agente en Configuración.")
			row++
		}
		s.place(row+2, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"}, btnDef{label: "Retomar", id: "ok", primary: true, focused: focusButton})})
		s.closeX("cancel")
		s.flush(true)
		keys, err := readKeys()
		if err != nil {
			return "", false
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(s.hits, k.x, k.y)
			}
			switch {
			case id == "cancel" || k.kind == keyEsc:
				return "", false
			case strings.HasPrefix(id, "alias:"):
				selected = atoiDefault(strings.TrimPrefix(id, "alias:"), -1)
				focusButton = false
			case (id == "ok" || (k.kind == keyEnter && focusButton)) && selected >= 0 && selected < len(choices):
				return choices[selected].Name, true
			case (k.kind == keyDown || k.kind == keyRight) && len(choices) > 0:
				selected = (selected + 1) % len(choices)
				focusButton = false
			case (k.kind == keyUp || k.kind == keyLeft) && len(choices) > 0:
				if selected <= 0 {
					selected = len(choices) - 1
				} else {
					selected--
				}
				focusButton = false
			case (k.kind == keyTab || k.kind == keyEnter) && selected >= 0:
				focusButton = true
			}
		}
	}
}
