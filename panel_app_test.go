//go:build !terminal

package main

import (
	"os/exec"
	"testing"
)

// TestAppChildDoesNotTakeFocus: in the app, a session or tab opened by a coordinator does not
// change the tab or view the user is looking at; one the user opens does.
func TestAppChildDoesNotTakeFocus(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "foco-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	ln, err := newLauncher(mux.WS, cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	coord := Slot{Kind: kindShell, Alias: "coord", Cwd: dir}
	if err := appOpenSlot(mux, ln, &coord, false); err != nil {
		t.Fatal(err)
	}
	layout := &appLayout{Active: "tab", Tabs: []*appTab{{ID: "tab", Title: "T", Dir: dir, Active: "view",
		Views: []*appView{{ID: "view", Slots: []string{coord.ID}, Focus: coord.ID}}}}}
	if err := mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	if _, err := wsAction(mux, &doReq{Action: "slot.new", Tab: "tab", Kind: kindShell, Name: "hija", Parent: coord.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := wsAction(mux, &doReq{Action: "tab.new", Dir: dir, Title: "otra", Start: kindShell, Parent: coord.ID}); err != nil {
		t.Fatal(err)
	}
	l := mux.loadLayout()
	if l.Active != "tab" || l.Tabs[0].Active != "view" || len(l.Tabs[0].Views) != 2 || len(l.Tabs) != 2 {
		t.Fatalf("una sesión abierta por la coordinadora tomó el foco: activo %s/%s, vistas %d, pestañas %d",
			l.Active, l.Tabs[0].Active, len(l.Tabs[0].Views), len(l.Tabs))
	}
	if _, err := wsAction(mux, &doReq{Action: "slot.new", Tab: "tab", Kind: kindShell, Name: "mia"}); err != nil {
		t.Fatal(err)
	}
	if l := mux.loadLayout(); l.Tabs[0].Active == "view" {
		t.Fatal("una sesión abierta por el usuario no tomó el foco")
	}
}
