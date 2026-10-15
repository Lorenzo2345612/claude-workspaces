// Command cw keeps named workspaces of Claude Code sessions and shells in tabs, each tab rooted
// in a directory, and saves / restores the whole set on demand. tmux does the terminal work.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const usage = `cw — workspaces de sesiones de Claude Code en pestañas

Uso:
  cw                       inicio: crear un workspace en este directorio o retomar uno
  cw new                   crea directamente un workspace en este directorio
  cw new --id <ID>         crea un workspace con esa conversación de Claude (en su carpeta)
  cw start <nombre> [dir]  entra al workspace: si corre se reconecta, si está guardado lo
                           restaura, y si no existe lo crea con una pestaña en [dir] (por defecto .)
  cw ls                    lista los workspaces guardados y los que están corriendo
  cw save <nombre>         guarda el estado del workspace sin detenerlo
  cw stop <nombre>         guarda el estado y apaga el workspace (también: cw apagar)
  cw rm <nombre>           borra el estado guardado de un workspace apagado
  cw config                muestra la ruta y el contenido de la configuración
  cw version               muestra la versión
  cw update [--check]      se actualiza a la última versión publicada (--check solo avisa)

Dentro del workspace: Alt+m menú · Alt+t nueva pestaña · Alt+n nueva sesión · Alt+h ayuda ·
Alt+←/→ sesiones · Alt+↑/↓ pestañas · todo con clic; clic derecho y arrastrar para reordenar.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cw:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if cfg, err := loadConfig(); err == nil {
		applyTheme(cfg.Theme)
	}
	if len(args) == 0 {
		return cmdLauncher()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "start", "attach":
		here := false
		var pos []string
		for _, a := range rest {
			if a == "--here" {
				here = true
			} else {
				pos = append(pos, a)
			}
		}
		if len(pos) < 1 || len(pos) > 2 {
			return errors.New("uso: cw start [--here] <nombre> [dir]")
		}
		dir := "."
		if len(pos) == 2 {
			dir = pos[1]
		}
		return cmdStart(resolveWorkspace(pos[0]), dir, here)
	case "new", "nuevo":
		if len(rest) == 2 && rest[0] == "--id" {
			return cmdNewFromID(rest[1])
		}
		if len(rest) != 0 {
			return errors.New("uso: cw new [--id <ID de una conversación>]")
		}
		return cmdNewHere()
	case "ls", "list":
		return cmdList()
	case "save":
		return withName(rest, func(ws string) error { return cmdSave(ws, false) })
	case "stop", "apagar":
		return withName(rest, cmdStop)
	case "rm":
		return withName(rest, cmdRemove)
	case "config":
		return cmdConfig()
	case "update", "actualizar":
		return cmdUpdate(rest)
	case "version", "--version", "-v":
		fmt.Println("cw", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	// Internal commands, called from the tmux configuration, the hooks and the placeholders.
	case "_new":
		return cmdNew(rest)
	case "_ui":
		return cmdUI(rest)
	case "_respawn":
		return cmdRespawn(rest)
	case "_slotmenu":
		return cmdSlotMenu(rest)
	case "_join":
		return cmdJoin(rest)
	case "_splitout":
		return cmdSplitOut(rest)
	case "_layout":
		return cmdLayout(rest)
	case "_barclick":
		return cmdBarClick(rest)
	case "_configmenu":
		return cmdConfigMenu(rest)
	case "_conf":
		return cmdConf(rest)
	case "_winmove":
		return cmdWinMove(rest)
	case "_tabmove":
		return cmdTabMove(rest)
	case "_labels":
		return withName(rest, func(ws string) error { refreshAllLabels(Tmux{WS: ws}); return nil })
	case "_home":
		return cmdHome(rest)
	case "_title":
		return cmdTitle(rest)
	case "_hook":
		return cmdHook()
	case "_wait":
		return cmdWait(rest)
	case "_autosave":
		return withName(rest, cmdAutosave)
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("comando desconocido %q", cmd)
}

func withName(args []string, f func(string) error) error {
	if len(args) != 1 {
		return errors.New("falta el nombre del workspace")
	}
	ws := resolveWorkspace(args[0])
	if err := validWorkspaceName(ws); err != nil {
		return err
	}
	return f(ws)
}

func cmdStart(ws, dir string, here bool) error { return startWorkspace(ws, dir, here, nil, "") }

// startWorkspace enters a workspace: reconnects if it runs, restores it if saved, or creates it.
// A new workspace gets one tab in dir, or seed (a tab already filled, e.g. with a conversation
// brought in by its ID, opened as a resumed session) and title as its visible name.
func startWorkspace(ws, dir string, here bool, seed *Tab, title string) error {
	if err := validWorkspaceName(ws); err != nil {
		return err
	}
	if os.Getenv("CW_WS") != "" {
		return errors.New("ya estás dentro de un workspace; desconéctate primero (Alt+m → Desconectar)")
	}
	if err := checkTmux(); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("configuración inválida en %s: %w", configFile(), err)
	}
	if !here && seed == nil {
		if opened, err := openOwnWindow(ws, dir, cfg); opened || err != nil {
			return err
		}
	}
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(tmuxConfFile(ws), []byte(tmuxConf(ws, l.Bin, cfg.BarPosition != barBottom))); err != nil {
		return err
	}
	t := Tmux{WS: ws}

	if t.Running() {
		_, _ = t.Run("source-file", tmuxConfFile(ws)) // pick up a rebuilt binary or new styles
		migrateLegacyOptions(t)
		if rows, err := t.Lines("list-sessions", "-F", "#{session_name}"+sep+"#{@cw_title}"+sep+"#{@cw_dir}"); err == nil {
			for _, r := range rows {
				_ = t.setTabOptions(r[0], Tab{Title: r[1], Dir: r[2]})
			}
		}
		return attach(t, dir)
	}

	saved, err := loadWorkspace(ws)
	switch {
	case err == nil:
		if err := restore(t, l, saved); err != nil {
			return err
		}
	case errors.Is(err, fs.ErrNotExist):
		if seed != nil {
			l.Cfg.LazyStart = false // the user asked for this conversation now
			if err := l.openTab(t, t.nextTabSession(), seed, true); err != nil {
				return err
			}
			dir = seed.Dir
		} else {
			abs, err := expandPath(dir)
			if err != nil || !isDir(abs) {
				return fmt.Errorf("el directorio %q no existe", dir)
			}
			tab := Tab{Title: filepath.Base(abs), Dir: abs}
			if err := l.openTab(t, t.nextTabSession(), &tab, false); err != nil {
				return err
			}
		}
		if title != "" {
			_, _ = t.Run("set", "-g", "@cw_ws_title", title)
		}
		if err := cmdSave(ws, false); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no pude leer %s: %w", workspaceFile(ws), err)
	}
	return attach(t, dir)
}

