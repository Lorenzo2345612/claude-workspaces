//go:build !terminal

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexWrapperAliasesRetainExecutable(t *testing.T) {
	a, ok := parseClaudeAlias("claude-codex-yolo", "CLAUDE_YOLO=1 claude-codex --dangerously-skip-permissions")
	if !ok || a.Command != "claude-codex" || len(a.Args) != 1 || len(a.Env) != 1 {
		t.Fatal("Bridge alias was discarded", a, ok)
	}
	if _, ok := parseClaudeAlias("other", "printf claude-codex"); ok {
		t.Fatal("Accepted a command that merely mentions the bridge")
	}
	old := shellAliasCache
	t.Cleanup(func() { shellAliasCache = old })
	shellAliasCache = map[string]ClaudeAlias{a.Name: a}
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	cfg.ClaudeAliases = []ClaudeAlias{{Name: a.Name}}
	l := Launcher{WS: "not-running", Bin: "cw", ClaudeBin: "/unused/native-claude", Cfg: cfg}
	args, _ := l.claudeArgv(&Slot{ClaudeAlias: a.Name, Alias: "test", Cwd: t.TempDir()}, false)
	if len(args) < 2 || args[0] != "claude-codex" || args[1] != "--dangerously-skip-permissions" {
		t.Fatal("Fell back to native Claude without SHELL", args)
	}
	if !strings.Contains(cfg.claudeAlias(a.Name).summary(), "claude-codex") {
		t.Fatal("Preview hides actual launcher")
	}
}

func TestEditingCodexAliasDoesNotReplaceLauncher(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	old := shellAliasCache
	t.Cleanup(func() { shellAliasCache = old })
	shellAliasCache = map[string]ClaudeAlias{}
	cfg := defaultConfig()
	cfg.ClaudeAliases = []ClaudeAlias{{Name: "bridge", Command: "claude-codex", Args: []string{"--dangerously-skip-permissions"}}}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveAliasFromUI("bridge", "bridge", nil, []string{"--effort", "high"}); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.claudeAlias("bridge").Command != "claude-codex" {
		t.Fatal("Editing discarded the bridge executable")
	}
	if _, err := os.Stat(filepath.Join(configDir(), "config.json")); err != nil {
		t.Fatal(err)
	}
}
