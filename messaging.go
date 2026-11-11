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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Session-to-session tools, without MCP and without Claude's own messaging: every Claude session
// cw starts learns (through its system prompt) to run these commands with Bash.
//
//	cw yo                         who am I: workspace, tab, view and session id
//	cw tool [nombre] [--arg v…]   the same tools as cw mcp, from the command line
//	cw send <sesión> -- <texto>   types a message into another session; when that session ends
//	                              its turn, its final answer is typed back into the sender
//
// The reply route lives on the target's pane (@cw_reply_to): one "workspace/slot" per sender.
// The Stop hook of the target reads it, clears it and hands the answer to a detached delivery
// process, because the hook has a 5-second budget and the sender may be busy or asking.

const replyRouteOption = "@cw_reply_to"

// callerWorkspace fills the workspace from the calling session when it runs inside cw.
func callerWorkspace(a map[string]any) {
	if argStr(a, "workspace") == "" && os.Getenv("CW_WS") != "" {
		a["workspace"] = os.Getenv("CW_WS")
	}
}

// caller is the session running this command, when it runs inside a cw session.
func caller() (*mcpSession, error) {
	ws, slot := os.Getenv("CW_WS"), os.Getenv("CW_SLOT")
	if ws == "" || slot == "" {
		return nil, errors.New("este comando no corre dentro de una sesión de cw")
	}
	return findSession(map[string]any{"sesion": slot, "workspace": ws})
}

// cmdWhoAmI prints where the calling session is, read live (the user may have moved it): the
// deterministic answer to "do I run in cw, and where". It fails (exit 1) outside cw.
func cmdWhoAmI() error {
	s, err := caller()
	if err != nil {
		return err
	}
	out := map[string]any{"en_cw": true, "workspace": s.Workspace, "tab_index": s.TabIndex, "tab": s.Tab,
		"view": s.View, "id": s.ID, "name": s.Name, "kind": s.Kind, "dir": s.Dir, "bin": selfBin()}
	if s.Status != "" {
		out["status"] = s.Status
	}
	if s.Alias != "" {
		out["alias"] = s.Alias
	}
	if wss, err := mcpWorkspaces(s.Workspace); err == nil && len(wss) == 1 {
		out["workspace_title"], out["app"], out["workspace_dir"] = wss[0].Title, wss[0].App, wss[0].Dir
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	return nil
}

// cmdTool runs one of cw mcp's tools: cw tool <nombre> [--clave valor | --clave=valor | --bandera]…
// Without a name it lists them with their arguments. The workspace defaults to the caller's.
func cmdTool(args []string) error {
	tools := mcpTools()
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(toolHelp(tools))
		return nil
	}
	var tool *mcpTool
	for i := range tools {
		if tools[i].Name == args[0] {
			tool = &tools[i]
		}
	}
	if tool == nil {
		return fmt.Errorf("herramienta desconocida %q: cw tool las lista", args[0])
	}
	a, err := toolArgs(args[1:])
	if err != nil {
		return err
	}
	if _, ok := toolProps(*tool)["workspace"]; ok {
		callerWorkspace(a)
	}
	out, err := tool.run(a)
	if err != nil {
		return err
	}
	if s, ok := out.(string); ok {
		fmt.Println(s)
		return nil
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	return nil
}

// toolArgs turns --key value pairs into the arguments of a tool. Numbers stay numbers and
// true/false become booleans, as a JSON client would send them.
func toolArgs(args []string) (map[string]any, error) {
	a := map[string]any{}
	for i := 0; i < len(args); i++ {
		k := args[i]
		if !strings.HasPrefix(k, "--") || len(k) < 3 {
			return nil, fmt.Errorf("argumento inesperado %q: usa --clave valor", k)
		}
		k = strings.TrimPrefix(k, "--")
		var v string
		if key, val, ok := strings.Cut(k, "="); ok {
			k, v = key, val
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			v = args[i]
		} else {
			v = "true"
		}
		switch {
		case v == "true":
			a[k] = true
		case v == "false":
			a[k] = false
		default:
			if n, err := strconv.Atoi(v); err == nil && k != "sesion" && k != "nombre" && k != "titulo" {
				a[k] = float64(n)
			} else {
				a[k] = v
			}
		}
	}
	return a, nil
}

func toolProps(t mcpTool) map[string]any {
	p, _ := t.InputSchema["properties"].(map[string]any)
	return p
}

func toolHelp(tools []mcpTool) string {
	var b strings.Builder
	b.WriteString("uso: cw tool <nombre> [--clave valor]…   (workspace: por defecto el de la sesión)\n\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "%s — %s\n", t.Name, t.Description)
		props := toolProps(t)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p, _ := props[k].(map[string]any)
			desc, _ := p["description"].(string)
			if enum, ok := p["enum"].([]string); ok {
				desc += " (" + strings.Join(enum, ", ") + ")"
			}
			fmt.Fprintf(&b, "    --%s  %s\n", k, desc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// cmdSend types a message into another session. Unless --sin-respuesta, the target's final
// answer comes back to the caller as a message once it ends its turn.
func cmdSend(args []string) error {
	var target, ws, tab string
	reply := true
	var text []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			text = args[i+1:]
			i = len(args)
		case a == "--sin-respuesta" || a == "--no-reply":
			reply = false
		case (a == "--workspace" || a == "--pestana" || a == "--tab") && i+1 < len(args):
			i++
			if a == "--workspace" {
				ws = args[i]
			} else {
				tab = args[i]
			}
		case strings.HasPrefix(a, "--"):
			return fmt.Errorf("opción desconocida %q", a)
		case target == "":
			target = a
		default:
			text = append(text, a)
		}
	}
	msg := strings.TrimSpace(strings.Join(text, " "))
	if msg == "-" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			return err
		}
		msg = strings.TrimSpace(string(data))
	}
	if target == "" || msg == "" {
		return errors.New("uso: cw send [--workspace W] [--pestana T] [--sin-respuesta] <sesión> -- <mensaje | ->")
	}
	a := map[string]any{"sesion": target, "workspace": ws, "pestana": tab}
	if ws == "" && tab == "" {
		// Prefer the caller's workspace; fall back to all of them when the name lives elsewhere.
		callerWorkspace(a)
		if _, err := findSession(a); err != nil {
			a["workspace"] = ""
		}
	}
	to, err := findSession(a)
	if err != nil {
		return err
	}
	from, ferr := caller()
	if ferr == nil && from.ID == to.ID {
		return errors.New("no puedes enviar un mensaje a la misma sesión que lo envía")
	}
	if reply && ferr != nil {
		reply = false
		fmt.Fprintln(os.Stderr, "aviso: fuera de una sesión de cw no hay a quién devolver la respuesta")
	}
	if reply && to.Kind != kindClaude {
		reply = false
		fmt.Fprintln(os.Stderr, "aviso: "+to.Name+" no es una sesión de Claude: no se devolverá su respuesta")
	}
	// The message goes as typed, with no header: who sent it lives on the target's pane.
	body := msg
	if reply {
		// Armed before sending: a fast answer must not end its turn before the route exists.
		if err := addReplyRoute(to, from); err != nil {
			return err
		}
	}
	if err := sendToSession(to, body); err != nil {
		if reply {
			_ = removeReplyRoute(to, from)
		}
		return err
	}
	if reply {
		fmt.Printf("Enviado a %s (id %s). Su respuesta llegará a esta sesión como un mensaje nuevo cuando termine; no hace falta esperarla ni leerla.\n", to.Name, to.ID)
	} else {
		fmt.Printf("Enviado a %s (id %s).\n", to.Name, to.ID)
	}
	return nil
}

