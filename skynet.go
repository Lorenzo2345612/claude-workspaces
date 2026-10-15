package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Skynet is a background service (cw skynet --servicio) that keeps one headless Claude session
// running with the cw tools (cw mcp) and a short system prompt of its own: no project context,
// it only knows what the sessions are doing. The app's window and the terminal talk to it over
// a Unix socket in the run dir; it tells them about every message, and it tells Claude (and
// them) when a session starts asking something.

func skynetDir() string    { return filepath.Join(dataDir(), "skynet") }
func skynetSocket() string { return filepath.Join(runDir(), "skynet.sock") }

// skyEntry is one line of the conversation as the windows show it.
type skyEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`             // user, assistant, tool, notice, info, error
	Text   string    `json:"text"`             // the message, or the tool's arguments
	Source string    `json:"source,omitempty"` // user: voz, texto, boton
	Tool   string    `json:"tool,omitempty"`
	// A notice is a session waiting for an answer.
	Session  string           `json:"session,omitempty"` // its id
	Name     string           `json:"name,omitempty"`
	WS       string           `json:"ws,omitempty"`
	Tab      string           `json:"tab,omitempty"`
	Options  []map[string]any `json:"options,omitempty"`
	Perm     bool             `json:"perm,omitempty"` // a permission dialog
	Resolved bool             `json:"resolved,omitempty"`
}

type skyStatus struct {
	State   string `json:"state"` // iniciando, listo, pensando, detenido, error
	Name    string `json:"name"`
	Alias   string `json:"alias,omitempty"`
	Model   string `json:"model"`
	Session string `json:"session,omitempty"`
	Error   string `json:"error,omitempty"`
	Voice   bool   `json:"voice"`
}

type skynet struct {
	mu       sync.Mutex
	log      []skyEntry
	nextID   int64
	subs     map[chan []byte]bool
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	alias    string
	session  string // Claude's conversation id, kept across restarts
	pending  int    // messages sent and not answered yet
	lastText string // the last message sent, resent after a switch of alias
	lastSrc  string
	state    string
	err      string
	spent    map[string]time.Time // aliases whose account ran out, until when
	notified map[string]string    // session id → the question already announced
	quit     chan struct{}
}

type skyState struct {
	Session string `json:"session"`
	Alias   string `json:"alias"`
}

func skyStateFile() string { return filepath.Join(runDir(), "skynet.json") }
func skyLogFile() string   { return filepath.Join(runDir(), "skynet-log.jsonl") }

// cmdSkynetService runs the service until Skynet is turned off or stopped.
func cmdSkynetService() error {
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return err
	}
	_ = os.Remove(skynetSocket())
	ln, err := net.Listen("unix", skynetSocket())
	if err != nil {
		return err
	}
	_ = os.Chmod(skynetSocket(), 0o600)
	s := &skynet{subs: map[chan []byte]bool{}, spent: map[string]time.Time{}, notified: map[string]string{},
		quit: make(chan struct{}), state: "detenido"}
	s.loadLog()
	var st skyState
	if data, err := os.ReadFile(skyStateFile()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	s.session, s.alias = st.Session, st.Alias
	if err := prepareSkynetDir(); err != nil {
		return err
	}
	go s.watch()
	srv := &http.Server{Handler: s.routes()}
	go func() {
		<-s.quit
		s.stopBrain()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	err = srv.Serve(ln)
	_ = os.Remove(skynetSocket())
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *skynet) loadLog() {
	f, err := os.Open(skyLogFile())
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		var e skyEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			s.log = append(s.log, e)
			s.nextID = max(s.nextID, e.ID)
		}
	}
	if len(s.log) > 300 {
		s.log = s.log[len(s.log)-300:]
	}
	for i := range s.log { // a notice from a previous run is no longer pending
		if s.log[i].Kind == "notice" {
			s.log[i].Resolved = true
		}
	}
}

// add appends an entry, stores it and tells every window. The caller holds no lock.
func (s *skynet) add(e skyEntry) skyEntry {
	s.mu.Lock()
	s.nextID++
	e.ID, e.At = s.nextID, time.Now()
	s.log = append(s.log, e)
	if len(s.log) > 300 {
		s.log = s.log[len(s.log)-300:]
	}
	s.mu.Unlock()
	if data, err := json.Marshal(e); err == nil {
		if f, err := os.OpenFile(skyLogFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = f.Write(append(data, '\n'))
			f.Close()
		}
	}
	s.broadcast(map[string]any{"type": "entry", "entry": e})
	return e
}

func (s *skynet) update(id int64, change func(*skyEntry)) {
	s.mu.Lock()
	var e *skyEntry
	for i := range s.log {
		if s.log[i].ID == id {
			e = &s.log[i]
		}
	}
	if e == nil {
		s.mu.Unlock()
		return
	}
	change(e)
	copyE := *e
	s.mu.Unlock()
	s.broadcast(map[string]any{"type": "update", "entry": copyE})
}

func (s *skynet) broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- data:
		default: // a slow window misses an event; it catches up on reconnect
		}
	}
}

func (s *skynet) status() skyStatus {
	cfg, _ := loadConfig()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := skyStatus{State: s.state, Name: cfg.skynetName(), Alias: s.alias, Model: cfg.skynetModel(), Session: s.session,
		Error: s.err, Voice: cfg.SkynetVoice}
	if st.State == "listo" && s.pending > 0 {
		st.State = "pensando"
	}
	return st
}

func (s *skynet) setState(state, errMsg string) {
	s.mu.Lock()
	s.state, s.err = state, errMsg
	s.mu.Unlock()
	s.broadcast(map[string]any{"type": "status", "status": s.status()})
}

// pickAlias chooses the alias to run with: the pinned one, else one whose account has quota
// (the least used), never one that just ran out.
func (s *skynet) pickAlias(cfg Config) (string, error) {
	if cfg.SkynetAlias != "" {
		return cfg.claudeAlias(cfg.SkynetAlias).Name, nil
	}
	accounts := claudeLimits(cfg)
	best, bestUsed := "", 1000.0
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, acc := range accounts {
		if acc.External != "" || acc.outOfQuota() {
			continue
		}
		used := 0.0
		if acc.Err != "" {
			used = 500 // unknown: only when nothing else is left, and then the one that last worked
			for _, n := range acc.Aliases {
				if n == s.alias {
					used = 400
				}
			}
		} else if acc.Session != nil {
			used = acc.Session.Pct
		}
		for _, n := range acc.Aliases {
			if until, ok := s.spent[n]; ok && time.Now().Before(until) {
				continue
			}
			if used < bestUsed {
				best, bestUsed = n, used
			}
		}
	}
	if best == "" {
		return "", errors.New("ninguna cuenta de Claude tiene cuota ahora")
	}
	return best, nil
}

// brainEnv is the environment of the Claude process: the alias's own variables, none that
// another alias sets, and nothing that would make it think it runs inside a cw session.
func brainEnv(cfg Config, alias ClaudeAlias) []string {
	drop := map[string]bool{"CLAUDECODE": true, "CW_WS": true, "TMUX": true, "TMUX_PANE": true}
	for _, a := range cfg.claudeAliases() {
		for _, kv := range a.Env {
			k, _, _ := strings.Cut(kv, "=")
			drop[k] = true
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !drop[k] && !strings.HasPrefix(k, "CLAUDE_CODE_") {
			env = append(env, kv)
		}
	}
	return append(env, alias.Env...)
}

// startBrain launches the Claude process on an alias with quota, resuming the conversation.
func (s *skynet) startBrain() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s.setState("iniciando", "")
	name, err := s.pickAlias(cfg)
	if err != nil {
		s.setState("error", err.Error())
		return err
	}
	alias := cfg.claudeAlias(name).resolve()
	claudeBin, err := exec.LookPath(cfg.ClaudeCommand)
	if err != nil {
		s.setState("error", "no encuentro claude en el PATH")
		return err
	}
	self, _ := os.Executable()
	mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"cw": map[string]any{"command": self, "args": []string{"mcp"}}}})
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--model", cfg.skynetModel(), "--mcp-config", string(mcp), "--strict-mcp-config",
		"--tools", "Skill", "--allowedTools", "mcp__cw", "Skill", "--setting-sources", "project",
		"--system-prompt", skynetPrompt(cfg)}
	s.mu.Lock()
	sid := s.session
	s.mu.Unlock()
	if sid != "" {
		var others []string
		for _, a := range cfg.claudeAliases() {
			others = append(others, cfg.accountDir(a))
		}
		if path, inOwn := findTranscript(sid, cfg.accountDir(alias), others); path == "" {
			sid = "" // the conversation is gone: start a new one
		} else if inOwn {
			args = append(args, "--resume", sid)
		} else {
			args = append(args, "--resume", path) // it lives under another account
		}
	}
	if sid == "" {
		sid = newUUID()
		args = append(args, "--session-id", sid)
	}
	cmd := exec.Command(claudeBin, args...)
	cmd.Dir = skynetDir()
	cmd.Env = brainEnv(cfg, alias)
	cmd.Stderr, _ = os.OpenFile(filepath.Join(runDir(), "skynet-claude.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		s.setState("error", err.Error())
		return err
	}
	s.mu.Lock()
	s.cmd, s.stdin, s.alias, s.session, s.pending = cmd, stdin, name, sid, 0
	s.mu.Unlock()
	data, _ := json.Marshal(skyState{Session: sid, Alias: name})
	_ = writeFileAtomic(skyStateFile(), data)
	s.setState("listo", "")
	go s.read(cmd, stdout)
	return nil
}

func (s *skynet) stopBrain() {
	s.mu.Lock()
	cmd, stdin := s.cmd, s.stdin
	s.cmd, s.stdin, s.pending = nil, nil, 0
	s.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
}

// send gives Claude a message (from the user, a button or a notice).
func (s *skynet) send(text, source string, show bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("el mensaje está vacío")
	}
	if show {
		s.add(skyEntry{Kind: "user", Text: text, Source: source})
	}
	s.mu.Lock()
	running := s.cmd != nil
	s.mu.Unlock()
	if !running {
		if err := s.startBrain(); err != nil {
			s.add(skyEntry{Kind: "error", Text: err.Error()})
			return err
		}
	}
	msg, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin == nil {
		return errors.New("Skynet no está corriendo")
	}
	if _, err := s.stdin.Write(append(msg, '\n')); err != nil {
		return err
	}
	s.pending++
	s.lastText, s.lastSrc = text, source
	go func() { s.broadcast(map[string]any{"type": "status", "status": s.status()}) }()
	return nil
}

// read follows Claude's output: messages, tool calls and the end of each turn.
func (s *skynet) read(cmd *exec.Cmd, out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Session string `json:"session_id"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.Type {
		case "assistant":
			for _, b := range m.Message.Content {
				switch b.Type {
				case "text":
					// Claude Code's own "you hit your limit" comes as text: the switch of alias reports it.
					if t := strings.TrimSpace(b.Text); t != "" && !(len(t) < 200 && limitMessageRe.MatchString(t)) {
						s.add(skyEntry{Kind: "assistant", Text: t})
					}
				case "tool_use":
					s.add(skyEntry{Kind: "tool", Tool: strings.TrimPrefix(b.Name, "mcp__cw__"), Text: shorten(string(b.Input), 200)})
				}
			}
		case "result":
			s.mu.Lock()
			s.pending = max(0, s.pending-1)
			s.mu.Unlock()
			outOfQuota := limitMessageRe.MatchString(m.Result) && (m.IsError || len(m.Result) < 200)
			switch {
			case outOfQuota:
				go s.switchAlias()
			case m.IsError && strings.TrimSpace(m.Result) != "" && !s.lastAssistantIs(m.Result):
				s.add(skyEntry{Kind: "error", Text: m.Result})
			}
			s.setState("listo", "")
		}
	}
	_ = cmd.Wait()
	s.mu.Lock()
	mine := s.cmd == cmd
	if mine {
		s.cmd, s.stdin, s.pending = nil, nil, 0
	}
	s.mu.Unlock()
	if mine {
		s.setState("detenido", "")
	}
}

