package io

import (
	"flag"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

var (
	latencyFlag *int
	cpuWorkFlag *int
)

// NewBenchmarkFramework creates and returns a configured I/O benchmark framework
func NewBenchmarkFramework() *runner.BenchmarkFramework[IOTask, IOTaskResult] {
	latencyFlag = flag.Int("latency", 50, "Average I/O latency in milliseconds (default: 50ms)")
	cpuWorkFlag = flag.Int("cpuwork", 1000, "CPU work iterations for mixed workload (default: 1000)")

	return &runner.BenchmarkFramework[IOTask, IOTaskResult]{
		Name:          "I/O Scheduler Benchmark",
		AllStrategies: runner.AllStrategies,

		GenerateTasks: func(workload string, count int) []IOTask {
			switch workload {
			case "api":
				return generateAPITasks(count, *latencyFlag)
			case "database":
				return generateDatabaseTasks(count, *latencyFlag)
			case "file":
				return generateFileIOTasks(count, *latencyFlag)
			case "mixed":
				return generateMixedTasks(count, *latencyFlag, *cpuWorkFlag)
			default: // "api"
				return generateAPITasks(count, *latencyFlag)
			}
		},

		NewRunner: func(strategy string, tasks []IOTask, workers int) runner.BenchmarkRunner[IOTask, IOTaskResult] {
			return &IORunner{
				strategy:   strategy,
				tasks:      tasks,
				numWorkers: workers,
			}
		},

		PrintConfig:    printConfiguration,
		PrintResults:   printResults,
		CalculateStats: runner.DefaultCalculateStats,
	}
}
