//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

// setPTYSize tells the terminal (and so the program in it) its new size.
func setPTYSize(m *os.File, cols, rows int) error {
	ws := struct{ rows, cols, x, y uint16 }{uint16(rows), uint16(cols), 0, 0}
	return ioctl(m.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
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
