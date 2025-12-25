//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	enableVirtualTerminalProcessing = 0x0004
	stdOutputHandle                 = uint32(4294967285) // -11 as uint32
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetStdHandle   = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func enableWindowsANSI() {
	stdout, _, _ := procGetStdHandle.Call(uintptr(stdOutputHandle))

	var mode uint32
	procGetConsoleMode.Call(stdout, uintptr(unsafe.Pointer(&mode)))

	mode |= enableVirtualTerminalProcessing
	procSetConsoleMode.Call(stdout, uintptr(mode))
}
