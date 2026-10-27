package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// kindDoc marks the read-only Markdown window that cw open adds to a tab of the terminal
// version. Like the start screen it is not a session: it is never saved and holds no conversation.
const kindDoc = "doc"

// notSession reports whether a pane kind is one of cw's own screens rather than a session.
func notSession(kind string) bool { return kind == kindHome || kind == kindDoc }

const sgrCode = "\033[38;5;180m"

type mdSpan struct{ text, sgr string }

// mdLine is one logical line of a rendered document: lead goes before its first row and indent
// before every wrapped row; a rule fills the width.
type mdLine struct {
	lead, indent mdSpan
	spans        []mdSpan
	rule         bool
}

var (
	mdInline  = regexp.MustCompile("`[^`]+`|\\*\\*[^*]+\\*\\*|\\[[^\\]]+\\]\\([^)\\s]+\\)")
	mdLink    = regexp.MustCompile(`^\[([^\]]+)\]\(([^)\s]+)\)$`)
	mdFence   = regexp.MustCompile("^\\s*(```+|~~~+)")
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	mdItem    = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.+)$`)
	mdQuote   = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdRule    = regexp.MustCompile(`^\s*([-*_])(?:\s*[-*_]){2,}\s*$`)
)

// cleanTerminalText drops control characters, so a document can never move the cursor, retitle
// the terminal or slip escape sequences through the viewer.
func cleanTerminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\t", "    "))
}

func mdInlineSpans(text, base string) []mdSpan {
	var out []mdSpan
	last := 0
	for _, m := range mdInline.FindAllStringIndex(text, -1) {
		if m[0] > last {
			out = append(out, mdSpan{text[last:m[0]], base})
		}
		part := text[m[0]:m[1]]
		switch {
		case strings.HasPrefix(part, "`"):
			out = append(out, mdSpan{part[1 : len(part)-1], sgrCode})
		case strings.HasPrefix(part, "**"):
			out = append(out, mdSpan{part[2 : len(part)-2], base + sgrBold})
		default:
			l := mdLink.FindStringSubmatch(part)
			out = append(out, mdSpan{l[1], base + "\033[4m"}, mdSpan{" (" + l[2] + ")", sgrMuted})
		}
		last = m[1]
	}
	if last < len(text) {
		out = append(out, mdSpan{text[last:], base})
	}
	return out
}

// markdownLines renders the same subset as the app's viewer: headings, lists, bold, inline
// code, code blocks, quotes, rules and links.
func markdownLines(text string) []mdLine {
	var out []mdLine
	fence := ""
	for _, line := range strings.Split(cleanTerminalText(text), "\n") {
		if fence != "" {
			if strings.HasPrefix(strings.TrimSpace(line), fence) {
				fence = ""
				continue
			}
			pad := mdSpan{"  ", ""}
			out = append(out, mdLine{lead: pad, indent: pad, spans: []mdSpan{{line, sgrCode}}})
			continue
		}
		if m := mdFence.FindStringSubmatch(line); m != nil {
			fence = m[1]
			continue
		}
		if m := mdHeading.FindStringSubmatch(line); m != nil {
			style := sgrBold
			if len(m[1]) <= 2 {
				style = sgrAccent + sgrBold
			}
			out = append(out, mdLine{spans: mdInlineSpans(m[2], style)})
			continue
		}
		if m := mdItem.FindStringSubmatch(line); m != nil {
			marker := "•"
			if m[2][0] >= '0' && m[2][0] <= '9' {
				marker = m[2]
			}
			lead := strings.Repeat("  ", len(m[1])/2+1) + marker + " "
			out = append(out, mdLine{lead: mdSpan{lead, sgrAccent},
				indent: mdSpan{strings.Repeat(" ", utf8.RuneCountInString(lead)), ""}, spans: mdInlineSpans(m[3], "")})
			continue
		}
		if mdRule.MatchString(line) {
			out = append(out, mdLine{rule: true})
			continue
		}
		if m := mdQuote.FindStringSubmatch(line); m != nil {
			bar := mdSpan{"│ ", sgrDim}
			out = append(out, mdLine{lead: bar, indent: bar, spans: mdInlineSpans(m[1], sgrMuted)})
			continue
		}
		out = append(out, mdLine{spans: mdInlineSpans(strings.TrimRight(line, " "), "")})
	}
	return out
}

// rows wraps a line to width columns, breaking at a space when one is near the end.
func (l mdLine) rows(width int) []string {
	if l.rule {
		return []string{sgrDim + strings.Repeat("─", max(1, width)) + sgrReset}
	}
	type cell struct {
		r   rune
		sgr string
	}
	var cells []cell
	for _, s := range l.spans {
		for _, r := range s.text {
			cells = append(cells, cell{r, s.sgr})
		}
	}
	avail := max(10, width-utf8.RuneCountInString(l.lead.text))
	var rows []string
	for first := true; first || len(cells) > 0; first = false {
		n, skip := len(cells), 0
		if n > avail {
			n = avail
			for i := avail; i > avail/2; i-- {
				if cells[i].r == ' ' {
					n, skip = i, 1
					break
				}
			}
		}
		lead := l.indent
		if first {
			lead = l.lead
		}
		var b strings.Builder
		b.WriteString(lead.sgr + lead.text + sgrReset)
		current := ""
		for _, c := range cells[:n] {
			if c.sgr != current {
				b.WriteString(sgrReset + c.sgr)
				current = c.sgr
			}
			b.WriteRune(c.r)
		}
		b.WriteString(sgrReset)
		rows = append(rows, b.String())
		cells = cells[n+skip:]
	}
	return rows
}

