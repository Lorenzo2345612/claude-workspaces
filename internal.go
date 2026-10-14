package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var stdin = bufio.NewReader(os.Stdin)

// prompt reads one line; an empty answer returns def.
func prompt(question, def string) string {
	if def != "" {
		fmt.Printf("%s[%s] ", question, def)
	} else {
		fmt.Print(question)
	}
	line, err := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		return def
	}
	if line == "" {
		return def
	}
	return line
}

// cleanLabel keeps titles and aliases safe for the tmux field separator and the status bar.
func cleanLabel(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ", sep, " ", "#", "", "'", "", `"`, "").Replace(s)
	return strings.TrimSpace(s)
}

func pause(msg string) {
	fmt.Println(msg)
	fmt.Print("Enter para cerrar…")
	_, _ = stdin.ReadString('\n')
}

func tabDir(t Tmux, session string) string {
	dir, _ := t.Run("show", "-t", session, "-qv", "@cw_dir")
	if dir == "" || !isDir(dir) {
		return homeDir()
	}
	return dir
}

// addSlot opens a new window in an existing tab, in the tab's base directory, and focuses it.
func addSlot(ws, session, kind, alias string) error {
	return openInTab(ws, session, Slot{Kind: kind, Alias: alias, Cwd: tabDir(Tmux{WS: ws}, session)}, false)
}

// openInTab opens a slot in an existing tab and focuses it. resuming=true reopens the slot's
// conversation (never lazily, never with the continue message: the user asked for it now).
func openInTab(ws, session string, s Slot, resuming bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.LazyStart = false
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	t := Tmux{WS: ws}
	s.Status = statusIdle
	if s.Kind == kindClaude {
		title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		recordAlias(s.Alias, title)
	}
	id, err := l.openSlot(t, session, false, &s, resuming)
	if err != nil {
		return err
	}
	_, err = t.Run("select-window", "-t", id)
	closeHome(t, session)
	return err
}

// cmdNew: _new shell <ws> <session> | _new claude <ws> <session> <alias>
func cmdNew(args []string) error {
	if len(args) < 3 {
		return errors.New("uso: _new shell|claude <ws> <sesión> [alias]")
	}
	kind, ws, session := args[0], args[1], args[2]
	switch kind {
	case kindShell:
		return addSlot(ws, session, kindShell, "shell")
	case kindClaude:
		if len(args) != 4 || cleanLabel(args[3]) == "" {
			return errors.New("falta el alias")
		}
		return addSlot(ws, session, kindClaude, cleanLabel(args[3]))
	}
	return fmt.Errorf("tipo desconocido %q", kind)
}

// cmdTitle renames a tab: _title <ws> <session> <title>.
func cmdTitle(args []string) error {
	if len(args) != 3 || cleanLabel(args[2]) == "" {
		return errors.New("uso: _title <ws> <sesión> <nombre>")
	}
	t := Tmux{WS: args[0]}
	return t.setTabOptions(args[1], Tab{Title: cleanLabel(args[2]), Dir: tabDir(t, args[1])})
}

// cmdUI runs the interactive dialogs shown in tmux popups.
func cmdUI(args []string) error {
	if len(args) < 2 {
		return errors.New("uso: _ui newtab|newclaude|config <ws> …")
	}
	var err error
	switch args[0] {
	case "newtab":
		err = uiNewTab(args[1])
	case "newclaude":
		if err = uiNewClaude(args[1]); err == errCancelled {
			err = nil
		}
	case "config":
		err = uiConfig(args[1])
	case "help":
		uiHelp()
	case "sessions":
		err = uiSessions(args[1])
	case "confirm":
		if len(args) != 3 {
			return errors.New("uso: _ui confirm <ws> stop|close")
		}
		err = uiConfirm(args[1], args[2])
	case "editcurrent":
		err = uiEditCurrent(args[1])
	default:
		err = fmt.Errorf("diálogo desconocido %q", args[0])
	}
	if err != nil {
		pause("Error: " + err.Error())
	}
	return nil
}

// currentClient returns the client the user is on and its tab. display-popup does not expand
// formats in its command, so the dialogs ask tmux instead of receiving them as arguments.
func currentClient(t Tmux) (client, session string, err error) {
	out, err := t.Run("display", "-p", "#{client_name}"+sep+"#{client_session}")
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(out, sep)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", errors.New("no encuentro la terminal conectada al workspace")
	}
	return parts[0], parts[1], nil
}

