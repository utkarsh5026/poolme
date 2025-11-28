//go:build !debug

package scheduler

// debugLog is a no-op when not built with -tags debug
func debugLog(format string, args ...any) {}
