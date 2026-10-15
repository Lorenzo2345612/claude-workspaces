package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Usage of each Claude account the aliases run with: how much of the 5-hour session window and
// of the weekly window is used and when each resets (the numbers Claude Code's /usage shows, from
// the same endpoint, read with each account's own OAuth token), plus the tokens spent, summed
// from the transcripts.

const usageAPI = "https://api.anthropic.com/api/oauth/usage"

type usageWindow struct {
	Pct      float64   `json:"pct"`
	ResetsAt time.Time `json:"resetsAt"`
}

type tokTotals struct {
	In     int64 `json:"in"`
	Out    int64 `json:"out"`
	CacheW int64 `json:"cacheW"`
	CacheR int64 `json:"cacheR"`
}

func (t tokTotals) Sum() int64 { return t.In + t.Out + t.CacheW + t.CacheR }

func (t *tokTotals) add(o tokTotals) {
	t.In += o.In
	t.Out += o.Out
	t.CacheW += o.CacheW
	t.CacheR += o.CacheR
}

type accountUsage struct {
	ConfigDir  string       `json:"configDir"`
	Short      string       `json:"short"`
	Aliases    []string     `json:"aliases"`
	Plan       string       `json:"plan"`
	External   string       `json:"external,omitempty"` // another provider (ANTHROPIC_BASE_URL…): no Claude limits
	Err        string       `json:"error,omitempty"`
	Session    *usageWindow `json:"session,omitempty"` // the 5-hour window
	Week       *usageWindow `json:"week,omitempty"`
	WeekOpus   *usageWindow `json:"weekOpus,omitempty"`
	WeekSonnet *usageWindow `json:"weekSonnet,omitempty"`
	TokSession tokTotals    `json:"tokSession"`
	TokWeek    tokTotals    `json:"tokWeek"`
	SharedWith []string     `json:"sharedWith,omitempty"` // accounts whose transcripts live in the same folder
}

// claudeUsage reports every account the configured aliases use.
func claudeUsage(cfg Config) []accountUsage { return claudeUsageWith(cfg, true) }

func claudeUsageWith(cfg Config, tokens bool) []accountUsage {
	type group struct {
		dir      string
		aliases  []string
		external string
	}
	var order []string
	groups := map[string]*group{}
	for _, a := range cfg.claudeAliases() {
		dir := cfg.accountDir(a)
		ext := ""
		for _, kv := range a.Env {
			k, v, _ := strings.Cut(kv, "=")
			if k == "ANTHROPIC_BASE_URL" && v != "" {
				ext = v
			}
		}
		key := dir
		if ext != "" {
			key = "ext:" + a.Name
		}
		g := groups[key]
		if g == nil {
			g = &group{dir: dir, external: ext}
			groups[key] = g
			order = append(order, key)
		}
		g.aliases = append(g.aliases, a.Name)
	}
	out := make([]accountUsage, len(order))
	var wg sync.WaitGroup
	for i, key := range order {
		g := groups[key]
		out[i] = accountUsage{ConfigDir: g.dir, Short: shortPath(g.dir), Aliases: g.aliases, External: g.external}
		if g.external != "" {
			continue
		}
		wg.Add(1)
		go func(u *accountUsage) {
			defer wg.Done()
			fillAccountUsage(u)
		}(&out[i])
	}
	wg.Wait()
	if !tokens {
		return out
	}
	// Tokens: one scan per transcripts folder (accounts often share one through a symlink).
	byFolder := map[string][]int{}
	for i := range out {
		if out[i].External != "" {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(out[i].ConfigDir, "projects"))
		if err != nil {
			continue
		}
		byFolder[real] = append(byFolder[real], i)
	}
	for folder, idx := range byFolder {
		for _, i := range idx { // each account counts from its own windows
			sessionFrom, weekFrom := time.Now().Add(-5*time.Hour), time.Now().Add(-7*24*time.Hour)
			if w := out[i].Session; w != nil && !w.ResetsAt.IsZero() {
				sessionFrom = w.ResetsAt.Add(-5 * time.Hour)
			}
			if w := out[i].Week; w != nil && !w.ResetsAt.IsZero() {
				weekFrom = w.ResetsAt.Add(-7 * 24 * time.Hour)
			}
			out[i].TokSession, out[i].TokWeek = transcriptTokens(folder, sessionFrom, weekFrom)
			for _, j := range idx {
				if j != i {
					out[i].SharedWith = append(out[i].SharedWith, strings.Join(out[j].Aliases, " · "))
				}
			}
		}
	}
	return out
}

