package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The read side of cw mcp: a snapshot of every running workspace that the tools filter, and the
// tools that only look (lists, a session's screen, its pending question, usage, resources).

// mcpSession is one session as the tools show it.
type mcpSession struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // claude or shell
	Status    string `json:"status,omitempty"`
	Alias     string `json:"alias,omitempty"` // the claude alias it runs with
	Dir       string `json:"dir"`
	Workspace string `json:"workspace"`
	Tab       string `json:"tab"`
	TabIndex  int    `json:"tab_index"` // 1-based
	View      int    `json:"view"`      // 1-based, within the tab
	pane      string
}

type mcpTab struct {
	Index    int            `json:"index"`
	Title    string         `json:"title"`
	Dir      string         `json:"dir"`
	Active   bool           `json:"active,omitempty"`
	Views    int            `json:"views"`
	Counts   map[string]int `json:"counts,omitempty"`
	sessions []mcpSession
	id       string   // app workspaces: the tab's id in the layout
	session  string   // terminal workspaces: the tmux session that is the tab
	viewIDs  []string // app workspaces: the id of each view, in order
}

type mcpWorkspace struct {
	Name    string         `json:"name"`
	Title   string         `json:"title,omitempty"`
	Running bool           `json:"running"`
	App     bool           `json:"app,omitempty"` // opened by the desktop app (it has views)
	Dir     string         `json:"dir,omitempty"`
	Counts  map[string]int `json:"counts,omitempty"`
	tabs    []mcpTab
}

// Status names the tools use: what the user reads in the app.
func mcpStatus(s Slot) string {
	switch {
	case s.Kind != kindClaude:
		return ""
	case s.Suspended || s.Status == statusSleeping:
		return "suspendida"
	case s.Status == statusBusy:
		return "trabajando"
	case s.Status == statusWaiting:
		return "pregunta"
	}
	return "lista"
}

// mcpLoad reads one running workspace.
func mcpLoad(ws string) (*mcpWorkspace, error) {
	t := Tmux{WS: ws}
	if !t.Running() {
		return nil, fmt.Errorf("el workspace %q no está abierto", ws)
	}
	var w *Workspace
	var err error
	app := appMode(t)
	if app {
		w, err = appCapture(t)
	} else {
		w, err = t.capture()
	}
	if err != nil {
		return nil, err
	}
	panes := map[string]string{}
	if rows, err := t.Lines("list-panes", "-a", "-F", "#{@cw_slot}"+sep+"#{pane_id}"); err == nil {
		for _, r := range rows {
			if len(r) == 2 && r[0] != "" {
				panes[r[0]] = r[1]
			}
		}
	}
	out := &mcpWorkspace{Name: ws, Title: w.Title, Running: true, App: app, Dir: w.Dir, Counts: map[string]int{}}
	// The ids the actions need, in the order the capture lists tabs and views.
	var layout *appLayout
	var tmuxSessions []string
	if app {
		layout = t.loadLayout()
	} else if rows, err := t.Lines("list-sessions", "-F", "#{session_name}"); err == nil {
		for _, r := range rows {
			tmuxSessions = append(tmuxSessions, r[0])
		}
		sort.Strings(tmuxSessions)
	}
	for i, tab := range w.Tabs {
		mt := mcpTab{Index: i + 1, Title: tab.Title, Dir: tab.Dir, Active: i == w.ActiveTab, Counts: map[string]int{}}
		if layout != nil && i < len(layout.Tabs) {
			mt.id = layout.Tabs[i].ID
			for _, v := range layout.Tabs[i].Views {
				for _, id := range v.Slots {
					if panes[id] != "" {
						mt.viewIDs = append(mt.viewIDs, v.ID)
						break
					}
				}
			}
		} else if i < len(tmuxSessions) {
			mt.session = tmuxSessions[i]
		}
		for _, s := range tab.Slots {
			ms := mcpSession{ID: s.ID, Name: s.Alias, Kind: s.Kind, Status: mcpStatus(s), Dir: s.Cwd,
				Workspace: ws, Tab: tab.Title, TabIndex: i + 1, View: s.View + 1, pane: panes[s.ID]}
			if s.Kind == kindClaude {
				ms.Alias = s.ClaudeAlias
				mt.Counts[ms.Status]++
				out.Counts[ms.Status]++
			}
			mt.Views = max(mt.Views, s.View+1)
			mt.sessions = append(mt.sessions, ms)
		}
		out.tabs = append(out.tabs, mt)
	}
	return out, nil
}

// mcpWorkspaces reads the named workspace, or every running one when ws is empty.
func mcpWorkspaces(ws string) ([]*mcpWorkspace, error) {
	if ws != "" {
		w, err := mcpLoad(resolveWorkspace(ws))
		if err != nil {
			return nil, err
		}
		return []*mcpWorkspace{w}, nil
	}
	var out []*mcpWorkspace
	for _, n := range runningWorkspaces() {
		if w, err := mcpLoad(n); err == nil {
			out = append(out, w)
		}
	}
	return out, nil
}

