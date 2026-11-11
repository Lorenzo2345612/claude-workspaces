package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestToolArgs(t *testing.T) {
	a, err := toolArgs([]string{"--accion", "nueva", "--pestana", "2", "--titulo=42", "--confirmado_por_usuario", "--sesion", "1234"})
	if err != nil {
		t.Fatal(err)
	}
	if a["accion"] != "nueva" || a["pestana"] != float64(2) || a["titulo"] != "42" || a["confirmado_por_usuario"] != true || a["sesion"] != "1234" {
		t.Fatalf("%#v", a)
	}
	if _, err := toolArgs([]string{"suelto"}); err == nil {
		t.Fatal("aceptó un argumento sin --clave")
	}
}

func TestLastAssistantText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"hola"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"primera"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"final"},{"type":"text","text":"y más"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lastAssistantText(path); got != "final\ny más" {
		t.Fatalf("%q", got)
	}
	if lastAssistantText("") != "" || lastAssistantText(path+".no") != "" {
		t.Fatal("sin transcript debe quedar vacío")
	}
}

func TestInstructionsMentionTools(t *testing.T) {
	text := cwInstructions("/tmp/cw")
	for _, want := range []string{"open --", "send <sesión>", "tool pestana --accion nueva", "SendMessage", "CW_WS", "yo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("falta %q", want)
		}
	}
}

// TestSendAndReply: A sends to B, B ends its turn, and B's answer is typed into A.
func TestSendAndReply(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "send-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	root := t.TempDir()
	a := Slot{ID: newUUID(), Kind: kindClaude, Alias: "alfa", Cwd: root}
	b := Slot{ID: newUUID(), Kind: kindClaude, Alias: "beta", Cwd: root}
	if _, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: a, name: "alfa", argv: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mux.createWindow(windowSpec{session: "t01", slot: b, name: "beta", argv: []string{"cat"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_WS", mux.WS)
	t.Setenv("CW_SLOT", a.ID)
	if err := cmdSend([]string{"beta", "--", "revisa", "el", "plan"}); err != nil {
		t.Fatal(err)
	}
	to, err := findSession(map[string]any{"sesion": b.ID, "workspace": mux.WS})
	if err != nil {
		t.Fatal(err)
	}
	waitScreen(t, to, "revisa el plan")
	waitScreen(t, to, "Mensaje de la sesión «alfa»")
	if r := replyRoutes(mux, to.pane); len(r) != 1 || r[0] != mux.WS+"/"+a.ID {
		t.Fatal("ruta de respuesta", r)
	}
	// A second message from the same sender keeps a single route.
	if err := cmdSend([]string{b.ID, "--", "otra"}); err != nil {
		t.Fatal(err)
	}
	if r := replyRoutes(mux, to.pane); len(r) != 1 {
		t.Fatal("ruta duplicada", r)
	}
	// An interrupted turn (nothing said) keeps the route.
	if takeReply(mux, to.pane, hookInput{}) != nil || len(replyRoutes(mux, to.pane)) != 1 {
		t.Fatal("un turno sin respuesta no debe consumir la ruta")
	}
	p := takeReply(mux, to.pane, hookInput{LastAssistantMessage: "plan aprobado"})
	if p == nil || p.FromID != b.ID || p.FromName != "beta" || p.Text != "plan aprobado" {
		t.Fatalf("%#v", p)
	}
	if len(replyRoutes(mux, to.pane)) != 0 {
		t.Fatal("la ruta debe borrarse al entregar")
	}
	// Deliver synchronously, as the detached _deliver process would.
	file := filepath.Join(t.TempDir(), "r.json")
	data := `{"from_ws":"` + mux.WS + `","from_id":"` + p.FromID + `","from_name":"beta","routes":["` + p.Routes[0] + `"],"text":"plan aprobado"}`
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdDeliver([]string{file}); err != nil {
		t.Fatal(err)
	}
	from, err := findSession(map[string]any{"sesion": a.ID, "workspace": mux.WS})
	if err != nil {
		t.Fatal(err)
	}
	waitScreen(t, from, "Respuesta de la sesión «beta»")
	waitScreen(t, from, "plan aprobado")
	// No route back: B's answer must not bounce from A to B.
	if len(replyRoutes(mux, from.pane)) != 0 {
		t.Fatal("la respuesta armó una ruta de vuelta")
	}
	if err := cmdSend([]string{a.ID, "--", "yo"}); err == nil {
		t.Fatal("permitió enviarse a sí misma")
	}
}

func waitScreen(t *testing.T, s *mcpSession, want string) {
	t.Helper()
	var text string
	for range 40 {
		text, _ = screenOf(s, 200)
		if strings.Contains(strings.ReplaceAll(text, "\n", ""), want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("falta %q en la pantalla de %s:\n%s", want, s.Name, text)
}
