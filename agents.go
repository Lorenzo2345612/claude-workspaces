package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Codex is a separate CLI, not a Claude-compatible wrapper such as claude-codex.
func (a ClaudeAlias) isCodex() bool {
	return filepath.Base(a.Command) == "codex" || (a.Command == "" && a.Name == "codex")
}

func handoffSlot(source Slot, target string, cfg Config) (Slot, error) {
	if source.Kind != kindClaude || source.SessionID == "" {
		return Slot{}, errors.New("la sesión aún no tiene un ID de conversación; inicia una conversación antes de retomarla")
	}
	if target != "claude" && target != "codex" {
		return Slot{}, errors.New("agente desconocido")
	}
	origin := "claude"
	if cfg.claudeAlias(source.ClaudeAlias).isCodex() {
		origin = "codex"
	}
	skill, alias := cfg.ResumeClaudeSkill, ""
	if target == "codex" {
		skill, alias = cfg.ResumeCodexSkill, "codex"
	} else {
		preferred := cfg.claudeAlias(cfg.defaultAlias())
		if !preferred.isCodex() {
			alias = preferred.Name
		} else {
			for _, a := range cfg.claudeAliases() {
				if !a.isCodex() {
					alias = a.Name
					break
				}
			}
		}
	}
	if alias == "" {
		return Slot{}, errors.New("configura un alias de Claude para retomar")
	}
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return Slot{}, fmt.Errorf("configura la skill para retomar con %s en Configuración", target)
	}
	return Slot{Kind: kindClaude, Alias: source.Alias + " · " + target, Cwd: source.Cwd,
		ClaudeAlias: alias, Status: statusIdle,
		InitialMessage: fmt.Sprintf("%s retoma la sesión de %s con id %s", skill, origin, source.SessionID)}, nil
}

// Inline TOML overrides register cw's lifecycle hooks without editing the project.
// Codex asks the user to trust these hooks through /hooks on first use.
func (l *Launcher) codexArgv(s *Slot, a ClaudeAlias, resuming bool) ([]string, string) {
	s.ClaudeAlias = a.Name
	command := a.Command
	if command == "" || command == "codex" {
		command = l.Cfg.CodexCommand
	}
	if command == "" {
		command = "codex"
	}
	rest := append([]string{}, a.Args...)
	if resuming && s.SessionID != "" {
		rest = append(rest, "resume", s.SessionID)
	}
	rest = append(rest, l.Cfg.CodexArgs...)
	rest = append(rest, s.Args...)
	// Hook commands run inside the CLI and inherit the pane's environment.
	hook := strconv.Quote(joinArgs([]string{"env", "CW_WS=" + l.WS, "CW_SLOT=" + s.ID, l.Bin, "_hook"}))
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PermissionRequest", "PostToolUse", "Stop"} {
		rest = append(rest, "-c", "hooks."+event+"=[{hooks=[{type=\"command\",command="+hook+",timeout=5}]}]")
	}
	display := statusIdle
	if s.InitialMessage != "" {
		rest = append(rest, "--", s.InitialMessage)
	} else if resuming && s.SessionID != "" && s.wasWorking() && l.Cfg.ContinueMessage != "" {
		rest = append(rest, "--", l.Cfg.ContinueMessage)
		display = statusBusy
	}
	argv := append([]string{command}, rest...)
	if a.ViaShell {
		argv = []string{os.Getenv("SHELL"), "-ic", a.Name + " " + joinArgs(rest)}
	}
	argv = l.unsetOtherAliasVars(a, argv)
	if s.Suspended || (resuming && display != statusBusy && l.Cfg.LazyStart) {
		display = statusSleeping
		argv = append([]string{l.Bin, "_wait", l.WS, s.Alias, "--"}, argv...)
	}
	return argv, display
}

func checkAgentAlias(cfg Config, old, next string) error {
	if cfg.claudeAlias(old).isCodex() != cfg.claudeAlias(next).isCodex() {
		return errors.New("para cambiar de agente usa Retomar con Codex o Retomar con Claude")
	}
	return nil
}
