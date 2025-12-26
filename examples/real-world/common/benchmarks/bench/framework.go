package bench

import (
	"flag"
	"sort"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

var cpuComplexityFlag *int

// NewCPUBenchmarkFramework creates and returns a configured CPU benchmark framework
func NewCPUBenchmarkFramework() *runner.BenchmarkFramework[CPUTask, CPUTaskResult] {
	cpuComplexityFlag = flag.Int("complexity", 10_000, "Base CPU work per task in iterations (default: 10,000)")

	return &runner.BenchmarkFramework[CPUTask, CPUTaskResult]{
		Name:          "CPU-Bound Scheduler Benchmark",
		AllStrategies: runner.AllStrategies,

		GenerateTasks: func(workload string, count int) []CPUTask {
			switch workload {
			case WorkloadPriority:
				return generatePriorityCPUTasks(count, *cpuComplexityFlag)
			case WorkloadImbalanced:
				return generateImbalancedCPUTasks(count, *cpuComplexityFlag)
			default: // WorkloadBalanced
				return generateBalancedCPUTasks(count, *cpuComplexityFlag)
			}
		},

		NewRunner: func(strategy string, tasks []CPUTask, workers int) runner.BenchmarkRunner[CPUTask, CPUTaskResult] {
			return &CPURunner{
				strategy:   strategy,
				tasks:      tasks,
				numWorkers: workers,
			}
		},

		PrintConfig: func(workers int, taskCount int, workload string) {
			printCPUConfiguration(workers, taskCount, workload, *cpuComplexityFlag)
		},
		PrintResults:   printCPUResults,
		CalculateStats: runner.CalculateStatsWithLatencyAveraging,
	}
}

var (
	ioLatencyFlag *int
	ioCPUWorkFlag *int
)

// NewIOBenchmarkFramework creates and returns a configured I/O benchmark framework
func NewIOBenchmarkFramework() *runner.BenchmarkFramework[IOTask, IOTaskResult] {
	ioLatencyFlag = flag.Int("latency", 50, "Average I/O latency in milliseconds (default: 50ms)")
	ioCPUWorkFlag = flag.Int("cpuwork", 1000, "CPU work iterations for mixed workload (default: 1000)")

	return &runner.BenchmarkFramework[IOTask, IOTaskResult]{
		Name:          "I/O Scheduler Benchmark",
		AllStrategies: runner.AllStrategies,

		GenerateTasks: func(workload string, count int) []IOTask {
			switch workload {
			case WorkloadAPI:
				return generateAPITasks(count, *ioLatencyFlag)
			case WorkloadDatabase:
				return generateDatabaseTasks(count, *ioLatencyFlag)
			case WorkloadFile:
				return generateFileIOTasks(count, *ioLatencyFlag)
			case WorkloadMixed:
				return generateMixedTasks(count, *ioLatencyFlag, *ioCPUWorkFlag)
			default: // WorkloadAPI
				return generateAPITasks(count, *ioLatencyFlag)
			}
		},

		NewRunner: func(strategy string, tasks []IOTask, workers int) runner.BenchmarkRunner[IOTask, IOTaskResult] {
			return &IORunner{
				strategy:   strategy,
				tasks:      tasks,
				numWorkers: workers,
			}
		},

		PrintConfig: func(workers int, taskCount int, workload string) {
			printIOConfiguration(workers, taskCount, workload, *ioLatencyFlag)
		},
		PrintResults:   printIOResults,
		CalculateStats: runner.DefaultCalculateStats,
	}
}

var (
	pipelineRecordSizeFlag   *int
	pipelineReadLatencyFlag  *int
	pipelineWriteLatencyFlag *int
	pipelineCPUWorkFlag      *int
)

// calculatePipelineStats calculates statistics using median-based approach (specific to pipeline)
func calculatePipelineStats(strategyName string, results []runner.StrategyResult) runner.StrategyResult {
	if len(results) == 0 {
		return runner.StrategyResult{Name: strategyName}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	medianIdx := len(results) / 2
	return results[medianIdx]
}

// NewPipelineBenchmarkFramework creates and returns a configured Pipeline benchmark framework
func NewPipelineBenchmarkFramework() *runner.BenchmarkFramework[PipelineTask, PipelineResult] {
	pipelineRecordSizeFlag = flag.Int("recordsize", 10, "Average record size in KB (default: 10KB)")
	pipelineReadLatencyFlag = flag.Int("readlatency", 10, "Base read latency in milliseconds (default: 10ms)")
	pipelineWriteLatencyFlag = flag.Int("writelatency", 15, "Base write latency in milliseconds (default: 15ms)")
	pipelineCPUWorkFlag = flag.Int("cpuwork", 2000, "CPU work iterations for transformation (default: 2000)")

	return &runner.BenchmarkFramework[PipelineTask, PipelineResult]{
		Name:          "Data Pipeline Benchmark",
		AllStrategies: runner.AllStrategies,

		GenerateTasks: func(workload string, count int) []PipelineTask {
			switch workload {
			case WorkloadStreaming:
				return generateStreamingTasks(count, *pipelineRecordSizeFlag, *pipelineReadLatencyFlag, *pipelineWriteLatencyFlag, *pipelineCPUWorkFlag)
			case WorkloadBatch:
				return generateBatchTasks(count, *pipelineRecordSizeFlag, *pipelineReadLatencyFlag, *pipelineWriteLatencyFlag, *pipelineCPUWorkFlag)
			default: // WorkloadETL
				return generateETLTasks(count, *pipelineRecordSizeFlag, *pipelineReadLatencyFlag, *pipelineWriteLatencyFlag, *pipelineCPUWorkFlag)
			}
		},

		NewRunner: func(strategy string, tasks []PipelineTask, workers int) runner.BenchmarkRunner[PipelineTask, PipelineResult] {
			return &PipelineRunner{
				strategy:   strategy,
				tasks:      tasks,
				numWorkers: workers,
				results:    make([]PipelineResult, 0, len(tasks)),
			}
		},

		PrintConfig: func(workers int, taskCount int, workload string) {
			printPipelineConfiguration(workers, taskCount, workload, *pipelineRecordSizeFlag)
		},
		PrintResults:   printPipelineResults,
		CalculateStats: calculatePipelineStats,
	}
}
