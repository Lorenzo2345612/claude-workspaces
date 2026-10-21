package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Launcher builds the command each window runs.
type Launcher struct {
	WS        string
	Bin       string // absolute path of this cw binary (hooks and lazy placeholders call back into it)
	ClaudeBin string
	Cfg       Config
}

func newLauncher(ws string, cfg Config) (*Launcher, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	claudeBin, err := exec.LookPath(cfg.ClaudeCommand)
	if err != nil {
		return nil, fmt.Errorf("no encuentro %q en el PATH: %w", cfg.ClaudeCommand, err)
	}
	return &Launcher{WS: ws, Bin: bin, ClaudeBin: claudeBin, Cfg: cfg}, nil
}

// hookSettings is passed with --settings. Hook lists merge with the user's own settings files,
// so this adds our status tracking without replacing anything the user configured.
func (l *Launcher) hookSettings() string {
	cmd := fmt.Sprintf("'%s' _hook", l.Bin)
	entry := func(matcher string) []map[string]any {
		e := map[string]any{"hooks": []map[string]any{{"type": "command", "command": cmd, "timeout": 5}}}
		if matcher != "" {
			e["matcher"] = matcher
		}
		return []map[string]any{e}
	}
	data, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"SessionStart":       entry(""),
		"UserPromptSubmit":   entry(""),
		"PreToolUse":         entry("AskUserQuestion"),
		"PostToolUse":        entry("*"),
		"PostToolUseFailure": entry("*"),
		"PermissionRequest":  entry("*"),
		"Elicitation":        entry(""),
		"ElicitationResult":  entry(""),
		"Notification":       entry("permission_prompt|elicitation_dialog|elicitation_url_dialog|agent_needs_input|worker_permission_prompt"),
		"Stop":               entry(""),
		"StopFailure":        entry(""),
	}})
	return string(data)
}

// claudeArgv returns the command for a Claude slot and the status to display.
// resuming=false starts a brand-new conversation with a pre-assigned ID.
func (l *Launcher) claudeArgv(s *Slot, resuming bool) ([]string, string) {
	alias := l.Cfg.claudeAlias(s.ClaudeAlias)
	s.ClaudeAlias = alias.Name
	var others []string
	for _, a := range l.Cfg.claudeAliases() {
		if d := a.configDir(); d != alias.configDir() {
			others = append(others, d)
		}
	}
	path, inOwnDir := "", false
	if resuming {
		path, inOwnDir = findTranscript(s.SessionID, alias.configDir(), others)
	}
	fresh := path == ""
	if s.SessionID == "" {
		s.SessionID = newUUID()
	}
	var rest []string // everything after the claude executable (or after the alias command)
	switch {
	case fresh:
		rest = append(rest, "--session-id", s.SessionID)
	case inOwnDir:
		rest = append(rest, "--resume", s.SessionID)
	default:
		// The conversation lives under another alias's config dir (the session switched alias):
		// claude accepts the transcript's path in place of the ID.
		rest = append(rest, "--resume", path)
	}
	rest = append(rest, "--name", s.Alias, "--settings", l.hookSettings())
	if appMode(Tmux{WS: l.WS}) {
		rest = append(rest, "--append-system-prompt", markdownInstructions(l.Bin))
	}
	rest = append(rest, l.Cfg.ClaudeArgs...)
	rest = append(rest, s.Args...)
	display := statusIdle
	if resuming && !fresh && s.wasWorking() {
		display = statusBusy
		if l.Cfg.ContinueMessage != "" {
			rest = append(rest, l.Cfg.ContinueMessage)
		}
	}

	var argv []string
	if alias.ViaShell {
		// Run the user's own command (alias, function or script) exactly as typed in a terminal.
		argv = []string{os.Getenv("SHELL"), "-ic", alias.Name + " " + joinArgs(rest)}
	} else {
		argv = append(append([]string{l.ClaudeBin}, alias.Args...), rest...)
	}
	argv = l.unsetOtherAliasVars(alias, argv)

	if s.Suspended || (display != statusBusy && resuming && l.Cfg.LazyStart) {
		display = statusSleeping
		argv = append([]string{l.Bin, "_wait", l.WS, s.Alias, "--"}, argv...)
	}
	return argv, display
}

