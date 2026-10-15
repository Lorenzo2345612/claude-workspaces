package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// The tools that talk to a session: send it a message, answer its question, interrupt it. They
// type into the session's pane, so they work the same in app and terminal workspaces.

// permissionRe recognises a permission dialog (as opposed to a question Claude asks the user).
var permissionRe = regexp.MustCompile(`(?i)(do you want to|allow .* to|¿quieres|permitir)`)

// afterAction waits for the session to redraw and returns what it shows now.
func afterAction(s *mcpSession, what string) string {
	time.Sleep(900 * time.Millisecond)
	text, _ := screenOf(s, 0)
	rows := strings.Split(text, "\n")
	if len(rows) > 25 {
		rows = rows[len(rows)-25:]
	}
	return what + "\n\nAhora muestra:\n" + strings.Join(rows, "\n")
}

// pasteInto types text into a pane as one bracketed paste, so newlines do not submit it early.
func pasteInto(s *mcpSession, text string) error {
	t := Tmux{WS: s.Workspace}
	f, err := os.CreateTemp("", "cw-paste-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	f.Close()
	buf := "cw-mcp-" + shortID()
	if _, err := t.Run("load-buffer", "-b", buf, f.Name()); err != nil {
		return err
	}
	_, err = t.Run("paste-buffer", "-p", "-d", "-b", buf, "-t", s.pane)
	return err
}

func sendToSession(s *mcpSession, text string) error {
	switch s.Status {
	case "pregunta":
		return fmt.Errorf("%s está esperando una respuesta: usa responder_pregunta", s.Name)
	case "suspendida":
		return fmt.Errorf("%s está suspendida: reanúdala antes de escribirle", s.Name)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("el mensaje está vacío")
	}
	if err := pasteInto(s, text); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond) // let the paste land before submitting it
	_, err := Tmux{WS: s.Workspace}.Run("send-keys", "-t", s.pane, "Enter")
	return err
}

// answerQuestion moves the selection of the open dialog to option n and confirms it; with text,
// it picks the dialog's free-text option and types the answer there.
func answerQuestion(s *mcpSession, n int, text string, confirmed bool) (string, error) {
	q, err := pendingQuestion(s)
	if err != nil {
		return "", err
	}
	if _, waiting := q["aviso"]; waiting {
		return "", fmt.Errorf("%s no está esperando una respuesta ahora", s.Name)
	}
	screen, _ := q["pantalla"].(string)
	if permissionRe.MatchString(screen) && !confirmed {
		return "", fmt.Errorf("es un permiso: pregúntale al usuario y vuelve a llamar con confirmado_por_usuario=true cuando lo autorice")
	}
	opts, _ := q["opciones"].([]map[string]any)
	if len(opts) == 0 {
		return "", fmt.Errorf("no encontré opciones numeradas en la pantalla de %s", s.Name)
	}
	if text != "" && n == 0 {
		for _, o := range opts {
			t, _ := o["texto"].(string)
			if regexp.MustCompile(`(?i)^(type something|escribe|other|otra)`).MatchString(t) {
				n = o["n"].(int)
			}
		}
		if n == 0 {
			return "", fmt.Errorf("esta pregunta no tiene una opción para escribir una respuesta; elige una opción")
		}
	}
	if n < 1 || n > len(opts) {
		return "", fmt.Errorf("la opción %d no existe: hay %d", n, len(opts))
	}
	cur, _ := q["seleccionada"].(int)
	if cur == 0 {
		cur = 1
	}
	t := Tmux{WS: s.Workspace}
	key := "Down"
	if n < cur {
		key = "Up"
	}
	for i := 0; i < abs(n-cur); i++ {
		if _, err := t.Run("send-keys", "-t", s.pane, key); err != nil {
			return "", err
		}
		time.Sleep(60 * time.Millisecond)
	}
	if text != "" {
		time.Sleep(150 * time.Millisecond)
		if err := pasteInto(s, text); err != nil {
			return "", err
		}
		time.Sleep(250 * time.Millisecond)
	}
	if _, err := t.Run("send-keys", "-t", s.pane, "Enter"); err != nil {
		return "", err
	}
	chosen := opts[n-1]["texto"]
	return afterAction(s, fmt.Sprintf("Respondí a %s: opción %d (%v).", s.Name, n, chosen)), nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func mcpSessionTools() []mcpTool {
	sesProp := [2]string{"string", "Sesión: su id (o sus primeros caracteres) o su nombre."}
	wsProp := [2]string{"string", "Workspace (nombre o título), si el nombre de la sesión se repite."}
	tabProp := [2]string{"string", "Pestaña (número o título), si el nombre de la sesión se repite."}
	return []mcpTool{
		{Name: "enviar_a_sesion", Description: "Escribe un mensaje en una sesión y lo envía, como si el usuario lo tecleara. " +
			"En una sesión de Claude es una instrucción (si está trabajando, Claude la recibe al terminar el paso actual); en una terminal, un comando.",
			InputSchema: schema([]string{"sesion", "mensaje"}, map[string][2]string{"sesion": sesProp, "workspace": wsProp, "pestana": tabProp,
				"mensaje": {"string", "Texto a enviar."}}),
			run: func(a map[string]any) (any, error) {
				s, err := findSession(a)
				if err != nil {
					return nil, err
				}
				if err := sendToSession(s, argStr(a, "mensaje")); err != nil {
					return nil, err
				}
				return afterAction(s, "Enviado a "+s.Name+"."), nil
			}},
		{Name: "responder_pregunta", Description: "Responde la pregunta o el permiso que espera una sesión: elige la opción 'opcion' (número), " +
			"o escribe 'texto' en la opción de respuesta libre. Un permiso solo se responde con confirmado_por_usuario=true, " +
			"después de que el usuario lo autorizó explícitamente. Mira antes las opciones con ver_pregunta.",
			InputSchema: schema([]string{"sesion"}, map[string][2]string{"sesion": sesProp, "workspace": wsProp, "pestana": tabProp,
				"opcion":                 {"integer", "Número de la opción elegida."},
				"texto":                  {"string", "Respuesta escrita (usa la opción de respuesta libre)."},
				"confirmado_por_usuario": {"boolean", "true solo si el usuario autorizó esta respuesta a un permiso."}}),
			run: func(a map[string]any) (any, error) {
				s, err := findSession(a)
				if err != nil {
					return nil, err
				}
				ok, _ := a["confirmado_por_usuario"].(bool)
				if !ok {
					cfg, _ := loadConfig()
					ok = cfg.SkynetAutoApprove
				}
				return answerQuestion(s, argInt(a, "opcion", 0), argStr(a, "texto"), ok)
			}},
		{Name: "interrumpir", Description: "Interrumpe lo que hace una sesión: Esc en Claude (deja de trabajar y espera instrucciones), Ctrl+C en una terminal.",
			InputSchema: schema([]string{"sesion"}, map[string][2]string{"sesion": sesProp, "workspace": wsProp, "pestana": tabProp}),
			run: func(a map[string]any) (any, error) {
				s, err := findSession(a)
				if err != nil {
					return nil, err
				}
				key := "Escape"
				if s.Kind != kindClaude {
					key = "C-c"
				}
				if _, err := (Tmux{WS: s.Workspace}).Run("send-keys", "-t", s.pane, key); err != nil {
					return nil, err
				}
				return afterAction(s, "Interrumpí "+s.Name+"."), nil
			}},
	}
}
