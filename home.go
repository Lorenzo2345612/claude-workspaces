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
	hits              []hit
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
	h.recents = recentConversations(h.dir, 5, h.openSessionIDs(), cfg)

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
	if rows, err := h.t.Lines("list-panes", "-a", "-F", "#{@cw_sid}"); err == nil {
		for _, r := range rows {
			ids[r[0]] = true
		}
	}
	return ids
}

func (h *homeScreen) handle(k key) (bool, error) {
	action := ""
	if k.kind == keyClick {
		action = hitAt(h.hits, k.x, k.y)
	}
	switch {
	case action == "claude" || (k.kind == keyRune && (k.r == 'c' || k.r == 'C')):
		return h.dialog(nil)
	case action == "byid" || (k.kind == keyRune && (k.r == 'i' || k.r == 'I')):
		h.inDialog = true
		setRaw(false)
		err := uiResumeByID(h.ws)
		h.inDialog = false
		if kind, _ := h.t.Run("display", "-p", "-t", h.pane, "#{@cw_kind}"); kind == kindHome {
			setRaw(true) // still an empty tab (cancelled): back to the start screen
			return false, err
		}
		return true, err
	case action == "shell" || (k.kind == keyRune && (k.r == 't' || k.r == 'T')):
		setRaw(false)
		return true, addSlot(h.ws, h.session, kindShell, "shell")
	case strings.HasPrefix(action, "conv:"):
		i, _ := strconv.Atoi(action[5:])
		h.sel = i
		return h.dialog(&h.recents[i])
	case k.kind == keyRune && k.r >= '1' && k.r <= '9':
		if i := int(k.r - '1'); i < len(h.recents) {
			h.sel = i
			return h.dialog(&h.recents[i])
		}
	case (k.kind == keyUp || k.kind == keyWheelUp) && len(h.recents) > 0:
		h.sel = (h.sel - 1 + len(h.recents)) % len(h.recents)
	case (k.kind == keyDown || k.kind == keyWheelDown) && len(h.recents) > 0:
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
	var lines []lineItem
	add := func(li lineItem) { lines = append(lines, li) }

	head := sgrAccent + sgrBold + h.title + sgrReset + "   " + sgrMuted + shortPath(h.dir) + sgrReset
	add(plainLine(strings.Repeat(" ", max((blockW-visibleLen(head))/2, 0)) + head))
	add(plainLine(""))
	ca, ta := sgrAccent, sgrDim
	cards := []string{
		"   " + ca + "╭──────────────────────────╮" + sgrReset + "   " + ta + "╭──────────────────────────╮" + sgrReset,
		"   " + ca + "│" + sgrReset + "  " + sgrAccent + sgrBold + "✦  Claude" + sgrReset + "               " + ca + "│" + sgrReset +
			"   " + ta + "│" + sgrReset + "  " + sgrBold + "❯  Terminal" + sgrReset + "             " + ta + "│" + sgrReset,
		"   " + ca + "│" + sgrReset + "  " + sgrMuted + fit("sesión nueva", 24) + sgrReset + ca + "│" + sgrReset +
			"   " + ta + "│" + sgrReset + "  " + sgrMuted + fit("shell normal", 24) + sgrReset + ta + "│" + sgrReset,
		"   " + ca + "╰──────────────────────────╯" + sgrReset + "   " + ta + "╰──────────────────────────╯" + sgrReset,
	}
	for _, c := range cards { // each card is its own click target
		add(lineItem{text: c, zones: []hit{{col: 3, width: 28, id: "claude"}, {col: 34, width: 28, id: "shell"}}})
	}
	add(plainLine(""))
	if len(h.recents) > 0 {
		add(plainLine("   " + sgrMuted + "RETOMAR UNA CONVERSACIÓN" + sgrReset + "   " + sgrDim + "clic para retomarla" + sgrReset))
		for i, r := range h.recents {
			name := r.Title
			if name == "" {
				name = "(sin nombre)"
			}
			row := fmt.Sprintf(" %s %s %s «%s» ", fit(name, 16), fit(r.Alias, 10), fit(whenES(r.Modified), 10), fit(r.First, 20))
			style := sgrMuted
			if i == h.sel {
				style = sgrSel
			}
			add(lineItem{text: "  " + style + row + sgrReset, zones: []hit{{col: 2, width: visibleLen(row), id: "conv:" + strconv.Itoa(i)}}})
		}
	} else {
		add(plainLine("   " + sgrDim + "No hay conversaciones anteriores de Claude en este directorio." + sgrReset))
	}
	add(plainLine(""))
	add(buttonLine(3, btnDef{label: "↻ Retomar una conversación por su ID…", id: "byid"}))

	var s screen
	s.place(max((ht-len(lines))/2, 0)+1, left, lines)
	s.b.WriteString("\033[?25l")
	h.hits = s.hits
	s.flush(true)
}

// closeHome removes the start screen of a tab once it has a real session.
func closeHome(t Tmux, session string) {
	rows, err := t.Lines("list-panes", "-s", "-t", session, "-F", "#{pane_id}"+sep+"#{@cw_kind}")
	if err != nil {
		return
	}
	for _, r := range rows {
		if len(r) == 2 && r[1] == kindHome {
			_, _ = t.Run("kill-pane", "-t", r[0])
		}
	}
}
