package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/utkarsh5026/poolme/pool"

	"github.com/utkarsh5026/poolme/examples/real-world/common/runner"
)

// Task represents a unit of work with configurable CPU complexity
type Task struct {
	ID         int
	Complexity int
	Weight     string
}

// TaskResult represents the result of processing a task
type TaskResult struct {
	ID          int
	ProcessTime time.Duration
}

var (
	bold = color.New(color.Bold)
)

func processTask(_ context.Context, task Task) (TaskResult, error) {
	start := time.Now()
	state := uint64(task.ID)
	if state == 0 {
		state = 1
	}

	result := 0.0
	for i := 0; i < task.Complexity; i++ {
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		if i%100 == 0 {
			result += math.Sin(float64(x)) * math.Cos(float64(i))
		}
	}
	_ = result
	return TaskResult{
		ID:          task.ID,
		ProcessTime: time.Since(start),
	}, nil
}

// generateBalancedTasks creates tasks with uniform complexity
func generateBalancedTasks(count int, complexity int) []Task {
	tasks := make([]Task, count)
	for i := range count {
		tasks[i] = Task{
			ID:         i,
			Complexity: complexity,
			Weight:     "Medium",
		}
	}
	return tasks
}

// generateImbalancedTasks creates tasks with varying complexity
func generateImbalancedTasks(count int, baseComplexity int) []Task {
	tasks := make([]Task, count)
	heavyCount := count * 10 / 100
	mediumCount := count * 20 / 100

	for i := range count {
		task := Task{ID: i}

		if i < heavyCount {
			task.Complexity = baseComplexity * 10
			task.Weight = "Heavy"
		} else if i < heavyCount+mediumCount {
			task.Complexity = baseComplexity * 5
			task.Weight = "Medium"
		} else {
			task.Complexity = baseComplexity
			task.Weight = "Light"
		}

		tasks[i] = task
	}

	return tasks
}

// generatePriorityTasks creates imbalanced tasks in reversed order
func generatePriorityTasks(count int, baseComplexity int) []Task {
	tasks := generateImbalancedTasks(count, baseComplexity)
	slices.Reverse(tasks)
	return tasks
}

// CPURunner implements the benchmark runner for CPU workloads
type CPURunner struct {
	strategy   string
	numWorkers int
	tasks      []Task
}

func (r *CPURunner) Run() runner.StrategyResult {
	ctx := context.Background()
	wPool := r.selectStrategy()
	start := time.Now()

	results, err := wPool.Process(ctx, r.tasks, processTask)
	if err != nil {
		_, _ = runner.Red.Printf("Error processing %s: %v\n", r.strategy, err)
		return runner.StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)

	taskCount := len(r.tasks)
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()

	latencies := make([]time.Duration, 0, len(results))
	for _, result := range results {
		latencies = append(latencies, result.ProcessTime)
	}
	slices.Sort(latencies)

	p50, p95, p99 := calculatePercentiles(latencies)

	return runner.StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputMBPS:   0,
		ThroughputRowsPS: throughputTasksPS,
		P50Latency:       p50,
		P95Latency:       p95,
		P99Latency:       p99,
	}
}

func (r *CPURunner) selectStrategy() *pool.WorkerPool[Task, TaskResult] {
	switch r.strategy {
	case "Work-Stealing":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithPriorityQueue(func(a, b Task) bool {
				return a.Complexity > b.Complexity
			}),
		)
	case "Skip List":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithSkipList(func(a, b Task) bool {
				return a.Complexity > b.Complexity
			}),
		)
	case "Bitmask":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithLmax(),
		)
	default:
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
		)
	}
}

func calculatePercentiles(sortedLatencies []time.Duration) (p50, p95, p99 time.Duration) {
	if len(sortedLatencies) == 0 {
		return 0, 0, 0
	}

	n := len(sortedLatencies)
	p50Idx := n * 50 / 100
	p95Idx := n * 95 / 100
	p99Idx := n * 99 / 100

	if p50Idx >= n {
		p50Idx = n - 1
	}
	if p95Idx >= n {
		p95Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}

	return sortedLatencies[p50Idx], sortedLatencies[p95Idx], sortedLatencies[p99Idx]
}

