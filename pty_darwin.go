//go:build darwin

package main

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

const (
	ioctlTIOCPTYGRANT = 0x20007454
	ioctlTIOCPTYUNLK  = 0x20007452
	ioctlTIOCPTYGNAME = 0x40807453
)

// openPTY returns the master side of a new pseudo-terminal and the path of its slave.
func openPTY() (*os.File, string, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	if err := ioctl(m.Fd(), ioctlTIOCPTYGRANT, 0); err != nil {
		m.Close()
		return nil, "", err
	}
	if err := ioctl(m.Fd(), ioctlTIOCPTYUNLK, 0); err != nil {
		m.Close()
		return nil, "", err
	}
	name := make([]byte, 128)
	if err := ioctl(m.Fd(), ioctlTIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		m.Close()
		return nil, "", err
	}
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return m, string(name), nil
}
