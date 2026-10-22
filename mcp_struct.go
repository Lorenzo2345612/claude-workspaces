package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The tools that change the structure: sessions, tabs, views and workspaces. In a workspace
// opened by the app they go through the app's server (appDo), which holds the workspace's lock
// and redraws the window; in a terminal workspace they use the terminal version's own steps.

var errAppOnly = errors.New("eso solo se puede en los workspaces abiertos en la app")

// mcpTarget is what an action works on, resolved from the tool's arguments.
type mcpTarget struct {
	ws   *mcpWorkspace
	tab  *mcpTab
	sess *mcpSession
}

func (m mcpTarget) tmux() Tmux { return Tmux{WS: m.ws.Name} }

// targetSession resolves the session an action names, with its workspace and tab.
func targetSession(a map[string]any) (mcpTarget, error) {
	s, err := findSession(a)
	if err != nil {
		return mcpTarget{}, err
	}
	w, err := mcpLoad(s.Workspace)
	if err != nil {
		return mcpTarget{}, err
	}
	return mcpTarget{ws: w, tab: &w.tabs[s.TabIndex-1], sess: s}, nil
}

// targetTab resolves the workspace and the tab an action names.
func targetTab(a map[string]any, needTab bool) (mcpTarget, error) {
	ref := argStr(a, "workspace")
	if ref == "" {
		running := runningWorkspaces()
		if len(running) != 1 {
			return mcpTarget{}, errors.New("indica el workspace")
		}
		ref = running[0]
	}
	w, err := mcpLoad(resolveWorkspace(ref))
	if err != nil {
		return mcpTarget{}, err
	}
	m := mcpTarget{ws: w}
	if ref := argStr(a, "pestana"); ref != "" || needTab {
		if ref == "" {
			return m, errors.New("indica la pestaña (número o título)")
		}
		if m.tab, err = w.findTab(ref); err != nil {
			return m, err
		}
	}
	return m, nil
}

// do sends an action to the app's server for this workspace.
func (m mcpTarget) do(action string, fields map[string]any) (map[string]any, error) {
	q := map[string]any{"action": action, "ws": m.ws.Name}
	for k, v := range fields {
		q[k] = v
	}
	return appDo(q)
}

func (m mcpTarget) viewID(a map[string]any, key string) (string, error) {
	n := argInt(a, key, 0)
	if m.tab == nil || n < 1 || n > len(m.tab.viewIDs) {
		return "", fmt.Errorf("indica la vista con su número (1 a %d) dentro de la pestaña", len(m.tab.viewIDs))
	}
	return m.tab.viewIDs[n-1], nil
}

// switchAlias moves a Claude session to another alias: it restarts in its conversation, and a
// session that was working gets the continue message.
func switchAlias(ws, pane, alias string) error {
	t := Tmux{WS: ws}
	cur, _ := t.Run("display", "-p", "-t", pane, "#{@cw_status}")
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	source, err := paneSlot(t, pane)
	if err != nil {
		return err
	}
	if err := checkAgentAlias(cfg, source.ClaudeAlias, alias); err != nil {
		return err
	}
	if _, err := t.Run("set", "-p", "-t", pane, "@cw_claude_alias", alias); err != nil {
		return err
	}
	return respawnSlot(ws, pane, cur == statusBusy || cur == statusWaiting)
}

func checkAlias(name string) (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	for _, a := range cfg.claudeAliases() {
		if strings.EqualFold(a.Name, name) {
			return a.Name, nil
		}
	}
	var names []string
	for _, a := range cfg.claudeAliases() {
		names = append(names, a.Name)
	}
	return "", fmt.Errorf("no hay un alias %q; los configurados son: %s", name, strings.Join(names, ", "))
}

