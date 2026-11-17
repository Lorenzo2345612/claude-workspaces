package main

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Whatever the user was typing in a session's input box survives a message cw types into it
// (cw send, the replies, Skynet):
//
//   - Claude Code: Ctrl+S stashes the draft and Claude restores it on its own right after the
//     next message is submitted, also when that message is queued because Claude is working.
//     The draft comes back exactly, newlines included.
//   - Codex has no stash: cw reads the draft from the screen, clears the box with Ctrl+U, sends
//     the message and pastes the draft back. The screen is all cw can read, so a soft wrap and
//     a typed newline are told apart by width.
//
// When the draft cannot be moved out of the way, nothing is sent: mixing the message into what
// the user wrote would be worse than failing.

var (
	sgrRe      = regexp.MustCompile("\033\\[([0-9;]*)m")
	menuItemRe = regexp.MustCompile(`^[❯›] +\d{1,2}\. `)
)

// stripANSI drops colour codes and other escape sequences (OSC 8 links included).
func stripANSI(s string) string {
	s = regexp.MustCompile("\033\\][^\007\033]*(\007|\033\\\\)").ReplaceAllString(s, "")
	return ansiRe.ReplaceAllString(s, "")
}

// draftBox is the input box as the screen shows it.
type draftBox struct {
	found   bool
	present bool     // the user typed something (not the dim placeholder)
	rows    []string // the box's rows without the prompt or the indent
	width   int      // the pane's width
}

func (b draftBox) String() string { return strings.Join(b.rows, "\n") }

// readDraftBox finds the input box at the bottom of the screen. Claude draws it between two
// rules with the prompt "❯", Codex with the prompt "›" up to the first blank row.
func readDraftBox(s *mcpSession, codex bool) (draftBox, error) {
	t := Tmux{WS: s.Workspace}
	raw, err := t.Run("capture-pane", "-e", "-p", "-t", s.pane)
	if err != nil {
		return draftBox{}, err
	}
	box := draftBox{}
	if w, err := t.Run("display", "-p", "-t", s.pane, "#{pane_width}"); err == nil {
		box.width, _ = strconv.Atoi(strings.TrimSpace(w))
	}
	return parseDraftBox(strings.Split(raw, "\n"), codex, box.width), nil
}

func isRule(plain string) bool {
	plain = strings.TrimSpace(plain)
	return utf8.RuneCountInString(plain) >= 10 && strings.Trim(plain, "─") == ""
}

func parseDraftBox(raw []string, codex bool, width int) draftBox {
	box := draftBox{width: width}
	plain := make([]string, len(raw))
	for i, r := range raw {
		plain[i] = strings.TrimRight(stripANSI(r), " ")
	}
	prompt := "❯"
	if codex {
		prompt = "›"
	}
	start := -1
	for i := len(plain) - 1; i >= 0; i-- {
		p := strings.ReplaceAll(plain[i], " ", " ")
		if p != prompt && !strings.HasPrefix(p, prompt+" ") {
			continue
		}
		if !codex && (i == 0 || !isRule(plain[i-1])) {
			continue // a "❯" outside the box: a menu, the history
		}
		if menuItemRe.MatchString(p) {
			continue // the selected option of a menu (Codex's update prompt, a dialog)
		}
		start = i
		break
	}
	if start < 0 {
		return box
	}
	box.found = true
	end := start + 1
	for end < len(plain) {
		if (!codex && isRule(plain[end])) || (codex && strings.TrimSpace(plain[end]) == "") {
			break
		}
		end++
	}
	if !codex && end == len(plain) {
		box.found = false // no closing rule: not the input box
		return box
	}
	first := strings.ReplaceAll(plain[start], " ", " ")
	first = strings.TrimPrefix(strings.TrimPrefix(first, prompt), " ")
	box.rows = append(box.rows, first)
	for _, r := range plain[start+1 : end] {
		box.rows = append(box.rows, strings.TrimPrefix(r, "  "))
	}
	for len(box.rows) > 0 && strings.TrimSpace(box.rows[len(box.rows)-1]) == "" {
		box.rows = box.rows[:len(box.rows)-1]
	}
	if len(box.rows) == 0 {
		return box
	}
	// The placeholder ("Try …", "Ask Codex to do anything") is dim and alone; typed text is not.
	if len(box.rows) == 1 && dimAfterPrompt(raw[start], prompt) {
		box.rows = nil
		return box
	}
	box.present = true
	return box
}

