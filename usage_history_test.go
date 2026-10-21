package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateUsage(t *testing.T) {
	t.Helper()
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SHELL", "")
}

func historyLine(id string, at time.Time, out int) string {
	return fmt.Sprintf("{\"type\":\"assistant\",\"sessionId\":\"session\",\"timestamp\":%q,\"message\":{\"id\":%q,\"model\":\"test-model\",\"usage\":{\"input_tokens\":10,\"output_tokens\":%d,\"cache_creation_input_tokens\":20,\"cache_read_input_tokens\":30}}}\n", at.Format(time.RFC3339Nano), id, out)
}

func TestHistoryIncrementalAttribution(t *testing.T) {
	isolateUsage(t)
	root := t.TempDir()
	folder := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{HideClaudeAlias: true, ClaudeAliases: []ClaudeAlias{
		{Name: "first", Env: []string{"CLAUDE_CONFIG_DIR=" + root}},
		{Name: "second", Env: []string{"CLAUDE_CONFIG_DIR=" + root}},
	}}
	now := time.Now().UTC().Add(-time.Minute)
	owners := []usageOwner{{"session", "first", now.Add(-time.Hour)}, {"session", "second", now.Add(time.Second)}}
	f, err := os.Create(filepath.Join(dataDir(), "usage-owners.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range owners {
		if err := json.NewEncoder(f).Encode(owner); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	path := filepath.Join(folder, "session.jsonl")
	first := historyLine("one", now, 1)
	partial := historyLine("two", now.Add(2*time.Second), 3)
	if err := os.WriteFile(path, []byte(first+historyLine("one", now, 5)+partial[:len(partial)-1]), 0600); err != nil {
		t.Fatal(err)
	}
	if err := collectUsageHistory(cfg); err != nil {
		t.Fatal(err)
	}
	h, err := readUsageHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Messages) != 1 || h.Messages["one"].Point.Tokens.Out != 5 || h.Messages["one"].Point.Alias != "first" {
		t.Fatalf("Primera lectura incorrecta: %+v", h.Messages)
	}
	if err := collectUsageHistory(cfg); err != nil {
		t.Fatal(err)
	}
	appendFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	appendFile.WriteString("\n")
	appendFile.Close()
	if err := collectUsageHistory(cfg); err != nil {
		t.Fatal(err)
	}
	h, err = readUsageHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Messages) != 2 || h.Messages["two"].Point.Alias != "second" {
		t.Fatalf("Lectura incremental incorrecta: %+v", h.Messages)
	}
	var total tokTotals
	for _, p := range h.Points {
		total.add(p.Tokens)
	}
	if total != (tokTotals{In: 20, Out: 8, CacheW: 40, CacheR: 60}) {
		t.Fatalf("Tokens duplicados: %+v", total)
	}
}

func TestHistorySharedFolderUnattributed(t *testing.T) {
	isolateUsage(t)
	root := t.TempDir()
	folder := filepath.Join(root, "projects")
	os.MkdirAll(folder, 0755)
	os.WriteFile(filepath.Join(folder, "session.jsonl"), []byte(historyLine("one", time.Now(), 1)), 0600)
	cfg := Config{HideClaudeAlias: true, ClaudeAliases: []ClaudeAlias{
		{Name: "a", Env: []string{"CLAUDE_CONFIG_DIR=" + root}},
		{Name: "b", Env: []string{"CLAUDE_CONFIG_DIR=" + root}},
	}}
	if err := collectUsageHistory(cfg); err != nil {
		t.Fatal(err)
	}
	h, err := readUsageHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Points) != 1 || !strings.HasPrefix(h.Points[0].Alias, "Sin atribuir") {
		t.Fatalf("Atribución inventada: %+v", h.Points)
	}
}

type usageTransport func(*http.Request) (*http.Response, error)

func (f usageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUsageCredentialCacheAndFresh(t *testing.T) {
	isolateUsage(t)
	root := t.TempDir()
	token := "first"
	writeCredentials := func() {
		t.Helper()
		if err := writeJSONFile(filepath.Join(root, ".credentials.json"), map[string]any{"claudeAiOauth": claudeOAuth{AccessToken: token}}); err != nil {
			t.Fatal(err)
		}
	}
	writeCredentials()
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := 0
	http.DefaultTransport = usageTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		pct := 12
		if r.Header.Get("Authorization") == "Bearer second" {
			pct = 80
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf("{\"five_hour\":{\"utilization\":%d}}", pct))), Header: make(http.Header)}, nil
	})
	check := func(fresh bool, want float64) {
		t.Helper()
		u := accountUsage{ConfigDir: root}
		fillAccountUsage(&u, fresh)
		if u.Err != "" || u.Session == nil || u.Session.Pct != want {
			t.Fatalf("Cuota incorrecta: %+v", u)
		}
	}
	check(false, 12)
	check(false, 12)
	if calls != 1 {
		t.Fatal("No reutilizó caché")
	}
	token = "second"
	writeCredentials()
	check(false, 80)
	if calls != 2 {
		t.Fatal("Reutilizó cuota de otra credencial")
	}
	check(true, 80)
	if calls != 3 {
		t.Fatal("Actualizar no omitió caché")
	}
}

func TestUsageInterval(t *testing.T) {
	if (Config{}).usageInterval() != time.Hour {
		t.Fatal("Intervalo predeterminado incorrecto")
	}
	if (Config{UsageIntervalSeconds: 120}).usageInterval() != 2*time.Minute {
		t.Fatal("Intervalo configurable incorrecto")
	}
}
