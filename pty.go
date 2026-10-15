//go:build (linux || darwin) && !terminal

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// setPTYSize tells the terminal (and so the program in it) its new size. x/sys calls the libc
// ioctl: a raw syscall.Syscall(SYS_IOCTL) goes through the variadic syscall() on macOS, whose
// arguments arrive scrambled on Apple Silicon ("inappropriate ioctl for device").
func setPTYSize(m *os.File, cols, rows int) error {
	return unix.IoctlSetWinsize(int(m.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)})
}

// startInPTY runs cmd as the session leader of a new pseudo-terminal of the given size and
// returns its master side.
func startInPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	m, slavePath, err := openPTY()
	if err != nil {
		return nil, fmt.Errorf("abrir la pseudo-terminal: %w", err)
	}
	// The slave is opened before any terminal ioctl on the master: macOS refuses them
	// ("inappropriate ioctl for device") until the slave side is open; Linux does not mind.
	s, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("abrir %s: %w", slavePath, err)
	}
	defer s.Close()
	// Size it on the slave, then on the master; a refusal only leaves the default size, which
	// the window corrects with its first resize.
	if setPTYSize(s, cols, rows) != nil {
		_ = setPTYSize(m, cols, rows)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, fmt.Errorf("arrancar %s en la terminal: %w", cmd.Path, err)
	}
	return m, nil
}
