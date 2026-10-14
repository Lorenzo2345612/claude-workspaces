package main

import (
	"os"
	"os/exec"
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
	keyOther
)

type key struct {
	kind int
	r    rune
}

func setRaw(on bool) {
	args := []string{"-icanon", "-echo", "-isig", "min", "1"}
	if !on {
		args = []string{"icanon", "echo", "isig"}
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
type screen struct{ b strings.Builder }

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
	sgrReset  = "\033[0m"
	sgrAccent = "\033[38;5;75m"
	sgrBold   = "\033[1m"
	sgrMuted  = "\033[38;5;245m"
	sgrDim    = "\033[38;5;240m"
	sgrGreen  = "\033[38;5;114m"
	sgrRed    = "\033[38;5;203m"
	sgrBusy   = "\033[38;5;214m"
	sgrField  = "\033[48;5;238m\033[38;5;252m"
	sgrFocus  = "\033[48;5;240m\033[38;5;255m"
	sgrSel    = "\033[48;5;31m\033[38;5;255m\033[1m"
)

func sttyOutput(args ...string) (string, error) {
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}
