package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// foundConv is a conversation located on disk from (part of) its ID.
type foundConv struct {
	conv recentConv
	cwd  string // directory the conversation was started in
	path string
}

// findConversations looks a session ID (or its first characters, or a transcript path) up in
// the config dir of every claude alias. Config dirs that share one projects folder are read once.
func findConversations(query string, aliases []ClaudeAlias) []foundConv {
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
			c := recentConv{ID: id, Title: title, First: first, Alias: a.Name}
			if st != nil {
				c.Modified = st.ModTime()
			}
			out = append(out, foundConv{conv: c, cwd: transcriptCwd(m), path: m})
		}
	}
	return out
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
	f, err := pickConversation(cfg.claudeAliases(), open)
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

// pickConversation asks for a conversation ID (or its first characters) and shows what it
// matches; it returns nil when the user cancels. open marks conversations already in a session.
func pickConversation(aliases []ClaudeAlias, open map[string]string) (*foundConv, error) {
	var input []rune
	var found []foundConv
	sel := 0
	msg := ""
	var hits []hit
	lookup := func() {
		found = findConversations(string(input), aliases)
		sel = 0
		msg = ""
	}
	draw := func() {
		var s screen
		s.at(2, 4, sgrMuted+"Pega el ID de una conversación de Claude (completo o sus primeros caracteres)."+sgrReset)
		s.at(4, 4, "ID")
		s.at(4, 17, sgrFocus+" "+fit(string(input), fieldWidth-1)+sgrReset)
		row := 6
		switch {
		case len(input) > 0 && len(input) < 6:
			s.at(row, 17, sgrDim+"escribe al menos 6 caracteres"+sgrReset)
		case len(input) >= 6 && len(found) == 0:
			s.at(row, 17, sgrRed+"no encontré ninguna conversación con ese ID"+sgrReset)
		}
		for i, f := range found {
			if i >= 5 {
				break
			}
			name := f.conv.Title
			if name == "" {
				name = "(sin nombre)"
			}
			line := " " + fit(f.conv.ID[:min(8, len(f.conv.ID))], 9) + fit(name, 18) + fit(whenES(f.conv.Modified), 11) + fit(shortPath(f.cwd), 22) + " "
			if i == sel {
				line = sgrSel + line + sgrReset
			}
			s.btn(row+i, 3, line, "pick:"+itoa(i))
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
		hits = s.hits
		s.flush(true)
	}
	setRaw(true)
	defer setRaw(false)
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
				sel = (sel - 1 + min(len(found), 5)) % min(len(found), 5)
			case k.kind == keyDown && len(found) > 0:
				sel = (sel + 1) % min(len(found), 5)
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
					msg = "Primero escribe un ID que exista."
					break
				}
				f := found[sel]
				return &f, nil
			}
		}
		draw()
	}
}
