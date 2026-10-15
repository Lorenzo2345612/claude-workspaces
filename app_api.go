package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type uiSlot struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	Alias      string   `json:"alias"`
	Cwd        string   `json:"cwd"`
	Short      string   `json:"short"`
	SID        string   `json:"sid"`
	Args       []string `json:"args"`
	Dead       bool     `json:"dead"`
	DeadStatus string   `json:"deadStatus"`
}

type uiTab struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Dir     string     `json:"dir"`
	Short   string     `json:"short"`
	Busy    int        `json:"busy"`
	Waiting int        `json:"waiting"`
	Idle    int        `json:"idle"`
	Asleep  int        `json:"asleep"`
	Views   []*appView `json:"views"`
	Active  string     `json:"active"`
}

type uiState struct {
	WS      string            `json:"ws"`
	Title   string            `json:"title"`
	Running bool              `json:"running"`
	Mode    string            `json:"mode"` // "app", "terminal" or "" when stopped
	Tabs    []uiTab           `json:"tabs"`
	Active  string            `json:"active"`
	Slots   map[string]uiSlot `json:"slots"`
	Version string            `json:"version"`
}

// appSnapshot is the state the window draws, read from tmux (and tidied up) on every call.
func appSnapshot(ws string) uiState {
	st := uiState{WS: ws, Title: ws, Version: version, Slots: map[string]uiSlot{}}
	t := Tmux{WS: ws}
	if !t.Running() {
		if w, err := loadWorkspace(ws); err == nil {
			st.Title = w.displayTitle()
		}
		return st
	}
	st.Running = true
	if !appMode(t) {
		st.Mode = "terminal"
		return st
	}
	st.Mode = "app"
	if title, _ := t.Run("show", "-gqv", optWSTitle); title != "" {
		st.Title = title
	}
	a := appWS(ws)
	a.mu.Lock()
	defer a.mu.Unlock()
	panes, err := t.listSlotPanes()
	if err != nil {
		return st
	}
	l := t.loadLayout()
	if l.reconcile(panes) {
		_ = t.storeLayout(l)
	}
	if at := l.activeTab(); at != nil {
		st.Active = at.ID
	}
	for _, at := range l.Tabs {
		u := uiTab{ID: at.ID, Title: at.Title, Dir: at.Dir, Short: shortPath(at.Dir), Views: at.Views}
		if v := at.activeView(); v != nil {
			u.Active = v.ID
			if v.Focus == "" && len(v.Slots) > 0 {
				v.Focus = v.Slots[0]
			}
		}
		if u.Views == nil {
			u.Views = []*appView{}
		}
		for _, v := range at.Views {
			for _, id := range v.Slots {
				p := panes[id]
				switch {
				case p.slot.Kind != kindClaude:
				case p.slot.Status == statusBusy:
					u.Busy++
				case p.slot.Status == statusWaiting:
					u.Waiting++
				case p.dead:
				case p.slot.Status == statusSleeping:
					u.Asleep++
				default:
					u.Idle++
				}
			}
		}
		st.Tabs = append(st.Tabs, u)
	}
	cfg, _ := loadConfig()
	for id, p := range panes {
		name := p.slot.Alias
		if name == "" {
			name = "terminal"
		}
		cwd := p.slot.Cwd
		if p.slot.Kind == kindShell && p.path != "" {
			cwd = p.path
		}
		alias := ""
		if p.slot.Kind == kindClaude {
			alias = cfg.claudeAlias(p.slot.ClaudeAlias).Name
		}
		st.Slots[id] = uiSlot{ID: id, Name: name, Kind: p.slot.Kind, Status: p.slot.Status, Alias: alias, Cwd: cwd,
			Short: shortPath(cwd), SID: p.slot.SessionID, Args: p.slot.Args, Dead: p.dead, DeadStatus: p.deadStatus}
	}
	return st
}

type uiWorkspace struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	State    string   `json:"state"` // app, terminal, stopped
	Tabs     int      `json:"tabs"`
	Sessions int      `json:"sessions"`
	Working  int      `json:"working"`
	When     string   `json:"when"`
	At       int64    `json:"at"`
	TabNames []string `json:"tabNames"`
}