func restore(t Tmux, l *Launcher, w *Workspace) error {
	if len(w.Tabs) == 0 {
		w.Tabs = []Tab{{Title: "inicio", Dir: homeDir()}}
	}
	activeSession := ""
	for i := range w.Tabs {
		session := fmt.Sprintf("t%02d", i+1)
		if err := l.openTab(t, session, &w.Tabs[i], true); err != nil {
			return fmt.Errorf("pestaña %q: %w", w.Tabs[i].Title, err)
		}
		if i == w.ActiveTab {
			activeSession = session
		}
	}
	if activeSession == "" {
		activeSession = "t01"
	}
	_, err := t.Run("set", "-g", "@cw_active", activeSession)
	return err
}

// attach replaces this process with the tmux client, landing on the last active tab.
// launchDir — where cw was opened — becomes the default directory of the next new tab.
func attach(t Tmux, launchDir string) error {
	if cur, _ := t.Run("show", "-gqv", "@cw_ws_title"); cur == "" {
		title := t.WS
		if w, err := loadWorkspace(t.WS); err == nil && w.Title != "" {
			title = w.Title
		}
		_, _ = t.Run("set", "-g", "@cw_ws_title", title)
	}
	if abs, err := expandPath(launchDir); err == nil && isDir(abs) {
		_, _ = t.Run("set", "-g", "@cw_launch_dir", abs)
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	argv := append([]string{"tmux"}, t.baseArgs()...)
	argv = append(argv, "attach")
	if active, _ := t.Run("show", "-gqv", "@cw_active"); active != "" {
		argv = append(argv, "-t", active)
	}
	return syscall.Exec(tmuxBin, argv, os.Environ())
}

func cmdSave(ws string, quiet bool) error {
	t := Tmux{WS: ws}
	if !t.Running() {
		return fmt.Errorf("el workspace %q no está corriendo", ws)
	}
	w, err := t.capture()
	if err != nil {
		return err
	}
	if len(w.Tabs) == 0 {
		return nil // never overwrite a good state with an empty one
	}
	w.SavedAt = time.Now()
	if err := saveWorkspace(w); err != nil {
		return err
	}
	if !quiet && os.Getenv("CW_WS") == "" {
		fmt.Printf("guardado %s (%d pestañas)\n", workspaceFile(ws), len(w.Tabs))
	}
	return nil
}

func cmdStop(ws string) error {
	t := Tmux{WS: ws}
	if !t.Running() {
		return fmt.Errorf("el workspace %q no está corriendo", ws)
	}
	if err := cmdSave(ws, true); err != nil {
		return fmt.Errorf("no apagué porque no se pudo guardar: %w", err)
	}
	_, err := t.Run("kill-server")
	// kill-server returns before the server is gone; a `cw start` right after would attach to it.
	for i := 0; i < 50 && t.Running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if err == nil && os.Getenv("CW_WS") == "" {
		fmt.Printf("workspace %s guardado y apagado\n", ws)
	}
	return err
}

func cmdAutosave(ws string) error {
	cfg, _ := loadConfig()
	if st, err := os.Stat(workspaceFile(ws)); err == nil &&
		time.Since(st.ModTime()) < time.Duration(cfg.AutosaveSeconds)*time.Second {
		return nil
	}
	_ = cmdSave(ws, true) // status-bar job: failures must stay silent
	return nil
}

func cmdRemove(ws string) error {
	if (Tmux{WS: ws}).Running() {
		return fmt.Errorf("el workspace %q está corriendo; apágalo primero con: cw stop %s", ws, ws)
	}
	if err := os.Remove(workspaceFile(ws)); err != nil {
		return err
	}
	_ = os.Remove(workspaceFile(ws) + ".bak")
	fmt.Printf("borrado %s\n", ws)
	return nil
}

func cmdConfig() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := os.Stat(configFile()); errors.Is(err, fs.ErrNotExist) {
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}
	data, _ := os.ReadFile(configFile())
	fmt.Printf("%s\n\n%s", configFile(), data)
	return nil
}

