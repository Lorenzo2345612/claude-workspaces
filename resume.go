package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// foundConv is a conversation located on disk from (part of) its ID.
type foundConv struct {
	conv   recentConv
	cwd    string // directory the conversation was started in
	path   string
	search string // lowercased ID, title, first prompt and directory, for filtering
}

// findConversations looks a session ID (or its first characters, or a transcript path) up in
// the config dir of every claude alias. Config dirs that share one projects folder are read once.
func findConversations(query string, cfg Config) []foundConv {
	aliases := cfg.claudeAliases()
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if strings.HasSuffix(q, ".jsonl") { // a transcript path
		q = strings.TrimSuffix(filepath.Base(q), ".jsonl")
	}
	if len(q) < 6 || strings.ContainsAny(q, "/*?[ ") {
		return nil
	}
	var out []foundConv
	seenDir, seenID := map[string]bool{}, map[string]bool{}
	for _, a := range aliases {
		if a.isCodex() {
			continue
		}
		projects := filepath.Join(a.configDir(), "projects")
		real, err := filepath.EvalSymlinks(projects)
		if err != nil || seenDir[real] {
			continue
		}
		seenDir[real] = true
		matches, _ := filepath.Glob(filepath.Join(projects, "*", q+"*.jsonl"))
		for _, m := range matches {
			id := strings.TrimSuffix(filepath.Base(m), ".jsonl")
			if seenID[id] {
				continue
			}
			seenID[id] = true
			title, first := readTranscriptSummary(m)
			st, _ := os.Stat(m)
			c := recentConv{ID: id, Title: title, First: first, Alias: guessAlias(cfg, id, m)}
			if st != nil {
				c.Modified = st.ModTime()
			}
			out = append(out, foundConv{conv: c, cwd: transcriptCwd(m), path: m})
		}
	}
	return out
}

// browseConversations lists resumable conversations across configured Claude accounts.
// Exact ID lookup stays separate because launch paths rely on its strict semantics.
func browseConversations(query string, cfg Config) []foundConv {
	return filterConversations(scanConversations(cfg), query, newAliasGuesser(cfg))
}

