package main

import (
	"fmt"
	"os"
	"strings"
)

// popupCmd builds a display-popup command line for the client the user is on.
func popupCmd(client, title string, w, h int, inner ...string) []string {
	return []string{"display-popup", "-c", client, "-E", "-w", itoa(w), "-h", itoa(h), "-T", " " + title + " ",
		joinArgs(inner)}
}

// cmdBarClick handles the ✕ buttons of the bar: _barclick <ws> <client> xt:<session>|xw:<window>.
// display-popup does not expand formats, so the bar passes the clicked range to cw, which opens
// the confirmation for exactly that tab or session.
func cmdBarClick(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("uso: _barclick <ws> <cliente> <rango>")
	}
	ws, client, rng := args[0], args[1], args[2]
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	t := Tmux{WS: ws}
	switch {
	case strings.HasPrefix(rng, "xt:"):
		_, err = t.Run(popupCmd(client, "Cerrar pestaña", 66, 13, bin, "_ui", "confirm", ws, "closetab", rng[3:])...)
	case strings.HasPrefix(rng, "xw:"):
		_, err = t.Run(popupCmd(client, "Cerrar sesión", 66, 13, bin, "_ui", "confirm", ws, "close", rng[3:])...)
	}
	return err
}

// cmdConfigMenu shows the settings as a tmux menu with their current values:
// _configmenu <ws> <client>. Every entry either toggles a value (and reopens the menu) or opens
// a small dialog.
func cmdConfigMenu(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("uso: _configmenu <ws> <cliente>")
	}
	ws, client := args[0], args[1]
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	q := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	popup := func(title string, h int, inner ...string) string {
		return fmt.Sprintf(`display-popup -E -w 84 -h %d -T " %s " "%s"`, h, title, q(joinArgs(append([]string{bin}, inner...))))
	}
	toggle := func(what string) string {
		return fmt.Sprintf(`run-shell "%s"`, q(joinArgs([]string{bin, "_conf", ws, what, client})))
	}
	yesNo := func(b bool) string {
		if b {
			return "sí"
		}
		return "no"
	}
	window := map[string]string{windowCurrent: "terminal actual", windowMaximized: "ventana maximizada",
		windowFullscreen: "pantalla completa"}[cfg.Window]
	bar := "arriba"
	if cfg.BarPosition == barBottom {
		bar = "abajo"
	}
	args2 := "ninguno"
	if len(cfg.ClaudeArgs) > 0 {
		args2 = shorten(joinArgs(cfg.ClaudeArgs), 24)
	}
	item := func(label, value, key, cmd string) []string {
		return []string{fmt.Sprintf("  %s  #[fg=colour245]%s", fit(label, 28), q(value)), key, cmd}
	}
	menu := []string{"display-menu", "-c", client, "-O", "-T", "#[align=centre] Configuración · cw " + version + " ", "-x", "C", "-y", "C", "--"}
	menu = append(menu, "-SESIONES", "", "")
	menu = append(menu, item("Mensaje de continuación", "«"+shorten(cfg.ContinueMessage, 20)+"»", "m",
		popup("Mensaje de continuación", 13, "_ui", "setconf", ws, "msg"))...)
	menu = append(menu, item("Arranque diferido", yesNo(cfg.LazyStart), "l", toggle("lazy"))...)
	menu = append(menu, item("Argumentos para todo claude", args2, "g",
		popup("Argumentos para claude", 13, "_ui", "setconf", ws, "args"))...)
	menu = append(menu, item("Alias de claude", itoa(len(cfg.claudeAliases()))+" · por defecto "+cfg.defaultAlias(), "a",
		fmt.Sprintf(`display-popup -E -w 90 -h 30 -T " Alias de claude " "%s"`, q(joinArgs([]string{bin, "_ui", "aliases", ws}))))...)
	menu = append(menu, "-APARIENCIA", "", "")
	menu = append(menu, item("Tema de color", themeLabel(cfg.Theme), "t", toggle("theme"))...)
	menu = append(menu, item("Barra de pestañas", bar, "b", toggle("bar"))...)
	menu = append(menu, item("Al iniciar, abrir en", window, "v", toggle("window"))...)
	menu = append(menu, "-GUARDADO", "", "")
	menu = append(menu, item("Autoguardado cada", itoa(cfg.AutosaveSeconds)+" s", "s",
		popup("Autoguardado", 13, "_ui", "setconf", ws, "autosave"))...)
	menu = append(menu, "", "", "")
	menu = append(menu, "  Archivo: "+q(shortPath(configFile())), "", "")
	_, err = Tmux{WS: ws}.Run(menu...)
	return err
}

