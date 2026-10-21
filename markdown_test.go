package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMarkdown(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name, content string
		valid         bool
	}{
		{"notes.md", "# Notas\nTexto **seguro**", true},
		{"notes.markdown", "", true},
		{"notes.txt", "texto", false},
		{"invalid.md", string([]byte{0xff}), false},
		{"large.md", strings.Repeat("x", markdownMaxBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, tc.name)
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readMarkdown(path)
			if (err == nil) != tc.valid {
				t.Fatalf("Resultado inesperado: %v", err)
			}
			if tc.valid && got != tc.content {
				t.Fatal("Contenido modificado")
			}
		})
	}
	if _, err := readMarkdown(filepath.Join(root, "missing.md")); err == nil {
		t.Fatal("Aceptó archivo inexistente")
	}
	dir := filepath.Join(root, "directory.md")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readMarkdown(dir); err == nil {
		t.Fatal("Aceptó directorio")
	}
}

func TestMarkdownWithoutMCP(t *testing.T) {
	for _, tool := range mcpTools() {
		if tool.Name == "abrir_markdown" {
			t.Fatal("El visor no debe formar parte del MCP de Skynet")
		}
	}
	instruction := markdownInstructions("/tmp/cw con espacios")
	if !strings.Contains(instruction, shQuote("/tmp/cw con espacios")+" open --") {
		t.Fatal("Falta el comando con su ruta entre comillas")
	}
}

func TestOpenArguments(t *testing.T) {
	isolateUsage(t)
	t.Setenv("CW_WS", "")
	t.Setenv("CW_SLOT", "")
	for _, args := range [][]string{nil, {"--unknown"}, {"uno.md", "dos.md"}} {
		if err := cmdOpen(args); err == nil {
			t.Fatalf("Aceptó argumentos inválidos: %v", args)
		}
	}
	path := filepath.Join(t.TempDir(), "notas.md")
	if err := os.WriteFile(path, []byte("# Notas"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmdOpen([]string{"--", path}); err == nil || !strings.Contains(err.Error(), "fuera de cw") {
		t.Fatalf("Falta diagnóstico de destino: %v", err)
	}
}
