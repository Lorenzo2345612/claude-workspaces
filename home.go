package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ansiRe = regexp.MustCompile("\033\\[[0-9;?]*[A-Za-z]")

// kindHome marks the placeholder window of a tab without sessions. It is never saved.
const kindHome = "home"

// homeScreen is the start screen of an empty tab: start Claude or a shell, or resume one of the
// latest Claude conversations held for the tab's directory.
type homeScreen struct {
	t                 Tmux
	ws, pane, session string
	title, dir        string
	recents           []recentConv
	sel               int
	inDialog          bool // the new-session form owns the screen
}

func cmdHome(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: _home <ws>")
	}
	h := &homeScreen{t: Tmux{WS: args[0]}, ws: args[0], pane: os.Getenv("TMUX_PANE")}
	var err error
	if h.session, err = h.t.Run("display", "-p", "-t", h.pane, "#{session_name}"); err != nil {
		return err
	}
	for i := 0; i < 20 && h.dir == ""; i++ { // wait for openTab to set the tab's options
		h.dir, _ = h.t.Run("show", "-t", h.session, "-qv", "@cw_dir")
		if h.dir == "" {
			time.Sleep(50 * time.Millisecond)
		}
	}
	cfg, _ := loadConfig()
	h.recents = recentConversations(h.dir, 5, h.openSessionIDs(), cfg.claudeAliases())

	// Keys are read on this goroutine only: the new-session form reads the same terminal while it
	// is open, so a background reader would steal its keystrokes. Resizes redraw from a goroutine.
	var mu sync.Mutex
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			mu.Lock()
			h.draw()
			mu.Unlock()
		}
	}()
	setRaw(true)
	defer setRaw(false)
	mu.Lock()
	h.draw()
	mu.Unlock()
	for {
		ks, err := readKeys()
		if err != nil {
			return nil
		}
		for _, k := range ks {
			if done, err := h.handle(k); done {
				return err
			}
		}
		mu.Lock()
		h.draw()
		mu.Unlock()
	}
}

// openSessionIDs lists the Claude conversations already open in this workspace.
func (h *homeScreen) openSessionIDs() map[string]bool {
	ids := map[string]bool{}
	if rows, err := h.t.Lines("list-windows", "-a", "-F", "#{@cw_sid}"); err == nil {
		for _, r := range rows {
			ids[r[0]] = true
		}
	}
	return ids
}

func (h *homeScreen) handle(k key) (bool, error) {
	switch {
	case k.kind == keyRune && (k.r == 'c' || k.r == 'C'):
		return h.dialog(nil)
	case k.kind == keyRune && (k.r == 't' || k.r == 'T'):
		setRaw(false)
		return true, addSlot(h.ws, h.session, kindShell, "shell")
	case k.kind == keyRune && k.r >= '1' && k.r <= '9':
		if i := int(k.r - '1'); i < len(h.recents) {
			h.sel = i
			return h.dialog(&h.recents[i])
		}
	case k.kind == keyUp && len(h.recents) > 0:
		h.sel = (h.sel - 1 + len(h.recents)) % len(h.recents)
	case k.kind == keyDown && len(h.recents) > 0:
		h.sel = (h.sel + 1) % len(h.recents)
	case k.kind == keyEnter && len(h.recents) > 0:
		return h.dialog(&h.recents[h.sel])
	}
	return false, nil
}

// dialog runs the new-session form in this pane; cancelling it comes back to the start screen.
func (h *homeScreen) dialog(resume *recentConv) (bool, error) {
	h.inDialog = true
	setRaw(false)
	err := newClaudeDialog(h.ws, h.session, resume)
	h.inDialog = false
	if err == errCancelled {
		setRaw(true)
		return false, nil
	}
	return true, err
}

func (h *homeScreen) draw() {
	if h.inDialog {
		return
	}
	size, _ := h.t.Run("display", "-p", "-t", h.pane, "#{pane_width} #{pane_height}")
	w, ht := 80, 24
	if f := strings.Fields(size); len(f) == 2 {
		w, _ = strconv.Atoi(f[0])
		ht, _ = strconv.Atoi(f[1])
	}
	// The tab's options are set right after this window is created: read them on every draw.
	h.title, _ = h.t.Run("show", "-t", h.session, "-qv", "@cw_title")
	h.dir, _ = h.t.Run("show", "-t", h.session, "-qv", "@cw_dir")
	const blockW = 64
	left := max((w-blockW)/2, 1) + 1
	var lines []string
	add := func(s string) { lines = append(lines, s) }

	head := sgrAccent + sgrBold + h.title + sgrReset + "   " + sgrMuted + shortPath(h.dir) + sgrReset
	add(strings.Repeat(" ", max((blockW-visibleLen(head))/2, 0)) + head)
	add("")
	card := func(on bool) (string, string) {
		if on {
			return sgrAccent, sgrReset
		}
		return sgrDim, sgrReset
	}
	ca, cr := card(true)
	ta, tr := card(false)
	add("   " + ca + "╭──────────────────────────╮" + cr + "   " + ta + "╭──────────────────────────╮" + tr)
	add("   " + ca + "│" + cr + "  " + sgrAccent + sgrBold + "✦  Claude" + sgrReset + "          " + sgrMuted + "(c)" + sgrReset + "  " + ca + "│" + cr +
		"   " + ta + "│" + tr + "  " + sgrBold + "❯  Terminal" + sgrReset + "        " + sgrMuted + "(t)" + sgrReset + "  " + ta + "│" + tr)
	add("   " + ca + "│" + cr + "  " + sgrMuted + fit("sesión nueva", 24) + sgrReset + ca + "│" + cr +
		"   " + ta + "│" + tr + "  " + sgrMuted + fit("shell normal", 24) + sgrReset + ta + "│" + tr)
	add("   " + ca + "╰──────────────────────────╯" + cr + "   " + ta + "╰──────────────────────────╯" + tr)
	add("")
	if len(h.recents) > 0 {
		add("   " + sgrMuted + "RETOMAR UNA CONVERSACIÓN" + sgrReset + "   " + sgrDim + "↑↓ y Enter, o su número" + sgrReset)
		for i, r := range h.recents {
			name := r.Title
			if name == "" {
				name = "(sin nombre)"
			}
			row := fmt.Sprintf(" %d  %s %s %s «%s» ", i+1, fit(name, 14), fit(r.Alias, 10), fit(whenES(r.Modified), 10), fit(r.First, 20))
			if i == h.sel {
				add("  " + sgrSel + row + sgrReset)
			} else {
				add("  " + sgrMuted + row + sgrReset)
			}
		}
	} else {
		add("   " + sgrDim + "No hay conversaciones anteriores de Claude en este directorio." + sgrReset)
	}
	add("")
	add("")

	var s screen
	top := max((ht-len(lines))/2, 0) + 1
	for i, l := range lines {
		s.at(top+i, left, l)
	}
	s.b.WriteString("\033[?25l")
	s.flush(true)
}

// closeHome removes the start screen of a tab once it has a real session.
func closeHome(t Tmux, session string) {
	rows, err := t.Lines("list-windows", "-t", session, "-F", "#{window_id}"+sep+"#{@cw_kind}")
	if err != nil {
		return
	}
	for _, r := range rows {
		if len(r) == 2 && r[1] == kindHome {
			_, _ = t.Run("kill-window", "-t", r[0])
		}
	}
}