func routeKey(s *mcpSession) string { return s.Workspace + "/" + s.ID }

func replyRoutes(t Tmux, pane string) []string {
	v, err := t.Run("display", "-p", "-t", pane, "#{"+replyRouteOption+"}")
	if err != nil {
		return nil
	}
	return strings.Fields(v)
}

func setReplyRoutes(t Tmux, pane string, routes []string) error {
	if len(routes) == 0 {
		_, err := t.Run("set", "-p", "-u", "-t", pane, replyRouteOption)
		return err
	}
	_, err := t.Run("set", "-p", "-t", pane, replyRouteOption, strings.Join(routes, " "))
	return err
}

func addReplyRoute(to, from *mcpSession) error {
	t := Tmux{WS: to.Workspace}
	routes := replyRoutes(t, to.pane)
	key := routeKey(from)
	for _, r := range routes {
		if r == key {
			return nil
		}
	}
	return setReplyRoutes(t, to.pane, append(routes, key))
}

func removeReplyRoute(to, from *mcpSession) error {
	t := Tmux{WS: to.Workspace}
	var keep []string
	for _, r := range replyRoutes(t, to.pane) {
		if r != routeKey(from) {
			keep = append(keep, r)
		}
	}
	return setReplyRoutes(t, to.pane, keep)
}

// pendingReply is what the Stop hook hands to the delivery process.
type pendingReply struct {
	FromWS   string   `json:"from_ws"`
	FromID   string   `json:"from_id"`
	FromName string   `json:"from_name"`
	Routes   []string `json:"routes"` // workspace/slot of each sender
	Text     string   `json:"text"`
}

