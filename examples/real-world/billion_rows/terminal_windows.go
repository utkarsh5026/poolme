//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableWindowsANSI enables ANSI escape sequence support on Windows
func enableWindowsANSI() {
	// Enable virtual terminal processing for Windows 10+
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	var mode uint32
	handle := syscall.Handle(os.Stdout.Fd())

	// Get current console mode
	syscall.Syscall(kernel32.NewProc("GetConsoleMode").Addr(), 2, uintptr(handle), uintptr(unsafe.Pointer(&mode)), 0)

	// Enable ENABLE_VIRTUAL_TERMINAL_PROCESSING (0x0004)
	mode |= 0x0004

	// Set the new console mode
	setConsoleMode.Call(uintptr(handle), uintptr(mode))
}
