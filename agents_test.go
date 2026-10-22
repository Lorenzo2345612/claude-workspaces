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
		prefix := "/"
		if pair[1] == "codex" {
			prefix = "$"
		}
		expected := prefix + "mi-skill-" + pair[1] + " retoma la sesión de " + pair[0] + " con id source-conversation"
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

func TestExplicitAgentSelectionAndTerminalMenus(t *testing.T) {
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	cfg.DefaultClaudeAlias = "codex"
	if got := cfg.aliasForAgent("claude", cfg.defaultAlias()); got != "claude" {
		t.Fatal("Claude choice inherited Codex default", got)
	}
	kind, alias, err := cfg.newAgentSlot("codex", "")
	if err != nil || kind != kindClaude || alias != "codex" {
		t.Fatal(kind, alias, err)
	}
	if _, _, err := cfg.newAgentSlot("codex", "claude"); err == nil {
		t.Fatal("explicit Codex accepted a Claude launcher")
	}
	form := newTabForm{claudeAliases: cfg.claudeAliases()}
	form.chooseStart(startCodex)
	if !form.claudeAliases[form.aliasIdx].isCodex() {
		t.Fatal("Codex tab chose Claude")
	}
	form.cycleAlias(1)
	if !form.claudeAliases[form.aliasIdx].isCodex() {
		t.Fatal("Codex tab cycled into Claude")
	}
	form.chooseStart(startClaude)
	if form.claudeAliases[form.aliasIdx].isCodex() {
		t.Fatal("Claude tab chose Codex")
	}
	session := sessionForm{claudeAliases: cfg.claudeAliases()}
	session.selectAgent("codex")
	if !cfg.claudeAlias(session.selectedAlias()).isCodex() {
		t.Fatal("session agent selector did not choose Codex")
	}
	session.selectAgent("claude")
	if cfg.claudeAlias(session.selectedAlias()).isCodex() {
		t.Fatal("session agent selector did not choose Claude")
	}
	conf := tmuxConf("test", "/cw", true, true)
	if strings.Contains(conf, "%!") {
		t.Fatal("invalid menu format", conf)
	}
	for _, text := range []string{"_ui newcodex", "+ Codex", "Sesión de Codex\" D", "newcodex]"} {
		if !strings.Contains(conf, text) {
			t.Fatalf("Codex missing in terminal entry point %q", text)
		}
	}
}

func TestHandoffRejectsUnselectedAndWrongAgentAliases(t *testing.T) {
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	cfg.ClaudeAliases = []ClaudeAlias{{Name: "codex-custom", Command: "codex"}, {Name: "claude-custom", Args: []string{"--model", "test"}}}
	source := Slot{Kind: kindClaude, Alias: "source", SessionID: "source-id", ClaudeAlias: "claude"}
	for _, alias := range []string{"", "unknown", "claude-custom"} {
		if _, err := handoffSlot(source, "codex", cfg, alias); err == nil {
			t.Fatal("invalid alias accepted", alias)
		}
	}
	next, err := handoffSlot(source, "codex", cfg, "codex-custom")
	if err != nil || next.ClaudeAlias != "codex-custom" {
		t.Fatal("explicit alias ignored", next, err)
	}
	for _, agent := range []string{"claude", "codex"} {
		if ctxDialogs["handoff-"+agent].w == 0 {
			t.Fatal("terminal handoff skips alias dialog", agent)
		}
	}
}