// findTab picks a tab by its 1-based index or its title (case-insensitive).
func (w *mcpWorkspace) findTab(ref string) (*mcpTab, error) {
	if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(w.tabs) {
		return &w.tabs[n-1], nil
	}
	for i := range w.tabs {
		if strings.EqualFold(w.tabs[i].Title, ref) {
			return &w.tabs[i], nil
		}
	}
	return nil, fmt.Errorf("no hay una pestaña %q en %s", ref, w.Name)
}

// findSession resolves a session by id (or its first characters) or by name, optionally within
// a workspace and a tab. An ambiguous name is an error that lists the candidates.
func findSession(a map[string]any) (*mcpSession, error) {
	ref := argStr(a, "sesion")
	if ref == "" {
		return nil, fmt.Errorf("falta la sesión (su id o su nombre)")
	}
	wss, err := mcpWorkspaces(argStr(a, "workspace"))
	if err != nil {
		return nil, err
	}
	tabRef := argStr(a, "pestana")
	var byID, byName []mcpSession
	for _, w := range wss {
		tabs := w.tabs
		if tabRef != "" {
			t, err := w.findTab(tabRef)
			if err != nil {
				continue
			}
			tabs = []mcpTab{*t}
		}
		for _, t := range tabs {
			for _, s := range t.sessions {
				if s.ID == ref || (len(ref) >= 4 && strings.HasPrefix(s.ID, ref)) {
					byID = append(byID, s)
				} else if strings.EqualFold(s.Name, ref) {
					byName = append(byName, s)
				}
			}
		}
	}
	found := byID
	if len(found) == 0 {
		found = byName
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no encontré la sesión %q", ref)
	case 1:
		if found[0].pane == "" {
			return nil, fmt.Errorf("la sesión %q no tiene un panel abierto", ref)
		}
		return &found[0], nil
	}
	var c []string
	for _, s := range found {
		c = append(c, fmt.Sprintf("%s (id %s, %s › %s)", s.Name, s.ID[:min(8, len(s.ID))], s.Workspace, s.Tab))
	}
	return nil, fmt.Errorf("hay varias sesiones %q: %s. Indica el id, el workspace o la pestaña", ref, strings.Join(c, "; "))
}

// screenOf returns the visible screen of a session, or its last lines when lines > 0, without
// the trailing blank lines.
func screenOf(s *mcpSession, lines int) (string, error) {
	t := Tmux{WS: s.Workspace}
	args := []string{"capture-pane", "-p", "-J", "-t", s.pane}
	if lines > 0 {
		args = append(args, "-S", strconv.Itoa(-lines))
	}
	text, err := t.Run(args...)
	if err != nil {
		return "", err
	}
	rows := strings.Split(text, "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return strings.Join(rows, "\n"), nil
}

var optionLineRe = regexp.MustCompile(`^\s*(?:[❯>›]\s*)?(\d{1,2})\.\s+(\S.*)$`)

// pendingQuestion reads the dialog a session waits on: its text and numbered options.
func pendingQuestion(s *mcpSession) (map[string]any, error) {
	text, err := screenOf(s, 0)
	if err != nil {
		return nil, err
	}
	rows := strings.Split(text, "\n")
	start := max(0, len(rows)-40)
	rows = rows[start:]
	var opts []map[string]any
	selected := 0
	for _, r := range rows {
		if m := optionLineRe.FindStringSubmatch(r); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n == 1 {
				opts, selected = nil, 0 // a new list starts: keep only the last one on screen
			}
			opts = append(opts, map[string]any{"n": n, "texto": strings.TrimSpace(m[2])})
			if strings.ContainsAny(strings.SplitN(r, m[1]+".", 2)[0], "❯>›") {
				selected = n
			}
		}
	}
	out := map[string]any{"sesion": s.Name, "id": s.ID, "estado": s.Status, "pantalla": strings.Join(rows, "\n")}
	if s.Status != "pregunta" || !dialogOpenRe.MatchString(text) {
		out["aviso"] = "la sesión no está esperando una respuesta ahora"
	}
	if len(opts) > 0 {
		out["opciones"] = opts
		out["seleccionada"] = selected
	}
	return out, nil
}