func printConfiguration(numWorkers int, numTasks int, workload string) {
	complexity := *complexityFlag

	_, _ = bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:    %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Tasks:      %s concurrent tasks\n", runner.FormatNumber(numTasks))
	fmt.Printf("  Complexity: %s iterations per task\n", runner.FormatNumber(complexity))
	fmt.Printf("  Workload:   %s\n", workload)
	fmt.Printf("  Strategies: 7 schedulers\n")
	fmt.Println()

	_, _ = bold.Println("📊 Workload Distribution:")
	switch workload {
	case "balanced":
		fmt.Printf("  • All %s tasks have equal complexity (%s iterations)\n",
			runner.FormatNumber(numTasks), runner.FormatNumber(complexity))
	case "imbalanced":
		fmt.Printf("  • 10%% Heavy tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*10/100), runner.FormatNumber(complexity*10))
		fmt.Printf("  • 20%% Medium tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*20/100), runner.FormatNumber(complexity*5))
		fmt.Printf("  • 70%% Light tasks (%s tasks × %s iterations)\n",
			runner.FormatNumber(numTasks*70/100), runner.FormatNumber(complexity))
	case "priority":
		fmt.Printf("  • Imbalanced workload submitted in reversed order (light → heavy)\n")
		fmt.Printf("  • Tests if priority schedulers reorder tasks effectively\n")
	}
	fmt.Println()
}

func printResults(results []runner.StrategyResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	for i := range results {
		results[i].Rank = i + 1
	}

	printComparisonTable(results)
}

func printComparisonTable(results []runner.StrategyResult) {
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("📊 THROUGHPUT COMPARISON")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Scheduler", "Total Time", "Tasks/sec", "vs Fastest")

	fastestTime := results[0].TotalTime.Seconds()

	for _, r := range results {
		rankIcon := fmt.Sprintf("%d", r.Rank)
		switch r.Rank {
		case 1:
			rankIcon = "🥇"
		case 2:
			rankIcon = "🥈"
		case 3:
			rankIcon = "🥉"
		}

		timeStr := r.TotalTime.Round(time.Millisecond).String()
		throughputStr := runner.FormatNumber(int(r.ThroughputRowsPS))

		var comparison string
		if r.Rank == 1 {
			comparison = "baseline"
		} else {
			pct := ((r.TotalTime.Seconds() / fastestTime) - 1) * 100
			comparison = fmt.Sprintf("+%.1f%%", pct)
		}

		_ = table.Append(rankIcon, r.Name, timeStr, throughputStr, comparison)
	}

	_ = table.Render()
	fmt.Println()
	fmt.Println()

	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("⚡ LATENCY COMPARISON")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	latencyTable := tablewriter.NewWriter(os.Stdout)
	latencyTable.Header("Rank", "Scheduler", "P50 (median)", "P95", "P99")

	for _, r := range results {
		rankIcon := fmt.Sprintf("%d", r.Rank)
		switch r.Rank {
		case 1:
			rankIcon = "🥇"
		case 2:
			rankIcon = "🥈"
		case 3:
			rankIcon = "🥉"
		}

		_ = latencyTable.Append(
			rankIcon,
			r.Name,
			runner.FormatLatency(r.P50Latency),
			runner.FormatLatency(r.P95Latency),
			runner.FormatLatency(r.P99Latency),
		)
	}

	_ = latencyTable.Render()
	fmt.Println()
}

var complexityFlag *int

func main() {
	runner.EnableWindowsANSI()

	commonFlags := runner.DefineCommonFlags()
	complexityFlag = flag.Int("complexity", 10_000, "Base CPU work per task in iterations (default: 10,000)")
	flag.Parse()

	framework := &runner.BenchmarkFramework[Task, TaskResult]{
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

	framework.Run(commonFlags)
}