// uiNewTab asks for a new tab. The directory defaults to where cw was opened (@cw_launch_dir).
func uiNewTab(ws string) error {
	client, _, err := currentClient(Tmux{WS: ws})
	if err != nil {
		return err
	}
	curDir, _ := Tmux{WS: ws}.Run("show", "-gqv", "@cw_launch_dir")
	if curDir == "" || !isDir(curDir) {
		curDir = homeDir()
	}
	t := Tmux{WS: ws}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	f := &newTabForm{dir: []rune(shortPath(curDir)), recents: recentDirs(t), work: workDirs(), claudeAliases: cfg.claudeAliases()}
	for i, a := range f.claudeAliases {
		if a.Name == cfg.defaultAlias() {
			f.aliasIdx = i
		}
	}
	if !f.run() {
		return nil
	}
	title := cleanLabel(string(f.name))
	dir, _ := expandPath(string(f.dir))
	var slots []Slot
	switch f.start {
	case startClaude:
		alias := cleanLabel(string(f.alias))
		if alias == "" {
			alias = title
		}
		slots = []Slot{{Kind: kindClaude, Alias: alias, Cwd: dir, ClaudeAlias: f.claudeAliases[f.aliasIdx].Name}}
		recordAlias(alias, title)
	case startShell:
		slots = []Slot{{Kind: kindShell, Alias: "shell", Cwd: dir}}
	}

	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	session := t.nextTabSession()
	tab := Tab{Title: title, Dir: dir, Slots: slots}
	if err := l.openTab(t, session, &tab, false); err != nil {
		return err
	}
	_, err = t.Run("switch-client", "-c", client, "-t", session)
	return err
}

func uiNewClaude(ws string) error {
	_, session, err := currentClient(Tmux{WS: ws})
	if err != nil {
		return err
	}
	return newClaudeDialog(ws, session, nil)
}

// newClaudeDialog asks for a new Claude session in a tab (name, claude alias, arguments) and
// opens it. With resume set, it reopens that conversation instead of starting a new one.
func newClaudeDialog(ws, session string, resume *recentConv) error {
	t := Tmux{WS: ws}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
	heading := "Nueva sesión en " + title + " · " + shortPath(tabDir(t, session))
	sid, resuming := "", false
	f := &sessionForm{history: aliasHistory(), globalArgs: cfg.ClaudeArgs, claudeAliases: cfg.claudeAliases()}
	f.selectAlias(cfg.defaultAlias())
	if resume != nil {
		sid, resuming = resume.ID, true
		heading = "Retomar «" + fit(resume.First, 30) + "» en " + title
		f.alias = []rune(resume.Title)
		f.selectAlias(resume.Alias)
	}
	f.heading = heading
	f.preview = func(name string, args []string, alias string) string {
		return l.commandPreview(Slot{Kind: kindClaude, Alias: name, Args: args, ClaudeAlias: alias, SessionID: sid}, resuming)
	}
	if f.run() == actCancel {
		return errCancelled
	}
	return openInTab(ws, session, Slot{Kind: kindClaude, Alias: cleanLabel(string(f.alias)), Cwd: tabDir(t, session),
		Args: splitArgs(string(f.args)), ClaudeAlias: f.selectedAlias(), SessionID: sid}, resuming)
}

var errCancelled = errors.New("cancelado")