func (s *skynet) lastAssistantIs(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.log) - 1; i >= 0; i-- {
		if s.log[i].Kind == "assistant" {
			return strings.TrimSpace(s.log[i].Text) == strings.TrimSpace(text)
		}
	}
	return false
}

// switchAlias moves Skynet off an account that ran out: it restarts on another alias in the same
// conversation and repeats the last message.
func (s *skynet) switchAlias() {
	cfg, _ := loadConfig()
	s.mu.Lock()
	old, last, src := s.alias, s.lastText, s.lastSrc
	// Every alias of the same account is out too.
	account := cfg.accountDir(cfg.claudeAlias(old))
	for _, a := range cfg.claudeAliases() {
		if cfg.accountDir(a) == account {
			s.spent[a.Name] = time.Now().Add(15 * time.Minute)
		}
	}
	s.mu.Unlock()
	s.stopBrain()
	if err := s.startBrain(); err != nil {
		s.add(skyEntry{Kind: "error", Text: "Sin cuota en " + old + " y no pude cambiar de alias: " + err.Error()})
		return
	}
	s.add(skyEntry{Kind: "info", Text: fmt.Sprintf("Sin cuota en %s: ahora uso %s.", old, s.status().Alias)})
	if last != "" {
		_ = s.send(last, src, false)
	}
}

