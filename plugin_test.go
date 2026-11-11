package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePlugin(t *testing.T) {
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })
	dir := filepath.Join(t.TempDir(), "plugin")
	for range 2 { // rewriting over an older copy must work
		if err := writePlugin(dir); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		".claude-plugin/marketplace.json", ".agents/plugins/marketplace.json",
		"plugins/cw/.claude-plugin/plugin.json", "plugins/cw/.codex-plugin/plugin.json",
	} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if strings.Contains(f, "plugin.json") && m["version"] != "1.2.3" {
			t.Fatalf("%s: versión %v", f, m["version"])
		}
	}
	for _, skill := range []string{"cw", "cw-coordinar"} {
		data, err := os.ReadFile(filepath.Join(dir, "plugins/cw/skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), "---\nname: "+skill+"\ndescription: ") || !strings.Contains(string(data), `"$CW_BIN"`) {
			t.Fatalf("skill %s mal formada", skill)
		}
	}
}

func TestPluginTargetsOnePerAccount(t *testing.T) {
	cfg := Config{ClaudeCommand: "claude", ClaudeAliases: []ClaudeAlias{
		{Name: "dos", Env: []string{"CLAUDE_CONFIG_DIR=~/.claude-2"}},
		{Name: "dos-yolo", Env: []string{"CLAUDE_CONFIG_DIR=~/.claude-2", "CLAUDE_YOLO=1"}},
	}}
	var claude []string
	for _, tg := range pluginTargets(cfg) {
		if tg.Agent == "claude" {
			claude = append(claude, strings.Join(tg.Env, ","))
		}
	}
	if len(claude) != 2 || claude[0] != "" || !strings.HasSuffix(claude[1], "/.claude-2") {
		t.Fatalf("%q", claude)
	}
}

// TestPaneKnowsItRunsInCW: the pane's environment carries the workspace, the session and cw's
// binary, and cw yo answers from inside it.
func TestPaneKnowsItRunsInCW(t *testing.T) {
	isolateUsage(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux no disponible")
	}
	mux := Tmux{WS: "yo-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	slot := Slot{ID: newUUID(), Kind: kindShell, Alias: "shell", Cwd: t.TempDir()}
	if _, err := mux.createWindow(windowSpec{session: "t01", first: true, slot: slot, name: "shell",
		argv: []string{"sh", "-c", "env | grep ^CW_; sleep 300"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_WS", mux.WS)
	t.Setenv("CW_SLOT", slot.ID)
	s, err := caller()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CW_WS=" + mux.WS, "CW_SLOT=" + slot.ID, "CW_BIN=" + selfBin()} {
		waitScreen(t, s, want)
	}
	if err := cmdWhoAmI(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_SLOT", "")
	if err := cmdWhoAmI(); err == nil {
		t.Fatal("cw yo debe fallar fuera de cw")
	}
}