func fillAccountUsage(u *accountUsage) {
	cred, err := readClaudeCredentials(u.ConfigDir)
	if err != nil {
		u.Err = err.Error()
		return
	}
	u.Plan = planLabel(cred.SubscriptionType, cred.RateLimitTier)
	if cred.ExpiresAt > 0 && time.UnixMilli(cred.ExpiresAt).Before(time.Now()) {
		u.Err = "la sesión de esta cuenta venció; abre Claude con uno de sus alias para renovarla"
		return
	}
	// Every cw process (app server, quota check, popups, cw mcp) asks for the same numbers: they
	// share one answer per account for a minute, and a failed query falls back to the last one.
	cache := filepath.Join(runDir(), "usage-"+shortHash(u.ConfigDir)+".json")
	var cached usageCache
	if data, err := os.ReadFile(cache); err == nil && json.Unmarshal(data, &cached) == nil {
		if time.Since(cached.Tried) < usageCacheFor {
			cached.apply(u)
			return
		}
	}
	cached.Tried = time.Now()
	fail := func(msg string) {
		_ = writeJSONFile(cache, cached)
		if !cached.At.IsZero() && time.Since(cached.At) < 30*time.Minute {
			cached.apply(u)
			return
		}
		u.Err = msg
	}
	req, _ := http.NewRequest("GET", usageAPI, nil)
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "cw/"+version)
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		fail("no pude consultar el uso: " + err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		u.Err = "la sesión de esta cuenta venció; abre Claude con uno de sus alias para renovarla"
		return
	}
	if resp.StatusCode != http.StatusOK {
		fail("el servicio de uso respondió " + resp.Status)
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		u.Err = err.Error()
		return
	}
	win := func(key string) *usageWindow {
		var w struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    string   `json:"resets_at"`
		}
		if raw, ok := body[key]; !ok || json.Unmarshal(raw, &w) != nil || w.Utilization == nil {
			return nil
		}
		t, _ := time.Parse(time.RFC3339Nano, w.ResetsAt)
		return &usageWindow{Pct: *w.Utilization, ResetsAt: t}
	}
	u.Session, u.Week, u.WeekOpus, u.WeekSonnet = win("five_hour"), win("seven_day"), win("seven_day_opus"), win("seven_day_sonnet")
	cached = usageCache{At: cached.Tried, Tried: cached.Tried, Session: u.Session, Week: u.Week, WeekOpus: u.WeekOpus, WeekSonnet: u.WeekSonnet}
	_ = writeJSONFile(cache, cached)
}

const usageCacheFor = 50 * time.Second

// usageCache is the last answer of the usage service for one account.
type usageCache struct {
	At         time.Time    `json:"at"`    // when the windows below were read (zero: never)
	Tried      time.Time    `json:"tried"` // the last query, successful or not
	Session    *usageWindow `json:"session,omitempty"`
	Week       *usageWindow `json:"week,omitempty"`
	WeekOpus   *usageWindow `json:"weekOpus,omitempty"`
	WeekSonnet *usageWindow `json:"weekSonnet,omitempty"`
}

