package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	windowCurrent    = "current"
	windowMaximized  = "maximized"
	windowFullscreen = "fullscreen"
)

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// openOwnWindow opens the workspace in a new terminal window of its own, maximized or full screen,
// and reports whether it did. Today that is Windows Terminal under WSL; elsewhere (native Linux,
// macOS) cw keeps using the terminal it was started from.
func openOwnWindow(ws, dir string, cfg Config) (bool, error) {
	if cfg.Window == windowCurrent || os.Getenv("WSL_DISTRO_NAME") == "" {
		return false, nil
	}
	wt, err := exec.LookPath("wt.exe")
	if err != nil {
		return false, nil
	}
	abs, err := expandPath(dir)
	if err != nil {
		return false, err
	}
	bin, err := os.Executable()
	if err != nil {
		return false, err
	}
	size := "--maximized"
	if cfg.Window == windowFullscreen {
		size = "--fullscreen"
	}
	// A login + interactive bash loads the user's PATH (claude lives in ~/.local/bin). If cw fails,
	// the window stays open on the error instead of vanishing. No ';' anywhere: wt.exe splits on it.
	script := fmt.Sprintf(`%s start --here %s %s || read -r -p 'Enter para cerrar' _`,
		shQuote(bin), shQuote(ws), shQuote(abs))
	cmd := exec.Command(wt, "-w", "new", size, "new-tab", "--title", "Agent Workspaces terminal · "+ws, "--useApplicationTitle",
		"wsl.exe", "-d", os.Getenv("WSL_DISTRO_NAME"), "--", "bash", "-lic", script)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("no pude abrir Windows Terminal: %w", err)
	}
	_ = cmd.Process.Release()
	fmt.Printf("abriendo %s en una ventana nueva (%s)\n", ws, cfg.Window)
	return true, nil
}
