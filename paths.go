package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var workspaceNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

func validWorkspaceName(name string) error {
	if !workspaceNameRe.MatchString(name) {
		return fmt.Errorf("nombre de workspace inválido %q: usa letras, números, '-' o '_' (máximo 40)", name)
	}
	return nil
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

func xdgDir(envVar, fallback string) string {
	if v := os.Getenv(envVar); v != "" {
		return filepath.Join(v, "cw")
	}
	return filepath.Join(homeDir(), fallback, "cw")
}

// configDir honours CW_CONFIG_DIR first, for the same reason as dataDir.
func configDir() string {
	if v := os.Getenv("CW_CONFIG_DIR"); v != "" {
		return v
	}
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// dataDir honours CW_DATA_DIR first. Isolate cw with it, never with XDG_DATA_HOME: Claude Code
// installs its own updates under $XDG_DATA_HOME/claude and repoints ~/.local/bin/claude there.
func dataDir() string {
	if v := os.Getenv("CW_DATA_DIR"); v != "" {
		return v
	}
	return xdgDir("XDG_DATA_HOME", ".local/share")
}
func workspacesDir() string { return filepath.Join(dataDir(), "workspaces") }
func runDir() string        { return filepath.Join(dataDir(), "run") }

func workspaceFile(ws string) string { return filepath.Join(workspacesDir(), ws+".json") }
func tmuxConfFile(ws string) string  { return filepath.Join(runDir(), ws+".tmux.conf") }

// claudeConfigDir mirrors Claude Code's own lookup: CLAUDE_CONFIG_DIR or ~/.claude.
func claudeConfigDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(homeDir(), ".claude")
}

// expandPath resolves "~" and relative paths to an absolute, cleaned path.
func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "~" {
		p = homeDir()
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(homeDir(), p[2:])
	}
	return filepath.Abs(p)
}

// shortPath renders a directory for the status bar: the home prefix becomes "~".
func shortPath(p string) string {
	h := homeDir()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(os.PathSeparator)) {
		return "~" + p[len(h):]
	}
	return p
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
