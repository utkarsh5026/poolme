package cpu

import (
	"flag"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

var complexityFlag *int

// NewBenchmarkFramework creates and returns a configured CPU benchmark framework
func NewBenchmarkFramework() *runner.BenchmarkFramework[Task, TaskResult] {
	complexityFlag = flag.Int("complexity", 10_000, "Base CPU work per task in iterations (default: 10,000)")

	return &runner.BenchmarkFramework[Task, TaskResult]{
		Name:          "CPU-Bound Scheduler Benchmark",
		AllStrategies: runner.AllStrategies,

		GenerateTasks: func(workload string, count int) []Task {
			switch workload {
			case "priority":
				return generatePriorityTasks(count, *complexityFlag)
			case "imbalanced":
				return generateImbalancedTasks(count, *complexityFlag)
			default: // "balanced"
				return generateBalancedTasks(count, *complexityFlag)
			}
		},

		NewRunner: func(strategy string, tasks []Task, workers int) runner.BenchmarkRunner[Task, TaskResult] {
			return &CPURunner{
				strategy:   strategy,
				tasks:      tasks,
				numWorkers: workers,
			}
		},

		PrintConfig:    printConfiguration,
		PrintResults:   printResults,
		CalculateStats: runner.CalculateStatsWithLatencyAveraging,
	}
}
