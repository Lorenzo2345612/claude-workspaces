//go:build !terminal

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppHandoffBothDirectionsPreservesSource(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	root := t.TempDir()
	program := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\nexec sleep 300\n"), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cfg := defaultConfig()
	cfg.ClaudeCommand, cfg.CodexCommand = program("claude"), program("codex")
	cfg.ResumeCodexSkill, cfg.ResumeClaudeSkill = "transfer-to-codex", "transfer-to-claude"
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "app-handoff-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	launcher, err := newLauncher(mux.WS, cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := Slot{ID: newUUID(), Kind: kindClaude, Alias: "source", Cwd: root, SessionID: "original-claude-id", ClaudeAlias: "claude"}
	if err := appOpenSlot(mux, launcher, &source, false); err != nil {
		t.Fatal(err)
	}
	layout := &appLayout{Active: "tab", Tabs: []*appTab{{ID: "tab", Title: "test", Dir: root, Views: []*appView{{ID: "view", Slots: []string{source.ID}, Focus: source.ID}}}}}
	if err := mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	pane, err := mux.Run("display", "-p", "-t", "="+slotSession(source.ID)+":", "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_sid}|#{@cw_claude_alias}")
	result, err := wsAction(mux, &doReq{Action: "slot.handoff", Slot: source.ID, Alias: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	codexID := result["slot"].(string)
	codexPane, err := mux.Run("display", "-p", "-t", "="+slotSession(codexID)+":", "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	waitFor := func(pane, message string) {
		t.Helper()
		var output string
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			output, _ = mux.Run("capture-pane", "-p", "-J", "-S", "-", "-t", pane)
			if strings.Contains(output, message) {
				return
			}
		}
		t.Fatalf("missing initial message %q in %q", message, output)
	}
	waitFor(codexPane, "transfer-to-codex retoma la sesión de claude con id original-claude-id")
	after, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_sid}|#{@cw_claude_alias}")
	if before != after {
		t.Fatal("handoff changed original session", before, after)
	}
	// Emulate the real Codex SessionStart hook's allocated conversation ID.
	if _, err := mux.Run("set", "-p", "-t", codexPane, "@cw_sid", "actual-codex-id"); err != nil {
		t.Fatal(err)
	}
	result, err = wsAction(mux, &doReq{Action: "slot.handoff", Slot: codexID, Alias: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	targetPane, err := mux.Run("display", "-p", "-t", "="+slotSession(result["slot"].(string))+":", "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(targetPane, "transfer-to-claude retoma la sesión de codex con id actual-codex-id")
	if got := len(mux.loadLayout().Tabs[0].Views); got != 3 {
		t.Fatal("handoff views not saved", got)
	}
}