// dimAfterPrompt reports whether the first text after the prompt is drawn dim (SGR 2).
func dimAfterPrompt(raw, prompt string) bool {
	_, rest, ok := strings.Cut(raw, prompt)
	if !ok {
		return false
	}
	dim := false
	for {
		rest = strings.TrimLeft(rest, "  ")
		loc := sgrRe.FindStringSubmatchIndex(rest)
		if loc == nil || loc[0] != 0 {
			return dim
		}
		for _, p := range strings.Split(rest[loc[2]:loc[3]], ";") {
			switch p {
			case "2":
				dim = true
			case "0", "", "22":
				dim = false
			}
		}
		rest = rest[loc[1]:]
	}
}

// codexDraftText rebuilds what was typed from the rows: a row that could not fit the next row's
// first word was wrapped by Codex, so they join; otherwise the user typed a newline there.
func codexDraftText(b draftBox) string {
	width := b.width - 2 // the prompt or the indent
	if width < 10 {
		return strings.Join(b.rows, "\n")
	}
	var out strings.Builder
	for i, r := range b.rows {
		out.WriteString(r)
		if i == len(b.rows)-1 {
			break
		}
		next := b.rows[i+1]
		word, _, _ := strings.Cut(strings.TrimLeft(next, " "), " ")
		n := utf8.RuneCountInString(r)
		switch {
		case n >= width:
			// filled to the edge: a long word cut in two
		case n+1+utf8.RuneCountInString(word) > width-2:
			out.WriteString(" ")
		default:
			out.WriteString("\n")
		}
	}
	return out.String()
}

func isCodexSession(s *mcpSession) bool {
	if s.Kind != kindClaude {
		return false
	}
	cfg, err := loadConfig()
	if err != nil {
		return false
	}
	return cfg.claudeAlias(s.Alias).isCodex()
}

// pasteAndSubmit types text as one paste and presses Enter.
func pasteAndSubmit(s *mcpSession, text string) error {
	if err := pasteInto(s, text); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond) // let the paste land before submitting it
	_, err := Tmux{WS: s.Workspace}.Run("send-keys", "-t", s.pane, "Enter")
	return err
}

// submitChecked pastes text and submits it, checking that it really left the box: an agent
// that is starting or redrawing can swallow the Enter (or the paste lands after it). It
// compares the box with what the paste left there, so long messages count too.
func submitChecked(s *mcpSession, codex bool, text string) error {
	pasted, ok, err := pasteVerified(s, codex, text)
	if err != nil {
		return err
	}
	t := Tmux{WS: s.Workspace}
	for attempt := 0; attempt < 4; attempt++ {
		time.Sleep(250 * time.Millisecond) // let the paste settle before submitting it
		if _, err := t.Run("send-keys", "-t", s.pane, "Enter"); err != nil {
			return err
		}
		if !ok {
			return nil // the box never showed the paste: nothing reliable to compare with
		}
		if _, gone := waitBox(s, codex, 1500*time.Millisecond, func(b draftBox) bool {
			return !b.found || !b.present || b.String() != pasted.String()
		}); gone {
			return nil
		}
	}
	return fmt.Errorf("pegué el mensaje en %s pero no se envió: sigue en su caja de texto", s.Name)
}

// pasteVerified pastes text and waits for the box to show it. A session that just started can
// drop a paste: when the box is still empty, it pastes once more (never when the text showed
// up late, which would type it twice).
func pasteVerified(s *mcpSession, codex bool, text string) (draftBox, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := pasteInto(s, text); err != nil {
			return draftBox{}, false, err
		}
		b, ok := waitBox(s, codex, 3*time.Second, func(b draftBox) bool { return b.present })
		if ok || !b.found {
			return b, ok, nil
		}
	}
	return draftBox{}, false, nil
}

