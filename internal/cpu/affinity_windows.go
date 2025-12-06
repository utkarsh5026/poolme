//go:build windows

package cpu

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	setThreadAffinityMask = kernel32.NewProc("SetThreadAffinityMask")
	getCurrentThread      = kernel32.NewProc("GetCurrentThread")
)

// pinToCore pins the current OS thread to a specific CPU core.
// Must be called after runtime.LockOSThread().
//
// cpuID should be in range [0, runtime.NumCPU()-1]
// Returns the previous affinity mask on success.
func pinToCore(cpuID int) (uintptr, error) {
	numCPU := runtime.NumCPU()
	if cpuID < 0 || cpuID >= numCPU {
		cpuID = cpuID % numCPU
	}

	handle, _, _ := getCurrentThread.Call()

	// Create affinity mask for the specific CPU
	// Bit N = CPU N, so for CPU 0 it's 1, for CPU 1 it's 2, etc.
	mask := uintptr(1 << cpuID)

	// Set the affinity
	prevMask, _, err := setThreadAffinityMask.Call(handle, mask)
	if prevMask == 0 {
		return 0, err
	}

	return prevMask, nil
}

// SetupWorkerAffinity is a convenience function that locks the goroutine
// to an OS thread and pins it to a specific CPU core.
// Returns a cleanup function that should be deferred.
func SetupWorkerAffinity(workerID int) func() {
	runtime.LockOSThread()
	_, _ = pinToCore(workerID)

	return func() {
		runtime.UnlockOSThread()
	}
}

// Ensure the unsafe import is used (for documentation/future use)
var _ = unsafe.Sizeof(0)
