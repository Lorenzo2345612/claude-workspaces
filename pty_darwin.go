//go:build darwin && !terminal

package main

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY returns the master side of a new pseudo-terminal and the path of its slave.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	fd := int(m.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil { // grantpt
		m.Close()
		return nil, "", fmt.Errorf("grantpt: %w", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil { // unlockpt
		m.Close()
		return nil, "", fmt.Errorf("unlockpt: %w", err)
	}
	// ptsname: TIOCPTYGNAME writes the slave's path into a 128-byte buffer. x/sys exports no
	// generic pointer ioctl, so its winsize setter (an ioctl with a pointer argument and any
	// request) carries the buffer's address; the kernel writes the full 128 bytes into it.
	name := make([]byte, 128)
	if err := unix.IoctlSetWinsize(fd, unix.TIOCPTYGNAME, (*unix.Winsize)(unsafe.Pointer(&name[0]))); err != nil {
		m.Close()
		return nil, "", fmt.Errorf("ptsname: %w", err)
	}
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return m, string(name), nil
}
