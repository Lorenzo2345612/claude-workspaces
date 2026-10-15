package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// When a Claude account runs out of quota (its 5-hour or weekly window at 100 %), the sessions
// it leaves stuck move to an alias of another account that still has quota: they restart in
// their conversation and get the continue message. On by default; see Config.AutoSwitchAlias.

const quotaCheckEvery = 55 * time.Second

// limitMessageRe recognises Claude Code's notice that the account hit its limit.
var limitMessageRe = regexp.MustCompile(`(?i)(usage limit|limit reached|límite de uso|limite alcanzado|límite alcanzado|out of (extra )?usage)`)

// quotaTick runs the check at most once every quotaCheckEvery across every cw process (the app's
// server and the terminal workspaces' autosave both call it).
func quotaTick() {
	cfg, err := loadConfig()
	if err != nil || !cfg.autoSwitchAlias() {
		return
	}
	stamp := filepath.Join(runDir(), "quota-check")
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < quotaCheckEvery {
		return
	}
	_ = writeFileAtomic(stamp, []byte(time.Now().Format(time.RFC3339)))
	accounts := claudeLimits(cfg)
	out := map[string]bool{} // alias → its account is out of quota
	anyOut := false
	for _, a := range accounts {
		if a.outOfQuota() {
			anyOut = true
			for _, n := range a.Aliases {
				out[n] = true
			}
		}
	}
	if !anyOut {
		return
	}
	for _, ws := range runningWorkspaces() {
		// CW_QUOTA_WS limits the check to some workspaces (tests must not touch the user's own).
		if only := os.Getenv("CW_QUOTA_WS"); only != "" && !strings.Contains(","+only+",", ","+ws+",") {
			continue
		}
		autoSwitchWorkspace(ws, cfg, accounts, out)
	}
}

func (a accountUsage) outOfQuota() bool {
	return a.External == "" && a.Err == "" &&
		((a.Session != nil && a.Session.Pct >= 100) || (a.Week != nil && a.Week.Pct >= 100))
}

// claudeLimits is claudeUsage without the token count: only the windows of each account.
func claudeLimits(cfg Config) []accountUsage {
	return claudeUsageWith(cfg, false)
}

// autoSwitchWorkspace moves the stuck sessions of one workspace off the exhausted accounts.
func autoSwitchWorkspace(ws string, cfg Config, accounts []accountUsage, out map[string]bool) {
	t := Tmux{WS: ws}
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{"#{pane_id}", "#{@cw_kind}", "#{@cw_alias}",
		"#{@cw_claude_alias}", "#{@cw_status}", "#{pane_dead}"}, sep))
	if err != nil {
		return
	}
	var notes []string
	for _, r := range rows {
		if len(r) != 6 || r[1] != kindClaude || r[5] == "1" || r[4] == statusSleeping {
			continue
		}
		pane, name, current := r[0], r[2], cfg.claudeAlias(r[3]).Name
		if !out[current] {
			continue
		}
		// Only sessions the limit actually stopped: working ones, or showing Claude's notice.
		stuck := r[4] == statusBusy || r[4] == statusWaiting
		if !stuck {
			if text, err := t.Run("capture-pane", "-p", "-t", pane, "-S", "-15"); err == nil && limitMessageRe.MatchString(text) {
				stuck = true
			}
		}
		if !stuck {
			continue
		}
		target := bestAliasWithQuota(cfg, current, accounts, out)
		if target == "" {
			continue
		}
		// Busy makes the restart send the continue message: the session resumes its work.
		if _, err := t.Run("set", "-p", "-t", pane, "@cw_claude_alias", target, ";", "set", "-p", "-t", pane, "@cw_status", statusBusy); err != nil {
			continue
		}
		if respawnSlot(ws, pane, true) == nil {
			notes = append(notes, fmt.Sprintf("%s: %s → %s", name, current, target))
		}
	}
	if len(notes) == 0 {
		return
	}
	msg := "Sin cuota: " + strings.Join(notes, " · ")
	_, _ = t.Run("display-message", "-d", "10000", "cw · "+msg)
	if data, err := json.Marshal(map[string]any{"at": time.Now().Unix(), "text": msg}); err == nil {
		_, _ = t.Run("set", "-g", optNotice, string(data))
	}
	saveNow(ws)
}

const optNotice = "@cw_notice" // the last event the app's window should show ({"at", "text"})

// bestAliasWithQuota picks the alias to move a session to: one whose account has quota and is
// not another provider, preferring the same kind of launch (the same arguments, e.g. both skip
// permissions) and then the account with the most room left.
func bestAliasWithQuota(cfg Config, from string, accounts []accountUsage, out map[string]bool) string {
	src := cfg.claudeAlias(from)
	account := map[string]accountUsage{}
	for _, a := range accounts {
		for _, n := range a.Aliases {
			account[n] = a
		}
	}
	type cand struct {
		name  string
		score int
		used  float64
	}
	var cands []cand
	for _, a := range cfg.claudeAliases() {
		acc, ok := account[a.Name]
		if !ok || out[a.Name] || acc.External != "" || acc.Err != "" || a.Name == from {
			continue
		}
		c := cand{name: a.Name}
		if strings.Join(a.Args, " ") == strings.Join(src.Args, " ") {
			c.score += 2
		}
		if strings.Contains(a.Name, "yolo") == strings.Contains(src.Name, "yolo") {
			c.score++
		}
		if acc.Session != nil {
			c.used = acc.Session.Pct
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return ""
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].used < cands[j].used
	})
	return cands[0].name
}
