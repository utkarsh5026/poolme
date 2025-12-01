//go:build debug

package pool

import (
	"fmt"
	"log"
	"os"
)

var debugLogger = log.New(os.Stderr, "[POOLME DEBUG] ", log.Ltime|log.Lmicroseconds|log.Lshortfile)

// debugLog logs debug messages when built with -tags debug
func debugLog(format string, args ...interface{}) {
	debugLogger.Output(2, fmt.Sprintf(format, args...))
}
