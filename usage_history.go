package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const historyDays = 30

type usagePoint struct {
	Hour   int64     `json:"hour"`
	Alias  string    `json:"alias"`
	Model  string    `json:"model"`
	Tokens tokTotals `json:"tokens"`
}

type historyMessage struct {
	Point usagePoint `json:"point"`
}

type usageHistory struct {
	At       time.Time                 `json:"at"`
	Files    map[string]int64          `json:"files"`
	Messages map[string]historyMessage `json:"messages"`
	Points   []usagePoint              `json:"points"`
}

type usageOwner struct {
	Session string    `json:"session"`
	Alias   string    `json:"alias"`
	From    time.Time `json:"from"`
}

func historyPath() string { return filepath.Join(dataDir(), "usage-history.json") }

func (c Config) usageInterval() time.Duration {
	if c.UsageIntervalSeconds < 60 {
		return time.Hour
	}
	return time.Duration(c.UsageIntervalSeconds) * time.Second
}

func usageLock(name string, nonblock bool) (*os.File, error) {
	if err := os.MkdirAll(runDir(), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(runDir(), name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flags := unix.LOCK_EX
	if nonblock {
		flags |= unix.LOCK_NB
	}
	if err := unix.Flock(int(f.Fd()), flags); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// El worker no consulta modelos ni credenciales: solo lee los cambios en transcripts locales.
func ensureUsageWorker() {
	lock, err := usageLock("usage-worker.lock", true)
	if err != nil {
		return
	}
	lock.Close()
	bin, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "_usage-worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

func runUsageWorker() error {
	lock, err := usageLock("usage-worker.lock", true)
	if err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer lock.Close()
	for {
		cfg, err := loadConfig()
		if err == nil {
			st, err := os.Stat(historyPath())
			if err != nil || time.Since(st.ModTime()) >= cfg.usageInterval() {
				_ = collectUsageHistory(cfg)
			}
		}
		time.Sleep(30 * time.Second)
	}
}

func readUsageHistory() (usageHistory, error) {
	h := usageHistory{Files: map[string]int64{}, Messages: map[string]historyMessage{}, Points: []usagePoint{}}
	data, err := os.ReadFile(historyPath())
	if errors.Is(err, fs.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(data, &h)
	if h.Files == nil {
		h.Files = map[string]int64{}
	}
	if h.Messages == nil {
		h.Messages = map[string]historyMessage{}
	}
	if h.Points == nil {
		h.Points = []usagePoint{}
	}
	return h, err
}

// Los cambios de cuenta quedan fechados; una conversación retomada no cambia de dueño retroactivamente.
func recordUsageOwner(session, alias string) {
	if session == "" || alias == "" {
		return
	}
	lock, err := usageLock("usage-owners.lock", false)
	if err != nil {
		return
	}
	defer lock.Close()
	f, err := os.OpenFile(filepath.Join(dataDir(), "usage-owners.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(usageOwner{session, alias, time.Now().UTC()})
}

func collectUsageHistory(cfg Config) error {
	lock, err := usageLock("usage-collect.lock", false)
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := readUsageHistory()
	if err != nil {
		return err
	}
	owners := map[string][]usageOwner{}
	if f, err := os.Open(filepath.Join(dataDir(), "usage-owners.jsonl")); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			var o usageOwner
			if json.Unmarshal(s.Bytes(), &o) == nil {
				owners[o.Session] = append(owners[o.Session], o)
			}
		}
		f.Close()
	}
	folders := map[string][]string{}
	for _, a := range cfg.claudeAliases() {
		if a.isCodex() {
			continue
		}
		dir, err := filepath.EvalSymlinks(filepath.Join(cfg.accountDir(a), "projects"))
		if err == nil {
			folders[dir] = append(folders[dir], a.Name)
		}
	}
	cutoff := time.Now().AddDate(0, 0, -historyDays)
	for folder, aliases := range folders {
		sort.Strings(aliases)
		fallback := aliases[0]
		if len(aliases) > 1 {
			fallback = "Sin atribuir (" + strings.Join(aliases, ", ") + ")"
		}
		err := filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Before(cutoff) {
				return nil
			}
			offset := h.Files[path]
			if info.Size() < offset {
				offset = 0
			}
			if info.Size() == offset {
				return nil
			}
			next, err := scanHistoryTranscript(path, offset, fallback, owners, &h, cutoff)
			h.Files[path] = next
			return err
		})
		if err != nil {
			return err
		}
	}
	for path := range h.Files {
		if st, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) || (err == nil && st.ModTime().Before(cutoff)) {
			delete(h.Files, path)
		}
	}
	buckets := map[string]*usagePoint{}
	for id, msg := range h.Messages {
		p := msg.Point
		if p.Hour < cutoff.Truncate(time.Hour).Unix() {
			delete(h.Messages, id)
			continue
		}
		key := fmt.Sprintf("%d\x00%s\x00%s", p.Hour, p.Alias, p.Model)
		b := buckets[key]
		if b == nil {
			b = &usagePoint{Hour: p.Hour, Alias: p.Alias, Model: p.Model}
			buckets[key] = b
		}
		b.Tokens.add(p.Tokens)
	}
	h.Points = []usagePoint{}
	for _, p := range buckets {
		h.Points = append(h.Points, *p)
	}
	sort.Slice(h.Points, func(i, j int) bool {
		a, b := h.Points[i], h.Points[j]
		if a.Hour != b.Hour {
			return a.Hour < b.Hour
		}
		if a.Alias != b.Alias {
			return a.Alias < b.Alias
		}
		return a.Model < b.Model
	})
	h.At = time.Now().UTC()
	return writeJSONFile(historyPath(), h)
}

func scanHistoryTranscript(path string, offset int64, fallback string, owners map[string][]usageOwner, h *usageHistory, cutoff time.Time) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return offset, nil
			}
			return offset, err
		}
		start := offset
		offset += int64(len(line))
		var e struct {
			Type      string    `json:"type"`
			Session   string    `json:"sessionId"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Usage *struct {
					In     int64 `json:"input_tokens"`
					Out    int64 `json:"output_tokens"`
					CacheW int64 `json:"cache_creation_input_tokens"`
					CacheR int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Message.Usage == nil || e.Timestamp.Before(cutoff) {
			continue
		}
		if e.Session == "" {
			e.Session = strings.TrimSuffix(filepath.Base(path), ".jsonl")
		}
		alias := fallback
		for _, o := range owners[e.Session] {
			if !e.Timestamp.Before(o.From) {
				alias = o.Alias
			}
		}
		model := e.Message.Model
		if model == "" {
			model = "desconocido"
		}
		id := e.Message.ID
		if id == "" {
			id = fmt.Sprintf("%s:%d", path, start)
		}
		u := e.Message.Usage
		p := usagePoint{Hour: e.Timestamp.Truncate(time.Hour).Unix(), Alias: alias, Model: model,
			Tokens: tokTotals{In: max(0, u.In), Out: max(0, u.Out), CacheW: max(0, u.CacheW), CacheR: max(0, u.CacheR)}}
		if old, ok := h.Messages[id]; ok {
			p = old.Point
			p.Tokens.In = max(p.Tokens.In, u.In)
			p.Tokens.Out = max(p.Tokens.Out, u.Out)
			p.Tokens.CacheW = max(p.Tokens.CacheW, u.CacheW)
			p.Tokens.CacheR = max(p.Tokens.CacheR, u.CacheR)
		}
		h.Messages[id] = historyMessage{p}
	}
}

func printUsageHistory() error {
	h, err := readUsageHistory()
	if err != nil {
		return err
	}
	fmt.Println("\nHistorial por alias y modelo · últimos 30 días · tokens/hora")
	if h.At.IsZero() {
		fmt.Println("Primera lectura pendiente.")
		return nil
	}
	type series struct{ alias, model string }
	totals := map[series]tokTotals{}
	hours := map[series]map[int64]int64{}
	var order []series
	for _, p := range h.Points {
		k := series{p.Alias, p.Model}
		if _, ok := totals[k]; !ok {
			order = append(order, k)
			hours[k] = map[int64]int64{}
		}
		t := totals[k]
		t.add(p.Tokens)
		totals[k] = t
		hours[k][p.Hour] += p.Tokens.Sum()
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].alias != order[j].alias {
			return order[i].alias < order[j].alias
		}
		return order[i].model < order[j].model
	})
	for _, k := range order {
		t := totals[k]
		fmt.Printf("%s · %s\n", k.alias, k.model)
		end := h.At.Truncate(time.Hour).Unix()
		peak := int64(1)
		for hour, n := range hours[k] {
			if hour >= end-23*3600 && hour <= end {
				peak = max(peak, n)
			}
		}
		var chart strings.Builder
		for hour := end - 23*3600; hour <= end; hour += 3600 {
			n := hours[k][hour]
			r := []rune(" ▁▂▃▄▅▆▇█")[int(float64(n)/float64(peak)*8)]
			chart.WriteRune(r)
		}
		fmt.Printf("  %s  últimas 24 h · máximo %s tokens/h\n", chart.String(), fmtTokens(peak))
		fmt.Printf("  entrada %s · salida %s · caché escritura %s · caché lectura %s\n", fmtTokens(t.In), fmtTokens(t.Out), fmtTokens(t.CacheW), fmtTokens(t.CacheR))
	}
	fmt.Println("Actualizado:", h.At.Local().Format("2006-01-02 15:04"))
	return nil
}
