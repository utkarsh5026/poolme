//go:build darwin

package cpu

import (
	"runtime"
)

// SetupWorkerAffinity locks the goroutine to an OS thread.
// CPU pinning is not available on macOS.
func SetupWorkerAffinity(workerID int) func() {
	runtime.LockOSThread()

	return func() {
		runtime.UnlockOSThread()
	}
}
