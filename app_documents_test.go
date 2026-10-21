//go:build !terminal

package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDocumentTabs(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "document-test-" + shortID()}
	if _, err := mux.Run("new-session", "-d", "-s", "test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	root := t.TempDir()
	for _, name := range []string{"first.md", "second.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("# Documento"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := &appLayout{Tabs: []*appTab{{ID: "tab", Dir: root, Views: []*appView{{ID: "view", Slots: []string{"session"}}}}}}
	if err := mux.storeLayout(state); err != nil {
		t.Fatal(err)
	}
	open := func(path string) string {
		t.Helper()
		result, err := wsAction(mux, &doReq{Action: "document.open", View: "view", Path: path})
		if err != nil {
			t.Fatal(err)
		}
		return result["document"].(string)
	}
	first := open("first.md")
	second := open("second.md")
	if open("first.md") != first {
		t.Fatal("Duplicó archivo abierto")
	}
	_, view := mux.loadLayout().findView("view")
	if len(view.Documents) != 2 || len(view.Slots) != 1 || view.DocumentActive != first {
		t.Fatalf("Estado incorrecto: %+v", view)
	}
	server := &appServer{}
	response := httptest.NewRecorder()
	server.handleGet(response, httptest.NewRequest("GET", "/api/get/document?ws="+mux.WS+"&view=view&document="+second, nil))
	if response.Code != 200 {
		t.Fatalf("Lectura fallida: %s", response.Body.String())
	}
	if _, err := wsAction(mux, &doReq{Action: "document.select", View: "view"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wsAction(mux, &doReq{Action: "document.close", View: "view", Document: first}); err != nil {
		t.Fatal(err)
	}
	_, view = mux.loadLayout().findView("view")
	if len(view.Documents) != 1 || view.Documents[0].ID != second || view.DocumentActive != "" {
		t.Fatalf("Cierre incorrecto: %+v", view)
	}
	response = httptest.NewRecorder()
	server.handleGet(response, httptest.NewRequest("GET", "/api/get/document?ws="+mux.WS+"&view=view&document="+first, nil))
	if response.Code == 200 {
		t.Fatal("Leyó documento cerrado")
	}
}
