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
	s.Kind, s.ClaudeAlias, err = cfg.newAgentSlot(s.Kind, s.ClaudeAlias)
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
	saveNow(ws) // before closeHome: it may kill this very process (the start screen of the tab)
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
	case "newclaude", "newcodex":
		agent := strings.TrimPrefix(args[0], "new")
		if err = uiNewAgent(args[1], agent); err == errCancelled {
			err = nil
		}
	case "help":
		uiHelp()
	case "sessions":
		err = uiSessions(args[1])
	case "confirm":
		if len(args) < 3 {
			return errors.New("uso: _ui confirm <ws> stop|close|closetab [destino]")
		}
		explicit := ""
		if len(args) > 3 {
			explicit = args[3]
		}
		err = uiConfirm(args[1], args[2], explicit)
	case "bulkalias":
		if len(args) != 3 {
			return errors.New("uso: _ui bulkalias <ws> tab|view|all")
		}
		err = uiBulkAlias(args[1], args[2])
	case "notifications":
		err = uiPendingNotifications(args[1])
	case "sounds":
		if len(args) != 3 {
			return errors.New("uso: _ui sounds <ws> question|completion")
		}
		err = uiNotificationSounds(args[2])
	case "aliases":
		err = uiClaudeAliases()
	case "resumeid":
		err = uiResumeByID(args[1])
	case "combine":
		err = uiCombine(args[1])
	case "ctx":
		if len(args) != 5 {
			return errors.New("uso: _ui ctx <ws> tab|slot <destino> <archivo>")
		}
		err = uiContext(args[1], args[2], args[3], args[4])
	case "ctxdialog":
		if len(args) != 5 {
			return errors.New("uso: _ui ctxdialog <ws> tab|slot <destino> <acción>")
		}
		err = uiCtxDialog(args[1], args[2], args[3], args[4])
	case "editpane":
		if len(args) != 3 {
			return errors.New("uso: _ui editpane <ws> <panel>")
		}
		err = uiEditPane(args[1], args[2])
	case "renamews":
		err = uiRenameWorkspace(args[1])
	case "update":
		err = uiUpdate(args[1])
	case "uso":
		err = uiUsage(args[1])
	case "recursos":
		err = uiResources(args[1])
	case "wsdir":
		err = uiWorkspaceDir(args[1])
	case "tabdir":
		session := ""
		if len(args) > 2 {
			session = args[2]
		}
		err = uiTabDir(args[1], session)
	case "rename":
		switch {
		case len(args) == 4 && args[2] == "slot":
			err = renameSlot(Tmux{WS: args[1]}, args[3])
		case len(args) == 3:
			err = uiRename(args[1], args[2])
		default:
			return errors.New("uso: _ui rename <ws> tab|slot [panel]")
		}
	case "setconf":
		if len(args) != 3 {
			return errors.New("uso: _ui setconf <ws> msg|args|autosave")
		}
		err = uiSetConf(args[2])
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
	curDir := workspaceDir(Tmux{WS: ws})
	t := Tmux{WS: ws}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	f := &newTabForm{dir: []rune(shortPath(curDir)), recents: recentDirs(t), work: workDirs(), claudeAliases: cfg.claudeAliases()}
	for i, a := range f.claudeAliases {
		if a.Name == cfg.defaultAlias() {
			f.aliasIdx = i
			if a.isCodex() {
				f.start = startCodex
			}
		}
	}
	if !f.run() {
		return nil
	}
	title := cleanLabel(string(f.name))
	dir, _ := expandPath(string(f.dir))
	var slots []Slot
	switch f.start {
	case startClaude, startCodex:
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

func uiNewClaude(ws string) error { return uiNewAgent(ws, "claude") }

func uiNewAgent(ws, agent string) error {
	_, session, err := currentClient(Tmux{WS: ws})
	if err != nil {
		return err
	}
	return newAgentDialog(ws, session, nil, agent)
}

// newClaudeDialog asks for a new Claude session in a tab (name, claude alias, arguments) and
// opens it. With resume set, it reopens that conversation instead of starting a new one.
func newClaudeDialog(ws, session string, resume *recentConv) error {
	return newAgentDialog(ws, session, resume, "")
}

func newAgentDialog(ws, session string, resume *recentConv, agent string) error {
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
	if agent != "" {
		alias := cfg.aliasForAgent(agent, cfg.defaultAlias())
		if alias == "" {
			return fmt.Errorf("configura un alias de %s", agent)
		}
		f.selectAlias(alias)
	}
	if resume != nil {
		sid, resuming = resume.ID, true
		heading = "Retomar «" + fit(resume.First, 30) + "» en " + title
		f.alias = []rune(resume.Title)
		if len(f.alias) == 0 { // unnamed conversation: suggest its first words
			f.alias = []rune(cleanLabel(shorten(strings.Join(strings.Fields(resume.First), "-"), 24)))
		}
		f.selectAlias(resume.Alias)
	}
	f.heading = heading
	f.preview = func(name string, args []string, alias string) string {
		return l.commandPreview(Slot{Kind: kindClaude, Alias: name, Args: args, ClaudeAlias: alias, SessionID: sid}, resuming)
	}
	if f.run() == actCancel {
		return errCancelled
	}
	cwd := tabDir(t, session)
	if resume != nil && resume.Cwd != "" && isDir(resume.Cwd) {
		cwd = resume.Cwd // a conversation brought in by ID reopens where it was started
	}
	s := Slot{Kind: kindClaude, Alias: cleanLabel(string(f.alias)), Cwd: cwd,
		Args: splitArgs(string(f.args)), ClaudeAlias: f.selectedAlias(), SessionID: sid}
	if resume != nil && cfg.claudeAlias(s.ClaudeAlias).isCodex() {
		source := Slot{Kind: kindClaude, Alias: s.Alias, Cwd: cwd, SessionID: sid, ClaudeAlias: resume.Alias}
		next, err := handoffSlot(source, "codex", cfg)
		if err != nil {
			return err
		}
		s, resuming = next, false
	}
	return openInTab(ws, session, s, resuming)
}

var errCancelled = errors.New("cancelado")

// askText edits one value in a small form; ok is false when cancelled.
func askText(heading, label, value, hint string) (string, bool) {
	f := &textForm{heading: heading, labels: []string{label}, values: [][]rune{[]rune(value)}, hints: []string{hint},
		check: func([]string) string { return "" }}
	if !f.run() {
		return "", false
	}
	return strings.TrimSpace(string(f.values[0])), true
}

// hookInput is the subset of Claude Code's hook payload cw uses.
type hookInput struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	// BackgroundTasks (Stop only) lists the shells, subagents, monitors… still in flight: the turn
	// ended but Claude will wake up again when they report back.
	BackgroundTasks []struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	} `json:"background_tasks"`
}

