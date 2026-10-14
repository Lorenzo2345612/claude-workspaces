package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ClaudeAlias is a named way of launching Claude Code, like a shell alias:
// environment variables (CLAUDE_CONFIG_DIR, CLAUDE_YOLO…) plus arguments (--model…).
type ClaudeAlias struct {
	Name string   `json:"name"`
	Env  []string `json:"env,omitempty"`  // KEY=VALUE
	Args []string `json:"args,omitempty"` // passed right after the claude executable
	// FromShell is set when Env/Args come from the shell alias of the same name (the alias was
	// registered with just its name): it is read from the rc files on every launch.
	FromShell bool `json:"-"`
	// ViaShell is set for aliases registered with just their name: the session runs that very
	// command through the user's shell ($SHELL -ic 'name …'), wherever and however it is defined.
	ViaShell bool `json:"-"`
}

// launchEnv is the environment cw adds itself; a ViaShell alias sets its own.
func (a ClaudeAlias) launchEnv() []string {
	if a.ViaShell {
		return nil
	}
	return a.Env
}

var shellAliasCache map[string]ClaudeAlias

// resolve fills an alias registered with only its name from the shell alias of that name.
func (a ClaudeAlias) resolve() ClaudeAlias {
	if len(a.Env) > 0 || len(a.Args) > 0 || a.Name == builtinAlias {
		return a
	}
	a.ViaShell = os.Getenv("SHELL") != ""
	if shellAliasCache == nil {
		shellAliasCache = map[string]ClaudeAlias{}
		for _, s := range importShellAliases() {
			shellAliasCache[s.Name] = s
		}
	}
	if s, ok := shellAliasCache[a.Name]; ok {
		// Keep its definition (env, args) to know its config dir and to describe it.
		s.FromShell, s.ViaShell = true, a.ViaShell
		return s
	}
	return a
}

const builtinAlias = "claude"

// claudeAliases returns the configured aliases, always including the plain "claude".
func (c Config) claudeAliases() []ClaudeAlias {
	out := []ClaudeAlias{}
	hasBuiltin := false
	for _, a := range c.ClaudeAliases {
		if a.Name == builtinAlias {
			hasBuiltin = true
		}
		out = append(out, a.resolve())
	}
	if !hasBuiltin {
		out = append([]ClaudeAlias{{Name: builtinAlias}}, out...)
	}
	return out
}

func (c Config) claudeAlias(name string) ClaudeAlias {
	if name == "" {
		name = c.defaultAlias()
	}
	for _, a := range c.claudeAliases() {
		if a.Name == name {
			return a
		}
	}
	return ClaudeAlias{Name: builtinAlias}
}

func (c Config) defaultAlias() string {
	if c.DefaultClaudeAlias != "" {
		for _, a := range c.claudeAliases() {
			if a.Name == c.DefaultClaudeAlias {
				return a.Name
			}
		}
	}
	return builtinAlias
}

func (c Config) aliasNames() []string {
	var names []string
	for _, a := range c.claudeAliases() {
		names = append(names, a.Name)
	}
	return names
}

// configDir is the Claude config directory this alias uses, where its transcripts live.
func (a ClaudeAlias) configDir() string {
	for _, kv := range a.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == "CLAUDE_CONFIG_DIR" && v != "" {
			return expandHome(v)
		}
	}
	return claudeConfigDir()
}

// summary is a one-line description: "CLAUDE_YOLO=1 claude --dangerously-skip-permissions".
func (a ClaudeAlias) summary() string {
	parts := append([]string{}, a.Env...)
	parts = append(parts, "claude")
	parts = append(parts, a.Args...)
	switch {
	case a.ViaShell && a.FromShell:
		return "tu comando " + a.Name + " = " + joinArgs(parts)
	case a.ViaShell:
		return "tu comando " + a.Name + ", ejecutado con tu shell"
	case a.FromShell:
		return "(de tu shell) " + joinArgs(parts)
	}
	return joinArgs(parts)
}

func expandHome(p string) string {
	h := homeDir()
	p = strings.ReplaceAll(p, "${HOME}", h)
	p = strings.ReplaceAll(p, "$HOME", h)
	if p == "~" {
		return h
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(h, p[2:])
	}
	return p
}

var shellAliasRe = regexp.MustCompile(`^\s*alias\s+([A-Za-z0-9_.-]+)=(?:'([^']*)'|"([^"]*)")\s*$`)
var envWordRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// `alias` output: bash prints «alias name='value'», zsh prints «name='value'» or «name=value».
var aliasListRe = regexp.MustCompile(`^(?:alias\s+)?([A-Za-z0-9_.+-]+)=(.*)$`)

// importShellAliases returns the shell aliases that launch claude. It asks the user's own shell
// first (`$SHELL -ic alias`), which sees aliases from any file or plugin and in any syntax, and
// falls back to reading the usual rc files.
func importShellAliases() []ClaudeAlias {
	defs := map[string]string{}
	var order []string
	for name, body := range shellAliasesFromShell() {
		defs[name] = body
		order = append(order, name)
	}
	sort.Strings(order)
	for _, rc := range []string{".bashrc", ".bash_aliases", ".zshrc", ".zprofile", ".aliases"} {
		f, err := os.Open(filepath.Join(homeDir(), rc))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if m := shellAliasRe.FindStringSubmatch(sc.Text()); m != nil {
				if _, ok := defs[m[1]]; !ok {
					defs[m[1]] = m[2] + m[3]
					order = append(order, m[1])
				}
			}
		}
		f.Close()
	}
	var out []ClaudeAlias
	for _, name := range order {
		if a, ok := parseClaudeAlias(name, defs[name]); ok {
			out = append(out, a)
		}
	}
	return out
}

// shellAliasesFromShell runs the user's interactive shell once to list its aliases.
func shellAliasesFromShell() map[string]string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, "-ic", "alias")
	cmd.Env = append(os.Environ(), "TERM=dumb")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	defs := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		m := aliasListRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		v := m[2]
		if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], `'\''`, "'")
		}
		defs[m[1]] = v
	}
	return defs
}

// parseClaudeAlias reads an alias body «[VAR=val…] claude [args…]»; ok is false for other commands.
func parseClaudeAlias(name, body string) (ClaudeAlias, bool) {
	words := splitArgs(body)
	a := ClaudeAlias{Name: name}
	i := 0
	for i < len(words) && envWordRe.MatchString(words[i]) {
		k, v, _ := strings.Cut(words[i], "=")
		a.Env = append(a.Env, k+"="+expandHome(v))
		i++
	}
	if i >= len(words) || filepath.Base(words[i]) != "claude" {
		return a, false
	}
	a.Args = append(a.Args, words[i+1:]...)
	return a, true
}