func uiConfig(ws string) error {
	for {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		lazy := "no (todas arrancan al iniciar)"
		if cfg.LazyStart {
			lazy = "sí (las ociosas esperan a que pulses Enter)"
		}
		fmt.Print("\033[H\033[2J")
		fmt.Printf("  1) Mensaje de continuación: %q\n", cfg.ContinueMessage)
		fmt.Printf("  2) Arranque diferido: %s\n", lazy)
		fmt.Printf("  3) Argumentos extra para claude: %s\n", strings.Join(cfg.ClaudeArgs, " "))
		fmt.Printf("  4) Autoguardado cada: %d s\n", cfg.AutosaveSeconds)
		fmt.Printf("  5) Ventana al iniciar: %s (maximized, fullscreen o current)\n", cfg.Window)
		bar := "arriba"
		if cfg.BarPosition == barBottom {
			bar = "abajo"
		}
		fmt.Printf("  6) Barra de pestañas: %s\n", bar)
		fmt.Printf("  7) Alias de claude: %d registrados, por defecto %s\n", len(cfg.claudeAliases()), cfg.defaultAlias())
		fmt.Printf("\n  Archivo: %s\n\n", shortPath(configFile()))
		switch prompt("Opción (Enter para salir): ", "") {
		case "1":
			cfg.ContinueMessage = prompt("Nuevo mensaje: ", cfg.ContinueMessage)
		case "2":
			cfg.LazyStart = !cfg.LazyStart
		case "3":
			cfg.ClaudeArgs = strings.Fields(prompt("Argumentos (separados por espacio, '-' para ninguno): ", strings.Join(cfg.ClaudeArgs, " ")))
			if len(cfg.ClaudeArgs) == 1 && cfg.ClaudeArgs[0] == "-" {
				cfg.ClaudeArgs = []string{}
			}
		case "4":
			if n, err := strconv.Atoi(prompt("Segundos: ", strconv.Itoa(cfg.AutosaveSeconds))); err == nil && n >= 30 {
				cfg.AutosaveSeconds = n
			}
		case "5":
			switch prompt("Ventana (maximized/fullscreen/current): ", cfg.Window) {
			case windowMaximized:
				cfg.Window = windowMaximized
			case windowFullscreen:
				cfg.Window = windowFullscreen
			case windowCurrent:
				cfg.Window = windowCurrent
			}
		case "7":
			if err := uiClaudeAliases(); err != nil {
				return err
			}
			continue
		case "6":
			if cfg.BarPosition == barBottom {
				cfg.BarPosition = barTop
			} else {
				cfg.BarPosition = barBottom
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			if err := reloadConf(ws, cfg); err != nil {
				return err
			}
			continue
		case "":
			return nil
		default:
			continue
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}
}

// hookInput is the subset of Claude Code's hook payload cw uses.
type hookInput struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
}

// cmdHook records a Claude session's current ID and activity on its tmux window.
// It must print nothing: SessionStart and UserPromptSubmit stdout would reach Claude's context.
func cmdHook() error {
	ws, pane := os.Getenv("CW_WS"), os.Getenv("TMUX_PANE")
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if ws == "" || pane == "" || validWorkspaceName(ws) != nil {
		return nil
	}
	var in hookInput
	if json.Unmarshal(data, &in) != nil {
		return nil
	}
	status := map[string]string{
		"SessionStart":     statusIdle,
		"UserPromptSubmit": statusBusy,
		"PostToolUse":      statusBusy,
		"Notification":     statusWaiting,
		"Stop":             statusIdle,
	}[in.HookEventName]
	if status == "" {
		return nil
	}
	args := []string{"set", "-w", "-t", pane, "@cw_status", status}
	if in.HookEventName == "SessionStart" && in.SessionID != "" {
		args = append(args, ";", "set", "-w", "-t", pane, "@cw_sid", in.SessionID)
	}
	t := Tmux{WS: ws}
	_, _ = t.Run(args...)
	if session, err := t.Run("display", "-p", "-t", pane, "#{session_name}"); err == nil {
		refreshTabLabel(t, session)
	}
	return nil
}

// cmdWait is the lazy-start placeholder: it waits for Enter, then becomes the Claude process.
func cmdWait(args []string) error {
	i := 0
	for i < len(args) && args[i] != "--" {
		i++
	}
	if i < 2 || i+1 >= len(args) {
		return errors.New("uso: _wait <ws> <alias> -- <comando…>")
	}
	ws, alias, argv := args[0], args[1], args[i+1:]
	fmt.Printf("\n  z  %s está dormida para ahorrar recursos.\n\n  Pulsa Enter para retomarla.\n", alias)
	_, _ = stdin.ReadString('\n')
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		_, _ = Tmux{WS: ws}.Run("set", "-w", "-t", pane, "@cw_status", statusIdle)
	}
	return syscall.Exec(argv[0], argv, os.Environ())
}

// cmdRespawn reopens a session whose process exited with an error: _respawn <ws> <pane>.
// A Claude session resumes its conversation when one exists, so a retry never collides with
// the session ID it was first launched with.
func cmdRespawn(args []string) error {
	if len(args) != 2 {
		return errors.New("uso: _respawn <ws> <panel>")
	}
	return respawnSlot(args[0], args[1], false)
}