// cmdHook records a Claude session's current ID and activity on its tmux window.
// It must print nothing: SessionStart and UserPromptSubmit stdout would reach Claude's context.
func cmdHook() error {
	ws, pane := os.Getenv("CW_WS"), os.Getenv("TMUX_PANE")
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if ws == "" || validWorkspaceName(ws) != nil {
		return nil
	}
	if slot := os.Getenv("CW_SLOT"); slot != "" {
		rows, err := (Tmux{WS: ws}).Lines("list-panes", "-a", "-F", "#{pane_id}"+sep+"#{@cw_slot}")
		if err != nil {
			return nil
		}
		pane = ""
		for _, r := range rows {
			if len(r) == 2 && r[1] == slot {
				pane = r[0]
				break
			}
		}
	}
	if pane == "" {
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
		// Claude waits for the user: a permission dialog, a question (AskUserQuestion), an MCP
		// server's form. PermissionRequest and PreToolUse fire when the dialog opens; the
		// Notification only after it has been open a few seconds, so it is just a backstop.
		"PermissionRequest":  statusWaiting,
		"PreToolUse":         statusWaiting,
		"Elicitation":        statusWaiting,
		"Notification":       statusWaiting,
		"ElicitationResult":  statusBusy,
		"PostToolUseFailure": statusBusy,
		"Stop":               statusIdle,
		"StopFailure":        statusIdle,
	}[in.HookEventName]
	if status == "" {
		return nil
	}
	if in.HookEventName == "Stop" && len(in.BackgroundTasks) > 0 {
		status = statusBusy
	}
	lock, err := soundHookLock(ws, pane)
	if err != nil {
		return nil
	}
	defer lock.Close()
	t := Tmux{WS: ws}
	previous, err := t.Run("display", "-p", "-t", pane, "#{@cw_status}")
	if err != nil {
		return nil
	}
	args := []string{"set", "-p", "-t", pane, "@cw_status", status}
	if in.SessionID != "" {
		args = append(args, ";", "set", "-p", "-t", pane, "@cw_sid", in.SessionID)
	}
	if in.HookEventName == "SessionStart" && in.SessionID != "" {
		if alias, err := t.Run("display", "-p", "-t", pane, "#{@cw_claude_alias}"); err == nil {
			cfg, _ := loadConfig()
			if !cfg.claudeAlias(alias).isCodex() {
				recordUsageOwner(in.SessionID, cfg.claudeAlias(alias).Name)
			}
		}
	}
	if _, err := t.Run(args...); err != nil {
		return nil
	}
	_ = recordHookNotification(t, pane, previous, in.HookEventName, status)
	if cfg, err := loadConfig(); err == nil {
		if id := soundForHook(cfg, previous, in.HookEventName, status); id != "" {
			_ = playNotification(id, cfg.SoundVolume)
		}
	}
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
	fmt.Printf("\n  z  %s está suspendida: no corre Claude, así que no consume recursos.\n\n  Pulsa Enter para reanudarla en su conversación.\n", alias)
	_, _ = stdin.ReadString('\n')
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		t := Tmux{WS: ws}
		_, _ = t.Run("set", "-p", "-t", pane, "@cw_status", statusIdle, ";", "set", "-p", "-t", pane, "@cw_suspended", "")
		if session, err := t.Run("display", "-p", "-t", pane, "#{session_name}"); err == nil {
			refreshTabLabel(t, session)
		}
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
	return respawnSlotMode(ws, pane, sendContinue, false)
}