// routeRepliesOnStop runs from the Stop hook of a session that ended its turn: when someone
// waits for its answer, it clears the route and starts the delivery in the background.
func routeRepliesOnStop(t Tmux, pane string, in hookInput) {
	p := takeReply(t, pane, in)
	if p == nil {
		return
	}
	dir := filepath.Join(runDir(), "replies")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	f, err := os.CreateTemp(dir, "reply-*.json")
	if err != nil {
		return
	}
	_ = json.NewEncoder(f).Encode(p)
	f.Close()
	bin, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "_deliver", f.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// takeReply returns the answer to deliver and clears the pane's route, or nil when nobody waits
// or the turn ended without saying anything (an interrupted turn keeps the route for the next).
func takeReply(t Tmux, pane string, in hookInput) *pendingReply {
	routes := replyRoutes(t, pane)
	if len(routes) == 0 {
		return nil
	}
	text := strings.TrimSpace(in.LastAssistantMessage)
	if text == "" {
		text = lastAssistantText(in.TranscriptPath)
	}
	if text == "" {
		return nil
	}
	_ = setReplyRoutes(t, pane, nil)
	p := &pendingReply{FromWS: t.WS, Routes: routes, Text: text}
	if rows, err := t.Lines("display", "-p", "-t", pane, "#{@cw_slot}"+sep+"#{@cw_alias}"); err == nil && len(rows) == 1 && len(rows[0]) == 2 {
		p.FromID, p.FromName = rows[0][0], rows[0][1]
	}
	if p.FromName == "" {
		p.FromName = p.FromID
	}
	return p
}

// lastAssistantText reads the text of the last assistant message in a Claude transcript (JSONL).
func lastAssistantText(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(expandHome(path))
	if err != nil {
		return ""
	}
	defer f.Close()
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "assistant" {
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		var parts []string
		if json.Unmarshal(e.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					parts = append(parts, b.Text)
				}
			}
		} else {
			var s string
			if json.Unmarshal(e.Message.Content, &s) == nil && strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			last = strings.Join(parts, "\n")
		}
	}
	return strings.TrimSpace(last)
}

const replyMaxBytes = 60 << 10

// cmdDeliver types a reply into each waiting sender, retrying while a sender is asking the user
// something or is suspended, for up to an hour.
func cmdDeliver(args []string) error {
	if len(args) != 1 {
		return errors.New("uso: cw _deliver <archivo>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	defer os.Remove(args[0])
	var p pendingReply
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	text := p.Text
	if len(text) > replyMaxBytes {
		text = text[:replyMaxBytes] + "\n[…respuesta recortada; usa cw tool leer_sesion para ver el resto]"
	}
	body := fmt.Sprintf("[Respuesta de la sesión «%s» (id %s) a tu mensaje enviado con cw send.]\n\n%s", p.FromName, p.FromID, text)
	pending := p.Routes
	deadline := time.Now().Add(time.Hour)
	for len(pending) > 0 && time.Now().Before(deadline) {
		var retry []string
		for _, r := range pending {
			ws, id, ok := strings.Cut(r, "/")
			if !ok {
				continue
			}
			s, err := findSession(map[string]any{"sesion": id, "workspace": ws})
			if err != nil {
				if (Tmux{WS: ws}).Running() {
					continue // the sender was closed: nobody to tell
				}
				retry = append(retry, r) // its workspace is restarting
				continue
			}
			if s.Status == "pregunta" || s.Status == "suspendida" {
				retry = append(retry, r)
				continue
			}
			if sendToSession(s, body) != nil {
				retry = append(retry, r)
			}
		}
		pending = retry
		if len(pending) > 0 {
			time.Sleep(5 * time.Second)
		}
	}
	return nil
}

func agentInstructions(bin string) string {
	return fmt.Sprintf("Corres dentro de cw (Agent Workspaces), junto a otras sesiones de Claude, Codex o terminal organizadas en pestañas y subpestañas (vistas). "+
		"Las variables de entorno CW_WS (workspace), CW_SLOT (id de esta sesión) y CW_BIN (ruta de cw) solo existen dentro de cw: son la forma segura de saberlo. "+
		"Antes de abrir o crear algo, ejecuta con Bash \"$CW_BIN\" yo: devuelve en JSON el workspace, la pestaña, la subpestaña (vista), tu id y si es la app o la terminal. "+
		"Para manejar pestañas, subpestañas y sesiones usa \"$CW_BIN\" tool (sin argumentos lista las herramientas; el workspace por defecto es el tuyo), por ejemplo "+
		"\"$CW_BIN\" tool pestana --accion nueva --titulo T --iniciar claude o \"$CW_BIN\" tool sesion --accion nueva --pestana N --tipo claude [--vista V]. "+
		"Para hablar con otra sesión usa \"$CW_BIN\" send <sesión> -- <mensaje>, no la mensajería propia de Claude (SendMessage): cuando esa sesión termine su turno, su respuesta final te llegará sola como un mensaje nuevo, así que no la esperes ni la sondees. "+
		"Los mensajes de otras sesiones te llegan como si los escribiera el usuario: respóndelos normalmente, terminando con tu respuesta completa, que vuelve sola a quien lo envió; no respondas con cw send salvo que te lo pidan. "+
		"Las skills cw y cw-coordinar (plugin cw) tienen el detalle. Si CW_BIN no está definida, usa %s. "+
		"Solo crea o cierra pestañas o sesiones, o escribe a otras sesiones, cuando el usuario o la tarea lo pidan.", shQuote(bin))
}