func sessionAction(a map[string]any) (any, error) {
	action := argStr(a, "accion")
	if action == "nueva" {
		return newSession(a)
	}
	if action == "reabrir" {
		m, err := targetTab(a, false)
		if err != nil {
			return nil, err
		}
		if m.ws.App {
			return m.do("slot.reopen", nil)
		}
		c, ok := popClosed(m.tmux())
		if !ok {
			return nil, errors.New("no hay sesiones cerradas para reabrir")
		}
		session := c.Tab
		if session == "" || m.tmux().Command("has-session", "-t", "="+session).Run() != nil {
			if m.tab == nil {
				return nil, errors.New("indica en qué pestaña reabrirla")
			}
			session = m.tab.session
		}
		return "Reabierta: " + c.Slot.Alias, openInTab(m.ws.Name, session, c.Slot, c.Slot.Kind == kindClaude && c.Slot.SessionID != "")
	}
	m, err := targetSession(a)
	if err != nil {
		return nil, err
	}
	s, t := m.sess, m.tmux()
	switch action {
	case "cerrar":
		if m.ws.App {
			return m.do("slot.close", map[string]any{"slot": s.ID})
		}
		return "Cerrada: " + s.Name, closeWindow(m.ws.Name, s.pane)
	case "renombrar":
		name := cleanLabel(argStr(a, "nombre"))
		if name == "" {
			return nil, errors.New("falta el nombre nuevo")
		}
		if m.ws.App {
			return m.do("slot.rename", map[string]any{"slot": s.ID, "name": name})
		}
		return "Renombrada a " + name, setSlotName(t, s.pane, name)
	case "alias":
		if s.Kind != kindClaude {
			return nil, errors.New("una terminal no tiene alias de Claude")
		}
		alias, err := checkAlias(argStr(a, "alias"))
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("%s ahora usa %s (se reinició en su conversación)", s.Name, alias), switchAlias(m.ws.Name, s.pane, alias)
	case "reiniciar":
		return "Reiniciada: " + s.Name, respawnSlot(m.ws.Name, s.pane, false)
	case "suspender":
		return "Suspendida: " + s.Name, suspendPane(m.ws.Name, s.pane)
	case "reanudar":
		return "Reanudada: " + s.Name, wakePane(m.ws.Name, s.pane)
	case "enfocar":
		if m.ws.App {
			return m.do("slot.focus", map[string]any{"slot": s.ID})
		}
		_, err := t.Run("select-window", "-t", s.pane, ";", "select-pane", "-t", s.pane)
		return "Enfocada: " + s.Name, err
	case "separar":
		if !m.ws.App {
			return nil, errAppOnly
		}
		return m.do("slot.split", map[string]any{"slot": s.ID})
	}
	return nil, fmt.Errorf("acción desconocida %q", action)
}

func newSession(a map[string]any) (any, error) {
	m, err := targetTab(a, true)
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	kind := kindClaude
	if argStr(a, "tipo") == "codex" {
		kind = "codex"
	}
	if k := argStr(a, "tipo"); k == "terminal" || k == kindShell {
		kind = kindShell
	}
	name := cleanLabel(argStr(a, "nombre"))
	if name == "" {
		if kind == kindShell {
			name = "terminal"
		} else {
			name = m.tab.Title
		}
	}
	alias := ""
	if kind != kindShell && argStr(a, "alias") != "" {
		if alias, err = checkAlias(argStr(a, "alias")); err != nil {
			return nil, err
		}
	}
	kind, alias, err = cfg.newAgentSlot(kind, alias)
	if err != nil {
		return nil, err
	}
	dir := argStr(a, "carpeta")
	if m.ws.App {
		f := map[string]any{"tab": m.tab.id, "kind": kind, "name": name, "alias": alias, "args": argStr(a, "args"),
			"dir": dir, "resume": argStr(a, "conversacion")}
		if argInt(a, "vista", 0) > 0 {
			v, err := m.viewID(a, "vista")
			if err != nil {
				return nil, err
			}
			f["view"] = v
		}
		return m.do("slot.new", f)
	}
	s := Slot{Kind: kind, Alias: name, Cwd: tabDir(m.tmux(), m.tab.session), ClaudeAlias: alias, Args: splitArgs(argStr(a, "args"))}
	if dir != "" {
		if s.Cwd, err = checkDir(dir); err != nil {
			return nil, err
		}
	}
	resuming := false
	if id := argStr(a, "conversacion"); id != "" && kind == kindClaude {
		cfg, _ := loadConfig()
		found := findConversations(id, cfg)
		if len(found) == 0 {
			return nil, errors.New("no encontré esa conversación")
		}
		s.SessionID, resuming = found[0].conv.ID, true
		if dir == "" && isDir(found[0].cwd) {
			s.Cwd = found[0].cwd
		}
		if cfg.claudeAlias(s.ClaudeAlias).isCodex() {
			source := s
			source.ClaudeAlias = found[0].conv.Alias
			s, err = handoffSlot(source, "codex", cfg)
			if err != nil {
				return nil, err
			}
			resuming = false
		}
	}
	if err := openInTab(m.ws.Name, m.tab.session, s, resuming); err != nil {
		return nil, err
	}
	return fmt.Sprintf("Abrí %s en %s › %s", name, m.ws.Name, m.tab.Title), nil
}