// respawnSlotMode restarts a pane's process; with suspend, a Claude session restarts as the
// placeholder that waits for Enter (no Claude process until then).
func respawnSlotMode(ws, pane string, sendContinue, suspend bool) error {
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
	// The continue message goes only to a session that was working (or waiting for a permission)
	// when it is restarted, as on a workspace restore; an idle one just reopens.
	status := statusIdle
	if cur, _ := t.Run("display", "-p", "-t", pane, "#{@cw_status}"); sendContinue && (cur == statusBusy || cur == statusWaiting) {
		status = statusBusy // makes claudeArgv append the continue message
	}
	s := Slot{ID: f[0], Kind: f[1], Alias: f[2], Cwd: f[3], SessionID: f[4], Status: status,
		Args: argsFromOption(f[5]), ClaudeAlias: f[6], Suspended: suspend}
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
	_, err = t.Run("set", "-p", "-t", pane, "@cw_sid", s.SessionID, ";", "set", "-p", "-t", pane, "@cw_status", display,
		";", "set", "-p", "-t", pane, "@cw_claude_alias", s.ClaudeAlias, ";", "set", "-p", "-t", pane, "@cw_suspended", boolOpt(suspend))
	return err
}

// uiHelp shows the shortcuts and the meaning of each status, and closes on any key.
func uiHelp() {
	h := func(k string) string { return sgrBold + fit(k, 9) + sgrReset }
	lines := []string{
		"  " + sgrAccent + sgrBold + "cw" + sgrReset + " " + sgrDim + version + sgrReset,
		"  " + sgrMuted + fit("MOVERSE", 34) + "CREAR" + sgrReset,
		"  " + h("Alt+← →") + fit("sesión anterior / sig.", 25) + h("Alt+t") + "pestaña",
		"  " + h("Alt+↑ ↓") + fit("pestaña anterior / sig.", 25) + h("Alt+n") + "sesión",
		"  " + h("Alt+1..9") + fit("ir a la sesión N", 25) + h("Alt+m") + "menú",
		"  " + fit("", 34) + h("Alt+e") + "todas las sesiones",
		"  " + fit("", 34) + h("Alt+p") + "cambiar alias de claude",
		"  " + sgrMuted + fit("clic", 9) + sgrReset + "en una pestaña o una sesión · clic derecho: menú",
		"  " + sgrMuted + "REORDENAR" + sgrReset + "   arrastra pestañas y sesiones en la barra",
		"  " + sgrMuted + "CERRAR" + sgrReset + "      ✕ en cada pestaña o sesión · ⏻ arriba a la derecha apaga todo",
		"  " + sgrMuted + "RETOMAR" + sgrReset + "      Alt+n → Retomar Claude… elige una conversación anterior",
		"  " + sgrMuted + "VER JUNTAS" + sgrReset + "  clic derecho en una sesión → Ver junto a…, o Alt+m → Combinar",
		"  " + h("Alt+⇧←→") + fit("mover la sesión", 25) + h("Alt+< >") + "mover la pestaña",
		"  " + sgrMuted + "NAVEGADOR" + sgrReset + "   Ctrl+T nueva · Ctrl+W cierra · Ctrl+⇧T reabre · Ctrl+PgUp/PgDn",
		"  " + sgrMuted + "SUSPENDER" + sgrReset + "   clic derecho → Suspender (no corre Claude) · Enter reanuda",
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
	}
	stdoutWrite("\033[?25l" + strings.Join(lines, "\r\n"))
	var sc screen
	sc.closeX("close")
	sc.flush(false)
	setRaw(true)
	_, _ = readKeys() // any key or click closes it
	setRaw(false)
}