// respawnSlot restarts the process of a pane. With sendContinue, a Claude session that has a
// conversation resumes it and gets the configured continue message.
func respawnSlot(ws, pane string, sendContinue bool) error {
	t := Tmux{WS: ws}
	out, err := t.Run("display", "-p", "-t", pane, strings.Join([]string{
		"#{@cw_slot}", "#{@cw_kind}", "#{@cw_alias}", "#{@cw_cwd}", "#{@cw_sid}", "#{@cw_args}", "#{@cw_claude_alias}"}, sep))
	if err != nil {
		return err
	}
	f := strings.Split(out, sep)
	if len(f) != 7 {
		return errors.New("panel sin datos de cw")
	}
	status := statusIdle
	if sendContinue {
		status = statusBusy // makes claudeArgv append the continue message
	}
	s := Slot{ID: f[0], Kind: f[1], Alias: f[2], Cwd: f[3], SessionID: f[4], Status: status,
		Args: argsFromOption(f[5]), ClaudeAlias: f[6]}
	if !isDir(s.Cwd) {
		s.Cwd = homeDir()
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.LazyStart = false
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	var argv, env []string
	display := ""
	switch s.Kind {
	case kindClaude:
		argv, display = l.claudeArgv(&s, true)
		env = cfg.claudeAlias(s.ClaudeAlias).launchEnv()
	case kindHome:
		argv = []string{l.Bin, "_home", ws}
	}
	cmd := []string{"respawn-pane", "-k", "-t", pane, "-c", s.Cwd, "-e", "CW_WS=" + ws, "-e", "CW_SLOT=" + s.ID}
	for _, kv := range env {
		cmd = append(cmd, "-e", kv)
	}
	cmd = append(cmd, argv...)
	if _, err := t.Run(cmd...); err != nil {
		return err
	}
	_, err = t.Run("set", "-w", "-t", pane, "@cw_sid", s.SessionID, ";", "set", "-w", "-t", pane, "@cw_status", display,
		";", "set", "-w", "-t", pane, "@cw_claude_alias", s.ClaudeAlias)
	return err
}

// uiHelp shows the shortcuts and the meaning of each status, and closes on any key.
func uiHelp() {
	h := func(k string) string { return sgrBold + fit(k, 9) + sgrReset }
	lines := []string{
		"",
		"  " + sgrMuted + fit("MOVERSE", 34) + "CREAR" + sgrReset,
		"  " + h("Alt+← →") + fit("sesión anterior / sig.", 25) + h("Alt+t") + "pestaña",
		"  " + h("Alt+↑ ↓") + fit("pestaña anterior / sig.", 25) + h("Alt+n") + "sesión",
		"  " + h("Alt+1..9") + fit("ir a la sesión N", 25) + h("Alt+m") + "menú",
		"  " + fit("", 34) + h("Alt+e") + "todas las sesiones",
		"  " + fit("", 34) + h("Alt+p") + "cambiar alias de claude",
		"  " + sgrMuted + fit("clic", 9) + sgrReset + "en una pestaña o una sesión",
		"",
		"  " + sgrMuted + "ESTADOS" + sgrReset,
		"  " + sgrBusy + "●" + sgrReset + " trabajando   " + sgrRed + "?" + sgrReset + " pide permiso   " +
			sgrGreen + "○" + sgrReset + " lista   " + sgrMuted + "z" + sgrReset + " dormida",
		"  " + sgrBusy + "●2" + sgrReset + " / " + sgrRed + "?1" + sgrReset + " en una pestaña: cuántas sesiones están así",
		"",
		"  " + sgrMuted + "Una sesión que termina con error: Enter la reabre." + sgrReset,
		"  " + sgrMuted + "Rueda del ratón: desplaza; q o Esc vuelve." + sgrReset,
		"  " + sgrMuted + "Prefijo de tmux: Alt+Espacio." + sgrReset,
		"",
		"  " + sgrMuted + "Cualquier tecla para cerrar" + sgrReset,
	}
	stdoutWrite("\033[?25l" + strings.Join(lines, "\r\n"))
	setRaw(true)
	_, _ = readKeys()
	setRaw(false)
}

// reloadConf regenerates the workspace's tmux configuration and applies it to the running server.
func reloadConf(ws string, cfg Config) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(tmuxConfFile(ws), []byte(tmuxConf(ws, bin, cfg.BarPosition != barBottom))); err != nil {
		return err
	}
	_, err = Tmux{WS: ws}.Run("source-file", tmuxConfFile(ws))
	return err
}

