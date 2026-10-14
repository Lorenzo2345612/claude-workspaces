package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Minimal raw-terminal toolkit for the dialogs that run inside tmux popups (stdlib only).

const (
	keyRune = iota
	keyEnter
	keyTab
	keyBackTab
	keyBackspace
	keyEsc
	keyUp
	keyDown
	keyLeft
	keyRight
	keyCtrlU
	keyClick // left button pressed at (x, y)
	keyRightClick
	keyWheelUp
	keyWheelDown
	keyOther
)

type key struct {
	kind int
	r    rune
	x, y int // 1-based cell of a mouse event
}

// setRaw switches the terminal to raw input with mouse reporting (and back).
func setRaw(on bool) {
	args := []string{"-icanon", "-echo", "-isig", "min", "1"}
	if on {
		os.Stdout.WriteString("\033[?1000h\033[?1006h")
	} else {
		args = []string{"icanon", "echo", "isig"}
		os.Stdout.WriteString("\033[?1006l\033[?1000l")
	}
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	_ = c.Run()
}

// readKeys returns the keys in one read. Escape sequences arrive whole in a single read,
// so a read holding only ESC is the Esc key.
func readKeys() ([]key, error) {
	buf := make([]byte, 256)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return nil, err
	}
	b := buf[:n]
	var keys []key
	for len(b) > 0 {
		switch {
		case b[0] == 0x1b && len(b) == 1:
			keys = append(keys, key{kind: keyEsc})
			b = b[1:]
		case b[0] == 0x1b && len(b) >= 3 && b[1] == '[' && b[2] == '<':
			// SGR mouse report: ESC [ < button ; x ; y (M press | m release)
			i := 3
			for i < len(b) && b[i] != 'M' && b[i] != 'm' {
				i++
			}
			if i < len(b) {
				f := strings.Split(string(b[3:i]), ";")
				if len(f) == 3 && b[i] == 'M' {
					btn, _ := strconv.Atoi(f[0])
					x, _ := strconv.Atoi(f[1])
					y, _ := strconv.Atoi(f[2])
					switch {
					case btn == 64:
						keys = append(keys, key{kind: keyWheelUp, x: x, y: y})
					case btn == 65:
						keys = append(keys, key{kind: keyWheelDown, x: x, y: y})
					case btn&3 == 0 && btn&32 == 0: // left press, not a drag
						keys = append(keys, key{kind: keyClick, x: x, y: y})
					case btn&3 == 2 && btn&32 == 0:
						keys = append(keys, key{kind: keyRightClick, x: x, y: y})
					}
				}
			}
			b = b[min(i+1, len(b)):]
		case b[0] == 0x1b && len(b) >= 3 && (b[1] == '[' || b[1] == 'O'):
			// The key is the final byte, after any parameters: ESC [ D, ESC O D and
			// ESC [ 1 ; 2 D (arrows with modifiers, as some terminals send them) are all Left.
			i := 2
			for i < len(b) && !(b[i] >= 0x40 && b[i] <= 0x7e) {
				i++
			}
			k := keyOther
			if i < len(b) {
				if m, ok := map[byte]int{'A': keyUp, 'B': keyDown, 'C': keyRight, 'D': keyLeft, 'Z': keyBackTab}[b[i]]; ok {
					k = m
				}
			}
			keys = append(keys, key{kind: k})
			b = b[min(i+1, len(b)):]
		case b[0] == 0x1b:
			keys = append(keys, key{kind: keyEsc})
			b = b[1:]
		case b[0] == '\r' || b[0] == '\n':
			keys = append(keys, key{kind: keyEnter})
			b = b[1:]
		case b[0] == '\t':
			keys = append(keys, key{kind: keyTab})
			b = b[1:]
		case b[0] == 0x7f || b[0] == 0x08:
			keys = append(keys, key{kind: keyBackspace})
			b = b[1:]
		case b[0] == 0x15:
			keys = append(keys, key{kind: keyCtrlU})
			b = b[1:]
		case b[0] < 0x20:
			keys = append(keys, key{kind: keyOther})
			b = b[1:]
		default:
			r, size := utf8.DecodeRune(b)
			keys = append(keys, key{kind: keyRune, r: r})
			b = b[size:]
		}
	}
	return keys, nil
}

