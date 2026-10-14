package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// aliasUse remembers an alias given to a Claude session, so it can be picked again later.
type aliasUse struct {
	Alias string    `json:"alias"`
	Tab   string    `json:"tab"`
	At    time.Time `json:"at"`
}

const aliasHistoryMax = 300

func aliasHistoryFile() string { return filepath.Join(dataDir(), "aliases.json") }

// aliasHistory returns the remembered aliases, most recent first, one entry per alias. The
// aliases of saved workspaces count too, so the list is useful from the first run.
func aliasHistory() []aliasUse {
	var uses []aliasUse
	if data, err := os.ReadFile(aliasHistoryFile()); err == nil {
		_ = json.Unmarshal(data, &uses)
	}
	for _, name := range savedWorkspaces() {
		if w, err := loadWorkspace(name); err == nil {
			for _, t := range w.Tabs {
				for _, s := range t.Slots {
					if s.Kind == kindClaude {
						uses = append(uses, aliasUse{Alias: s.Alias, Tab: t.Title, At: w.SavedAt})
					}
				}
			}
		}
	}
	sort.SliceStable(uses, func(i, j int) bool { return uses[i].At.After(uses[j].At) })
	seen := map[string]bool{}
	var out []aliasUse
	for _, u := range uses {
		if u.Alias != "" && !seen[u.Alias] {
			seen[u.Alias] = true
			out = append(out, u)
		}
	}
	return out
}

func recordAlias(alias, tab string) {
	if alias == "" {
		return
	}
	var uses []aliasUse
	if data, err := os.ReadFile(aliasHistoryFile()); err == nil {
		_ = json.Unmarshal(data, &uses)
	}
	kept := []aliasUse{{Alias: alias, Tab: tab, At: time.Now()}}
	for _, u := range uses {
		if u.Alias != alias && len(kept) < aliasHistoryMax {
			kept = append(kept, u)
		}
	}
	if data, err := json.MarshalIndent(kept, "", "  "); err == nil {
		_ = writeFileAtomic(aliasHistoryFile(), data)
	}
}

// suggestAliases filters the history by substring, newest first.
func suggestAliases(query string, history []aliasUse, limit int) []aliasUse {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []aliasUse
	for _, u := range history {
		if len(out) >= limit {
			break
		}
		if q == "" || strings.Contains(strings.ToLower(u.Alias), q) {
			out = append(out, u)
		}
	}
	return out
}

// splitArgs splits a command-line string into words, honouring single and double quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

// joinArgs renders words back as a command line, quoting the ones that need it.
func joinArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"$`\\{}") {
			parts[i] = shQuote(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

func argsToOption(args []string) string {
	if len(args) == 0 {
		return ""
	}
	data, _ := json.Marshal(args)
	return string(data)
}

func argsFromOption(v string) []string {
	var args []string
	if v != "" {
		_ = json.Unmarshal([]byte(v), &args)
	}
	return args
}
