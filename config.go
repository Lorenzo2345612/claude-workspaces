package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the user-editable settings, stored in ~/.config/cw/config.json.
type Config struct {
	CodexCommand      string   `json:"codex_command"`
	CodexArgs         []string `json:"codex_args,omitempty"`
	ResumeClaudeSkill string   `json:"resume_claude_skill"`
	ResumeCodexSkill  string   `json:"resume_codex_skill"`
	// UsageIntervalSeconds controla la lectura incremental del historial; 0 equivale a una hora.
	UsageIntervalSeconds int `json:"usage_interval_seconds,omitempty"`
	// ContinueMessage is sent to every Claude session that was working when the workspace was stopped.
	ContinueMessage string `json:"continue_message"`
	// LazyStart, when true, leaves idle Claude sessions asleep until you press Enter in their tab.
	LazyStart bool `json:"lazy_start"`
	// ClaudeCommand is the executable used to launch Claude Code.
	ClaudeCommand string `json:"claude_command"`
	// ClaudeArgs are extra arguments appended to every Claude launch (e.g. "--permission-mode", "auto").
	ClaudeArgs []string `json:"claude_args"`
	// AutosaveSeconds is the minimum interval between automatic saves while the workspace runs.
	AutosaveSeconds int `json:"autosave_seconds"`
	// Window is where `cw start` opens the workspace: "maximized" or "fullscreen" open a new
	// terminal window of their own (Windows Terminal under WSL), "current" uses the terminal it runs in.
	Window string `json:"window"`
	// BarPosition puts the tab bar at the "top" or the "bottom" of the terminal.
	BarPosition string `json:"bar_position"`
	// ClaudeAliases are the named ways of launching claude (env vars + arguments), like the
	// shell aliases yolo or claude-glm; DefaultClaudeAlias is preselected for new sessions.
	ClaudeAliases      []ClaudeAlias `json:"claude_aliases"`
	DefaultClaudeAlias string        `json:"default_claude_alias"`
	// Theme is the colour scheme: azul, morado, verde, naranja, rosa or gris.
	Theme string `json:"theme"`
	// AppPort is the local port of the app's server (cw app); 0 means the default.
	AppPort int `json:"app_port,omitempty"`
	// AppFontSize is the font size of the app's terminals; 0 means the default.
	AppFontSize int `json:"app_font_size,omitempty"`
	// AutoSwitchAlias moves a session whose Claude account ran out of quota to an alias of an
	// account that still has it; nil means on.
	AutoSwitchAlias *bool `json:"auto_switch_alias,omitempty"`
	// Skynet is the orchestrator: a Claude session (Sonnet by default) that sees every workspace
	// and drives them through cw mcp, by text or by voice in the app. Off by default.
	SkynetEnabled bool   `json:"skynet_enabled,omitempty"`
	SkynetName    string `json:"skynet_name,omitempty"`  // the name that wakes it by voice; "" means Skynet
	SkynetModel   string `json:"skynet_model,omitempty"` // "" means sonnet
	// SkynetAlias pins the claude alias it runs with; "" picks one with quota and switches
	// to another when that account runs out.
	SkynetAlias string `json:"skynet_alias,omitempty"`
	// SkynetNotify tells the user when a session asks something; nil means on.
	SkynetNotify *bool `json:"skynet_notify,omitempty"`
	// SkynetVoice listens to the microphone in the app's window.
	SkynetVoice bool `json:"skynet_voice,omitempty"`
	// SkynetAutoApprove lets the orchestrator answer permission dialogs without asking the user.
	SkynetAutoApprove bool `json:"skynet_auto_approve,omitempty"`
	// HideClaudeAlias drops the plain "claude" alias from the list (when there are others).
	HideClaudeAlias bool `json:"hide_claude_alias,omitempty"`
	// AppMode is the app's light or dark mode ("light" or "dark"); "" means dark.
	AppMode string `json:"app_mode,omitempty"`
	// BrowserKeys turns on the browser shortcuts for the sessions of a tab (Ctrl+T, Ctrl+W,
	// Ctrl+Tab…); nil means on.
	BrowserKeys *bool `json:"browser_keys,omitempty"`
}

func (c Config) browserKeys() bool { return c.BrowserKeys == nil || *c.BrowserKeys }

const (
	barTop    = "top"
	barBottom = "bottom"
)

func defaultConfig() Config {
	return Config{
		ContinueMessage:   "continue",
		CodexCommand:      "codex",
		ResumeClaudeSkill: "retomar-con-claude",
		ResumeCodexSkill:  "retomar-con-codex",
		LazyStart:         false,
		ClaudeCommand:     "claude",
		ClaudeArgs:        []string{},
		AutosaveSeconds:   120,
		Window:            windowCurrent,
		BarPosition:       barTop,
	}
}

func configFile() string { return filepath.Join(configDir(), "config.json") }

func loadConfig() (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(configFile())
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ClaudeCommand == "" {
		cfg.ClaudeCommand = "claude"
	}
	if cfg.Window == "" {
		cfg.Window = windowCurrent
	}
	if cfg.AutosaveSeconds <= 0 {
		cfg.AutosaveSeconds = 120
	}
	return cfg, nil
}

func saveConfig(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(configFile(), append(data, '\n'))
}

// writeFileAtomic writes through a temp file + rename so a crash never leaves a truncated file.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (c Config) autoSwitchAlias() bool { return c.AutoSwitchAlias == nil || *c.AutoSwitchAlias }

func (c Config) skynetName() string {
	if n := strings.TrimSpace(c.SkynetName); n != "" {
		return n
	}
	return "Skynet"
}

func (c Config) skynetModel() string {
	if c.SkynetModel != "" {
		return c.SkynetModel
	}
	return "sonnet"
}

func (c Config) skynetNotify() bool { return c.SkynetNotify == nil || *c.SkynetNotify }
