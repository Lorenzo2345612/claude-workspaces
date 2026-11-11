package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The cw plugin: two skills (cw, cw-coordinar) that teach Claude Code and Codex how to tell
// whether they run inside cw, where, and how to drive it. One directory is a marketplace for
// both agents: .claude-plugin/marketplace.json for Claude, .agents/plugins/marketplace.json for
// Codex, and plugins/cw carries a manifest for each next to the shared skills/.

//go:embed all:plugin
var pluginFS embed.FS

const pluginMarketplace, pluginName = "cw", "cw"

func pluginDir() string { return filepath.Join(dataDir(), "plugin") }

// selfBin is the absolute path of this cw binary, as the hooks and the sessions call it.
func selfBin() string {
	bin, err := os.Executable()
	if err != nil {
		return "cw"
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		return resolved
	}
	return bin
}

// cwPaneEnv is the environment of every pane cw starts: it is how an agent knows, without
// guessing, that it runs in cw (CW_WS and CW_SLOT are set only there), in which workspace and
// as which session, and which binary to call (CW_BIN, even when cw is not on the PATH).
func cwPaneEnv(ws, slot string) []string {
	return []string{"-e", "CW_WS=" + ws, "-e", "CW_SLOT=" + slot, "-e", "CW_BIN=" + selfBin()}
}

// writePlugin writes the embedded marketplace to dir, stamping this cw's version so that the
// agents see an update whenever cw changes.
func writePlugin(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	v := version
	if v == "dev" || v == "" {
		v = "0.0.0-dev"
	}
	v = strings.TrimPrefix(v, "v")
	return fs.WalkDir(pluginFS, "plugin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, strings.TrimPrefix(strings.TrimPrefix(path, "plugin"), "/"))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := pluginFS.ReadFile(path)
		if err != nil {
			return err
		}
		data = []byte(strings.ReplaceAll(string(data), "{{VERSION}}", v))
		return os.WriteFile(target, data, 0o644)
	})
}

// pluginTarget is one agent installation to put the plugin in: a Claude config dir or a
// Codex home, with the command and environment that reach it.
type pluginTarget struct {
	Agent   string // claude or codex
	Command string
	Env     []string // CLAUDE_CONFIG_DIR=… or CODEX_HOME=…; empty for the default one
	Label   string
}

// pluginTargets lists every Claude account (config dir) and Codex home the aliases use, once.
func pluginTargets(cfg Config) []pluginTarget {
	seen := map[string]bool{}
	var out []pluginTarget
	for _, a := range cfg.claudeAliases() {
		agent, key, env := "claude", "CLAUDE_CONFIG_DIR", ""
		if a.isCodex() {
			agent, key = "codex", "CODEX_HOME"
		}
		for _, kv := range a.Env {
			if k, v, ok := strings.Cut(kv, "="); ok && k == key && v != "" {
				env = key + "=" + expandHome(v)
			}
		}
		id := agent + "|" + env
		if seen[id] {
			continue
		}
		seen[id] = true
		command := cfg.ClaudeCommand
		if agent == "codex" {
			command = cfg.CodexCommand
			if command == "" {
				command = "codex"
			}
		}
		if command == "" {
			command = "claude"
		}
		label := agent
		if env != "" {
			label += " (" + env + ")"
		}
		t := pluginTarget{Agent: agent, Command: command, Label: label}
		if env != "" {
			t.Env = []string{env}
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// steps are the agent commands that add the marketplace and install (or update) the plugin.
func (t pluginTarget) steps(dir string) [][]string {
	if t.Agent == "codex" {
		return [][]string{
			{"plugin", "marketplace", "add", dir},
			{"plugin", "add", pluginName + "@" + pluginMarketplace},
		}
	}
	return [][]string{
		{"plugin", "marketplace", "add", dir},
		{"plugin", "marketplace", "update", pluginMarketplace},
		{"plugin", "install", pluginName + "@" + pluginMarketplace},
		{"plugin", "update", pluginName + "@" + pluginMarketplace},
	}
}

var (
	regexpAlready = regexp.MustCompile(`(?i)already|up to date|up-to-date|latest version|no update`)
	regexpMissing = regexp.MustCompile(`(?i)not (found|installed|configured)|no such|unknown|does not exist`)
)

// alreadyDone recognises the agents' answers that mean there is nothing to do.
func alreadyDone(out string) bool {
	return regexpAlready.MatchString(out)
}

func (t pluginTarget) run(args ...string) (string, error) {
	bin, err := exec.LookPath(t.Command)
	if err != nil {
		return "", fmt.Errorf("no encontré %s", t.Command)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), t.Env...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (t pluginTarget) install(dir string) error {
	if _, err := exec.LookPath(t.Command); err != nil {
		return fmt.Errorf("no encontré %s", t.Command)
	}
	for _, step := range t.steps(dir) {
		out, err := t.run(step...)
		if err != nil && !alreadyDone(out) {
			return fmt.Errorf("%s %s: %s", t.Command, strings.Join(step, " "), lastLine(out, err))
		}
	}
	return nil
}

func (t pluginTarget) uninstall() error {
	steps := [][]string{{"plugin", "uninstall", pluginName + "@" + pluginMarketplace}, {"plugin", "marketplace", "remove", pluginMarketplace}}
	if t.Agent == "codex" {
		steps = [][]string{{"plugin", "remove", pluginName + "@" + pluginMarketplace}, {"plugin", "marketplace", "remove", pluginMarketplace}}
	}
	var errs []string
	for _, step := range steps {
		if out, err := t.run(step...); err != nil && !regexpMissing.MatchString(out) {
			errs = append(errs, lastLine(out, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// cmdPlugin: cw plugin [instalar|desinstalar|ruta] [--claude|--codex]
func cmdPlugin(args []string) error {
	action, only := "instalar", ""
	for _, a := range args {
		switch a {
		case "instalar", "install", "actualizar", "update":
			action = "instalar"
		case "desinstalar", "uninstall", "remove":
			action = "desinstalar"
		case "ruta", "path":
			action = "ruta"
		case "--claude":
			only = "claude"
		case "--codex":
			only = "codex"
		case "-h", "--help":
			fmt.Println("uso: cw plugin [instalar|desinstalar|ruta] [--claude|--codex]\n" +
				"Instala (o actualiza) las skills de cw como plugin en cada cuenta de Claude y de Codex que usan los alias.")
			return nil
		default:
			return fmt.Errorf("opción desconocida %q: cw plugin --help", a)
		}
	}
	dir := pluginDir()
	if action == "ruta" {
		if err := writePlugin(dir); err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if action == "instalar" {
		if err := writePlugin(dir); err != nil {
			return err
		}
	}
	failed := 0
	for _, t := range pluginTargets(cfg) {
		if only != "" && t.Agent != only {
			continue
		}
		var err error
		if action == "instalar" {
			err = t.install(dir)
		} else {
			err = t.uninstall()
		}
		if err != nil {
			failed++
			fmt.Printf("✗ %s: %v\n", t.Label, err)
			continue
		}
		fmt.Printf("✓ %s\n", t.Label)
	}
	if action == "instalar" {
		fmt.Println("Las sesiones que ya estaban abiertas deben reiniciarse para cargar el plugin.")
	}
	if failed > 0 {
		return fmt.Errorf("falló en %d de las instalaciones", failed)
	}
	return nil
}