// watch turns Skynet off with the setting, moves it off exhausted accounts while idle, and
// announces the sessions that start asking something.
func (s *skynet) watch() {
	lastQuota := time.Time{}
	for {
		select {
		case <-s.quit:
			return
		case <-time.After(2 * time.Second):
		}
		cfg, err := loadConfig()
		if err == nil && !cfg.SkynetEnabled {
			close(s.quit)
			return
		}
		if cfg.SkynetAlias == "" && time.Since(lastQuota) > time.Minute {
			lastQuota = time.Now()
			s.mu.Lock()
			idle, cur := s.cmd != nil && s.pending == 0, s.alias
			s.mu.Unlock()
			if idle {
				for _, acc := range claudeLimits(cfg) {
					for _, n := range acc.Aliases {
						if n == cur && acc.outOfQuota() {
							go s.switchAlias()
						}
					}
				}
			}
		}
		if cfg.skynetNotify() {
			s.announce()
		}
	}
}

func (s *skynet) announce() {
	wss, err := mcpWorkspaces("")
	if err != nil {
		return
	}
	asking := map[string]bool{}
	for _, w := range wss {
		for _, t := range w.tabs {
			for _, ses := range t.sessions {
				if ses.Status != "pregunta" || ses.pane == "" {
					continue
				}
				asking[ses.ID] = true
				q, err := pendingQuestion(&ses)
				if err != nil || q["aviso"] != nil {
					continue
				}
				screen, _ := q["pantalla"].(string)
				question := questionText(screen)
				s.mu.Lock()
				seen := s.notified[ses.ID] == question
				s.notified[ses.ID] = question
				s.mu.Unlock()
				if seen {
					continue
				}
				opts, _ := q["opciones"].([]map[string]any)
				perm := permissionRe.MatchString(screen)
				s.add(skyEntry{Kind: "notice", Session: ses.ID, Name: ses.Name, WS: w.Name, Tab: t.Title, Text: question, Options: opts, Perm: perm})
				var lines []string
				for _, o := range opts {
					lines = append(lines, fmt.Sprintf("%v. %v", o["n"], o["texto"]))
				}
				kind := "una pregunta"
				if perm {
					kind = "un permiso"
				}
				_ = s.send(fmt.Sprintf("[Aviso de cw] La sesión %q (id %s, %s › %s) espera respuesta a %s:\n%s\nOpciones:\n%s",
					ses.Name, ses.ID[:8], w.Name, t.Title, kind, question, strings.Join(lines, "\n")), "aviso", false)
			}
		}
	}
	// Notices whose session no longer asks are resolved.
	s.mu.Lock()
	var done []int64
	for i := range s.log {
		e := &s.log[i]
		if e.Kind == "notice" && !e.Resolved && !asking[e.Session] {
			done = append(done, e.ID)
		}
	}
	for id := range s.notified {
		if !asking[id] {
			delete(s.notified, id)
		}
	}
	s.mu.Unlock()
	for _, id := range done {
		s.update(id, func(e *skyEntry) { e.Resolved = true })
	}
}