// screen accumulates one frame of output.
// screen accumulates one frame of output and the clickable regions drawn in it.
type screen struct {
	b    strings.Builder
	hits []hit
}

// hit is a clickable rectangle (one row high) and the action it stands for.
type hit struct {
	row, col, width int
	id              string
}

// btn draws text at (row, col) and makes it clickable as id.
func (s *screen) btn(row, col int, text, id string) {
	s.at(row, col, text)
	s.hits = append(s.hits, hit{row, col, visibleLen(text), id})
}

// zone makes a region clickable without drawing anything.
func (s *screen) zone(row, col, width int, id string) {
	s.hits = append(s.hits, hit{row, col, width, id})
}

// hitAt returns the id of the region under a click, or "".
func hitAt(hits []hit, x, y int) string {
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if y == h.row && x >= h.col && x < h.col+h.width {
			return h.id
		}
	}
	return ""
}

// button renders a label as a clickable-looking button.
func button(label string, primary, focused bool) string {
	switch {
	case focused:
		return sgrSel + " " + label + " " + sgrReset
	case primary:
		return sgrPrimary + " " + label + " " + sgrReset
	}
	return "\033[48;5;239m\033[38;5;252m " + label + " " + sgrReset
}

func (s *screen) at(row, col int, text string) {
	s.b.WriteString("\033[" + itoa(row) + ";" + itoa(col) + "H" + text)
}
func (s *screen) flush(clear bool) {
	out := s.b.String()
	if clear {
		out = "\033[H\033[2J" + out
	}
	os.Stdout.WriteString(out)
	s.b.Reset()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

// fit truncates (with "…") or right-pads s to exactly w display columns.
func fit(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

func visibleLen(s string) int { return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, "")) }

const (
	sgrReset = "\033[0m"
	sgrBold  = "\033[1m"
	sgrMuted = "\033[38;5;245m"
	sgrDim   = "\033[38;5;240m"
	sgrGreen = "\033[38;5;114m"
	sgrRed   = "\033[38;5;203m"
	sgrBusy  = "\033[38;5;214m"
	sgrField = "\033[48;5;238m\033[38;5;252m"
	sgrFocus = "\033[48;5;240m\033[38;5;255m"
)

func sttyOutput(args ...string) (string, error) {
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

// lineItem is one line of a centred layout and its clickable parts (columns relative to the line).
type lineItem struct {
	text  string
	zones []hit
}

func plainLine(text string) lineItem { return lineItem{text: text} }

// clickLine makes the whole line one clickable region.
func clickLine(text, id string) lineItem {
	return lineItem{text: text, zones: []hit{{col: 0, width: visibleLen(text), id: id}}}
}

// btnDef describes a button: its label, the action id, and how it looks.
type btnDef struct {
	label, id string
	primary   bool
	focused   bool
	style     string // overrides the look when set (e.g. a red "danger" button)
}

// buttonLine lays buttons out side by side, two spaces apart, after an indent.
func buttonLine(indent int, btns ...btnDef) lineItem {
	li := lineItem{text: strings.Repeat(" ", indent)}
	col := indent
	for i, b := range btns {
		if i > 0 {
			li.text += "  "
			col += 2
		}
		t := button(b.label, b.primary, b.focused)
		if b.style != "" {
			t = b.style + " " + b.label + " " + sgrReset
		}
		w := visibleLen(t)
		li.zones = append(li.zones, hit{col: col, width: w, id: b.id})
		li.text += t
		col += w
	}
	return li
}

// place draws layout lines at (top, left) and registers their clickable zones.
func (s *screen) place(top, left int, lines []lineItem) {
	for i, li := range lines {
		s.at(top+i, left, li.text)
		for _, z := range li.zones {
			s.zone(top+i, left+z.col, z.width, z.id)
		}
	}
}

const sgrDanger = "\033[48;5;160m\033[38;5;255m\033[1m"
