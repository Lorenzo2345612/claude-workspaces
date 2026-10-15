//go:build (linux || darwin) && !terminal

package main

import (
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
		return nil, err
	}
	if err := setPTYSize(m, cols, rows); err != nil {
		m.Close()
		return nil, err
	}
	s, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, err
	}
	defer s.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}
