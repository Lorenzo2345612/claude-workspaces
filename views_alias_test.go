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

func TestJoinViewsPreservesAliasAndSwitchResumesThroughBridge(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	old := shellAliasCache
	t.Cleanup(func() { shellAliasCache = old })
	shellAliasCache = map[string]ClaudeAlias{}
	root := t.TempDir()
	program := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+name+"' \"$@\"\nexec sleep 300\n"), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	native, bridge := program("native-launcher"), program("bridge-launcher")
	cfg := defaultConfig()
	cfg.ClaudeCommand = native
	cfg.ClaudeAliases = []ClaudeAlias{{Name: "native"}, {Name: "claude-codex-yolo", Command: bridge, Args: []string{"--dangerously-skip-permissions"}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "join-alias-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	l := Launcher{WS: mux.WS, Bin: "/unused/cw", ClaudeBin: native, Cfg: cfg}
	slots := []Slot{{ID: newUUID(), Kind: kindClaude, Alias: "native", Cwd: root, SessionID: newUUID(), ClaudeAlias: "native"}, {ID: newUUID(), Kind: kindClaude, Alias: "codex", Cwd: root, SessionID: newUUID(), ClaudeAlias: "claude-codex-yolo"}}
	var windows, panes, before []string
	for i := range slots {
		win, err := l.openSlot(mux, "test", i == 0, &slots[i], false)
		if err != nil {
			t.Fatal(err)
		}
		windows = append(windows, win)
		pane, err := mux.Run("display", "-p", "-t", win, "#{pane_id}")
		if err != nil {
			t.Fatal(err)
		}
		panes = append(panes, pane)
		state, err := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_slot}|#{@cw_sid}|#{@cw_claude_alias}")
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, state)
	}
	if err := cmdJoin([]string{mux.WS, windows[1], panes[0]}); err != nil {
		t.Fatal(err)
	}
	for i, pane := range panes {
		after, err := mux.Run("display", "-p", "-t", pane, "#{pane_pid}|#{@cw_slot}|#{@cw_sid}|#{@cw_claude_alias}")
		if err != nil || after != before[i] {
			t.Fatal("Merge changed process, conversation or alias", before[i], after, err)
		}
	}
	saved, err := mux.capture()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tabs) != 1 || len(saved.Tabs[0].Slots) != 2 || saved.Tabs[0].Slots[1].ClaudeAlias != "claude-codex-yolo" {
		t.Fatalf("Saving merged views lost launcher: %+v", saved)
	}
	dir := filepath.Join(claudeConfigDir(), "projects", "fixture")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, slots[0].SessionID+".jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := switchAlias(mux.WS, panes[0], "claude-codex-yolo"); err != nil {
		t.Fatal(err)
	}
	var output string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		output, _ = mux.Run("capture-pane", "-p", "-S", "-", "-J", "-t", panes[0])
		if strings.Contains(output, "bridge-launcher") && strings.Contains(output, "--resume") {
			break
		}
	}
	if !strings.Contains(output, "bridge-launcher") || !strings.Contains(output, slots[0].SessionID) || !strings.Contains(output, "--resume") {
		t.Fatal("Switch after merging did not resume through bridge", output)
	}
	after, _ := mux.Run("display", "-p", "-t", panes[1], "#{pane_pid}|#{@cw_slot}|#{@cw_sid}|#{@cw_claude_alias}")
	if after != before[1] {
		t.Fatal("Switching one merged session restarted another")
	}
}
