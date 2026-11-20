package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// When a session that has a coordinator stops on a question or a permission, it cannot finish
// its turn, so its coordinator would never hear back. cw tells the coordinator instead: the
// question, its options and how to answer, asking it to put the question to the user with its
// own question tool (AskUserQuestion) rather than as plain text, and to answer with
// cw tool responder_pregunta.
//
// Claude's folder trust prompt ("Is this a project you created or one you trust?") shows before
// any hook runs, with unnumbered options and "No, exit" selected: it is recognised on the
// screen, offered as 1 = trust, 2 = do not trust, and answered with the arrow keys.

var trustDialogRe = regexp.MustCompile(`(?i)(is this a project you created or one you trust|do you trust the files in this folder|yes, i trust this folder)`)

func isTrustDialog(screen string) bool { return trustDialogRe.MatchString(screen) }

// trustQuestion describes the trust prompt as a question with numbered options.
func trustQuestion(s *mcpSession, screen string) map[string]any {
	return map[string]any{"sesion": s.Name, "id": s.ID, "estado": "pregunta", "confianza": true, "permiso": true,
		"pantalla": screen, "seleccionada": 0,
		"pregunta": fmt.Sprintf("Claude se abre por primera vez en %s y pregunta si confías en esa carpeta (podrá leer, editar y ejecutar archivos ahí).", s.Dir),
		"opciones": []map[string]any{{"n": 1, "texto": "Sí, confío en esta carpeta"}, {"n": 2, "texto": "No (cierra la sesión)"}}}
}

// answerTrust picks an option of the trust prompt: the selected row is the one with "❯".
func answerTrust(s *mcpSession, n int, confirmed bool) (string, error) {
	if n != 1 && n != 2 {
		return "", fmt.Errorf("el diálogo de confianza tiene 2 opciones: 1 (confiar) o 2 (no confiar)")
	}
	if n == 1 && !confirmed {
		return "", errors.New("confiar en una carpeta es un permiso: pregúntale al usuario y vuelve a llamar con confirmado_por_usuario=true cuando lo autorice")
	}
	screen, err := screenOf(s, 0)
	if err != nil {
		return "", err
	}
	onYes := regexp.MustCompile(`(?m)^\s*[❯>›]\s*(\d\.\s*)?Yes`).MatchString(screen)
	t := Tmux{WS: s.Workspace}
	var keys []string
	switch {
	case n == 1 && !onYes:
		keys = []string{"Down", "Enter"}
	case n == 2 && onYes:
		keys = []string{"Up", "Enter"}
	default:
		keys = []string{"Enter"}
	}
	for _, k := range keys {
		if _, err := t.Run("send-keys", "-t", s.pane, k); err != nil {
			return "", err
		}
		time.Sleep(150 * time.Millisecond)
	}
	if n == 1 {
		return afterAction(s, "Confié en la carpeta de "+s.Name+"; ya puedes enviarle mensajes."), nil
	}
	return afterAction(s, "No confié en la carpeta: "+s.Name+" se cierra."), nil
}

// notifyParentOnWait runs from the hook when a session starts waiting: if it has a
// coordinator, a detached process reads the question once it is drawn and tells it.
func notifyParentOnWait(t Tmux, pane string, data []byte) {
	parent, err := t.Run("display", "-p", "-t", pane, "#{@cw_parent}")
	if err != nil || strings.TrimSpace(parent) == "" {
		return
	}
	dir := filepath.Join(runDir(), "asks")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	f, err := os.CreateTemp(dir, "ask-*.json")
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	f.Close()
	cmd := exec.Command(selfBin(), "_ask-parent", t.WS, pane, f.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// clearAskedOnResume forgets the last question told to the coordinator once the session moves
// on, so the same question asked again later is told again.
func clearAskedOnResume(t Tmux, pane string) {
	_, _ = t.Run("set", "-p", "-u", "-t", pane, "@cw_asked")
}

// cmdAskParent: _ask-parent <ws> <pane> <hook payload file>.
func cmdAskParent(args []string) error {
	if len(args) != 3 {
		return errors.New("uso: cw _ask-parent <ws> <panel> <archivo>")
	}
	ws, pane, file := args[0], args[1], args[2]
	data, _ := os.ReadFile(file)
	_ = os.Remove(file)
	var in hookInput
	_ = json.Unmarshal(data, &in)
	t := Tmux{WS: ws}
	slot, err := t.Run("display", "-p", "-t", pane, "#{@cw_slot}")
	if err != nil {
		return err
	}
	// The hook fires before the dialog is drawn: wait for it.
	var s *mcpSession
	var q map[string]any
	for i := 0; i < 8; i++ {
		time.Sleep(time.Second)
		if s, err = findSession(map[string]any{"sesion": slot, "workspace": ws}); err != nil {
			return err
		}
		if s.Status != "pregunta" {
			return nil // answered meanwhile
		}
		if q, err = pendingQuestion(s); err == nil {
			if _, waiting := q["aviso"]; !waiting {
				break
			}
		}
		q = nil
	}
	if q == nil || s.Parent == "" {
		return nil
	}
	body := askParentMessage(s, q, in)
	sig := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(q["pregunta"], q["opciones"]))))[:16]
	if prev, _ := t.Run("display", "-p", "-t", pane, "#{@cw_asked}"); prev == sig {
		return nil // already told (PreToolUse and Notification fire for the same dialog)
	}
	_, _ = t.Run("set", "-p", "-t", pane, "@cw_asked", sig)
	deliverTo([]string{ws + "/" + s.Parent}, body, time.Hour)
	return nil
}

