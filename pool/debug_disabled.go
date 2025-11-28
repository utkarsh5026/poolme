//go:build !debug

package pool

// debugLog is a no-op when not built with -tags debug
func debugLog(format string, args ...any) {}

var _ = debugLog