// cmdMDView shows a Markdown file read-only, in the window cw open made for it: _mdview <path>.
// It is part of cw itself, so the viewer arrives with every install and update.
func cmdMDView(args []string) error {
	if len(args) != 1 {
		return errors.New("uso: _mdview <archivo.md>")
	}
	path := args[0]
	defer keepTabOpenFromPane()
	text, err := readMarkdown(path)
	if err != nil {
		fmt.Printf("\n  No pude abrir %s:\n  %s\n\n  Pulsa Enter para cerrar.", cleanTerminalText(path), cleanTerminalText(err.Error()))
		_, _ = stdin.ReadString('\n')
		return nil
	}
	lines := markdownLines(text)
	title := cleanTerminalText(filepath.Base(path))
	os.Stdout.WriteString("\033[?1049h\033[?25l")
	setRaw(true)
	defer func() {
		setRaw(false)
		os.Stdout.WriteString("\033[?25h\033[?1049l")
	}()
	keys := make(chan []key)
	go func() {
		for {
			k, err := readKeys()
			if err != nil {
				close(keys)
				return
			}
			keys <- k
		}
	}()
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	cols, height := termSize()
	var rows []string
	layout := func() {
		rows = rows[:0]
		for _, l := range lines {
			rows = append(rows, l.rows(cols-1)...)
		}
	}
	layout()
	top := 0
	for {
		page := max(1, height-1)
		top = min(max(0, top), max(0, len(rows)-page))
		var b strings.Builder
		b.WriteString("\033[H")
		for i := 0; i < page; i++ {
			if top+i < len(rows) {
				b.WriteString(rows[top+i])
			}
			b.WriteString("\033[K\r\n")
		}
		position := "todo"
		if len(rows) > page {
			position = itoa(min(100, (top+page)*100/len(rows))) + "%"
		}
		status := " " + title + " · ↑↓ PgUp PgDn desplazar · q cerrar · " + position + " "
		b.WriteString(sgrSel + fit(status, cols) + sgrReset + "\033[K")
		os.Stdout.WriteString(b.String())
		select {
		case <-resized:
			cols, height = termSize()
			layout()
		case ks, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range ks {
				switch {
				case k.kind == keyEsc || k.kind == keyRune && (k.r == 'q' || k.r == 'Q'):
					return nil
				case k.kind == keyDown || k.kind == keyEnter || k.kind == keyRune && k.r == 'j':
					top++
				case k.kind == keyUp || k.kind == keyRune && k.r == 'k':
					top--
				case k.kind == keyWheelDown:
					top += 3
				case k.kind == keyWheelUp:
					top -= 3
				case k.kind == keyPgDn || k.kind == keyRune && (k.r == ' ' || k.r == 'f'):
					top += page
				case k.kind == keyPgUp || k.kind == keyRune && k.r == 'b':
					top -= page
				case k.kind == keyHome || k.kind == keyRune && k.r == 'g':
					top = 0
				case k.kind == keyEnd || k.kind == keyRune && k.r == 'G':
					top = len(rows)
				}
			}
		}
	}
}

// openTerminalDocument opens a Markdown file in a window of its own in the tab, next to its
// sessions; opening the same file again brings its window back.
func openTerminalDocument(m mcpTarget, path string) error {
	if m.tab == nil || m.tab.session == "" {
		return errors.New("no encontré la pestaña de destino")
	}
	t := m.tmux()
	rows, err := t.Lines("list-panes", "-s", "-t", m.tab.session, "-F", "#{window_id}"+sep+"#{@cw_kind}"+sep+"#{@cw_doc}")
	if err != nil {
		return err
	}
	for _, r := range rows {
		if len(r) == 3 && r[1] == kindDoc && r[2] == path {
			_, err := t.Run("select-window", "-t", r[0])
			return err
		}
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	win, err := t.createWindow(windowSpec{session: m.tab.session, name: filepath.Base(path),
		slot: Slot{ID: newUUID(), Kind: kindDoc, Cwd: filepath.Dir(path)}, argv: []string{bin, "_mdview", path}})
	if err != nil {
		return err
	}
	_, err = t.Run("set", "-p", "-t", win, "@cw_doc", path, ";", "select-window", "-t", win)
	return err
}

// keepTabOpen gives a tab its start screen when window is about to close and no other view of
// sessions is left: documents do not count, and a tab with only documents would vanish with them.
func keepTabOpen(t Tmux, window string) error {
	session, err := t.Run("display", "-p", "-t", window, "#{session_name}")
	if err != nil {
		return err
	}
	rows, err := t.Lines("list-panes", "-s", "-t", session, "-F", "#{window_id}"+sep+"#{@cw_kind}")
	if err != nil {
		return err
	}
	for _, r := range rows {
		if len(r) == 2 && r[0] != window && r[1] != kindDoc {
			return nil
		}
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	home := Slot{ID: newUUID(), Kind: kindHome, Cwd: tabDir(t, session)}
	_, err = t.createWindow(windowSpec{session: session, name: "inicio", slot: home, argv: []string{bin, "_home", t.WS}})
	return err
}

func keepTabOpenFromPane() {
	ws, pane := os.Getenv("CW_WS"), os.Getenv("TMUX_PANE")
	if ws == "" || pane == "" || validWorkspaceName(ws) != nil {
		return
	}
	t := Tmux{WS: ws}
	if window, err := t.Run("display", "-p", "-t", pane, "#{window_id}"); err == nil {
		_ = keepTabOpen(t, window)
	}
}