// reloadConf regenerates the workspace's tmux configuration and applies it to the running server.
func reloadConf(ws string, cfg Config) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(tmuxConfFile(ws), []byte(tmuxConf(ws, bin, cfg.BarPosition != barBottom, cfg.browserKeys()))); err != nil {
		return err
	}
	_, err = Tmux{WS: ws}.Run("source-file", tmuxConfFile(ws))
	return err
}

// uiConfirm is a floating confirmation for destructive actions: _ui confirm <ws> stop|close.
func uiConfirm(ws, what, explicit string) error {
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
		if explicit != "" { // the ✕ of a specific tab
			session = explicit
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
		args := []string{"display", "-p", "-c", client}
		if explicit != "" { // the ✕ of a specific session
			args = append(args, "-t", explicit)
		}
		out, err := t.Run(append(args, "#{pane_id}"+sep+"#{@cw_alias}"+sep+"#{@cw_kind}"+sep+"#{window_panes}"+sep+"#{window_id}")...)
		if err != nil {
			return err
		}
		f := strings.Split(out, sep)
		target = f[0]
		question = "¿Cerrar la sesión " + f[1] + "?"
		detail = "La terminal se cierra y la sesión deja de guardarse en el workspace."
		if f[2] == kindClaude {
			detail = "Claude se cierra; la conversación queda en disco y puedes retomarla desde una pestaña vacía."
		}
		yes = "Cerrar"
		if strings.HasPrefix(explicit, "@") && f[3] != "1" { // the ✕ of a combined view's pill
			target = f[4]
			question = "¿Cerrar la vista con sus " + f[3] + " sesiones?"
			detail = "Se cierran todas las sesiones de esta vista. Las conversaciones de Claude quedan en disco."
			yes = "Cerrar vista"
		}
	default:
		return fmt.Errorf("confirmación desconocida %q", what)
	}
	focusYes := false
	var hits []hit
	draw := func() {
		var s screen
		s.at(2, 4, sgrBold+question+sgrReset)
		for i, line := range wrapWords(detail, 58, 3) {
			s.at(4+i, 4, sgrMuted+line+sgrReset)
		}
		danger := "\033[48;5;160m\033[38;5;255m\033[1m " + yes + " " + sgrReset
		if !focusYes {
			danger = "\033[48;5;52m\033[38;5;217m " + yes + " " + sgrReset
		}
		s.btn(8, 4, button("Cancelar", false, !focusYes), "no")
		s.btn(8, 18, danger, "yes")
		s.closeX("no")
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
			switch {
			case k.kind == keyClick && hitAt(hits, k.x, k.y) == "no",
				k.kind == keyEsc || (k.kind == keyRune && (k.r == 'n' || k.r == 'q')):
				setRaw(false)
				return nil
			case k.kind == keyLeft || k.kind == keyRight || k.kind == keyTab:
				focusYes = !focusYes
			case (k.kind == keyEnter && focusYes) || (k.kind == keyRune && (k.r == 'y' || k.r == 's')) ||
				(k.kind == keyClick && hitAt(hits, k.x, k.y) == "yes"):
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
func closeWindow(ws, target string) error {
	t := Tmux{WS: ws}
	// Remember what closes, so Ctrl+Shift+T (Reabrir sesión cerrada) can bring it back.
	if panes, err := t.Lines("list-panes", "-t", target, "-F", "#{pane_id}"+sep+"#{session_name}"); err == nil {
		if strings.HasPrefix(target, "%") {
			panes = [][]string{{target, ""}}
			if s, err := t.Run("display", "-p", "-t", target, "#{session_name}"); err == nil {
				panes[0][1] = s
			}
		}
		for _, p := range panes {
			if s, err := paneSlot(t, p[0]); err == nil && len(p) == 2 {
				pushClosed(t, s, p[1])
				_ = acknowledgeNotifications("", ws, s.ID, false)
			}
		}
	}
	// A pane of a view with other sessions closes alone; the view stays with the rest.
	if strings.HasPrefix(target, "%") {
		if n, _ := t.Run("display", "-p", "-t", target, "#{window_panes}"); n != "1" {
			win, _ := t.Run("display", "-p", "-t", target, "#{window_id}")
			if _, err := t.Run("kill-pane", "-t", target); err != nil {
				return err
			}
			t.arrangeView(win, "")
			return nil
		}
	}
	windowID, err := t.Run("display", "-p", "-t", target, "#{window_id}")
	if err != nil {
		return err
	}
	if err := keepTabOpen(t, windowID); err != nil {
		return err
	}
	_, err = t.Run("kill-window", "-t", windowID)
	return err
}

// closeTab closes a tab and every session in it. Closing the only tab leaves an empty one, so
// the workspace keeps running (use "Apagar y guardar" to stop it).
func closeTab(ws, session, client string) error {
	t := Tmux{WS: ws}
	if rows, _ := t.Lines("list-sessions", "-F", "#{session_name}"); len(rows) == 1 {
		dir := workspaceDir(t)
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
	if rows, err := t.Lines("list-panes", "-s", "-t", session, "-F", "#{@cw_slot}"); err == nil {
		for _, row := range rows {
			if len(row) > 0 && row[0] != "" {
				_ = acknowledgeNotifications("", ws, row[0], false)
			}
		}
	}
	_, err := t.Run("kill-session", "-t", session)
	return err
}

// uiRename renames the current tab or session in a centred dialog.
func uiRename(ws, what string) error {
	t := Tmux{WS: ws}
	client, session, err := currentClient(t)
	if err != nil {
		return err
	}
	if what == "tab" {
		title, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		v, ok := askText("Nuevo nombre de la pestaña", "Nombre", title, "")
		if !ok || cleanLabel(v) == "" {
			return nil
		}
		return t.setTabOptions(session, Tab{Title: cleanLabel(v), Dir: tabDir(t, session)})
	}
	pane, err := t.Run("display", "-p", "-c", client, "#{pane_id}")
	if err != nil {
		return err
	}
	return renameSlot(t, pane)
}

// renameSlot asks for a session's new name in a centred dialog and applies it to its pane (and
// to its view's window name when the view shows only that session).
func renameSlot(t Tmux, pane string) error {
	cur, _ := t.Run("display", "-p", "-t", pane, "#{@cw_alias}")
	v, ok := askText("Nuevo nombre de la sesión", "Nombre", cur, "")
	if !ok || cleanLabel(v) == "" {
		return nil
	}
	return setSlotName(t, pane, cleanLabel(v))
}

func setSlotName(t Tmux, pane, name string) error {
	_, err := t.Run("set", "-p", "-t", pane, "@cw_alias", name)
	if n, _ := t.Run("display", "-p", "-t", pane, "#{window_panes}"); n == "1" {
		_, _ = t.Run("rename-window", "-t", pane, name)
	}
	return err
}

// uiRenameWorkspace changes the visible name of the running workspace.
func uiRenameWorkspace(ws string) error {
	t := Tmux{WS: ws}
	cur, _ := t.Run("show", "-gqv", "@cw_ws_title")
	if cur == "" {
		cur = ws
	}
	v, ok := askText("Nuevo nombre del workspace", "Nombre", cur, "")
	if !ok {
		return nil
	}
	return renameWorkspace(ws, v)
}

// renameWorkspace sets a workspace's title, running or not.
func renameWorkspace(ws, title string) error {
	title = cleanLabel(title)
	if title == "" {
		return errors.New("el nombre no puede quedar vacío")
	}
	for _, n := range savedWorkspaces() {
		if n == ws {
			continue
		}
		if w, err := loadWorkspace(n); err == nil && (strings.EqualFold(w.displayTitle(), title) || strings.EqualFold(n, title)) {
			return fmt.Errorf("ya hay un workspace llamado %q", title)
		}
	}
	t := Tmux{WS: ws}
	if t.Running() {
		if _, err := t.Run("set", "-g", "@cw_ws_title", title); err != nil {
			return err
		}
		_, _ = t.Run("refresh-client", "-S") // fails harmlessly while no client is attached
		return cmdSave(ws, true)
	}
	w, err := loadWorkspace(ws)
	if err != nil {
		return err
	}
	w.Title = title
	return saveWorkspace(w)
}
