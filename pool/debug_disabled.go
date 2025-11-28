//go:build !debug

package pool

// debugLog is a no-op when not built with -tags debug (zero runtime cost)
func debugLog(format string, args ...interface{}) {}
