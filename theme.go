package main

import "strconv"

// theme is a colour scheme, in the terminal's 256-colour palette.
type theme struct {
	label   string
	accent  int // current tab, its underline, popup and menu borders, titles
	current int // background of the current session and of selected items
	primary int // background of primary buttons
}

// themes, in the order the configuration menu cycles through them.
var themeOrder = []string{"azul", "morado", "verde", "naranja", "rosa", "gris"}

var themes = map[string]theme{
	"azul":    {"Azul", 75, 31, 24},
	"morado":  {"Morado", 141, 98, 54},
	"verde":   {"Verde", 114, 29, 22},
	"naranja": {"Naranja", 215, 166, 130},
	"rosa":    {"Rosa", 211, 168, 125},
	"gris":    {"Gris", 250, 242, 238},
}

// Colours used by the bar (tmux names) and the dialogs (ANSI sequences); set by applyTheme.
var (
	colAccent, colPillOn          string
	sgrAccent, sgrSel, sgrPrimary string
)

func init() { applyTheme("azul") }

func applyTheme(name string) {
	t, ok := themes[name]
	if !ok {
		t = themes["azul"]
	}
	colAccent = "colour" + strconv.Itoa(t.accent)
	colPillOn = "colour" + strconv.Itoa(t.current)
	sgrAccent = "\033[38;5;" + strconv.Itoa(t.accent) + "m"
	sgrSel = "\033[48;5;" + strconv.Itoa(t.current) + "m\033[38;5;255m\033[1m"
	sgrPrimary = "\033[48;5;" + strconv.Itoa(t.primary) + "m\033[38;5;255m\033[1m"
}

func nextTheme(name string) string {
	for i, n := range themeOrder {
		if n == name {
			return themeOrder[(i+1)%len(themeOrder)]
		}
	}
	return themeOrder[1]
}

func themeLabel(name string) string {
	if t, ok := themes[name]; ok {
		return t.label
	}
	return themes["azul"].label
}
