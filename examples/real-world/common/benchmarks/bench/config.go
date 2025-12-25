package bench

import (
	"fmt"
	"runtime"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

// ConfigPrinter provides a framework for printing benchmark configuration.
// It handles common configuration output while allowing benchmark-specific customization.
type ConfigPrinter struct {
	Title        string
	NumWorkers   int
	NumTasks     int
	Workload     string
	CustomParams map[string]string
	WorkloadDesc map[string]string
}

// Print outputs the benchmark configuration in a standardized format.
func (cp *ConfigPrinter) Print() {
	_, _ = bold.Println(fmt.Sprintf("⚙️  %s", cp.Title))
	fmt.Printf("  Workers:    %d (using %d CPU cores)\n", cp.NumWorkers, runtime.NumCPU())
	fmt.Printf("  Tasks:      %s concurrent tasks\n", runner.FormatNumber(cp.NumTasks))

	for key, value := range cp.CustomParams {
		fmt.Printf("  %s: %s\n", key, value)
	}

	fmt.Printf("  Workload:   %s\n", cp.Workload)
	fmt.Printf("  Strategies: 7 schedulers\n")
	fmt.Println()

	_, _ = bold.Println("📊 Workload Distribution:")
	if desc, ok := cp.WorkloadDesc[cp.Workload]; ok {
		fmt.Println(desc)
	}
	fmt.Println()
}