func tabAction(a map[string]any) (any, error) {
	action := argStr(a, "accion")
	m, err := targetTab(a, action != "nueva")
	if err != nil {
		return nil, err
	}
	t := m.tmux()
	switch action {
	case "nueva":
		dir := argStr(a, "carpeta")
		if dir == "" {
			dir = m.ws.Dir
		}
		if dir == "" && len(m.ws.tabs) > 0 {
			dir = m.ws.tabs[0].Dir
		}
		if dir == "" {
			dir = homeDir()
		}
		if dir, err = checkDir(dir); err != nil {
			return nil, err
		}
		title := cleanLabel(argStr(a, "titulo"))
		if title == "" {
			title = baseName(dir)
		}
		start := ""
		switch argStr(a, "iniciar") {
		case "claude", "codex":
			start = argStr(a, "iniciar")
		case "terminal", kindShell:
			start = kindShell
		}
		alias := ""
		if (start == kindClaude || start == "codex") && argStr(a, "alias") != "" {
			if alias, err = checkAlias(argStr(a, "alias")); err != nil {
				return nil, err
			}
		}
		if m.ws.App {
			return m.do("tab.new", map[string]any{"dir": dir, "title": title, "start": start, "name": argStr(a, "nombre"), "alias": alias})
		}
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}
		l, err := newLauncher(m.ws.Name, cfg)
		if err != nil {
			return nil, err
		}
		start, alias, err = cfg.newAgentSlot(start, alias)
		if err != nil {
			return nil, err
		}
		tab := Tab{Title: title, Dir: dir}
		switch start {
		case kindClaude:
			name := cleanLabel(argStr(a, "nombre"))
			if name == "" {
				name = title
			}
			tab.Slots = []Slot{{Kind: kindClaude, Alias: name, Cwd: dir, ClaudeAlias: alias}}
		case kindShell:
			tab.Slots = []Slot{{Kind: kindShell, Alias: "shell", Cwd: dir}}
		}
		return "Pestaña nueva: " + title, l.openTab(t, t.nextTabSession(), &tab, false)
	case "renombrar":
		title := cleanLabel(argStr(a, "titulo"))
		if title == "" {
			return nil, errors.New("falta el título nuevo")
		}
		if m.ws.App {
			return m.do("tab.rename", map[string]any{"tab": m.tab.id, "title": title})
		}
		return "Renombrada a " + title, t.setTabOptions(m.tab.session, Tab{Title: title, Dir: tabDir(t, m.tab.session)})
	case "carpeta":
		dir, err := checkDir(argStr(a, "carpeta"))
		if err != nil {
			return nil, err
		}
		if m.ws.App {
			return m.do("tab.dir", map[string]any{"tab": m.tab.id, "dir": dir})
		}
		return "Carpeta de la pestaña: " + shortPath(dir), t.setTabOptions(m.tab.session, Tab{Title: m.tab.Title, Dir: dir})
	case "cerrar":
		if m.ws.App {
			return m.do("tab.close", map[string]any{"tab": m.tab.id})
		}
		return "Cerrada: " + m.tab.Title, closeTab(m.ws.Name, m.tab.session, "")
	case "suspender", "reanudar":
		n := 0
		for _, s := range m.tab.sessions {
			if s.Kind != kindClaude || s.pane == "" {
				continue
			}
			var err error
			if action == "suspender" && s.Status != "suspendida" {
				err = suspendPane(m.ws.Name, s.pane)
			} else if action == "reanudar" && s.Status == "suspendida" {
				err = wakePane(m.ws.Name, s.pane)
			} else {
				continue
			}
			if err != nil {
				return nil, err
			}
			n++
		}
		return fmt.Sprintf("%s › %s: %s %d sesiones", m.ws.Name, m.tab.Title, map[string]string{"suspender": "suspendí", "reanudar": "reanudé"}[action], n), nil
	case "alias":
		alias, err := checkAlias(argStr(a, "alias"))
		if err != nil {
			return nil, err
		}
		n := 0
		for _, s := range m.tab.sessions {
			if s.Kind == kindClaude && s.pane != "" && s.Alias != alias && s.Status != "suspendida" {
				if err := switchAlias(m.ws.Name, s.pane, alias); err != nil {
					return nil, err
				}
				n++
			}
		}
		return fmt.Sprintf("%d sesiones de %s ahora usan %s", n, m.tab.Title, alias), nil
	case "seleccionar":
		if !m.ws.App {
			return nil, errAppOnly
		}
		return m.do("tab.select", map[string]any{"tab": m.tab.id})
	case "mover":
		if !m.ws.App {
			return nil, errAppOnly
		}
		to := argInt(a, "posicion", 0)
		if to < 1 || to > len(m.ws.tabs) {
			return nil, fmt.Errorf("la posición va de 1 a %d", len(m.ws.tabs))
		}
		return m.do("tab.move", map[string]any{"tab": m.tab.id, "to": to - 1})
	}
	return nil, fmt.Errorf("acción desconocida %q", action)
}

