package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
)

// FormatNumber formats an integer with comma separators
func FormatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			_, _ = result.WriteString(",")
		}
		_, _ = result.WriteString(string(c))
	}
	return result.String()
}

// FormatLatency formats a duration in the most appropriate unit
func FormatLatency(d time.Duration) string {
	if d == 0 {
		return "0"
	}

	ns := d.Nanoseconds()

	if ns < 1000 {
		return fmt.Sprintf("%dns", ns)
	}

	if ns < 1_000_000 {
		us := float64(ns) / 1000.0
		if us == float64(int(us)) {
			return fmt.Sprintf("%dµs", int(us))
		}
		return fmt.Sprintf("%.1fµs", us)
	}

	if ns < 1_000_000_000 {
		ms := float64(ns) / 1_000_000.0
		if ms == float64(int(ms)) {
			return fmt.Sprintf("%dms", int(ms))
		}
		return fmt.Sprintf("%.2fms", ms)
	}

	s := float64(ns) / 1_000_000_000.0
	return fmt.Sprintf("%.2fs", s)
}

// EnableWindowsANSI is a no-op for the runner (colors work fine without it)
func EnableWindowsANSI() {
	// No-op - colors work fine in most terminals
}

func colorPrintLn(c *color.Color, a ...any) {
	_, _ = c.Println(a...)
}

func colorPrintf(c *color.Color, format string, a ...any) {
	_, _ = c.Printf(format, a...)
}
