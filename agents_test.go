package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNativeCodexLaunchAndRestore(t *testing.T) {
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	cfg.ClaudeArgs = []string{"--permission-mode", "plan"}
	cfg.CodexArgs = []string{"--model", "test-model"}
	l := Launcher{Bin: "/cw", WS: "test", Cfg: cfg}
	s := Slot{ID: "slot", Kind: kindClaude, ClaudeAlias: "codex", Args: []string{"--search"}}
	argv, _ := l.claudeArgv(&s, false)
	if argv[0] != "codex" || s.SessionID != "" {
		t.Fatal("new Codex must allocate its own conversation ID", argv, s)
	}
	for _, flag := range []string{"--session-id", "--resume", "--settings", "--name", "--permission-mode"} {
		if slices.Contains(argv, flag) {
			t.Fatalf("Claude flag %s leaked into Codex: %v", flag, argv)
		}
	}
	if !slices.Contains(argv, "test-model") || !slices.Contains(argv, "--search") {
		t.Fatal(argv)
	}
	if !strings.Contains(strings.Join(argv, " "), "hooks.SessionStart=") {
		t.Fatal("missing tracking hook", argv)
	}
	s.SessionID, s.Status = "codex-conversation", statusBusy
	argv, display := l.claudeArgv(&s, true)
	if !slices.Contains(argv, "resume") || !slices.Contains(argv, s.SessionID) || argv[len(argv)-1] != cfg.ContinueMessage || display != statusBusy {
		t.Fatal(argv, display)
	}
	s.Suspended = true
	argv, display = l.claudeArgv(&s, true)
	if argv[0] != "/cw" || argv[1] != "_wait" || display != statusSleeping {
		t.Fatal(argv, display)
	}
}

func TestHandoffUsesTargetSkillAndSourceID(t *testing.T) {
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	cfg.ResumeClaudeSkill, cfg.ResumeCodexSkill = "mi-skill-claude", "mi-skill-codex"
	l := Launcher{Bin: "/cw", WS: "test", ClaudeBin: "claude", Cfg: cfg}
	for _, pair := range [][2]string{{"claude", "codex"}, {"codex", "claude"}} {
		source := Slot{ID: "source", Kind: kindClaude, Alias: "trabajo", Cwd: t.TempDir(), ClaudeAlias: pair[0], SessionID: "source-conversation", Args: []string{"--source-only"}}
		next, err := handoffSlot(source, pair[1], cfg)
		if err != nil {
			t.Fatal(err)
		}
		if next.Cwd != source.Cwd || next.SessionID != "" || next.ID != "" || len(next.Args) != 0 {
			t.Fatal("handoff reused source identity or flags", next)
		}
		argv, _ := l.claudeArgv(&next, false)
		expected := "mi-skill-" + pair[1] + " retoma la sesión de " + pair[0] + " con id source-conversation"
		if argv[len(argv)-1] != expected {
			t.Fatal(argv)
		}
		if next.SessionID == source.SessionID {
			t.Fatal("source ID passed as destination ID")
		}
		data, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		var restored Slot
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.InitialMessage != "" {
			t.Fatal("handoff would replay on workspace restore")
		}
		if source.SessionID != "source-conversation" {
			t.Fatal("source mutated")
		}
	}
	cfg.ResumeCodexSkill = ""
	if _, err := handoffSlot(Slot{Kind: kindClaude, SessionID: "id"}, "codex", cfg); err == nil {
		t.Fatal("empty skill accepted")
	}
	if _, err := handoffSlot(Slot{Kind: kindClaude, ClaudeAlias: "codex"}, "claude", cfg); err == nil {
		t.Fatal("missing source ID accepted")
	}
	if err := checkAgentAlias(cfg, "claude", "codex"); err == nil {
		t.Fatal("cross-provider alias switch accepted")
	}
}

func TestConfigAddsCodexWithoutClaudeInstalled(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	cfg := defaultConfig()
	cfg.ClaudeCommand = "/missing/claude"
	cfg.ResumeClaudeSkill, cfg.ResumeCodexSkill = "custom-claude", "custom-codex"
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	restored, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if restored.ResumeClaudeSkill != cfg.ResumeClaudeSkill || restored.ResumeCodexSkill != cfg.ResumeCodexSkill {
		t.Fatal(restored)
	}
	if !restored.claudeAlias("codex").isCodex() {
		t.Fatal("Codex unavailable in agent selector")
	}
	if _, err := newLauncher("test", restored); err != nil {
		t.Fatal(err)
	}
	a, ok := parseClaudeAlias("codex-work", "codex --model test-model")
	if !ok || !a.isCodex() {
		t.Fatal(a, ok)
	}
	if (ClaudeAlias{Command: "claude-codex"}).isCodex() {
		t.Fatal("Claude-compatible bridge mistaken for native Codex")
	}
}

func TestCodexHookTracksCorrectPaneAndRestoresID(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "codex-hook-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	root := t.TempDir()
	fake := filepath.Join(root, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 300\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.CodexCommand = fake
	l := Launcher{Bin: "/cw", WS: mux.WS, Cfg: cfg}
	s := Slot{ID: newUUID(), Kind: kindClaude, ClaudeAlias: "codex", Alias: "codex", Cwd: root}
	win, err := l.openSlot(mux, "test", true, &s, false)
	if err != nil {
		t.Fatal(err)
	}
	pane, err := mux.Run("display", "-p", "-t", win, "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CW_WS", mux.WS)
	t.Setenv("CW_SLOT", s.ID)
	t.Setenv("TMUX_PANE", "%wrong-pane")
	input, err := os.CreateTemp(root, "hook-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString(`{"hook_event_name":"UserPromptSubmit","session_id":"actual-codex-id"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = old }()
	if err := cmdHook(); err != nil {
		t.Fatal(err)
	}
	recorded, err := paneSlot(mux, pane)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.SessionID != "actual-codex-id" || recorded.Status != statusBusy {
		t.Fatal(recorded)
	}
	saved, err := mux.capture()
	if err != nil {
		t.Fatal(err)
	}
	restored := saved.Tabs[0].Slots[0]
	argv, _ := l.claudeArgv(&restored, true)
	if !slices.Contains(argv, "resume") || !slices.Contains(argv, "actual-codex-id") {
		t.Fatal("restore lost Codex conversation", argv)
	}
	if _, err := os.Stat(filepath.Join(dataDir(), "usage-owners.jsonl")); !os.IsNotExist(err) {
		t.Fatal("Codex attributed to Claude usage", err)
	}
}