func (c usageCache) apply(u *accountUsage) {
	if c.At.IsZero() {
		u.Err = "el servicio de uso no respondió; reintento en un momento"
		return
	}
	u.Session, u.Week, u.WeekOpus, u.WeekSonnet = c.Session, c.Week, c.WeekOpus, c.WeekSonnet
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:4])
}

func writeJSONFile(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

type claudeOAuth struct {
	AccessToken      string `json:"accessToken"`
	ExpiresAt        int64  `json:"expiresAt"`
	SubscriptionType string `json:"subscriptionType"`
	RateLimitTier    string `json:"rateLimitTier"`
}

// readClaudeCredentials reads the OAuth credentials Claude Code keeps for a config dir: the
// .credentials.json file (Linux, WSL) or the login keychain (macOS), under the service name
// Claude Code uses: "Claude Code-credentials", plus "-<8 hex of sha256(dir)>" for a custom dir.
func readClaudeCredentials(dir string) (*claudeOAuth, error) {
	var raw []byte
	if data, err := os.ReadFile(filepath.Join(dir, ".credentials.json")); err == nil {
		raw = data
	} else if runtime.GOOS == "darwin" {
		service := "Claude Code-credentials"
		if filepath.Clean(dir) != filepath.Join(homeDir(), ".claude") {
			sum := sha256.Sum256([]byte(dir))
			service += "-" + hex.EncodeToString(sum[:])[:8]
		}
		user := os.Getenv("USER")
		out, err := exec.Command("security", "find-generic-password", "-a", user, "-w", "-s", service).Output()
		if err != nil {
			return nil, errors.New("no encontré la sesión de esta cuenta en el Llavero; abre Claude con uno de sus alias")
		}
		raw = bytes.TrimSpace(out)
	} else {
		return nil, errors.New("esta cuenta no tiene sesión de Claude (falta " + shortPath(filepath.Join(dir, ".credentials.json")) + ")")
	}
	var c struct {
		OAuth *claudeOAuth `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &c) != nil || c.OAuth == nil || c.OAuth.AccessToken == "" {
		return nil, errors.New("esta cuenta no inició sesión con una suscripción de Claude (usa una API key)")
	}
	return c.OAuth, nil
}

func planLabel(sub, tier string) string {
	name := map[string]string{"max": "Max", "pro": "Pro", "team": "Team", "enterprise": "Enterprise"}[sub]
	if name == "" {
		name = sub
	}
	switch {
	case strings.Contains(tier, "20x"):
		name += " 20x"
	case strings.Contains(tier, "5x"):
		name += " 5x"
	}
	return name
}

// ---------------------------------------------------------------- tokens from transcripts

// tokenCache keeps, per transcript, how far it was read and its tokens per hour, so only what
// was appended since is parsed again (the folders hold gigabytes of transcripts).
type tokenFile struct {
	Size    int64              `json:"size"`
	Off     int64              `json:"off"`
	Buckets map[int64][4]int64 `json:"buckets"` // hour (unix) → in, out, cache write, cache read
	Seen    []string           `json:"seen"`    // last message ids counted (lines repeat them)
}

var tokenCacheMu sync.Mutex

func tokenCacheFile() string { return filepath.Join(runDir(), "tokens-cache.json") }

// transcriptTokens sums the tokens of the assistant replies in a transcripts folder since two
// moments (the current 5-hour window and the week), at hour precision.
func transcriptTokens(folder string, sessionFrom, weekFrom time.Time) (session, week tokTotals) {
	tokenCacheMu.Lock()
	defer tokenCacheMu.Unlock()
	cache := map[string]*tokenFile{}
	if data, err := os.ReadFile(tokenCacheFile()); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	oldest := weekFrom
	if sessionFrom.Before(oldest) {
		oldest = sessionFrom
	}
	seen := map[string]bool{}
	_ = filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(oldest) {
			return nil
		}
		seen[path] = true
		tf := cache[path]
		if tf == nil || info.Size() < tf.Off {
			tf = &tokenFile{Buckets: map[int64][4]int64{}}
			cache[path] = tf
		}
		if info.Size() > tf.Off {
			scanTranscript(path, tf)
		}
		tf.Size = info.Size()
		return nil
	})
	sh, wh := sessionFrom.Truncate(time.Hour).Unix(), weekFrom.Truncate(time.Hour).Unix()
	for path, tf := range cache {
		if !seen[path] {
			if st, err := os.Stat(path); err != nil || st.ModTime().Before(time.Now().Add(-8*24*time.Hour)) {
				delete(cache, path) // gone or too old to matter
			}
			continue
		}
		for h, v := range tf.Buckets {
			t := tokTotals{In: v[0], Out: v[1], CacheW: v[2], CacheR: v[3]}
			if h >= sh {
				session.add(t)
			}
			if h >= wh {
				week.add(t)
			}
		}
	}
	if data, err := json.Marshal(cache); err == nil {
		_ = writeFileAtomic(tokenCacheFile(), data)
	}
	return
}

// scanTranscript reads a transcript from where it was left and adds its replies' tokens.
func scanTranscript(path string, tf *tokenFile) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(tf.Off, io.SeekStart); err != nil {
		return
	}
	seen := map[string]bool{}
	for _, id := range tf.Seen {
		seen[id] = true
	}
	r := bufio.NewReaderSize(f, 1<<20)
	off := tf.Off
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // a last line without its newline is still being written: read it next time
		}
		off += int64(len(line))
		if !bytes.Contains(line, []byte(`"usage"`)) || !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var e struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				ID    string `json:"id"`
				Usage *struct {
					In     int64 `json:"input_tokens"`
					Out    int64 `json:"output_tokens"`
					CacheW int64 `json:"cache_creation_input_tokens"`
					CacheR int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Message.Usage == nil || e.Timestamp.IsZero() {
			continue
		}
		if id := e.Message.ID; id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
			tf.Seen = append(tf.Seen, id)
		}
		h := e.Timestamp.Truncate(time.Hour).Unix()
		b := tf.Buckets[h]
		u := e.Message.Usage
		b[0] += u.In
		b[1] += u.Out
		b[2] += u.CacheW
		b[3] += u.CacheR
		tf.Buckets[h] = b
	}
	if len(tf.Seen) > 300 {
		tf.Seen = tf.Seen[len(tf.Seen)-300:]
	}
	tf.Off = off
}

// ---------------------------------------------------------------- wording

// fmtTokens renders a count as "850", "12 k" or "3.4 M".
func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e9), ".0") + " mil M"
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + " M"
	case n >= 1000:
		return fmt.Sprintf("%d k", (n+500)/1000)
	}
	return fmt.Sprintf("%d", n)
}

var weekdaysES = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}

// fmtReset says when a window resets: "a las 14:29 (en 1 h 12 min)" or "el sáb 18 oct a las
// 12:59 (en 2 días)".
func fmtReset(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	d := time.Until(t)
	in := ""
	switch {
	case d <= 0:
		in = "ya"
	case d < time.Hour:
		in = fmt.Sprintf("en %d min", int(d.Minutes())+1)
	case d < 24*time.Hour:
		in = fmt.Sprintf("en %d h %d min", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours() / 24)
		in = fmt.Sprintf("en %d días", days)
		if days == 1 {
			in = "en 1 día"
		}
	}
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return fmt.Sprintf("a las %s (%s)", t.Format("15:04"), in)
	}
	return fmt.Sprintf("el %s %d %s a las %s (%s)", weekdaysES[t.Weekday()], t.Day(), monthsES[t.Month()-1], t.Format("15:04"), in)
}

// sortAccounts puts accounts with limits first, external providers last.
func sortAccounts(a []accountUsage) {
	sort.SliceStable(a, func(i, j int) bool { return a[i].External == "" && a[j].External != "" })
}
