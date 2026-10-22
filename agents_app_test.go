//go:build !terminal

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppHandoffReplacesSameSlotWithSelectedAlias(t *testing.T) {
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
	cfg.ClaudeAliases = []ClaudeAlias{{Name: "codex-work", Command: "codex", Args: []string{"--model", "selected-codex"}}, {Name: "claude-work", Args: []string{"--model", "selected-claude"}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "app-replace-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	launcher, err := newLauncher(mux.WS, cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := Slot{ID: newUUID(), Kind: kindClaude, Alias: "source", Cwd: root, SessionID: "original-claude-id", ClaudeAlias: "claude"}
	sibling := Slot{ID: newUUID(), Kind: kindShell, Alias: "sibling", Cwd: root}
	for _, s := range []*Slot{&source, &sibling} {
		if err := appOpenSlot(mux, launcher, s, false); err != nil {
			t.Fatal(err)
		}
	}
	layout := &appLayout{Active: "tab", Tabs: []*appTab{{ID: "tab", Title: "test", Dir: root, Active: "view", Views: []*appView{{ID: "view", Slots: []string{source.ID, sibling.ID}, Focus: source.ID, Layout: layoutCols, Weights: []float64{0.3, 0.7}}}}}}
	if err := mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	beforeLayout, _ := json.Marshal(mux.loadLayout())
	pane, err := mux.Run("display", "-p", "-t", "="+slotSession(source.ID)+":", "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	siblingPID, _ := mux.Run("display", "-p", "-t", "="+slotSession(sibling.ID)+":", "#{pane_pid}")
	before, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_sid}|#{@cw_claude_alias}")
	for _, alias := range []string{"", "unknown", "claude-work"} {
		if _, err := wsAction(mux, &doReq{Action: "slot.handoff", Slot: source.ID, Agent: "codex", Alias: alias}); err == nil {
			t.Fatal("invalid selection accepted", alias)
		}
		after, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_sid}|#{@cw_claude_alias}")
		if before != after {
			t.Fatal("failed selection changed live session", before, after)
		}
	}
	waitFor := func(message string) {
		t.Helper()
		var output string
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			output, _ = mux.Run("capture-pane", "-p", "-J", "-S", "-", "-t", pane)
			if strings.Contains(output, message) {
				return
			}
		}
		t.Fatalf("missing message %q in %q", message, output)
	}
	for _, step := range []struct{ agent, alias, message, model string }{{"codex", "codex-work", "transfer-to-codex retoma la sesión de claude con id original-claude-id", "selected-codex"}, {"claude", "claude-work", "transfer-to-claude retoma la sesión de codex con id actual-codex-id", "selected-claude"}} {
		result, err := wsAction(mux, &doReq{Action: "slot.handoff", Slot: source.ID, Agent: step.agent, Alias: step.alias})
		if err != nil {
			t.Fatal(err)
		}
		if result["slot"] != source.ID {
			t.Fatal("handoff allocated a new slot", result)
		}
		waitFor(step.message)
		waitFor(step.model)
		recorded, err := paneSlot(mux, pane)
		if err != nil {
			t.Fatal(err)
		}
		if recorded.ID != source.ID || recorded.Alias != source.Alias || recorded.Cwd != root || recorded.ClaudeAlias != step.alias {
			t.Fatal("replacement changed slot identity or selected alias", recorded)
		}
		if recorded.SessionID == "original-claude-id" {
			t.Fatal("source conversation ID was reused")
		}
		afterLayout, _ := json.Marshal(mux.loadLayout())
		if string(beforeLayout) != string(afterLayout) {
			t.Fatal("handoff changed view, order, focus or pane weights", string(afterLayout))
		}
		afterPID, _ := mux.Run("display", "-p", "-t", "="+slotSession(sibling.ID)+":", "#{pane_pid}")
		if siblingPID != afterPID {
			t.Fatal("handoff restarted another session")
		}
		if step.agent == "codex" {
			if _, err := mux.Run("set", "-p", "-t", pane, "@cw_sid", "actual-codex-id"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAppCreatesCodexSessionAndTabExplicitly(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	fake := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 300\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.CodexCommand = fake
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "explicit-codex-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	ln, err := newLauncher(mux.WS, cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seed := Slot{Kind: kindShell, Alias: "shell", Cwd: dir}
	if err := appOpenSlot(mux, ln, &seed, false); err != nil {
		t.Fatal(err)
	}
	layout := &appLayout{Active: "tab", Tabs: []*appTab{{ID: "tab", Title: "Test", Dir: dir, Views: []*appView{{ID: "view", Slots: []string{seed.ID}}}}}}
	if err := mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	result, err := wsAction(mux, &doReq{Action: "slot.new", Tab: "tab", Kind: "codex", Name: "Codex session"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(id string) {
		t.Helper()
		pane, err := mux.Run("display", "-p", "-t", "="+slotSession(id)+":", "#{pane_id}")
		if err != nil {
			t.Fatal(err)
		}
		s, err := paneSlot(mux, pane)
		if err != nil {
			t.Fatal(err)
		}
		if s.ClaudeAlias != "codex" || s.Kind != kindClaude {
			t.Fatal("Codex request launched another agent", s)
		}
	}
	check(result["slot"].(string))
	result, err = wsAction(mux, &doReq{Action: "tab.new", Title: "Codex tab", Dir: dir, Start: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	tab := mux.loadLayout().tab(result["tab"].(string))
	if tab == nil || len(tab.Views) != 1 || len(tab.Views[0].Slots) != 1 {
		t.Fatal("Codex tab has no session", tab)
	}
	check(tab.Views[0].Slots[0])
	for _, alias := range uiAliases(cfg) {
		if alias.Name == "codex" && alias.Agent != "codex" {
			t.Fatal("UI cannot distinguish Codex", alias)
		}
	}
}
