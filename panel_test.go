package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCoordinatorPanelSync(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	old := panelArgv
	panelArgv = func(ws, slot string) []string { return []string{"sleep", "300"} }
	t.Cleanup(func() { panelArgv = old })
	mux := Tmux{WS: "panel-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	dir := t.TempDir()
	coord := Slot{ID: newUUID(), Kind: kindShell, Alias: "coord", Cwd: dir}
	child := Slot{ID: newUUID(), Kind: kindShell, Alias: "hija", Cwd: dir, Parent: coord.ID}
	coordPane, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: coord, name: "coord", argv: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	childPane, err := mux.createWindow(windowSpec{session: "t01", slot: child, name: "hija", argv: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	syncCoordinators(mux)
	syncCoordinators(mux) // idempotent: still one panel
	rows, _ := listPaneRows(mux)
	var panels []paneRow
	for _, r := range rows {
		if r.kind == kindPanel {
			panels = append(panels, r)
		}
	}
	if len(panels) != 1 || panels[0].panelFor != coord.ID {
		t.Fatalf("paneles: %+v", panels)
	}
	if c, _ := mux.Run("display", "-p", "-t", coordPane, "#{@cw_coord}"); c != "1" {
		t.Fatal("la coordinadora no quedó marcada")
	}
	// At the right of the coordinator, in its view.
	geo, _ := mux.Run("display", "-p", "-t", panels[0].pane, "#{pane_left}"+sep+"#{window_id}")
	cgeo, _ := mux.Run("display", "-p", "-t", coordPane, "#{pane_left}"+sep+"#{window_id}")
	g, cg := strings.Split(geo, sep), strings.Split(cgeo, sep)
	if g[1] != cg[1] || g[0] <= cg[0] {
		t.Fatalf("el panel no está a la derecha de la coordinadora: %v %v", g, cg)
	}
	if label, _ := mux.Run("show", "-t", "t01", "-qv", "@cw_tab_label"); !strings.Contains(label, "◆") {
		t.Fatalf("la pestaña no muestra que coordina: %q", label)
	}
	// The panel is not a session: not captured, and the view keeps no layout.
	w, err := mux.capture()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range w.Tabs[0].Slots {
		if s.Kind == kindPanel {
			t.Fatal("el panel se guardó como sesión")
		}
		if s.ID == child.ID && s.Parent != coord.ID {
			t.Fatal("no se guardó quién coordina a la hija")
		}
	}
	for _, l := range w.Tabs[0].Layouts {
		if l != "" {
			t.Fatalf("se guardó el layout con el panel: %q", l)
		}
	}
	children, alive := panelChildren(mux, coord.ID)
	if !alive || len(children) != 1 || children[0].alias != "hija" {
		t.Fatalf("hijas: %+v %v", children, alive)
	}
	// Without children it stops being a coordinator.
	if _, err := mux.Run("kill-pane", "-t", childPane); err != nil {
		t.Fatal(err)
	}
	syncCoordinators(mux)
	rows, _ = listPaneRows(mux)
	for _, r := range rows {
		if r.kind == kindPanel {
			t.Fatal("el panel siguió sin hijas")
		}
	}
	if c, _ := mux.Run("display", "-p", "-t", coordPane, "#{@cw_coord}"); c == "1" {
		t.Fatal("siguió marcada como coordinadora")
	}
}

// TestPanelLive draws the real panel (CW_TEST_BIN) and goes to a child.
func TestPanelLive(t *testing.T) {
	bin := os.Getenv("CW_TEST_BIN")
	if os.Getenv("CW_LIVE_DRAFT") != "1" || bin == "" {
		t.Skip("CW_LIVE_DRAFT=1 y CW_TEST_BIN=<cw compilado>")
	}
	old := panelArgv
	panelArgv = func(ws, slot string) []string { return []string{bin, "_panel", ws, slot} }
	t.Cleanup(func() { panelArgv = old })
	mux := Tmux{WS: "panelv-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	dir := t.TempDir()
	coord := Slot{ID: newUUID(), Kind: kindShell, Alias: "coord", Cwd: dir}
	if _, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: coord, name: "coord", argv: []string{"sleep", "300"}}); err != nil {
		t.Fatal(err)
	}
	var childPane string
	for _, n := range []string{"bug-110", "bug-313"} {
		c := Slot{ID: newUUID(), Kind: kindClaude, Alias: n, Cwd: dir, Parent: coord.ID}
		p, err := mux.createWindow(windowSpec{session: "t02", first: n == "bug-110", slot: c, name: n, argv: []string{"sleep", "300"}})
		if err != nil {
			t.Fatal(err)
		}
		childPane = p
	}
	_, _ = mux.Run("set", "-p", "-t", childPane, "@cw_status", statusWaiting)
	_, _ = mux.Run("set", "-t", "t02", "@cw_title", "bugs")
	syncCoordinators(mux)
	rows, _ := listPaneRows(mux)
	var panel string
	for _, r := range rows {
		if r.kind == kindPanel {
			panel = r.pane
		}
	}
	s := &mcpSession{Name: "panel", Workspace: mux.WS, pane: panel}
	waitScreen(t, s, "Hijas (2)")
	waitScreen(t, s, "bug-313")
	waitScreen(t, s, "pregunta · bugs")
	text, _ := screenOf(s, 0)
	t.Log("\n" + text)
	children, _ := panelChildren(mux, coord.ID)
	goToChild(mux, children[1])
	if active, _ := mux.Run("display", "-p", "-t", "t02", "#{pane_id}"); active != children[1].pane {
		t.Fatalf("no fue a la hija: %s", active)
	}
}
