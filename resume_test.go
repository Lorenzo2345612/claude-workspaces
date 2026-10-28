package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowseConversations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	cfg := defaultConfig()
	dir := filepath.Join(home, "claude", "projects", "-project")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(id, title, prompt string, at time.Time) {
		t.Helper()
		path := filepath.Join(dir, id+".jsonl")
		raw := `{"type":"custom-title","customTitle":"` + title + `"}` + "\n" + `{"type":"user","cwd":"/project/backend","message":{"content":"` + prompt + `"}}` + "\n"
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	write("abcdef-old", "Pagos", "Revisar facturas", now.Add(-time.Hour))
	write("abcdef-new", "Usuarios", "Agregar permisos", now)
	write("empty", "Vacia", "", now)
	rows := browseConversations("", cfg)
	if len(rows) != 2 || rows[0].conv.ID != "abcdef-new" {
		t.Fatalf("unexpected recent list: %+v", rows)
	}
	for _, query := range []string{"PAGOS", "facturas", "old"} {
		rows := browseConversations(query, cfg)
		if len(rows) != 1 || rows[0].conv.ID != "abcdef-old" {
			t.Fatalf("query %q: %+v", query, rows)
		}
	}
	if len(browseConversations("backend", cfg)) != 2 {
		t.Fatal("directory search failed")
	}
	if len(browseConversations("missing", cfg)) != 0 {
		t.Fatal("unexpected match")
	}
	if len(findConversations("", cfg)) != 0 {
		t.Fatal("strict lookup must reject empty IDs")
	}
}