func listUIWorkspaces() []uiWorkspace {
	var out []uiWorkspace
	for _, e := range listEntries() {
		u := uiWorkspace{Name: e.name, Title: e.name, State: "stopped"}
		if e.saved != nil {
			u.Title = e.saved.displayTitle()
			u.When = whenES(e.saved.SavedAt)
			u.At = e.saved.SavedAt.Unix()
			u.Tabs = len(e.saved.Tabs)
			for _, t := range e.saved.Tabs {
				u.TabNames = append(u.TabNames, t.Title)
				u.Sessions += len(t.Slots)
				for _, s := range t.Slots {
					if s.wasWorking() {
						u.Working++
					}
				}
			}
		}
		if e.running {
			t := Tmux{WS: e.name}
			u.State = "terminal"
			if w, err := t.capture(); err == nil && !appMode(t) {
				u.Tabs, u.Sessions, u.Working, u.TabNames = len(w.Tabs), 0, 0, nil
				for _, tab := range w.Tabs {
					u.TabNames = append(u.TabNames, tab.Title)
					u.Sessions += len(tab.Slots)
					for _, sl := range tab.Slots {
						if sl.wasWorking() {
							u.Working++
						}
					}
				}
				if w.Title != "" {
					u.Title = w.Title
				}
			}
			if appMode(t) {
				u.State = "app"
				st := appSnapshot(e.name)
				u.Title = st.Title
				u.Tabs, u.Sessions, u.Working, u.TabNames = len(st.Tabs), len(st.Slots), 0, nil
				for _, tab := range st.Tabs {
					u.TabNames = append(u.TabNames, tab.Title)
					u.Working += tab.Busy + tab.Waiting
				}
				u.At = time.Now().Unix()
			}
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := out[i].State != "stopped", out[j].State != "stopped"
		if ri != rj {
			return ri
		}
		return out[i].At > out[j].At
	})
	if out == nil {
		out = []uiWorkspace{}
	}
	return out
}

type uiAlias struct {
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Env     []string `json:"env"`
	Args    []string `json:"args"`
	Shell   bool     `json:"shell"` // runs the user's own shell command of that name
	Builtin bool     `json:"builtin"`
}

func uiAliases(cfg Config) []uiAlias {
	var out []uiAlias
	for _, a := range cfg.claudeAliases() {
		u := uiAlias{Name: a.Name, Summary: a.summary(), Env: a.Env, Args: a.Args, Shell: a.ViaShell, Builtin: a.Name == builtinAlias}
		if a.Name == builtinAlias && len(a.Env) == 0 && len(a.Args) == 0 {
			u.Summary = "claude"
		}
		out = append(out, u)
	}
	return out
}

func (s *appServer) handleGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cfg, _ := loadConfig()
	switch strings.TrimPrefix(r.URL.Path, "/api/get/") {
	case "workspaces":
		writeJSON(w, listUIWorkspaces())
	case "state":
		writeJSON(w, appSnapshot(q.Get("ws")))
	case "info":
		dir := s.launchDir
		writeJSON(w, map[string]any{"version": version, "launchDir": dir, "launchShort": shortPath(dir), "home": homeDir(),
			"platform": runtimePlatform()})
	case "config":
		var shell []uiAlias
		have := map[string]bool{}
		for _, a := range cfg.claudeAliases() {
			have[a.Name] = true
		}
		for _, a := range importShellAliases() {
			if !have[a.Name] {
				shell = append(shell, uiAlias{Name: a.Name, Summary: a.summary(), Env: a.Env, Args: a.Args})
			}
		}
		writeJSON(w, map[string]any{"config": cfg, "aliases": uiAliases(cfg), "default": cfg.defaultAlias(),
			"shellAliases": shell, "themes": themeOrder, "configFile": configFile()})
	case "recent":
		ws, tabID := q.Get("ws"), q.Get("tab")
		t := Tmux{WS: ws}
		l := t.loadLayout()
		at := l.tab(tabID)
		if at == nil {
			writeJSON(w, []any{})
			return
		}
		open := map[string]bool{}
		if panes, err := t.listSlotPanes(); err == nil {
			for _, p := range panes {
				if p.slot.SessionID != "" {
					open[p.slot.SessionID] = true
				}
			}
		}
		var out []map[string]any
		for _, c := range recentConversations(at.Dir, 8, open, cfg) {
			out = append(out, map[string]any{"id": c.ID, "title": c.Title, "first": shorten(c.First, 140), "alias": c.Alias, "when": whenES(c.Modified)})
		}
		if out == nil {
			out = []map[string]any{}
		}
		writeJSON(w, out)
	case "conversations":
		open := map[string]string{}
		if ws := q.Get("ws"); ws != "" {
			t := Tmux{WS: ws}
			if panes, err := t.listSlotPanes(); err == nil {
				for _, p := range panes {
					if p.slot.SessionID != "" {
						open[p.slot.SessionID] = p.slot.Alias
					}
				}
			}
		}
		out := []map[string]any{}
		for i, f := range findConversations(q.Get("q"), cfg) {
			if i >= 8 {
				break
			}
			out = append(out, map[string]any{"id": f.conv.ID, "title": f.conv.Title, "first": shorten(f.conv.First, 140),
				"alias": f.conv.Alias, "when": whenES(f.conv.Modified), "cwd": f.cwd, "short": shortPath(f.cwd), "openAs": open[f.conv.ID]})
		}
		writeJSON(w, out)
	case "dirs":
		out := []map[string]any{}
		for _, d := range suggestDirs(q.Get("q"), recentDirs(Tmux{WS: "-"}), workDirs(), 8) {
			out = append(out, map[string]any{"path": d.path, "short": shortPath(d.path), "recent": d.recent})
		}
		writeJSON(w, out)
	case "preview":
		l, err := newLauncher(q.Get("ws"), cfg)
		if err != nil {
			writeErr(w, err)
			return
		}
		sl := Slot{Kind: kindClaude, Alias: q.Get("name"), Args: splitArgs(q.Get("args")), ClaudeAlias: q.Get("alias"), SessionID: q.Get("resume")}
		writeJSON(w, map[string]string{"command": l.commandPreview(sl, sl.SessionID != "")})
	default:
		http.NotFound(w, r)
	}
}

