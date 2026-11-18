package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const rule = "\x1b[38;5;244m" + "────────────────────────────────────────────────────────────" + "\x1b[39m"

// Screens captured from Claude Code 2.1.326 and Codex 0.167.0 (capture-pane -e).
func TestParseDraftBoxClaude(t *testing.T) {
	placeholder := []string{"", rule, "\x1b[39m❯\u00a0\x1b[2mTry \"how does main.go work?\"\x1b[0m", rule, "  \x1b[31m⚡ claude"}
	if b := parseDraftBox(placeholder, false, 100); !b.found || b.present {
		t.Fatalf("placeholder: %+v", b)
	}
	empty := []string{rule, "\x1b[39m❯\u00a0", rule, "  status"}
	if b := parseDraftBox(empty, false, 100); !b.found || b.present {
		t.Fatalf("vacía: %+v", b)
	}
	// cw starts Claude with --name: the top border carries the session's name.
	named := []string{"", "\x1b[38;5;244m" + strings.Repeat("─", 260) + " bug-110 ─\x1b[39m",
		"\x1b[39m❯\u00a0\x1b[2mTry \"edit report_type_service.py to…\"\x1b[0m", rule, "  status"}
	if b := parseDraftBox(named, false, 280); !b.found || b.present {
		t.Fatalf("borde con nombre: %+v", b)
	}
	named[2] = "\x1b[39m❯\u00a0borrador"
	if b := parseDraftBox(named, false, 280); !b.present || b.String() != "borrador" {
		t.Fatalf("borde con nombre y borrador: %+v", b)
	}
	if isRule("── texto largo que no es un borde ─") || isRule("texto ──────────────") {
		t.Fatal("tomó texto por un borde")
	}
	draft := []string{"\x1b[39m❯ responde solo: ok", "● ok", rule,
		"\x1b[39m❯\u00a0hola esto es un borrador largo que deberia envolverse",
		"  de claude code sin duda alguna", "  segunda linea", rule, "  status"}
	b := parseDraftBox(draft, false, 100)
	if !b.present || len(b.rows) != 3 || b.rows[0] != "hola esto es un borrador largo que deberia envolverse" || b.rows[2] != "segunda linea" {
		t.Fatalf("borrador: %+v", b)
	}
	// A "❯" of a menu (no rule above) is not the box.
	menu := []string{"Do you want to proceed?", "\x1b[39m❯ 1. Yes", "  2. No", ""}
	if b := parseDraftBox(menu, false, 100); b.found {
		t.Fatalf("menú tomado por la caja: %+v", b)
	}
}

func TestParseDraftBoxCodex(t *testing.T) {
	placeholder := []string{"\x1b[2m  permissions: \x1b[0m", "\x1b[1m›\x1b[0m \x1b[2mAsk Codex to do anything\x1b[0m", "", "  GPT medium"}
	if b := parseDraftBox(placeholder, true, 100); !b.found || b.present {
		t.Fatalf("placeholder: %+v", b)
	}
	rows := []string{"", "\x1b[1m›\x1b[0m primera linea corta",
		"  segunda linea que es bastante larga y deberia envolverse porque supera el ancho total de la caja",
		"  de codex", "  tercera", "", "  GPT medium"}
	update := []string{"  Update available", "", "\x1b[1;7m›\x1b[0m 1. Update now", "  2. Skip", "", "  enter continue"}
	if b := parseDraftBox(update, true, 100); b.found {
		t.Fatalf("menú tomado por la caja: %+v", b)
	}
	b := parseDraftBox(rows, true, 100)
	if !b.present || len(b.rows) != 4 {
		t.Fatalf("%+v", b)
	}
	want := "primera linea corta\nsegunda linea que es bastante larga y deberia envolverse porque supera el ancho total de la caja de codex\ntercera"
	if got := codexDraftText(b); got != want {
		t.Fatalf("\n%q\n%q", got, want)
	}
}