type entry struct {
	name    string
	running bool
	saved   *Workspace
}

func listEntries() []entry {
	running := map[string]bool{}
	for _, n := range runningWorkspaces() {
		running[n] = true
	}
	var out []entry
	seen := map[string]bool{}
	for _, n := range savedWorkspaces() {
		w, _ := loadWorkspace(n)
		out = append(out, entry{name: n, running: running[n], saved: w})
		seen[n] = true
	}
	for n := range running {
		if !seen[n] {
			out = append(out, entry{name: n, running: true})
		}
	}
	return out
}

func describe(e entry) string {
	state := "apagado"
	if e.running {
		state = "corriendo"
	}
	if e.saved == nil {
		return fmt.Sprintf("%-20s %-10s", e.name, state)
	}
	if e.saved.Title != "" && e.saved.Title != e.name {
		state += " (id " + e.name + ")"
	}
	var titles []string
	sessions := 0
	for _, t := range e.saved.Tabs {
		titles = append(titles, t.Title)
		sessions += len(t.Slots)
	}
	return fmt.Sprintf("%-20s %-10s %2d pestañas, %2d sesiones · %s · %s", e.saved.displayTitle(), state,
		len(e.saved.Tabs), sessions, e.saved.SavedAt.Local().Format("2006-01-02 15:04"), strings.Join(titles, ", "))
}

func cmdList() error {
	entries := listEntries()
	if len(entries) == 0 {
		fmt.Println("no hay workspaces; crea uno con: cw start <nombre> [dir]")
		return nil
	}
	for _, e := range entries {
		fmt.Println(describe(e))
	}
	return nil
}

// cmdNewHere creates a workspace named after the current directory (with a numeric suffix when
// that name is taken) and opens it on the start screen, which offers the latest conversations.
func cmdNewHere() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return cmdStart(freeWorkspaceName(cwd), cwd, false)
}

// freeWorkspaceName derives a workspace id from a directory name, with a numeric suffix when
// that id is taken.
func freeWorkspaceName(dir string) string {
	base := strings.Trim(regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(filepath.Base(dir), "-"), "-")
	if base == "" {
		base = "cw"
	}
	if len(base) > 34 {
		base = base[:34]
	}
	taken := map[string]bool{}
	for _, e := range listEntries() {
		taken[e.name] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// cmdNewFromID creates a workspace around an existing Claude conversation: cw new --id <ID>.
func cmdNewFromID(query string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	found := findConversations(query, cfg)
	switch len(found) {
	case 0:
		return fmt.Errorf("no encontré ninguna conversación de Claude con el ID %q", query)
	case 1:
		return startFromConversation(found[0])
	}
	var ids []string
	for _, f := range found {
		ids = append(ids, f.conv.ID)
	}
	return fmt.Errorf("el ID %q coincide con varias conversaciones: %s", query, strings.Join(ids, ", "))
}

// startFromConversation opens a new workspace whose first tab holds the conversation, resumed,
// in the directory where it started.
func startFromConversation(f foundConv) error {
	dir := f.cwd
	if !isDir(dir) {
		dir, _ = os.Getwd()
	}
	name := cleanLabel(f.conv.Title)
	if name == "" {
		name = cleanLabel(shorten(strings.Join(strings.Fields(f.conv.First), "-"), 24))
	}
	if name == "" {
		name = "claude"
	}
	tab := &Tab{Title: filepath.Base(dir), Dir: dir, Slots: []Slot{{Kind: kindClaude, Alias: name, Cwd: dir,
		SessionID: f.conv.ID, ClaudeAlias: f.conv.Alias, Status: statusIdle}}}
	return startWorkspace(freeWorkspaceName(dir), dir, true, tab, "")
}