func mcpReadTools() []mcpTool {
	wsProp := [2]string{"string", "Workspace (nombre o título). Sin él: todos los abiertos."}
	tabProp := [2]string{"string", "Pestaña: su número (1, 2…) o su título."}
	sesProp := [2]string{"string", "Sesión: su id (o sus primeros caracteres) o su nombre."}
	return []mcpTool{
		{Name: "listar_workspaces", Description: "Workspaces abiertos (con sus pestañas y cuántas sesiones hay en cada estado) y los guardados que están cerrados.",
			InputSchema: schema(nil, nil), run: func(a map[string]any) (any, error) {
				wss, _ := mcpWorkspaces("")
				open := map[string]bool{}
				var list []map[string]any
				for _, w := range wss {
					open[w.Name] = true
					var tabs []string
					for _, t := range w.tabs {
						tabs = append(tabs, t.Title)
					}
					list = append(list, map[string]any{"name": w.Name, "title": w.Title, "running": true, "app": w.App,
						"dir": w.Dir, "counts": w.Counts, "tabs": tabs})
				}
				for _, n := range savedWorkspaces() {
					if open[n] {
						continue
					}
					item := map[string]any{"name": n, "running": false}
					if w, err := loadWorkspace(n); err == nil {
						item["title"], item["tabs_guardadas"] = w.Title, len(w.Tabs)
					}
					list = append(list, item)
				}
				return list, nil
			}},
		{Name: "listar_pestanas", Description: "Pestañas de un workspace abierto: número, título, carpeta, cuántas vistas tiene y sesiones por estado.",
			InputSchema: schema([]string{"workspace"}, map[string][2]string{"workspace": wsProp}),
			run: func(a map[string]any) (any, error) {
				wss, err := mcpWorkspaces(argStr(a, "workspace"))
				if err != nil || len(wss) != 1 {
					return nil, fmt.Errorf("indica un workspace abierto")
				}
				return wss[0].tabs, nil
			}},
		{Name: "listar_vistas", Description: "Vistas de una pestaña (sesiones que se ven lado a lado) con las sesiones de cada una.",
			InputSchema: schema([]string{"workspace", "pestana"}, map[string][2]string{"workspace": wsProp, "pestana": tabProp}),
			run: func(a map[string]any) (any, error) {
				wss, err := mcpWorkspaces(argStr(a, "workspace"))
				if err != nil || len(wss) != 1 {
					return nil, fmt.Errorf("indica un workspace abierto")
				}
				t, err := wss[0].findTab(argStr(a, "pestana"))
				if err != nil {
					return nil, err
				}
				views := make([][]mcpSession, t.Views)
				for _, s := range t.sessions {
					views[s.View-1] = append(views[s.View-1], s)
				}
				var out []map[string]any
				for i, v := range views {
					out = append(out, map[string]any{"view": i + 1, "sessions": v})
				}
				return out, nil
			}},
		{Name: "listar_sesiones", Description: "Sesiones con su estado (trabajando, pregunta, lista, suspendida), alias, carpeta, pestaña y vista. " +
			"Filtra por workspace, pestaña o estado; sin filtros lista todas las de todos los workspaces abiertos.",
			InputSchema: schema(nil, map[string][2]string{"workspace": wsProp, "pestana": tabProp,
				"estado": {"string", "Solo las de este estado: trabajando, pregunta, lista o suspendida."}}),
			run: func(a map[string]any) (any, error) {
				wss, err := mcpWorkspaces(argStr(a, "workspace"))
				if err != nil {
					return nil, err
				}
				out := []mcpSession{}
				for _, w := range wss {
					tabs := w.tabs
					if ref := argStr(a, "pestana"); ref != "" {
						t, err := w.findTab(ref)
						if err != nil {
							return nil, err
						}
						tabs = []mcpTab{*t}
					}
					for _, t := range tabs {
						for _, s := range t.sessions {
							if st := argStr(a, "estado"); st == "" || st == s.Status {
								out = append(out, s)
							}
						}
					}
				}
				return out, nil
			}},
		{Name: "leer_sesion", Description: "Lo que muestra una sesión: su pantalla, o sus últimas N líneas con 'lineas'.",
			InputSchema: schema([]string{"sesion"}, map[string][2]string{"sesion": sesProp, "workspace": wsProp, "pestana": tabProp,
				"lineas": {"integer", "Cuántas líneas hacia atrás (máximo 500). Sin él: la pantalla visible."}}),
			run: func(a map[string]any) (any, error) {
				s, err := findSession(a)
				if err != nil {
					return nil, err
				}
				text, err := screenOf(s, min(500, max(0, argInt(a, "lineas", 0))))
				if err != nil {
					return nil, err
				}
				return fmt.Sprintf("[%s · %s › %s · %s]\n%s", s.Name, s.Workspace, s.Tab, s.Status, text), nil
			}},
		{Name: "ver_pregunta", Description: "La pregunta o el permiso que una sesión espera que le respondan, con sus opciones numeradas.",
			InputSchema: schema([]string{"sesion"}, map[string][2]string{"sesion": sesProp, "workspace": wsProp, "pestana": tabProp}),
			run: func(a map[string]any) (any, error) {
				s, err := findSession(a)
				if err != nil {
					return nil, err
				}
				return pendingQuestion(s)
			}},
		{Name: "preguntas_pendientes", Description: "Todas las sesiones que esperan una respuesta, con su pregunta y sus opciones.",
			InputSchema: schema(nil, map[string][2]string{"workspace": wsProp}),
			run: func(a map[string]any) (any, error) {
				wss, err := mcpWorkspaces(argStr(a, "workspace"))
				if err != nil {
					return nil, err
				}
				out := []map[string]any{}
				for _, w := range wss {
					for _, t := range w.tabs {
						for _, s := range t.sessions {
							if s.Status == "pregunta" && s.pane != "" {
								if q, err := pendingQuestion(&s); err == nil {
									q["workspace"], q["pestana"] = s.Workspace, s.Tab
									out = append(out, q)
								}
							}
						}
					}
				}
				return out, nil
			}},
		{Name: "conversaciones", Description: "Busca conversaciones de Claude que se pueden reanudar, por id o por texto de su título.",
			InputSchema: schema(nil, map[string][2]string{"buscar": {"string", "Id o parte de él; vacío: las más recientes."}}),
			run: func(a map[string]any) (any, error) {
				cfg, err := loadConfig()
				if err != nil {
					return nil, err
				}
				var found []foundConv
				if q := argStr(a, "buscar"); q != "" {
					found = findConversations(q, cfg)
				} else {
					found = recentEverywhere(cfg)
				}
				out := []map[string]any{}
				for i, f := range found {
					if i >= 15 {
						break
					}
					out = append(out, map[string]any{"id": f.conv.ID, "titulo": conversationTitle(f.conv), "alias": f.conv.Alias,
						"carpeta": f.cwd, "modificada": f.conv.Modified.Format("2006-01-02 15:04")})
				}
				return out, nil
			}},
		{Name: "uso", Description: "Cuota usada de cada cuenta de Claude (ventana de 5 horas y semanal), qué alias usa cada una y cuándo se reinicia.",
			InputSchema: schema(nil, nil), run: func(a map[string]any) (any, error) {
				cfg, err := loadConfig()
				if err != nil {
					return nil, err
				}
				acc := claudeUsageWith(cfg, false)
				sortAccounts(acc)
				return acc, nil
			}},
		{Name: "recursos", Description: "Memoria y CPU de cada sesión de un workspace, y la memoria del equipo.",
			InputSchema: schema([]string{"workspace"}, map[string][2]string{"workspace": wsProp}),
			run: func(a map[string]any) (any, error) {
				wss, err := mcpWorkspaces(argStr(a, "workspace"))
				if err != nil || len(wss) != 1 {
					return nil, fmt.Errorf("indica un workspace abierto")
				}
				w := wss[0]
				tabOf := map[string]string{}
				for _, t := range w.tabs {
					for _, s := range t.sessions {
						tabOf[s.ID] = t.Title
					}
				}
				t := Tmux{WS: w.Name}
				return workspaceResources(w.Name, func(session, title string) string {
					if title != "" {
						return title
					}
					if id, ok := strings.CutPrefix(session, slotPrefix); ok {
						return tabOf[id]
					}
					v, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
					return v
				}), nil
			}},
		{Name: "alias", Description: "Alias de Claude configurados: nombre, argumentos y la cuenta (carpeta de configuración) que usa cada uno.",
			InputSchema: schema(nil, nil), run: func(a map[string]any) (any, error) {
				cfg, err := loadConfig()
				if err != nil {
					return nil, err
				}
				var out []map[string]any
				for _, al := range cfg.claudeAliases() {
					// Only the names of the environment variables: their values can hold secrets.
					var env []string
					for _, kv := range al.Env {
						k, _, _ := strings.Cut(kv, "=")
						env = append(env, k)
					}
					out = append(out, map[string]any{"name": al.Name, "args": al.Args, "env": env, "cuenta": shortPath(cfg.accountDir(al))})
				}
				return out, nil
			}},
	}
}

// recentEverywhere lists the latest conversations started in the folders of the open tabs.
func recentEverywhere(cfg Config) []foundConv {
	seen := map[string]bool{}
	var out []foundConv
	wss, _ := mcpWorkspaces("")
	for _, w := range wss {
		for _, t := range w.tabs {
			if seen[t.Dir] {
				continue
			}
			seen[t.Dir] = true
			for _, c := range recentConversations(t.Dir, 10, nil, cfg) {
				out = append(out, foundConv{conv: c, cwd: t.Dir})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].conv.Modified.After(out[j].conv.Modified) })
	return out
}

// conversationTitle names a conversation by its title or its first prompt.
func conversationTitle(c recentConv) string {
	if c.Title != "" {
		return c.Title
	}
	return shorten(c.First, 80)
}
