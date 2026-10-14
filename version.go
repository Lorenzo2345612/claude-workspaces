package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// version is set at build time: -ldflags "-X main.version=…".
var version = "dev"

const minTmuxMajor, minTmuxMinor = 3, 3

var tmuxVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// checkTmux fails with an actionable message when tmux is missing or older than 3.3
// (popups with borders, status ranges and extended keys need it).
func checkTmux() error {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return fmt.Errorf("no encuentro tmux; instálalo (macOS: brew install tmux · Ubuntu/Debian: sudo apt install tmux) o vuelve a correr el instalador de cw")
	}
	m := tmuxVersionRe.FindStringSubmatch(string(out))
	if m == nil {
		return nil // unknown format (e.g. a git build): let it try
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < minTmuxMajor || (major == minTmuxMajor && minor < minTmuxMinor) {
		return fmt.Errorf("cw necesita tmux %d.%d o mayor y tienes %s.%s; vuelve a correr el instalador de cw, que instala uno reciente",
			minTmuxMajor, minTmuxMinor, m[1], m[2])
	}
	return nil
}
