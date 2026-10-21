package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type appDocument struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

const markdownMaxBytes = 2 << 20

// Solo se abren archivos Markdown locales regulares; nunca se ejecuta su contenido.
func readMarkdown(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".md" && ext != ".markdown" {
		return "", fmt.Errorf("el archivo debe ser .md o .markdown")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > markdownMaxBytes {
		return "", fmt.Errorf("se requiere un archivo regular de hasta 2 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > markdownMaxBytes {
		return "", fmt.Errorf("se requiere un archivo regular de hasta 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, markdownMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > markdownMaxBytes || !utf8.Valid(data) {
		return "", fmt.Errorf("Markdown inválido: máximo 2 MiB y texto UTF-8")
	}
	return string(data), nil
}

// cmdOpen abre un documento mediante la API local, sin servidor MCP.
func cmdOpen(args []string) error {
	flags := flag.NewFlagSet("open", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "Workspace de destino")
	session := flags.String("session", "", "ID o nombre de sesión")
	tab := flags.String("tab", "", "Número o título de pestaña")
	view := flags.Int("view", 0, "Número de vista")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Println("uso: cw open [--workspace N --session ID | --workspace N --tab T --view N] <archivo.md>")
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("uso: cw open [opciones de destino] <archivo.md>")
	}
	// Como cualquier comando de archivos, resuelve las rutas desde el directorio actual.
	path, err := filepath.Abs(expandHome(flags.Arg(0)))
	if err != nil {
		return err
	}
	if _, err := readMarkdown(path); err != nil {
		return err
	}
	a := map[string]any{"workspace": *workspace, "sesion": *session, "pestana": *tab, "vista": float64(*view)}
	if *workspace == "" && *session == "" && *tab == "" && *view == 0 {
		if os.Getenv("CW_WS") == "" || os.Getenv("CW_SLOT") == "" {
			return fmt.Errorf("fuera de cw indica --workspace y --session, o --workspace, --tab y --view")
		}
		a["workspace"], a["sesion"] = os.Getenv("CW_WS"), os.Getenv("CW_SLOT")
	}
	var m mcpTarget
	var id string
	if argStr(a, "sesion") != "" {
		m, err = targetSession(a)
		if err == nil && m.ws.App {
			id, err = m.viewID(map[string]any{"vista": float64(m.sess.View)}, "vista")
		}
	} else {
		m, err = targetTab(a, true)
		if err == nil && m.ws.App {
			id, err = m.viewID(a, "vista")
		}
	}
	if err != nil {
		return err
	}
	if !m.ws.App {
		return errAppOnly
	}
	if _, err := m.do("document.open", map[string]any{"view": id, "path": path}); err != nil {
		return err
	}
	fmt.Println("Markdown abierto:", path)
	return nil
}

func markdownInstructions(bin string) string {
	return fmt.Sprintf("Para mostrar un archivo Markdown al usuario en esta vista de la app, ejecuta con Bash: %s open -- <ruta.md>. Usa la ruta del archivo local (relativa al directorio actual o absoluta), entre comillas si contiene espacios. Se abre como subpestaña de solo lectura sin reemplazar ni detener las sesiones. No requiere MCP ni Skynet. Úsalo cuando el usuario pida abrir o mostrar el documento.", shQuote(bin))
}
