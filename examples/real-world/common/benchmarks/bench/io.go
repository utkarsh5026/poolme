package bench

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

var (
	totalAPICalls  atomic.Int64
	totalDBQueries atomic.Int64
	totalFileOps   atomic.Int64
)

// IOTask represents a task with I/O characteristics
type IOTask struct {
	ID          int
	IOLatencyMs int
	CPUWork     int
	TaskType    string
	Weight      string
}

// IOTaskResult represents the result of processing an I/O task
type IOTaskResult struct {
	ID          int
	ProcessTime time.Duration
	IOTime      time.Duration
	CPUTime     time.Duration
	TaskType    string
}

type ioTaskGenConfig struct {
	fastPercent, latencyDecrease int
	slowPercent, latencyIncrease int
}

// generateAPITasks creates tasks simulating API gateway workload
func generateAPITasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     15,
		latencyDecrease: 3,
		slowPercent:     95,
		latencyIncrease: 3,
	}
	return generateIOTasks(count, avgLatencyMs, "api", conf)
}

// generateDatabaseTasks creates tasks simulating database query workload
func generateDatabaseTasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     70,
		latencyDecrease: 2,
		slowPercent:     95,
		latencyIncrease: 5,
	}
	return generateIOTasks(count, avgLatencyMs, "database", conf)
}

// generateFileIOTasks creates tasks simulating file I/O workload
func generateFileIOTasks(count int, avgLatencyMs int) []IOTask {
	conf := ioTaskGenConfig{
		fastPercent:     50,
		latencyDecrease: 4,
		slowPercent:     90,
		latencyIncrease: 4,
	}
	return generateIOTasks(count, avgLatencyMs, "file", conf)
}

// generateIOTasks is a helper to create I/O tasks with configurable distributions
func generateIOTasks(count, avgLatencyMs int, taskType string, conf ioTaskGenConfig) []IOTask {
	tasks := make([]IOTask, count)
	for i := range count {
		latency := avgLatencyMs
		weight := "Medium"

		roll := rand.Intn(100)
		if roll < conf.fastPercent {
			latency = avgLatencyMs / conf.latencyDecrease
			weight = "Fast"
		} else if roll >= conf.slowPercent {
			latency = avgLatencyMs * conf.latencyIncrease
			weight = "Slow"
		}

		tasks[i] = IOTask{
			ID:          i,
			IOLatencyMs: latency,
			TaskType:    taskType,
			Weight:      weight,
		}
	}
	return tasks
}

// generateMixedTasks creates tasks with both I/O and CPU work
func generateMixedTasks(count int, avgLatencyMs int, cpuWork int) []IOTask {
	tasks := make([]IOTask, count)
	for i := range count {
		latency := avgLatencyMs
		cpu := cpuWork
		weight := "Medium"

		roll := rand.Intn(100)
		if roll < 60 {
			cpu = cpuWork / 4
			weight = "Fast"
		} else if roll >= 90 {
			cpu = cpuWork * 2
			latency = avgLatencyMs * 2
			weight = "Heavy"
		}

		tasks[i] = IOTask{
			ID:          i,
			IOLatencyMs: latency,
			CPUWork:     cpu,
			TaskType:    "mixed",
			Weight:      weight,
		}
	}
	return tasks
}

// simulateAPICall simulates an HTTP API call with variable latency
func simulateAPICall(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.3)
}

// simulateDatabaseQuery simulates a database query with connection pool contention
func simulateDatabaseQuery(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.15)
}

// simulateFileIO simulates file system I/O with seek times
func simulateFileIO(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.4)
}

func simulateWithJitter(ctx context.Context, baseLatencyMs int, jitterFactor float64) time.Duration {
	start := time.Now()
	jitter := float64(baseLatencyMs) * jitterFactor
	jitterRange := max(int(jitter*2), 1)
	actualLatency := max(baseLatencyMs+int(float64(rand.Intn(jitterRange))-jitter), 1)

	timer := time.NewTimer(time.Duration(actualLatency) * time.Millisecond)
	select {
	case <-timer.C:
		totalDBQueries.Add(1)
	case <-ctx.Done():
		timer.Stop()
		return time.Since(start)
	}

	return time.Since(start)
}

// performCPUWork simulates CPU computation
func performCPUWork(iterations int) time.Duration {
	start := time.Now()
	state := uint64(1)
	result := 0.0

	for i := range iterations {
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		if i%50 == 0 {
			result += math.Sin(float64(x)) * math.Cos(float64(i))
		}
	}
	_ = result
	return time.Since(start)
}

