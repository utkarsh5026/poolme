//go:build linux

package cpu

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// pinToCore pins the current OS thread to a specific CPU core.
// Must be called after runtime.LockOSThread().
//
// cpuID should be in range [0, runtime.NumCPU()-1]
func pinToCore(cpuID int) (uintptr, error) {
	numCPU := runtime.NumCPU()
	if cpuID < 0 || cpuID >= numCPU {
		cpuID = cpuID % numCPU
	}

	var mask unix.CPUSet
	mask.Zero()
	mask.Set(cpuID)

	err := unix.SchedSetaffinity(0, &mask) // 0 = current thread
	if err != nil {
		return 0, err
	}

	return uintptr(cpuID), nil
}

// GetNumCPU returns the number of logical CPUs available.
func GetNumCPU() int {
	return runtime.NumCPU()
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