// TestSendKeepsDraftLive drives real Claude Code and Codex sessions: it spends a tiny turn on
// each, so it runs only with CW_LIVE_DRAFT=1.
func TestSendKeepsDraftLive(t *testing.T) {
	if os.Getenv("CW_LIVE_DRAFT") != "1" {
		t.Skip("CW_LIVE_DRAFT=1 para probar con Claude y Codex reales")
	}
	dir, _ := os.Getwd()
	for _, agent := range []string{"claude", "codex"} {
		t.Run(agent, func(t *testing.T) {
			if _, err := exec.LookPath(agent); err != nil {
				t.Skip(agent + " no disponible")
			}
			mux := Tmux{WS: "draft-" + shortID()}
			t.Cleanup(func() { _, _ = mux.Run("kill-server") })
			slot := Slot{ID: newUUID(), Kind: kindClaude, Alias: agent, ClaudeAlias: agent, Cwd: dir}
			argv := []string{agent}
			if agent == "claude" {
				argv = append(argv, "--name", "prueba-borrador") // as cw starts it: the name goes on the box's border
			}
			pane, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: slot, name: agent, argv: argv})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = mux.Run("set", "-p", "-t", pane, "@cw_claude_alias", agent)
			_, _ = mux.Run("resize-window", "-t", pane, "-x", "100", "-y", "30")
			s := &mcpSession{ID: slot.ID, Name: agent, Kind: kindClaude, Alias: agent, Workspace: mux.WS, pane: pane}
			codex := agent == "codex"
			ready := 0 // the box must stay on screen: Codex may still pop its update prompt
			for i := 0; i < 80 && ready < 6; i++ {
				text, _ := screenOf(s, 0)
				if strings.Contains(text, "Skip until next version") {
					_, _ = mux.Run("send-keys", "-t", pane, "Escape")
					ready = 0
				} else if b, _ := readDraftBox(s, codex); b.found {
					ready++
				}
				time.Sleep(500 * time.Millisecond)
			}
			if ready < 6 {
				text, _ := screenOf(s, 0)
				t.Fatal("no apareció la caja:\n" + text)
			}
			draft := "borrador a medias\nsegunda linea del borrador"
			if _, ok, err := pasteVerified(s, codex, draft); err != nil || !ok {
				t.Fatal("el borrador no llegó a la caja", err)
			}
			if err := sendToSession(s, "responde solo: recibido"); err != nil {
				text, _ := screenOf(s, 0)
				t.Fatalf("%v:\n%s", err, text)
			}
			b, ok := waitBox(s, codex, 5*time.Second, func(b draftBox) bool { return b.present })
			got := b.String()
			if codex {
				got = codexDraftText(b)
			}
			if !ok || got != draft {
				text, _ := screenOf(s, 0)
				t.Fatalf("borrador %q, quería %q:\n%s", got, draft, text)
			}
			for range 60 {
				if text, _ := screenOf(s, 200); strings.Contains(text, "recibido") && strings.Count(text, "responde solo: recibido") == 1 {
					return
				}
				time.Sleep(time.Second)
			}
			text, _ := screenOf(s, 200)
			t.Fatal("el mensaje no se envió:\n" + text)
		})
	}
}

// TestSendToNewSessionLive sends a long message to a Claude session the moment it starts.
func TestSendToNewSessionLive(t *testing.T) {
	if os.Getenv("CW_LIVE_DRAFT") != "1" {
		t.Skip("CW_LIVE_DRAFT=1 para probar con Claude real")
	}
	dir, _ := os.Getwd()
	mux := Tmux{WS: "nueva-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	slot := Slot{ID: newUUID(), Kind: kindClaude, Alias: "claude", ClaudeAlias: "claude", Cwd: dir}
	pane, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: slot, name: "claude", argv: []string{"claude", "--name", "bug-110"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mux.Run("resize-window", "-t", pane, "-x", "100", "-y", "30")
	s := &mcpSession{ID: slot.ID, Name: "claude", Kind: kindClaude, Alias: "claude", Workspace: mux.WS, pane: pane}
	msg := "Esto es un mensaje largo de prueba.\n" + strings.Repeat("Linea de contexto que no hace falta leer con atencion. ", 12) + "\nResponde solo: recibido"
	if err := sendToSession(s, msg); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		text, _ := screenOf(s, 300)
		if strings.Contains(text, "● recibido") || strings.Contains(text, "⏺ recibido") {
			return
		}
		time.Sleep(time.Second)
	}
	text, _ := screenOf(s, 0)
	t.Fatal("el mensaje no se envió:\n" + text)
}