// transcriptCwd reads the working directory recorded in a transcript's first entries.
func transcriptCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for i := 0; i < 50 && sc.Scan(); i++ {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"cwd"`) {
			continue
		}
		var e struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &e) == nil && e.Cwd != "" {
			return e.Cwd
		}
	}
	return ""
}

// uiResumeByID opens an existing Claude conversation, given its ID, as a session of the current
// tab: for conversations started outside cw that should join a workspace.
func uiResumeByID(ws string) error {
	t := Tmux{WS: ws}
	_, session, err := currentClient(t)
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	open := map[string]string{} // session ID → name of the cw session that has it open
	for _, r := range listSessionRows(t) {
		if r.slot.SessionID != "" {
			open[r.slot.SessionID] = r.slot.Alias + " (pestaña " + r.tab + ")"
		}
	}
	f, err := pickConversation(cfg, open)
	if err != nil || f == nil {
		return err
	}
	c := f.conv
	c.Cwd = f.cwd
	err = newClaudeDialog(ws, session, &c)
	if errors.Is(err, errCancelled) {
		return nil
	}
	return err
}

// pickConversation lists recent conversations and filters them as the user types.
// It returns nil on cancel; open marks conversations already in a session.
func pickConversation(cfg Config, open map[string]string) (*foundConv, error) {
	var input []rune
	var found []foundConv
	sel := 0
	msg := ""
	var hits []hit
	// Scan once when the dialog opens and filter in memory while typing: a scan has to stat
	// every transcript of every account, too slow to repeat on each keystroke.
	var all []foundConv
	loading := true
	guesser := newAliasGuesser(cfg)
	lookup := func() {
		found = filterConversations(all, string(input), guesser)
		sel = 0
		msg = ""
	}
	draw := func() {
		var s screen
		s.at(2, 4, sgrMuted+"Elige una conversación de Claude o busca por nombre, texto, carpeta o ID."+sgrReset)
		s.at(4, 4, "Buscar")
		s.at(4, 17, sgrFocus+" "+fit(string(input), fieldWidth-1)+sgrReset)
		row := 6
		if loading {
			s.at(row, 4, sgrDim+"Cargando conversaciones…"+sgrReset)
		} else if len(found) == 0 {
			s.at(row, 4, sgrDim+"No hay conversaciones que coincidan."+sgrReset)
		}
		start := max(0, sel-4)

		for i, f := range found {
			if i < start {
				continue
			}
			if i >= start+5 {
				break
			}
			name := f.conv.Title
			if name == "" {
				name = f.conv.First
			}
			line := " " + fit(f.conv.ID[:min(8, len(f.conv.ID))], 9) + fit(name, 18) + fit(whenES(f.conv.Modified), 11) + fit(shortPath(f.cwd), 22) + " "
			if i == sel {
				line = sgrSel + line + sgrReset
			}
			s.btn(row+i-start, 3, line, "pick:"+itoa(i))
			if first := f.conv.First; i == sel && first != "" {
				s.at(row+min(len(found), 5)+1, 4, sgrDim+"«"+shorten(first, 64)+"»"+sgrReset)
			}
		}
		foot := row + 8
		if len(found) > 0 {
			if where, ok := open[found[sel].conv.ID]; ok {
				s.at(foot-1, 4, sgrBusy+"Ya está abierta en este workspace: "+where+sgrReset)
			}
		}
		if msg != "" {
			s.at(foot-1, 4, sgrRed+msg+sgrReset)
		}
		s.place(foot, 4, []lineItem{buttonLine(0, btnDef{label: "Cancelar", id: "cancel"},
			btnDef{label: "Siguiente", id: "next", primary: true})})
		s.at(4, 18+min(len(input), fieldWidth-2), "\033[?25h")
		s.closeX("cancel")
		hits = s.hits
		s.flush(true)
	}
	setRaw(true)
	defer setRaw(false)
	draw()
	all = scanConversations(cfg)
	loading = false
	lookup()
	draw()
	for {
		keys, err := readKeys()
		if err != nil {
			return nil, nil
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(hits, k.x, k.y)
			}
			switch {
			case id == "cancel" || k.kind == keyEsc:
				return nil, nil
			case strings.HasPrefix(id, "pick:"):
				sel = atoiDefault(id[5:], 0)
			case k.kind == keyUp && len(found) > 0:
				sel = (sel - 1 + len(found)) % len(found)
			case k.kind == keyDown && len(found) > 0:
				sel = (sel + 1) % len(found)
			case k.kind == keyBackspace:
				if len(input) > 0 {
					input = input[:len(input)-1]
				}
				lookup()
			case k.kind == keyCtrlU:
				input = nil
				lookup()
			case k.kind == keyRune:
				input = append(input, k.r)
				lookup()
			case id == "next" || k.kind == keyEnter:
				if len(found) == 0 {
					msg = "Elige una conversación de la lista."
					break
				}
				f := found[sel]
				return &f, nil
			}
		}
		draw()
	}
}

// guessAlias proposes the claude alias to reopen a conversation with. Aliases often share one
// projects folder, so several can reach the same transcript; prefer, in order: the alias cw last
// ran it with, the default alias, an alias that skips permissions when the conversation did,
// and finally the first alias that reaches it.
func guessAlias(cfg Config, id, path string) string {
	return newAliasGuesser(cfg).guess(id, path)
}

// aliasGuesser answers guessAlias for many conversations: it reads the saved workspaces once and
// remembers each answer, so the resume picker can call it on every keystroke.
type aliasGuesser struct {
	cfg   Config
	last  map[string]string // session ID → alias cw last ran it with
	known map[string]string // session ID → guessed alias
}

func newAliasGuesser(cfg Config) *aliasGuesser {
	g := &aliasGuesser{cfg: cfg, last: map[string]string{}, known: map[string]string{}}
	lastAt := map[string]time.Time{}
	for _, n := range savedWorkspaces() {
		w, err := loadWorkspace(n)
		if err != nil {
			continue
		}
		for _, t := range w.Tabs {
			for _, s := range t.Slots {
				if s.SessionID != "" && s.ClaudeAlias != "" && w.SavedAt.After(lastAt[s.SessionID]) {
					g.last[s.SessionID], lastAt[s.SessionID] = s.ClaudeAlias, w.SavedAt
				}
			}
		}
	}
	return g
}

func (g *aliasGuesser) guess(id, path string) string {
	if a, ok := g.known[id]; ok {
		return a
	}
	a := g.compute(id, path)
	g.known[id] = a
	return a
}

func (g *aliasGuesser) compute(id, path string) string {
	cfg, last := g.cfg, g.last[id]
	bypassed := transcriptBypassed(path)
	if last != "" {
		a := cfg.claudeAlias(last)
		// A record that contradicts the conversation (an alias that asks for permissions on a
		// conversation that ran without them) comes from the 0.6.1-and-older guess: skip it.
		if !bypassed || strings.Contains(a.summary(), "dangerously-skip-permissions") {
			return a.Name
		}
	}
	projects, _ := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(path)))
	var reach []ClaudeAlias
	for _, a := range cfg.claudeAliases() {
		if a.isCodex() {
			continue
		}
		if p, err := filepath.EvalSymlinks(filepath.Join(a.configDir(), "projects")); err == nil && p == projects {
			reach = append(reach, a)
		}
	}
	if len(reach) == 0 {
		return cfg.defaultAlias()
	}
	for _, a := range reach {
		if a.Name == cfg.defaultAlias() {
			return a.Name
		}
	}
	if bypassed {
		for _, a := range reach {
			if strings.Contains(a.summary(), "dangerously-skip-permissions") {
				return a.Name
			}
		}
	}
	return reach[0].Name
}

// transcriptBypassed reports whether the conversation ran without permission prompts.
func transcriptBypassed(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for i := 0; i < 50 && sc.Scan(); i++ {
		if strings.Contains(sc.Text(), `"permissionMode":"bypassPermissions"`) {
			return true
		}
	}
	return false
}
