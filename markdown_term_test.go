package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownTerminalRendering(t *testing.T) {
	text := "# Plan\x1b]0;titulo\x07\n\n- uno **dos** `tres`\n> cita\n---\n```\n**literal**\x1b[2J\n```\n[sitio](https://example.com)"
	var out []string
	for _, l := range markdownLines(text) {
		out = append(out, l.rows(40)...)
	}
	joined := strings.Join(out, "\n")
	plain := ansiRe.ReplaceAllString(joined, "")
	for _, want := range []string{"Plan", "• uno dos tres", "│ cita", "**literal**", "sitio (https://example.com)", strings.Repeat("─", 40)} {
		if !strings.Contains(plain, want) {
			t.Fatalf("falta %q en:\n%s", want, plain)
		}
	}
	// Only the viewer's own colour codes may reach the terminal: no OSC, no clear screen.
	if strings.Contains(joined, "\x07") || strings.Contains(joined, "\x1b]") || strings.Contains(joined, "\x1b[2J") {
		t.Fatalf("secuencias del documento llegaron a la terminal: %q", joined)
	}
	rows := mdLine{lead: mdSpan{"• ", ""}, indent: mdSpan{"  ", ""}, spans: []mdSpan{{strings.Repeat("palabra ", 8), ""}}}.rows(20)
	if len(rows) < 3 {
		t.Fatal(rows)
	}
	for i, r := range rows {
		r = ansiRe.ReplaceAllString(r, "")
		if len([]rune(r)) > 20 || (i > 0 && !strings.HasPrefix(r, "  ")) {
			t.Fatalf("fila %d mal ajustada: %q", i, r)
		}
	}
}

func TestPageKeys(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	w.WriteString("\x1b[5~\x1b[6~\x1b[H\x1b[4~")
	w.Close()
	keys, err := readKeys()
	if err != nil {
		t.Fatal(err)
	}
	want := []int{keyPgUp, keyPgDn, keyHome, keyEnd}
	if len(keys) != len(want) {
		t.Fatal(keys)
	}
	for i, k := range keys {
		if k.kind != want[i] {
			t.Fatalf("tecla %d: %d", i, k.kind)
		}
	}
}

func TestOpenCommandInTerminal(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "open-term-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	root := t.TempDir()
	slot := Slot{ID: newUUID(), Kind: kindShell, Alias: "shell", Cwd: root}
	win, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: slot, name: "shell", argv: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan.md")
	if err := os.WriteFile(path, []byte("# Plan"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_WS", mux.WS)
	t.Setenv("CW_SLOT", slot.ID)
	t.Chdir(root)
	for range 2 {
		if err := cmdOpen([]string{"--", "plan.md"}); err != nil {
			t.Fatal(err)
		}
	}
	docs := func() []string {
		t.Helper()
		rows, err := mux.Lines("list-panes", "-s", "-t", "t01", "-F", "#{window_id}"+sep+"#{@cw_kind}"+sep+"#{@cw_doc}")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			if len(r) == 3 && r[1] == kindDoc {
				out = append(out, r[0])
				if r[2] != path {
					t.Fatal("ruta del documento", r[2])
				}
			}
		}
		return out
	}
	opened := docs()
	if len(opened) != 1 {
		t.Fatal("documento duplicado o ausente", opened)
	}
	if active, _ := mux.Run("display", "-p", "-t", "t01", "#{window_id}"); active != opened[0] {
		t.Fatal("el documento no quedó a la vista", active)
	}
	saved, err := mux.capture()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(saved.Tabs[0].Slots); n != 1 || saved.Tabs[0].Slots[0].ID != slot.ID {
		t.Fatal("el documento se guardó como sesión", saved.Tabs[0].Slots)
	}
	// Closing the last session leaves the tab with its start screen, not just the document.
	if err := closeWindow(mux.WS, win); err != nil {
		t.Fatal(err)
	}
	kinds, err := mux.Lines("list-panes", "-s", "-t", "t01", "-F", "#{@cw_kind}")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range kinds {
		got = append(got, r[0])
	}
	if strings.Join(got, ",") != kindDoc+","+kindHome {
		t.Fatal("pestaña sin pantalla de inicio", got)
	}
}