func TestTerminalHandoffPreservesCombinedPaneAndRejectsMissingExecutable(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	root := t.TempDir()
	fake := filepath.Join(root, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 300\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.ClaudeCommand = fake
	cfg.CodexCommand = fake
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	mux := Tmux{WS: "terminal-handoff-" + shortID()}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	launcher, err := newLauncher(mux.WS, cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := Slot{ID: newUUID(), Kind: kindClaude, Alias: "source", Cwd: root, SessionID: "original-id", ClaudeAlias: "claude"}
	win, err := launcher.openSlot(mux, "test", true, &source, false)
	if err != nil {
		t.Fatal(err)
	}
	pane, err := mux.Run("display", "-p", "-t", win, "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	sibling := Slot{ID: newUUID(), Kind: kindShell, Alias: "shell", Cwd: root}
	other, err := mux.createPane(launcher.slotSpec("test", false, &sibling, false), win)
	if err != nil {
		t.Fatal(err)
	}
	oldPID, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}")
	siblingPID, _ := mux.Run("display", "-p", "-t", other, "#{pane_pid}")
	before, _ := mux.Run("list-panes", "-t", win, "-F", "#{pane_id}|#{@cw_slot}")
	cfg.CodexCommand = "/missing/codex"
	if _, err := replaceAgentPane(mux, pane, "codex", "codex", cfg); err == nil {
		t.Fatal("missing executable accepted")
	}
	afterPID, _ := mux.Run("display", "-p", "-t", pane, "#{pane_pid}")
	if afterPID != oldPID {
		t.Fatal("failed replacement killed source")
	}
	cfg.CodexCommand = fake
	next, err := replaceAgentPane(mux, pane, "codex", "codex", cfg)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := mux.Run("list-panes", "-t", win, "-F", "#{pane_id}|#{@cw_slot}")
	if before != after || next.ID != source.ID {
		t.Fatal("replacement added or moved a pane", before, after, next)
	}
	recorded, err := paneSlot(mux, pane)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.ClaudeAlias != "codex" || recorded.Alias != source.Alias || recorded.SessionID != "" {
		t.Fatal("invalid destination metadata", recorded)
	}
	afterSiblingPID, _ := mux.Run("display", "-p", "-t", other, "#{pane_pid}")
	if siblingPID != afterSiblingPID {
		t.Fatal("restarted sibling pane")
	}
}

func TestHandoffSkillInvocationPrefix(t *testing.T) {
	t.Setenv("SHELL", "")
	cases := []struct{ agent, skill, want string }{
		{"claude", "mi-skill", "/mi-skill retoma la sesión de claude con id source-id"},
		{"claude", " /mi-skill ", "/mi-skill retoma la sesión de claude con id source-id"},
		{"claude", "$mi-skill", "/mi-skill retoma la sesión de claude con id source-id"},
		{"codex", "mi-skill", "$mi-skill retoma la sesión de claude con id source-id"},
		{"codex", " $mi-skill ", "$mi-skill retoma la sesión de claude con id source-id"},
		{"codex", "/mi-skill", "$mi-skill retoma la sesión de claude con id source-id"},
	}
	for _, c := range cases {
		t.Run(c.agent+":"+c.skill, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.ResumeClaudeSkill, cfg.ResumeCodexSkill = c.skill, c.skill
			next, err := handoffSlot(Slot{Kind: kindClaude, SessionID: "source-id", ClaudeAlias: "claude"}, c.agent, cfg)
			if err != nil {
				t.Fatal(err)
			}
			launcher := Launcher{WS: "not-running", Bin: "/cw", ClaudeBin: "claude", Cfg: cfg}
			argv, _ := launcher.claudeArgv(&next, false)
			if argv[len(argv)-1] != c.want {
				t.Fatalf("want %q, got %q", c.want, argv[len(argv)-1])
			}
		})
	}
	for _, agent := range []string{"claude", "codex"} {
		cfg := defaultConfig()
		cfg.ResumeClaudeSkill, cfg.ResumeCodexSkill = " /$ ", " /$ "
		if _, err := handoffSlot(Slot{Kind: kindClaude, SessionID: "source-id"}, agent, cfg); err == nil {
			t.Fatal("empty skill after prefix accepted", agent)
		}
	}
}

func TestCodexSkillPrefixRemainsLiteralThroughShellAlias(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	cfg := defaultConfig()
	cfg.ResumeCodexSkill = "$my-skill"
	cfg.ClaudeAliases = []ClaudeAlias{{Name: "codex_shell", Command: "codex", ViaShell: true}}
	next, err := handoffSlot(Slot{Kind: kindClaude, SessionID: "source-id", ClaudeAlias: "claude"}, "codex", cfg, "codex_shell")
	if err != nil {
		t.Fatal(err)
	}
	launcher := Launcher{WS: "not-running", Bin: "/cw", Cfg: cfg}
	argv, _ := launcher.claudeArgv(&next, false)
	if len(argv) != 3 || argv[1] != "-ic" {
		t.Fatal("expected shell launcher", argv)
	}
	out, err := exec.Command("/bin/sh", "-c", `codex_shell() { for arg do printf '%s\n' "$arg"; done; }; `+argv[2]).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(out)), "$my-skill retoma la sesión de claude con id source-id") {
		t.Fatalf("shell expanded the skill prefix: %q", out)
	}
}
