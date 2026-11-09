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

// Typing in the resume picker must not reread transcripts: the dialog loads the index once and
// filters it in memory, and the on-disk index rereads only transcripts that changed.
func TestConversationIndexReadsEachTranscriptOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "")
	t.Setenv("CW_DATA_DIR", filepath.Join(home, "cw"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	cfg := defaultConfig()
	dir := filepath.Join(home, "claude", "projects", "-project")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(id, title, prompt string) {
		t.Helper()
		raw := `{"type":"user","cwd":"/project","message":{"content":"` + prompt + `"}}` + "\n" + `{"type":"custom-title","customTitle":"` + title + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaaaa-1", "Pagos", "Revisar facturas")
	write("bbbbbb-2", "Usuarios", "Agregar permisos")

	transcriptSummaryReads.Store(0)
	all := scanConversations(cfg)
	if len(all) != 2 || transcriptSummaryReads.Load() != 2 {
		t.Fatalf("first scan: %d conversations, %d reads", len(all), transcriptSummaryReads.Load())
	}
	g := newAliasGuesser(cfg)
	for _, q := range []string{"p", "pa", "pag", "pagos", ""} {
		filterConversations(all, q, g)
	}
	if rows := filterConversations(all, "FACTURAS", g); len(rows) != 1 || rows[0].conv.ID != "aaaaaa-1" || rows[0].conv.Title != "Pagos" {
		t.Fatalf("filter: %+v", rows)
	}
	if n := transcriptSummaryReads.Load(); n != 2 {
		t.Fatalf("filtering reread transcripts: %d reads", n)
	}

	// A later dialog (another process) reuses the saved index and reads only what changed: Claude
	// Code appends to a transcript, so only the new lines are read.
	path := filepath.Join(dir, "bbbbbb-2.jsonl")
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(`{"type":"custom-title","customTitle":"Permisos"}` + "\n" + `{"type":"custom-title","customTitle":"a medio escribir`); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	transcriptSummaryReads.Store(0)
	all = scanConversations(cfg)
	if n := transcriptSummaryReads.Load(); n != 1 {
		t.Fatalf("second scan reread %d transcripts, want 1", n)
	}
	if rows := filterConversations(all, "", g); len(rows) != 2 || rows[0].conv.ID != "bbbbbb-2" || rows[0].conv.Title != "Permisos" || rows[0].conv.First != "Agregar permisos" {
		t.Fatalf("changed transcript not refreshed: %+v", rows)
	}

	// The half-written line is read once it is complete.
	fh, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(`"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	later = later.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if rows := filterConversations(scanConversations(cfg), "", g); rows[0].conv.Title != "a medio escribir" {
		t.Fatalf("completed line not read: %+v", rows[0].conv)
	}

	// A transcript that shrank (replaced) is read again from the start.
	write("bbbbbb-2", "Nuevo", "Otro inicio")
	if rows := filterConversations(scanConversations(cfg), "otro inicio", g); len(rows) != 1 || rows[0].conv.Title != "Nuevo" {
		t.Fatalf("replaced transcript not reread: %+v", rows)
	}
}