// slotSpec prepares what a slot runs (resolving a missing directory, the claude command…).
func (l *Launcher) slotSpec(session string, first bool, s *Slot, resuming bool) windowSpec {
	if s.ID == "" {
		s.ID = newUUID()
	}
	if !isDir(s.Cwd) {
		fmt.Fprintf(os.Stderr, "aviso: el directorio %s ya no existe; %q abre en %s\n", s.Cwd, s.Alias, homeDir())
		s.Cwd = homeDir()
	}
	spec := windowSpec{session: session, first: first, slot: *s, name: s.Alias}
	if s.Kind == kindClaude {
		spec.argv, spec.display = l.claudeArgv(s, resuming)
		spec.env = l.Cfg.claudeAlias(s.ClaudeAlias).launchEnv()
		spec.slot = *s
	} else if spec.name == "" {
		spec.name = "shell"
	}
	return spec
}

// openSlot creates the tmux window for a slot (and the tab's session when first is true).
func (l *Launcher) openSlot(t Tmux, session string, first bool, s *Slot, resuming bool) (string, error) {
	return t.createWindow(l.slotSpec(session, first, s, resuming))
}

// openTab creates a whole tab: its tmux session, options, and every view with its sessions.
func (l *Launcher) openTab(t Tmux, session string, tab *Tab, resuming bool) error {
	if !isDir(tab.Dir) {
		tab.Dir = homeDir()
	}
	if len(tab.Slots) == 0 {
		// An empty tab shows the start screen; the placeholder is not a slot and is never saved.
		home := Slot{ID: newUUID(), Kind: kindHome, Cwd: tab.Dir}
		if _, err := t.createWindow(windowSpec{session: session, first: true, name: "inicio", slot: home,
			argv: []string{l.Bin, "_home", l.WS}}); err != nil {
			return err
		}
		return t.setTabOptions(session, *tab)
	}
	// State saved before views existed (0.3.x and older) has no layouts and every slot in view 0:
	// each of those sessions had its own window, so give each its own view again.
	if len(tab.Layouts) == 0 {
		for i := range tab.Slots {
			tab.Slots[i].View = i
		}
	}
	viewWindow := map[int]string{}
	var order []int
	activeTarget := ""
	for i := range tab.Slots {
		s := &tab.Slots[i]
		if s.Cwd == "" {
			s.Cwd = tab.Dir
		}
		spec := l.slotSpec(session, i == 0, s, resuming)
		target := ""
		if win, ok := viewWindow[s.View]; ok {
			pane, err := t.createPane(spec, win)
			if err != nil {
				return err
			}
			target = pane
		} else {
			win, err := t.createWindow(spec)
			if err != nil {
				return err
			}
			viewWindow[s.View] = win
			order = append(order, s.View)
			target = win
		}
		if i == tab.ActiveSlot {
			activeTarget = target
		}
	}
	for _, v := range order {
		layout := ""
		if v < len(tab.Layouts) {
			layout = tab.Layouts[v]
		}
		t.arrangeView(viewWindow[v], layout)
	}
	if err := t.setTabOptions(session, *tab); err != nil {
		return err
	}
	if activeTarget != "" {
		_, _ = t.Run("select-window", "-t", activeTarget, ";", "select-pane", "-t", activeTarget)
	}
	return nil
}

// unsetOtherAliasVars prefixes argv with `env -u VAR…` for every variable some other claude alias
// sets but this one does not, so a value inherited from the shell (CLAUDE_YOLO, CLAUDE_CONFIG_DIR…)
// never leaks into a session launched with a different alias.
func (l *Launcher) unsetOtherAliasVars(alias ClaudeAlias, argv []string) []string {
	own := map[string]bool{}
	for _, kv := range alias.Env {
		k, _, _ := strings.Cut(kv, "=")
		own[k] = true
	}
	var unset []string
	seen := map[string]bool{}
	for _, a := range l.Cfg.claudeAliases() {
		for _, kv := range a.Env {
			k, _, _ := strings.Cut(kv, "=")
			if !own[k] && !seen[k] {
				seen[k] = true
				unset = append(unset, "-u", k)
			}
		}
	}
	if len(unset) == 0 {
		return argv
	}
	envBin, err := exec.LookPath("env")
	if err != nil {
		return argv
	}
	return append(append([]string{envBin}, unset...), argv...)
}