var boxRuleRe = regexp.MustCompile(`^[\s─━╌═│┃]*$`)

// questionText keeps the question of a dialog: the lines above its options, without rules.
func questionText(screen string) string {
	rows := strings.Split(screen, "\n")
	end := len(rows)
	for i, r := range rows {
		if m := optionLineRe.FindStringSubmatch(r); m != nil && m[1] == "1" {
			end = i
		}
	}
	var keep []string
	for i := end - 1; i >= 0 && len(keep) < 8; i-- {
		r := strings.TrimSpace(rows[i])
		if boxRuleRe.MatchString(r) {
			if len(keep) > 0 {
				break
			}
			continue
		}
		keep = append([]string{r}, keep...)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

func (s *skynet) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) { writeJSONResp(w, s.status()) })
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text   string `json:"text"`
			Source string `json:"source"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Source == "" {
			req.Source = "texto"
		}
		if err := s.send(req.Text, req.Source, true); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		s.stopBrain()
		s.mu.Lock()
		s.session = ""
		s.mu.Unlock()
		_ = os.Remove(skyStateFile())
		s.add(skyEntry{Kind: "info", Text: "Conversación nueva."})
		s.setState("detenido", "")
		writeJSONResp(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, map[string]bool{"ok": true})
		go func() { time.Sleep(100 * time.Millisecond); close(s.quit) }()
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "sin streaming", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		ch := make(chan []byte, 64)
		s.mu.Lock()
		hist := append([]skyEntry{}, s.log...)
		s.subs[ch] = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.subs, ch)
			s.mu.Unlock()
		}()
		first, _ := json.Marshal(map[string]any{"type": "history", "entries": hist, "status": s.status()})
		fmt.Fprintf(w, "data: %s\n\n", first)
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-s.quit:
				return
			case data := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", data)
				fl.Flush()
			case <-time.After(25 * time.Second):
				fmt.Fprint(w, ": ping\n\n")
				fl.Flush()
			}
		}
	})
	return mux
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
