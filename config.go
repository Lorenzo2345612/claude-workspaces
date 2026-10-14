package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config holds the user-editable settings, stored in ~/.config/cw/config.json.
type Config struct {
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
}

const (
	barTop    = "top"
	barBottom = "bottom"
)

func defaultConfig() Config {
	return Config{
		ContinueMessage: "continue",
		LazyStart:       false,
		ClaudeCommand:   "claude",
		ClaudeArgs:      []string{},
		AutosaveSeconds: 120,
		Window:          windowCurrent,
		BarPosition:     barTop,
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
