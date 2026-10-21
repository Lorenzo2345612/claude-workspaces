//go:build !terminal

package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOpenCommandInApp(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "open-test-" + shortID()}
	if _, err := mux.Run("new-session", "-d", "-s", slotSession("session")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	if _, err := mux.Run("set", "-g", optAppMode, "1", ";", "set", "-p", "@cw_slot", "session"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "notas con espacios.md")
	if err := os.WriteFile(path, []byte("# Notas"), 0600); err != nil {
		t.Fatal(err)
	}
	layout := &appLayout{Tabs: []*appTab{{ID: "tab", Dir: root, Views: []*appView{{ID: "view", Slots: []string{"session"}, Focus: "session"}}}}}
	if err := mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	server := &appServer{}
	handler := http.NewServeMux()
	handler.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(version)) })
	handler.HandleFunc("/api/do", server.handleDo)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(httpServer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	data, _ := json.Marshal(appInfo{Port: n, Token: "test", PID: os.Getpid()})
	if err := writeFileAtomic(appInfoFile(), data); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_WS", mux.WS)
	t.Setenv("CW_SLOT", "session")
	t.Chdir(root)
	for _, args := range [][]string{
		{"--", filepath.Base(path)},
		{"--workspace", mux.WS, "--session", "session", "--", path},
		{"--workspace", mux.WS, "--tab", "1", "--view", "1", "--", path},
	} {
		if err := cmdOpen(args); err != nil {
			t.Fatalf("Falló cw open %v: %v", args, err)
		}
	}
	_, view := mux.loadLayout().findView("view")
	if len(view.Documents) != 1 || view.Documents[0].Path != path || view.DocumentActive != view.Documents[0].ID || len(view.Slots) != 1 {
		t.Fatalf("Estado incorrecto: %+v", view)
	}
	launcher := &Launcher{WS: mux.WS, Bin: "/tmp/cw con espacios", ClaudeBin: "claude", Cfg: defaultConfig()}
	argv, _ := launcher.claudeArgv(&Slot{Alias: "test"}, false)
	found := false
	for i, arg := range argv {
		if arg == "--append-system-prompt" && i+1 < len(argv) && argv[i+1] == markdownInstructions(launcher.Bin) {
			found = true
		}
	}
	if !found {
		t.Fatal("La sesión de la app no recibió la instrucción de cw open")
	}
}