func viewAction(a map[string]any) (any, error) {
	m, err := targetTab(a, true)
	if err != nil {
		return nil, err
	}
	if !m.ws.App {
		return nil, errAppOnly
	}
	v, err := m.viewID(a, "vista")
	if err != nil {
		return nil, err
	}
	switch action := argStr(a, "accion"); action {
	case "seleccionar":
		return m.do("view.select", map[string]any{"view": v})
	case "cerrar":
		return m.do("view.close", map[string]any{"view": v})
	case "mover":
		to := argInt(a, "posicion", 0)
		if to < 1 || to > len(m.tab.viewIDs) {
			return nil, fmt.Errorf("la posición va de 1 a %d", len(m.tab.viewIDs))
		}
		return m.do("view.move", map[string]any{"view": v, "to": to - 1})
	case "unir":
		target, err := m.viewID(a, "con_vista")
		if err != nil {
			return nil, err
		}
		return m.do("view.join", map[string]any{"view": v, "target": target})
	case "disposicion":
		layout := map[string]string{"columnas": layoutCols, "filas": layoutRows, "cuadricula": layoutGrid}[argStr(a, "disposicion")]
		if layout == "" {
			return nil, errors.New("la disposición es columnas, filas o cuadricula")
		}
		return m.do("view.layout", map[string]any{"view": v, "layout": layout})
	case "a_pestana":
		dst, err := m.ws.findTab(argStr(a, "a_pestana"))
		if err != nil {
			return nil, err
		}
		return m.do("view.totab", map[string]any{"view": v, "tab": dst.id})
	default:
		return nil, fmt.Errorf("acción desconocida %q", action)
	}
}

func workspaceAction(a map[string]any) (any, error) {
	action, ref := argStr(a, "accion"), argStr(a, "workspace")
	switch action {
	case "nuevo":
		dir, err := checkDir(argStr(a, "carpeta"))
		if err != nil {
			return nil, err
		}
		return appDo(map[string]any{"action": "ws.new", "dir": dir, "title": argStr(a, "titulo")})
	case "abrir":
		ws := resolveWorkspace(ref)
		if err := validWorkspaceName(ws); err != nil {
			return nil, err
		}
		if t := (Tmux{WS: ws}); t.Running() {
			return "Ya está abierto: " + ws, nil
		}
		return appDo(map[string]any{"action": "ws.open", "ws": ws})
	}
	if ref == "" {
		return nil, errors.New("indica el workspace")
	}
	m, err := targetTab(a, false)
	if err != nil {
		return nil, err
	}
	switch action {
	case "apagar":
		if m.ws.App {
			return m.do("ws.stop", nil)
		}
		return "Apagado y guardado: " + m.ws.Name, cmdStop(m.ws.Name)
	case "renombrar":
		return "Renombrado", renameWorkspace(m.ws.Name, argStr(a, "titulo"))
	case "carpeta":
		dir, err := checkDir(argStr(a, "carpeta"))
		if err != nil {
			return nil, err
		}
		_, err = m.tmux().Run("set", "-g", optWSDir, dir)
		return "Carpeta del workspace: " + shortPath(dir), err
	case "guardar":
		saveNow(m.ws.Name)
		return "Guardado: " + m.ws.Name, nil
	case "alias":
		alias, err := checkAlias(argStr(a, "alias"))
		if err != nil {
			return nil, err
		}
		n := 0
		for _, tab := range m.ws.tabs {
			for _, s := range tab.sessions {
				if s.Kind == kindClaude && s.pane != "" && s.Alias != alias && s.Status != "suspendida" {
					if err := switchAlias(m.ws.Name, s.pane, alias); err != nil {
						return nil, err
					}
					n++
				}
			}
		}
		return strconv.Itoa(n) + " sesiones ahora usan " + alias, nil
	}
	return nil, fmt.Errorf("acción desconocida %q", action)
}

func enumProp(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}