// uiConfirm is a floating confirmation for destructive actions: _ui confirm <ws> stop|close.
func uiConfirm(ws, what string) error {
	t := Tmux{WS: ws}
	var question, detail, yes string
	var target string
	switch what {
	case "stop":
		question = "¿Apagar el workspace " + ws + "?"
		detail = "Se guarda el estado y se cierran todas las sesiones. Lo retomas con: cw start " + ws
		yes = "Apagar y guardar"
	case "closetab":
		client, session, err := currentClient(t)
		if err != nil {
			return err
		}
		target = client + sep + session
		title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		n, _ := t.Run("display", "-p", "-t", session, "#{session_windows}")
		question = "¿Cerrar la pestaña " + title + "?"
		detail = "Se cierran sus " + n + " sesiones. Las conversaciones de Claude quedan en disco y puedes retomarlas desde una pestaña vacía."
		yes = "Cerrar pestaña"
	case "close":
		client, _, err := currentClient(t)
		if err != nil {
			return err
		}
		out, err := t.Run("display", "-p", "-c", client, "#{window_id}"+sep+"#{window_name}"+sep+"#{@cw_kind}")
		if err != nil {
			return err
		}
		f := strings.Split(out, sep)
		target = f[0]
		question = "¿Cerrar la sesión " + f[1] + "?"
		detail = "La terminal se cierra y la sesión deja de guardarse en el workspace."
		if len(f) > 2 && f[2] == kindClaude {
			detail = "Claude se cierra; la conversación queda en disco y puedes retomarla desde una pestaña vacía."
		}
		yes = "Cerrar"
	default:
		return fmt.Errorf("confirmación desconocida %q", what)
	}
	focusYes := false
	draw := func() {
		var s screen
		s.at(2, 4, sgrBold+question+sgrReset)
		for i, line := range wrapWords(detail, 58, 3) {
			s.at(4+i, 4, sgrMuted+line+sgrReset)
		}
		no, ok := sgrField+" Cancelar "+sgrReset, sgrField+" "+yes+" "+sgrReset
		if focusYes {
			ok = "\033[48;5;160m\033[38;5;255m\033[1m " + yes + " " + sgrReset
		} else {
			no = sgrSel + " Cancelar " + sgrReset
		}
		s.at(8, 4, no+"   "+ok)
		s.at(10, 4, sgrDim+"←→ elegir · Enter confirmar · Esc cancelar"+sgrReset)
		s.b.WriteString("\033[?25l")
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
			switch {
			case k.kind == keyEsc || (k.kind == keyRune && (k.r == 'n' || k.r == 'q')):
				setRaw(false)
				return nil
			case k.kind == keyLeft || k.kind == keyRight || k.kind == keyTab:
				focusYes = !focusYes
			case (k.kind == keyEnter && focusYes) || (k.kind == keyRune && (k.r == 'y' || k.r == 's')):
				setRaw(false)
				switch what {
				case "close":
					return closeWindow(ws, target)
				case "closetab":
					f := strings.Split(target, sep)
					return closeTab(ws, f[1], f[0])
				}
				// Detach the stop from this popup: stopping kills the server and every popup in it.
				cmd := exec.Command(os.Args[0], "stop", ws)
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				return cmd.Start()
			case k.kind == keyEnter:
				setRaw(false)
				return nil
			}
		}
		draw()
	}
}

// closeWindow closes one session. The last session of a tab leaves the tab on its start screen
// instead of removing the tab with it.
func closeWindow(ws, windowID string) error {
	t := Tmux{WS: ws}
	session, err := t.Run("display", "-p", "-t", windowID, "#{session_name}")
	if err != nil {
		return err
	}
	if n, _ := t.Run("display", "-p", "-t", windowID, "#{session_windows}"); n == "1" {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		home := Slot{ID: newUUID(), Kind: kindHome, Cwd: tabDir(t, session)}
		if _, err := t.createWindow(windowSpec{session: session, name: "inicio", slot: home,
			argv: []string{bin, "_home", ws}}); err != nil {
			return err
		}
	}
	_, err = t.Run("kill-window", "-t", windowID)
	return err
}

// closeTab closes a tab and every session in it. Closing the only tab leaves an empty one, so
// the workspace keeps running (use "Apagar y guardar" to stop it).
func closeTab(ws, session, client string) error {
	t := Tmux{WS: ws}
	if rows, _ := t.Lines("list-sessions", "-F", "#{session_name}"); len(rows) == 1 {
		dir, _ := t.Run("show", "-gqv", "@cw_launch_dir")
		if !isDir(dir) {
			dir = homeDir()
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		l, err := newLauncher(ws, cfg)
		if err != nil {
			return err
		}
		next := t.nextTabSession()
		if err := l.openTab(t, next, &Tab{Title: filepath.Base(dir), Dir: dir}, false); err != nil {
			return err
		}
		if client != "" {
			_, _ = t.Run("switch-client", "-c", client, "-t", next)
		}
	}
	_, err := t.Run("kill-session", "-t", session)
	return err
}