// askParentMessage is what the coordinator reads: who is stuck, on what, and how to answer.
func askParentMessage(s *mcpSession, q map[string]any, in hookInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[cw] La sesión «%s» (id %s), que coordinas, se detuvo esperando una respuesta y no puede seguir hasta que alguien conteste.\n\n", s.Name, s.ID)
	perm, _ := q["permiso"].(bool)
	switch {
	case in.ToolName == "AskUserQuestion" && len(in.ToolInput) > 0:
		var ask struct {
			Questions []struct {
				Question    string `json:"question"`
				MultiSelect bool   `json:"multiSelect"`
				Options     []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(in.ToolInput, &ask) == nil && len(ask.Questions) > 0 {
			for i, qq := range ask.Questions {
				if len(ask.Questions) > 1 {
					fmt.Fprintf(&b, "Pregunta %d de %d", i+1, len(ask.Questions))
					if qq.MultiSelect {
						b.WriteString(" (admite varias)")
					}
					b.WriteString(":\n")
				}
				b.WriteString(qq.Question + "\n")
				for j, o := range qq.Options {
					fmt.Fprintf(&b, "%d. %s", j+1, o.Label)
					if o.Description != "" {
						b.WriteString(" — " + o.Description)
					}
					b.WriteString("\n")
				}
				b.WriteString("\n")
			}
			break
		}
		fallthrough
	default:
		if in.ToolName != "" && in.ToolName != "AskUserQuestion" {
			perm = true
			fmt.Fprintf(&b, "Permiso: quiere usar %s", in.ToolName)
			if what := toolInputSummary(in.ToolInput); what != "" {
				b.WriteString(": " + what)
			}
			b.WriteString("\n")
		} else if text, _ := q["pregunta"].(string); text != "" {
			b.WriteString(text + "\n")
		} else {
			screen, _ := q["pantalla"].(string)
			rows := strings.Split(screen, "\n")
			b.WriteString(strings.Join(rows[max(0, len(rows)-14):], "\n") + "\n")
		}
		if opts, ok := q["opciones"].([]map[string]any); ok {
			b.WriteString("Opciones en pantalla:\n")
			for _, o := range opts {
				fmt.Fprintf(&b, "%v. %v\n", o["n"], o["texto"])
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Pregúntaselo al usuario con tu herramienta de preguntas (AskUserQuestion), con estas mismas opciones, en lugar de escribírselo como texto. No elijas tú sin preguntarle. Con su respuesta, ejecuta:\n")
	if perm {
		fmt.Fprintf(&b, "\"$CW_BIN\" tool responder_pregunta --sesion %s --opcion N --confirmado_por_usuario\n", s.ID)
	} else {
		fmt.Fprintf(&b, "\"$CW_BIN\" tool responder_pregunta --sesion %s --opcion N\n", s.ID)
		fmt.Fprintf(&b, "o, para una respuesta escrita: \"$CW_BIN\" tool responder_pregunta --sesion %s --texto \"…\"\n", s.ID)
	}
	b.WriteString("Si hay varias preguntas, aparecen una tras otra: responde cada una (\"$CW_BIN\" tool ver_pregunta --sesion " + s.ID + " muestra la actual).")
	return b.String()
}

// toolInputSummary shows the gist of a tool call: the command, the file, the URL.
func toolInputSummary(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "url", "pattern", "prompt", "description"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			if len([]rune(v)) > 400 {
				v = string([]rune(v)[:400]) + "…"
			}
			return v
		}
	}
	return ""
}

// trustError tells whoever wanted to write to a session that it waits on the trust prompt.
func trustError(s *mcpSession) error {
	return fmt.Errorf("%s no puede recibir mensajes todavía: Claude se abrió por primera vez en %s y pregunta si confías en esa carpeta. "+
		"Pregúntaselo al usuario con AskUserQuestion; si acepta, ejecuta \"$CW_BIN\" tool responder_pregunta --sesion %s --opcion 1 --confirmado_por_usuario "+
		"y vuelve a enviar el mensaje. No envié nada", s.Name, s.Dir, s.ID)
}