// cmdConf flips a setting from the configuration menu and reopens it: _conf <ws> <what> <client>.
func cmdConf(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("uso: _conf <ws> lazy|bar|window <cliente>")
	}
	ws, what, client := args[0], args[1], args[2]
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	switch what {
	case "lazy":
		cfg.LazyStart = !cfg.LazyStart
	case "bar":
		cfg.BarPosition = map[bool]string{true: barTop, false: barBottom}[cfg.BarPosition == barBottom]
	case "theme":
		cfg.Theme = nextTheme(cfg.Theme)
		applyTheme(cfg.Theme)
	case "window":
		cfg.Window = map[string]string{windowCurrent: windowMaximized, windowMaximized: windowFullscreen,
			windowFullscreen: windowCurrent}[cfg.Window]
	default:
		return fmt.Errorf("opción desconocida %q", what)
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if what == "bar" || what == "theme" {
		if err := reloadConf(ws, cfg); err != nil {
			return err
		}
	}
	return cmdConfigMenu([]string{ws, client})
}

// uiSetConf edits one text setting in a popup.
func uiSetConf(what string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	switch what {
	case "msg":
		if v, ok := askText("Mensaje que reciben las sesiones que estaban trabajando", "Mensaje", cfg.ContinueMessage, "vacío = no mandar nada"); ok {
			cfg.ContinueMessage = v
		}
	case "args":
		if v, ok := askText("Argumentos que se agregan a todas las sesiones de claude", "Argumentos", joinArgs(cfg.ClaudeArgs), "ej. --model opus"); ok {
			cfg.ClaudeArgs = splitArgs(v)
		}
	case "autosave":
		if v, ok := askText("Cada cuánto se guarda solo el estado", "Segundos", itoa(cfg.AutosaveSeconds), "mínimo 30"); ok {
			if n := atoiDefault(v, 0); n >= 30 {
				cfg.AutosaveSeconds = n
			}
		}
	default:
		return fmt.Errorf("opción desconocida %q", what)
	}
	return saveConfig(cfg)
}

func atoiDefault(s string, def int) int {
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// uiBulkAlias switches the claude alias of every Claude session in the current tab ("tab") or in
// the whole workspace ("all"), restarting each one in its conversation.
func uiBulkAlias(ws, scope string) error {
	t := Tmux{WS: ws}
	_, session, err := currentClient(t)
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var targets []sessionRow
	for _, r := range listSessionRows(t) {
		if r.slot.Kind == kindClaude && (scope == "all" || r.session == session) {
			targets = append(targets, r)
		}
	}
	where := "todo el workspace " + ws
	if scope == "tab" {
		title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		where = "la pestaña " + title
	}
	if len(targets) == 0 {
		pause("No hay sesiones de Claude en " + where + ".")
		return nil
	}
	aliases := cfg.claudeAliases()
	idx := 0
	for i, a := range aliases {
		if a.Name == cfg.defaultAlias() {
			idx = i
		}
	}
	sendContinue := true
	var hits []hit
	draw := func() {
		var s screen
		s.at(2, 4, sgrBold+"Cambiar el alias de claude de "+itoa(len(targets))+" sesiones"+sgrReset)
		s.at(3, 4, sgrMuted+"en "+where+sgrReset)
		a := aliases[idx]
		s.at(5, 4, "Claude")
		s.at(5, 17, sgrSel+" ‹ "+fit(a.Name, 24)+" › "+sgrReset)
		s.zone(5, 17, 3, "prev")
		s.zone(5, 20, 24, "next")
		s.zone(5, 44, 3, "next")
		s.at(6, 17, sgrDim+" "+fit(a.summary(), 44)+sgrReset)
		box := "[ ]"
		if sendContinue {
			box = "[x]"
		}
		s.btn(8, 4, sgrBusy+" "+box+" Al reiniciar, mandar «"+shorten(cfg.ContinueMessage, 20)+"» "+sgrReset, "check")
		var names []string
		for _, r := range targets {
			names = append(names, r.slot.Alias)
		}
		s.at(10, 4, sgrDim+fit("Se reinician: "+strings.Join(names, ", "), 60)+sgrReset)
		s.place(12, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"},
			btnDef{label: "Cambiar " + itoa(len(targets)) + " sesiones", id: "ok", primary: true})})
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
			case id == "prev" || k.kind == keyLeft:
				idx = (idx - 1 + len(aliases)) % len(aliases)
			case id == "next" || k.kind == keyRight:
				idx = (idx + 1) % len(aliases)
			case id == "check" || (k.kind == keyRune && k.r == ' '):
				sendContinue = !sendContinue
			case id == "ok" || k.kind == keyEnter:
				setRaw(false)
				name := aliases[idx].Name
				for _, r := range targets {
					if cfg.claudeAlias(r.slot.ClaudeAlias).Name == name {
						continue
					}
					if _, err := t.Run("set", "-p", "-t", r.paneID, "@cw_claude_alias", name); err != nil {
						return err
					}
					if err := respawnSlot(ws, r.paneID, sendContinue); err != nil {
						return err
					}
				}
				return nil
			}
		}
		draw()
	}
}
