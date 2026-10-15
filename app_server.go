package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

const appDefaultPort = 47821

// appInfo is how a `cw app` finds the server another one started: runDir()/app.json.
type appInfo struct {
	Port    int    `json:"port"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func appInfoFile() string { return filepath.Join(runDir(), "app.json") }

func readAppInfo() (*appInfo, error) {
	data, err := os.ReadFile(appInfoFile())
	if err != nil {
		return nil, err
	}
	var i appInfo
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func (i *appInfo) base() string { return "http://127.0.0.1:" + strconv.Itoa(i.Port) }

// call makes an authenticated request to a running server.
func (i *appInfo) call(path string) (string, error) {
	req, _ := http.NewRequest("GET", i.base()+path, nil)
	req.AddCookie(&http.Cookie{Name: "cw_k", Value: i.Token})
	req.Header.Set("X-CW", "1")
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(body), nil
}

const appUsage = `uso: cw app [--sin-ventana | --url | --detener | --mantener]
  cw app               abre la app (inicia su servidor local si no corre)
  cw app --sin-ventana inicia el servidor y muestra la dirección, sin abrir ventana
  cw app --url         muestra la dirección para abrirla en un navegador
  cw app --detener     detiene el servidor de la app (las sesiones siguen corriendo)
  cw app --mantener    abre la app y espera mientras el servidor corra (accesos directos de Windows)`

// cmdApp opens the desktop app: a window of the system's browser engine in app mode, served
// by a local cw server that runs in the background and keeps the sessions when the window closes.
func cmdApp(args []string) error {
	window, printURL, stop, hold, foreground := true, false, false, false, false
	for _, a := range args {
		switch a {
		case "--sin-ventana", "--no-window":
			window = false
		case "--url":
			window, printURL = false, true
		case "--detener", "--stop":
			stop = true
		case "--mantener", "--hold":
			hold = true
		case "--servidor":
			foreground = true
		default:
			return errors.New(appUsage)
		}
	}
	dir, _ := os.Getwd()
	if foreground {
		return serveApp(dir)
	}
	if os.Getenv("CW_WS") != "" && window {
		return errors.New("estás dentro de un workspace de la terminal; abre la app desde otra terminal")
	}
	info := runningApp()
	if info != nil && strings.TrimSpace(info.Version) != version && !stop {
		// A server from before an update: replace it (the sessions live in tmux and stay).
		_, _ = info.call("/api/quit")
		time.Sleep(500 * time.Millisecond)
		info = nil
	}
	if stop {
		if info == nil {
			fmt.Println("el servidor de la app no está corriendo")
			return nil
		}
		_, _ = info.call("/api/quit")
		fmt.Println("servidor de la app detenido; las sesiones siguen corriendo")
		return nil
	}
	if info == nil {
		var err error
		if info, err = startAppServer(dir); err != nil {
			return err
		}
	}
	url := info.base() + "/?k=" + info.Token + "&dir=" + urlQueryEscape(dir)
	switch {
	case printURL:
		fmt.Println(url)
	case !window:
		fmt.Println("la app corre en", info.base())
		fmt.Println("abre:", url)
	default:
		if err := openAppWindow(url); err != nil {
			fmt.Println("abre en tu navegador:", url)
			return err
		}
	}
	// Under WSL, a Windows shortcut runs cw through wsl.exe; waiting here keeps that wsl.exe —
	// and with it the Linux side, the server and the sessions — alive while the server runs.
	for hold && runningApp() != nil {
		time.Sleep(20 * time.Second)
	}
	return nil
}

// runningApp returns the server that answers, or nil.
func runningApp() *appInfo {
	info, err := readAppInfo()
	if err != nil {
		return nil
	}
	v, err := info.call("/api/ping")
	if err != nil {
		return nil
	}
	info.Version = strings.TrimSpace(v)
	return info
}

// startAppServer starts the server in the background (its own session, log in the run dir)
// and waits until it answers.
func startAppServer(dir string) (*appInfo, error) {
	if err := checkTmux(); err != nil {
		return nil, err
	}
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(filepath.Join(runDir(), "app.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer logf.Close()
	cmd := exec.Command(bin, "app", "--servidor")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	for i := 0; i < 100; i++ {
		select {
		case <-exited:
			data, _ := os.ReadFile(filepath.Join(runDir(), "app.log"))
			return nil, fmt.Errorf("el servidor de la app no arrancó: %s", strings.TrimSpace(string(data)))
		case <-time.After(60 * time.Millisecond):
		}
		if info := runningApp(); info != nil {
			return info, nil
		}
	}
	return nil, errors.New("el servidor de la app no respondió")
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~/", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func serveApp(launchDir string) error {
	if err := checkTmux(); err != nil {
		return err
	}
	cfg, _ := loadConfig()
	port := cfg.AppPort
	if port == 0 {
		port = appDefaultPort
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0") // the usual port is taken: any free one
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return err
	}
	// The token outlives the server, so windows opened before a restart (an update) keep access.
	tokenFile := filepath.Join(runDir(), "app.token")
	token := ""
	if b, err := os.ReadFile(tokenFile); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		token = strings.TrimSpace(string(b))
	} else {
		tok := make([]byte, 24)
		_, _ = rand.Read(tok)
		token = hex.EncodeToString(tok)
		if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
			return err
		}
	}
	info := &appInfo{Port: ln.Addr().(*net.TCPAddr).Port, Token: token, PID: os.Getpid(), Version: version}
	data, _ := json.Marshal(info)
	if err := writeFileAtomic(appInfoFile(), data); err != nil {
		return err
	}
	_ = os.Chmod(appInfoFile(), 0o600)

	srv := &appServer{info: info, launchDir: launchDir, quit: make(chan struct{})}
	hs := &http.Server{Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second}
	go srv.autosaveLoop()
	fmt.Println("cw app", version, "escuchando en", info.base())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		select {
		case <-sig:
		case <-srv.quit:
		}
		srv.saveAll()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = hs.Shutdown(ctx)
	}()
	err = hs.Serve(ln)
	if cur, e := readAppInfo(); e == nil && cur.PID == os.Getpid() {
		_ = os.Remove(appInfoFile())
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// openAppWindow opens url in a window without browser chrome (Edge or Chrome in app mode), and
// in the default browser when neither is found.
func openAppWindow(url string) error {
	appArgs := []string{"--app=" + url, "--window-size=1480,940"}
	try := func(bin string, args ...string) bool {
		cmd := exec.Command(bin, args...)
		cmd.Stdout, cmd.Stderr = nil, nil
		if strings.HasPrefix(bin, "/mnt/") {
			cmd.Dir = "/mnt/c" // Windows programs complain about a Linux working directory
		}
		if cmd.Start() != nil {
			return false
		}
		go func() { _ = cmd.Wait() }()
		return true
	}
	switch {
	case os.Getenv("WSL_DISTRO_NAME") != "":
		for _, p := range []string{
			"/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
			"/mnt/c/Program Files/Microsoft/Edge/Application/msedge.exe",
			"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
		} {
			if _, err := os.Stat(p); err == nil && try(p, appArgs...) {
				return nil
			}
		}
		if try("/mnt/c/Windows/System32/cmd.exe", "/c", "start", "", url) {
			return nil
		}
	case runtime.GOOS == "darwin":
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Chromium", "Brave Browser"} {
			if _, err := os.Stat("/Applications/" + app + ".app"); err == nil && try("open", append([]string{"-na", app, "--args"}, appArgs...)...) {
				return nil
			}
		}
		if try("open", url) {
			return nil
		}
	default:
		for _, b := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if p, err := exec.LookPath(b); err == nil && try(p, appArgs...) {
				return nil
			}
		}
		if p, err := exec.LookPath("xdg-open"); err == nil && try(p, url) {
			return nil
		}
	}
	return errors.New("no encontré un navegador para abrir la ventana")
}

type appServer struct {
	info      *appInfo
	launchDir string
	quit      chan struct{}
}

func (s *appServer) hostOK(r *http.Request) bool {
	p := strconv.Itoa(s.info.Port)
	return r.Host == "127.0.0.1:"+p || r.Host == "localhost:"+p
}

func (s *appServer) authed(r *http.Request) bool {
	c, err := r.Cookie("cw_k")
	return err == nil && c.Value == s.info.Token
}

func (s *appServer) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	p := strconv.Itoa(s.info.Port)
	return o == "" || o == "http://127.0.0.1:"+p || o == "http://localhost:"+p
}

func (s *appServer) routes() http.Handler {
	static, _ := fs.Sub(webFiles, "web")
	files := http.FileServer(http.FS(static))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, version) })
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
		go func() { time.Sleep(100 * time.Millisecond); close(s.quit) }()
	})
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/pty", s.handlePTY)
	mux.HandleFunc("/api/get/", s.handleGet)
	mux.HandleFunc("/api/do", s.handleDo)
	mux.Handle("/", files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r) {
			http.Error(w, "host no permitido", http.StatusForbidden)
			return
		}
		if k := r.URL.Query().Get("k"); k != "" && r.URL.Path == "/" {
			if k == s.info.Token {
				http.SetCookie(w, &http.Cookie{Name: "cw_k", Value: k, Path: "/", HttpOnly: true,
					SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
				target := "/"
				if d := r.URL.Query().Get("dir"); d != "" {
					target += "?dir=" + urlQueryEscape(d)
				}
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
		}
		if !s.authed(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>cw</title><body style="background:#0e0f13;color:#e8eaf0;font:15px system-ui;display:grid;place-items:center;height:90vh"><div>Abre cw con <code>cw app</code> desde tu terminal.</div>`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !s.originOK(r) || (r.Method != "GET" && r.Header.Get("X-CW") != "1") {
				http.Error(w, "origen no permitido", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// handleEvents streams the state of the open workspace (and the list of workspaces) as
// server-sent events, whenever it changes.
func (s *appServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "sin streaming", http.StatusInternalServerError)
		return
	}
	ws := r.URL.Query().Get("ws")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	lastState, lastList := "", ""
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	n := 0
	for {
		if ws != "" {
			st := appSnapshot(ws)
			data, _ := json.Marshal(st)
			if string(data) != lastState {
				lastState = string(data)
				fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
				fl.Flush()
			}
		}
		if n%5 == 0 {
			data, _ := json.Marshal(listUIWorkspaces())
			if string(data) != lastList {
				lastList = string(data)
				fmt.Fprintf(w, "event: workspaces\ndata: %s\n\n", data)
				fl.Flush()
			}
		}
		if n%40 == 39 {
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
		n++
		select {
		case <-r.Context().Done():
			return
		case <-s.quit:
			return
		case <-tick.C:
		}
	}
}

// autosaveLoop saves the running app workspaces every autosave_seconds.
func (s *appServer) autosaveLoop() {
	for {
		select {
		case <-s.quit:
			return
		case <-time.After(20 * time.Second):
		}
		cfg, _ := loadConfig()
		for _, ws := range runningWorkspaces() {
			t := Tmux{WS: ws}
			if !appMode(t) {
				continue
			}
			if st, err := os.Stat(workspaceFile(ws)); err == nil && time.Since(st.ModTime()) < time.Duration(cfg.AutosaveSeconds)*time.Second {
				continue
			}
			a := appWS(ws)
			a.mu.Lock()
			_ = appSave(ws)
			a.mu.Unlock()
		}
	}
}

func (s *appServer) saveAll() {
	for _, ws := range runningWorkspaces() {
		if appMode(Tmux{WS: ws}) {
			_ = appSave(ws)
		}
	}
}
