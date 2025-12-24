package runner

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

// SetupProfiling sets up CPU and memory profiling, returns cleanup function
func SetupProfiling(cpuProfile, memProfile string) func() {
	cleanups := make([]func(), 0, 2)

	if cpuProfile != "" {
		f, err := os.Create(cpuProfile)
		if err != nil {
			colorPrintf(Red, "Error creating CPU profile: %v\n", err)
			os.Exit(1)
		}

		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()
			colorPrintf(Red, "Error starting CPU profile: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("CPU profiling enabled, writing to: %s\n", cpuProfile)

		cleanups = append(cleanups, func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		})
	}

	if memProfile != "" {
		cleanups = append(cleanups, func() {
			f, err := os.Create(memProfile)
			if err != nil {
				colorPrintf(Red, "Error creating memory profile: %v\n", err)
				return
			}
			defer func(f *os.File) {
				err := f.Close()
				if err != nil {
					colorPrintf(Red, "Error closing memory profile file: %v\n", err)
				}
			}(f)

			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				colorPrintf(Red, "Error writing memory profile: %v\n", err)
			}
			fmt.Printf("Memory profile written to: %s\n", memProfile)
		})
	}

	return func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}
}