func mcpStructTools() []mcpTool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	obj := func(req []string, props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": req}
	}
	ws, tab, ses := str("Workspace (nombre o título)."), str("Pestaña: su número o su título."), str("Sesión: id o nombre.")
	return []mcpTool{
		{Name: "sesion", Description: "Maneja una sesión. nueva: abre una sesión con tipo=claude, tipo=codex o tipo=terminal en una pestaña, " +
			"opcionalmente reanudando una conversación o dentro de una vista. cerrar, renombrar, alias (la reinicia con otro alias de Claude " +
			"en su misma conversación), reiniciar, suspender, reanudar, enfocar, separar (la saca a una vista propia), reabrir (la última cerrada).",
			InputSchema: obj([]string{"accion"}, map[string]any{
				"accion": enumProp("Qué hacer.", "nueva", "cerrar", "renombrar", "alias", "reiniciar", "suspender", "reanudar", "enfocar", "separar", "reabrir"),
				"sesion": ses, "workspace": ws, "pestana": tab,
				"nombre": str("Nombre de la sesión (nueva, renombrar)."), "tipo": enumProp("Tipo de sesión nueva.", "claude", "codex", "terminal"),
				"alias": str("Alias del agente (nueva, alias)."), "args": str("Argumentos extra del agente (nueva)."),
				"carpeta":      str("Carpeta donde empieza (nueva); por defecto la de la pestaña."),
				"conversacion": str("Id de una conversación a reanudar (nueva)."), "vista": num("Vista (número) donde abrirla (nueva, solo app)."),
			}), run: sessionAction},
		{Name: "pestana", Description: "Maneja una pestaña. nueva (con titulo, carpeta e iniciar=claude|codex|terminal para abrirla con una sesión), " +
			"renombrar, carpeta (donde empiezan sus sesiones nuevas), cerrar (cierra todas sus sesiones), suspender o reanudar todas sus sesiones, " +
			"alias (todas sus sesiones de Claude a otro alias), seleccionar y mover (solo app).",
			InputSchema: obj([]string{"accion"}, map[string]any{
				"accion":    enumProp("Qué hacer.", "nueva", "renombrar", "carpeta", "cerrar", "suspender", "reanudar", "alias", "seleccionar", "mover"),
				"workspace": ws, "pestana": tab, "titulo": str("Título (nueva, renombrar)."), "carpeta": str("Carpeta (nueva, carpeta)."),
				"iniciar": enumProp("Sesión con la que abre una pestaña nueva.", "claude", "codex", "terminal", "ninguna"),
				"nombre":  str("Nombre de la sesión con la que abre (nueva)."), "alias": str("Alias del agente (nueva, alias)."),
				"posicion": num("Posición nueva, desde 1 (mover)."),
			}), run: tabAction},
		{Name: "vista", Description: "Maneja una vista de una pestaña (solo workspaces de la app): seleccionar, cerrar (cierra sus sesiones), " +
			"mover, unir (muestra sus sesiones junto a las de con_vista), disposicion (columnas, filas, cuadricula), a_pestana (la pasa a otra pestaña).",
			InputSchema: obj([]string{"accion", "pestana", "vista"}, map[string]any{
				"accion":    enumProp("Qué hacer.", "seleccionar", "cerrar", "mover", "unir", "disposicion", "a_pestana"),
				"workspace": ws, "pestana": tab, "vista": num("Número de la vista en la pestaña."),
				"con_vista": num("Vista con la que se une (unir)."), "posicion": num("Posición nueva, desde 1 (mover)."),
				"disposicion": enumProp("Distribución (disposicion).", "columnas", "filas", "cuadricula"),
				"a_pestana":   str("Pestaña destino (a_pestana)."),
			}), run: viewAction},
		{Name: "workspace", Description: "Maneja un workspace. nuevo (en una carpeta, se abre en la app), abrir (uno guardado), apagar (lo guarda y cierra " +
			"todas sus sesiones), renombrar, carpeta (donde empiezan las pestañas nuevas), guardar, alias (todas sus sesiones de Claude a otro alias).",
			InputSchema: obj([]string{"accion"}, map[string]any{
				"accion":    enumProp("Qué hacer.", "nuevo", "abrir", "apagar", "renombrar", "carpeta", "guardar", "alias"),
				"workspace": ws, "titulo": str("Título (nuevo, renombrar)."), "carpeta": str("Carpeta (nuevo, carpeta)."),
				"alias": str("Alias de Claude (alias)."),
			}), run: workspaceAction},
	}
}