func runtimePlatform() string {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return "wsl"
	}
	return strings.ToLower(osName())
}

// doReq is the body of every action: the fields each one uses.
type doReq struct {
	Action       string   `json:"action"`
	WS           string   `json:"ws"`
	Tab          string   `json:"tab"`
	View         string   `json:"view"`
	Target       string   `json:"target"`
	Slot         string   `json:"slot"`
	Title        string   `json:"title"`
	Dir          string   `json:"dir"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Alias        string   `json:"alias"`
	Args         string   `json:"args"`
	Resume       string   `json:"resume"`
	Start        string   `json:"start"`
	Layout       string   `json:"layout"`
	Scope        string   `json:"scope"`
	To           int      `json:"to"`
	Cols         int      `json:"cols"`
	Rows         int      `json:"rows"`
	Restart      bool     `json:"restart"`
	SendContinue bool     `json:"sendContinue"`
	Convert      bool     `json:"convert"`
	Old          string   `json:"old"`
	Env          []string `json:"env"`
	ArgList      []string `json:"argList"`
	Config       *struct {
		ContinueMessage    *string   `json:"continue_message"`
		LazyStart          *bool     `json:"lazy_start"`
		AutosaveSeconds    *int      `json:"autosave_seconds"`
		ClaudeArgs         *[]string `json:"claude_args"`
		Theme              *string   `json:"theme"`
		DefaultClaudeAlias *string   `json:"default_claude_alias"`
		AppFontSize        *int      `json:"app_font_size"`
	} `json:"config"`
}

func (s *appServer) handleDo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	var req doReq
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.do(&req)
	if err != nil {
		writeErr(w, err)
		return
	}
	if res == nil {
		res = map[string]any{"ok": true}
	}
	writeJSON(w, res)
}

// do runs one action of the window.
func (s *appServer) do(q *doReq) (map[string]any, error) {
	switch q.Action {
	case "ws.open":
		t := Tmux{WS: q.WS}
		if t.Running() && !appMode(t) {
			if !q.Convert {
				return nil, errInTerminal
			}
			if err := cmdStop(q.WS); err != nil {
				return nil, err
			}
		}
		return nil, appEnsure(q.WS, homeDir(), "", nil)
	case "ws.new":
		dir, err := expandPath(q.Dir)
		if err != nil || !isDir(dir) {
			return nil, fmt.Errorf("la carpeta %q no existe", q.Dir)
		}
		name := freeWorkspaceName(dir)
		title := cleanLabel(q.Title)
		if title != "" {
			if err := checkWorkspaceTitle(name, title); err != nil {
				return nil, err
			}
		}
		if err := appEnsure(name, dir, title, nil); err != nil {
			return nil, err
		}
		return map[string]any{"ws": name}, nil
	case "ws.fromid":
		cfg, _ := loadConfig()
		found := findConversations(q.Resume, cfg)
		if len(found) == 0 {
			return nil, errors.New("no encontré ninguna conversación con ese ID")
		}
		f := found[0]
		dir := f.cwd
		if !isDir(dir) {
			dir = homeDir()
		}
		name := cleanLabel(q.Name)
		if name == "" {
			name = conversationName(f.conv)
		}
		alias := q.Alias
		if alias == "" {
			alias = f.conv.Alias
		}
		seed := &Tab{Title: baseName(dir), Dir: dir, Slots: []Slot{{Kind: kindClaude, Alias: name, Cwd: dir,
			SessionID: f.conv.ID, ClaudeAlias: alias, Status: statusIdle}}}
		ws := freeWorkspaceName(dir)
		if err := appEnsure(ws, dir, "", seed); err != nil {
			return nil, err
		}
		return map[string]any{"ws": ws}, nil
	case "ws.rename":
		return nil, renameWorkspace(q.WS, q.Title)
	case "ws.stop":
		t := Tmux{WS: q.WS}
		if !t.Running() {
			return nil, nil
		}
		if appMode(t) {
			a := appWS(q.WS)
			a.mu.Lock()
			defer a.mu.Unlock()
			if err := appSave(q.WS); err != nil {
				return nil, fmt.Errorf("no apagué porque no se pudo guardar: %w", err)
			}
			_, _ = t.Run("kill-server")
			for i := 0; i < 50 && t.Running(); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			return nil, nil
		}
		return nil, cmdStop(q.WS)
	case "ws.delete":
		if (Tmux{WS: q.WS}).Running() {
			return nil, errors.New("apágalo antes de borrarlo")
		}
		if err := os.Remove(workspaceFile(q.WS)); err != nil {
			return nil, err
		}
		_ = os.Remove(workspaceFile(q.WS) + ".bak")
		return nil, nil
	case "ws.save":
		a := appWS(q.WS)
		a.mu.Lock()
		defer a.mu.Unlock()
		return nil, appSave(q.WS)
	case "config.set":
		return nil, applyConfigPatch(q)
	case "alias.save":
		return nil, saveAliasFromUI(q.Old, q.Name, q.Env, q.ArgList)
	case "alias.delete":
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}
		var kept []ClaudeAlias
		for _, a := range cfg.ClaudeAliases {
			if a.Name != q.Name {
				kept = append(kept, a)
			}
		}
		cfg.ClaudeAliases = kept
		if cfg.DefaultClaudeAlias == q.Name {
			cfg.DefaultClaudeAlias = ""
		}
		return nil, saveConfig(cfg)
	case "alias.import":
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}
		for _, a := range importShellAliases() {
			if a.Name == q.Name {
				cfg.ClaudeAliases = append(cfg.ClaudeAliases, ClaudeAlias{Name: a.Name})
				shellAliasCache = nil
				return nil, saveConfig(cfg)
			}
		}
		return nil, fmt.Errorf("no encontré el alias %q en tu shell", q.Name)
	}
	if err := validWorkspaceName(q.WS); err != nil {
		return nil, err
	}
	t := Tmux{WS: q.WS}
	if !t.Running() || !appMode(t) {
		return nil, errors.New("el workspace no está abierto en la app")
	}
	a := appWS(q.WS)
	a.mu.Lock()
	defer a.mu.Unlock()
	res, err := wsAction(t, q)
	if err == nil {
		_ = appSave(q.WS) // what goes through the window is saved at once; the autosave covers the rest
	}
	return res, err
}

func checkWorkspaceTitle(ws, title string) error {
	for _, n := range savedWorkspaces() {
		if n == ws {
			continue
		}
		if w, err := loadWorkspace(n); err == nil && (strings.EqualFold(w.displayTitle(), title) || strings.EqualFold(n, title)) {
			return fmt.Errorf("ya hay un workspace llamado %q", title)
		}
	}
	return nil
}

func conversationName(c recentConv) string {
	name := cleanLabel(c.Title)
	if name == "" {
		name = cleanLabel(shorten(strings.Join(strings.Fields(c.First), "-"), 24))
	}
	if name == "" {
		name = "claude"
	}
	return name
}

func baseName(dir string) string {
	if dir == "" {
		return "inicio"
	}
	return filepath.Base(dir)
}

func applyConfigPatch(q *doReq) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if c := q.Config; c != nil {
		if c.ContinueMessage != nil {
			cfg.ContinueMessage = *c.ContinueMessage
		}
		if c.LazyStart != nil {
			cfg.LazyStart = *c.LazyStart
		}
		if c.AutosaveSeconds != nil && *c.AutosaveSeconds >= 30 {
			cfg.AutosaveSeconds = *c.AutosaveSeconds
		}
		if c.ClaudeArgs != nil {
			cfg.ClaudeArgs = *c.ClaudeArgs
		}
		if c.Theme != nil {
			if _, ok := themes[*c.Theme]; ok {
				cfg.Theme = *c.Theme
			}
		}
		if c.DefaultClaudeAlias != nil {
			cfg.DefaultClaudeAlias = *c.DefaultClaudeAlias
		}
		if c.AppFontSize != nil && *c.AppFontSize >= 9 && *c.AppFontSize <= 24 {
			cfg.AppFontSize = *c.AppFontSize
		}
	}
	return saveConfig(cfg)
}

func saveAliasFromUI(old, name string, env, args []string) error {
	name = strings.TrimSpace(name)
	if !regexpAliasName(name) {
		return errors.New("el nombre solo puede tener letras, números, '.', '_' o '-'")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	a := ClaudeAlias{Name: name}
	for _, kv := range env {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		if !envWordRe.MatchString(kv) {
			return fmt.Errorf("%q no es una variable (usa NOMBRE=valor)", kv)
		}
		a.Env = append(a.Env, kv)
	}
	for _, x := range args {
		if x != "" {
			a.Args = append(a.Args, x)
		}
	}
	replaced := false
	for i := range cfg.ClaudeAliases {
		if cfg.ClaudeAliases[i].Name == old && old != "" || cfg.ClaudeAliases[i].Name == name {
			if !replaced {
				cfg.ClaudeAliases[i], replaced = a, true
			}
		}
	}
	if !replaced {
		for _, x := range cfg.ClaudeAliases {
			if x.Name == name {
				return fmt.Errorf("ya existe el alias %q", name)
			}
		}
		cfg.ClaudeAliases = append(cfg.ClaudeAliases, a)
	}
	if old != "" && old != name && cfg.DefaultClaudeAlias == old {
		cfg.DefaultClaudeAlias = name
	}
	shellAliasCache = nil
	return saveConfig(cfg)
}

func regexpAliasName(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// handlePTY connects a terminal of the window to one session: a tmux client attached to it,
// running in a pseudo-terminal, bridged over a WebSocket.
func (s *appServer) handlePTY(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ws, slot := q.Get("ws"), q.Get("slot")
	if validWorkspaceName(ws) != nil || slot == "" || strings.ContainsAny(slot, ":.= ") {
		http.Error(w, "parámetros inválidos", http.StatusBadRequest)
		return
	}
	t := Tmux{WS: ws}
	if _, err := t.Run("has-session", "-t", "="+slotSession(slot)); err != nil {
		http.Error(w, "la sesión no existe", http.StatusNotFound)
		return
	}
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	if cols < 10 || cols > 1000 {
		cols = 120
	}
	if rows < 3 || rows > 500 {
		rows = 40
	}
	c, err := wsUpgrade(w, r)
	if err != nil {
		return
	}
	defer c.Close()
	cmd := t.Command("attach", "-t", "="+slotSession(slot))
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX=") && !strings.HasPrefix(kv, "TMUX_PANE=") && !strings.HasPrefix(kv, "TERM=") &&
			!strings.HasPrefix(kv, "COLORTERM=") && !strings.HasPrefix(kv, "CW_WS=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	m, err := startInPTY(cmd, cols, rows)
	if err != nil {
		_ = c.WriteText([]byte(`{"error":"` + strings.ReplaceAll(err.Error(), `"`, `'`) + `"}`))
		return
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := m.Read(buf)
			if n > 0 {
				if c.WriteBinary(buf[:n]) != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		close(done)
		c.conn.Close()
	}()
	for {
		op, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		if op == wsBinary {
			if _, err := m.Write(data); err != nil {
				break
			}
			continue
		}
		var ctl struct {
			Type       string `json:"type"`
			Cols, Rows int
		}
		if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" && ctl.Cols >= 2 && ctl.Rows >= 2 {
			_ = setPTYSize(m, ctl.Cols, ctl.Rows)
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
	}
	m.Close()
	<-done
	_ = cmd.Wait()
}

func osName() string {
	out, err := exec.Command("uname", "-s").Output()
	if err != nil {
		return "unix"
	}
	return strings.TrimSpace(string(out))
}