func processIOTask(ctx context.Context, task IOTask) (IOTaskResult, error) {
	start := time.Now()
	result := IOTaskResult{
		ID:       task.ID,
		TaskType: task.TaskType,
	}

	var ioTime time.Duration

	switch task.TaskType {
	case "api":
		ioTime = simulateAPICall(ctx, task.IOLatencyMs)
	case "database":
		ioTime = simulateDatabaseQuery(ctx, task.IOLatencyMs)
	case "file":
		ioTime = simulateFileIO(ctx, task.IOLatencyMs)
	case "mixed":
		ioTime = simulateAPICall(ctx, task.IOLatencyMs)
		cpuTime := performCPUWork(task.CPUWork)
		result.CPUTime = cpuTime
	}

	result.IOTime = ioTime
	result.ProcessTime = time.Since(start)
	return result, nil
}

// IORunner implements the benchmark runner for I/O workloads
type IORunner struct {
	strategy   string
	numWorkers int
	tasks      []IOTask
	results    []IOTaskResult
}

func (r *IORunner) Run() runner.StrategyResult {
	ctx := context.Background()
	wPool := SelectStrategy(r.strategy, StrategyConfig[IOTask, IOTaskResult]{
		NumWorkers: r.numWorkers,
		Comparator: func(a, b IOTask) bool {
			return a.IOLatencyMs < b.IOLatencyMs
		},
	})

	start := time.Now()
	results, err := wPool.Process(ctx, r.tasks, processIOTask)
	if err != nil {
		_, _ = runner.Red.Printf("Error processing %s: %v\n", r.strategy, err)
		return runner.StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)
	r.results = results

	latencies := make([]time.Duration, len(results))
	for i, res := range results {
		latencies[i] = res.ProcessTime
	}
	slices.Sort(latencies)

	avgLatency := elapsed / time.Duration(len(results))
	p95Latency := latencies[int(float64(len(latencies))*0.95)]
	p99Latency := latencies[int(float64(len(latencies))*0.99)]

	taskCount := len(r.tasks)
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()

	return runner.StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputRowsPS: throughputTasksPS,
		AvgLatency:       avgLatency,
		P95Latency:       p95Latency,
		P99Latency:       p99Latency,
	}
}

// printIOConfiguration prints the I/O benchmark configuration
func printIOConfiguration(numWorkers int, numTasks int, workload string, latency int) {
	cp := ConfigPrinter{
		Title:      "I/O Benchmark Configuration:",
		NumWorkers: numWorkers,
		NumTasks:   numTasks,
		Workload:   workload,
		CustomParams: map[string]string{
			"Avg Latency": fmt.Sprintf("%dms per operation", latency),
		},
		WorkloadDesc: map[string]string{
			WorkloadAPI:      "  • Simulates API Gateway: 80% normal, 15% fast, 5% slow requests\n  • Network I/O with ±30% latency variance\n",
			WorkloadDatabase: "  • Simulates Database Queries: 70% fast, 25% medium, 5% slow (N+1)\n  • Query I/O with ±15% latency variance\n",
			WorkloadFile:     "  • Simulates File I/O: 50% cache hits, 40% disk reads, 10% slow seeks\n  • Disk I/O with ±40% latency variance\n",
			WorkloadMixed:    "  • Mixed I/O + CPU: 60% CPU-light, 30% balanced, 10% CPU-heavy\n  • Combines network I/O with computational work\n",
		},
	}
	cp.Print()
}

// printIOResults prints the I/O benchmark results
func printIOResults(results []runner.StrategyResult) {
	rr := ResultsRenderer{
		Title:          "I/O SCHEDULER PERFORMANCE",
		ShowAvgLatency: true,
		ShowVsFastest:  true,
	}
	rr.PrintComparisonTable(results)

	printIOOperationsSummary()
}

// printIOOperationsSummary prints the summary of I/O operations performed
func printIOOperationsSummary() {
	apiCalls := totalAPICalls.Load()
	dbQueries := totalDBQueries.Load()
	fileOps := totalFileOps.Load()

	if apiCalls > 0 || dbQueries > 0 || fileOps > 0 {
		_, _ = bold.Println("📈 I/O Operations Summary:")
		if apiCalls > 0 {
			fmt.Printf("  API Calls:       %s\n", runner.FormatNumber(int(apiCalls)))
		}
		if dbQueries > 0 {
			fmt.Printf("  DB Queries:      %s\n", runner.FormatNumber(int(dbQueries)))
		}
		if fileOps > 0 {
			fmt.Printf("  File Operations: %s\n", runner.FormatNumber(int(fileOps)))
		}
		fmt.Println()
	}
}
