//go:build !windows

package main

// enableWindowsANSI is a no-op on Unix systems
func enableWindowsANSI() {
	// Unix terminals support ANSI escape sequences by default
}