// waitReady waits until the session shows its input box steadily: a session that is still
// starting would swallow what cw types. It fails when the box never shows (a dialog is open,
// such as Claude asking whether to trust the folder).
func waitReady(s *mcpSession, codex bool) (draftBox, error) {
	deadline := time.Now().Add(45 * time.Second)
	steady := 0
	var b draftBox
	for time.Now().Before(deadline) {
		var err error
		if b, err = readDraftBox(s, codex); err != nil {
			return b, err
		}
		if b.found {
			if steady++; steady >= 3 {
				return b, nil
			}
		} else {
			steady = 0
		}
		time.Sleep(300 * time.Millisecond)
	}
	return b, fmt.Errorf("%s no muestra su caja de texto (¿está arrancando o tiene un diálogo abierto?); no envié nada", s.Name)
}

// waitBox polls the input box until cond holds or the time runs out.
func waitBox(s *mcpSession, codex bool, d time.Duration, cond func(draftBox) bool) (draftBox, bool) {
	deadline := time.Now().Add(d)
	for {
		b, err := readDraftBox(s, codex)
		if err == nil && cond(b) {
			return b, true
		}
		if time.Now().After(deadline) {
			return b, false
		}
		time.Sleep(120 * time.Millisecond)
	}
}

// sendKeepingDraft submits text to an agent session without losing the user's draft.
func sendKeepingDraft(s *mcpSession, text string) error {
	// One message at a time per session: two sends must not stash each other's drafts.
	key := sha256.Sum256([]byte(s.Workspace + ":" + s.pane))
	if lock, err := usageLock(fmt.Sprintf("send-%x.lock", key[:12]), false); err == nil {
		defer lock.Close()
	}
	codex := isCodexSession(s)
	box, err := waitReady(s, codex)
	if err != nil {
		return err
	}
	if !box.present {
		return submitChecked(s, codex, text)
	}
	t := Tmux{WS: s.Workspace}
	empty := func(b draftBox) bool { return b.found && !b.present }
	if !codex {
		if _, err := t.Run("send-keys", "-t", s.pane, "C-s"); err != nil {
			return err
		}
		if _, ok := waitBox(s, false, 2*time.Second, empty); !ok {
			return fmt.Errorf("%s tiene texto a medio escribir y no pude apartarlo; no envié nada", s.Name)
		}
		if err := submitChecked(s, false, text); err != nil {
			return err
		}
		// Claude puts the stashed draft back by itself once the message is submitted. Should a
		// version not do it, Ctrl+S brings it back by hand.
		if _, ok := waitBox(s, false, 3*time.Second, func(b draftBox) bool { return b.present }); !ok {
			_, _ = t.Run("send-keys", "-t", s.pane, "C-s")
		}
		return nil
	}
	draft := codexDraftText(box)
	clear := func(n int) {
		for i := 0; i < n; i++ {
			_, _ = t.Run("send-keys", "-t", s.pane, "C-u")
			time.Sleep(40 * time.Millisecond)
		}
	}
	clear(2*len(box.rows) + 2)
	if _, ok := waitBox(s, true, 1500*time.Millisecond, empty); !ok {
		clear(2*len(box.rows) + 4)
		if _, ok := waitBox(s, true, 1500*time.Millisecond, empty); !ok {
			return fmt.Errorf("%s tiene texto a medio escribir y no pude apartarlo; no envié nada", s.Name)
		}
	}
	if err := submitChecked(s, true, text); err != nil {
		_ = pasteInto(s, draft)
		return err
	}
	// Put the draft back once Codex has taken the message out of the box.
	waitBox(s, true, 3*time.Second, empty)
	if _, ok, err := pasteVerified(s, true, draft); err != nil || !ok {
		return fmt.Errorf("envié el mensaje a %s pero no pude devolver su borrador a la caja: %q", s.Name, draft)
	}
	return nil
}
