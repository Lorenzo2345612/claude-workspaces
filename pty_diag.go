//go:build (linux || darwin) && !terminal

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ansiAllRe matches terminal control sequences (CSI, OSC, charset and keypad switches).
var ansiAllRe = regexp.MustCompile(`\x1b(\[[0-9;?<=>]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[()][0-9A-Za-z]|[=>78DEHMc])`)

// cmdPTYDiag checks, step by step, what the app does to open a terminal: cw app --diagnostico.
func cmdPTYDiag() error {
	adoptShellPath()
	fmt.Printf("cw %s · %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	step := func(name string, err error) bool {
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", name, err)
			return false
		}
		fmt.Printf("  ✓ %s\n", name)
		return true
	}
	m, slave, err := openPTY()
	if !step("abrir la pseudo-terminal (ptmx, grantpt, unlockpt, ptsname)", err) {
		return nil
	}
	fmt.Printf("    terminal esclava: %s\n", slave)
	m.Close()
	tmuxPath, err := exec.LookPath("tmux")
	step("encontrar tmux en el PATH", err)
	fmt.Printf("    tmux: %s\n", tmuxPath)
	run := func(label string, argv ...string) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		m, err := startInPTY(cmd, 100, 30)
		if !step(label, err) {
			return
		}
		var out bytes.Buffer
		done := make(chan struct{})
		go func() { _, _ = io.Copy(&out, m); close(done) }()
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case err := <-exited:
			if err != nil {
				fmt.Printf("    salió con: %v\n", err)
			}
		case <-time.After(4 * time.Second):
			_ = cmd.Process.Kill()
			fmt.Println("    (sigue corriendo; lo detuve)")
		}
		time.Sleep(200 * time.Millisecond)
		m.Close()
		<-done
		shown := 0
		text := ansiAllRe.ReplaceAllString(strings.ReplaceAll(out.String(), "\r", "\n"), "")
		for _, l := range strings.Split(text, "\n") {
			if l = strings.TrimSpace(l); l != "" && shown < 8 {
				fmt.Printf("    | %s\n", shorten(l, 110))
				shown++
			}
		}
	}
	run("correr un programa en la terminal (tty; stty size)", "/bin/sh", "-c", "tty; stty size; echo listo")
	if tmuxPath != "" {
		run("correr tmux en la terminal", tmuxPath, "-L", "cw-diagnostico", "new-session", "sh", "-c", "echo tmux-ok; sleep 1")
		_ = exec.Command(tmuxPath, "-L", "cw-diagnostico", "kill-server").Run()
	}
	return nil
}
