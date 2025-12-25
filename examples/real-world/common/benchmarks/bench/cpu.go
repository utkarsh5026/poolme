package bench

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

// CPUTask represents a unit of work with configurable CPU complexity
type CPUTask struct {
	ID         int
	Complexity int
	Weight     string
}

// CPUTaskResult represents the result of processing a CPU task
type CPUTaskResult struct {
	ID          int
	ProcessTime time.Duration
}

func generateBalancedCPUTasks(count int, complexity int) []CPUTask {
	tasks := make([]CPUTask, count)
	for i := range count {
		tasks[i] = CPUTask{
			ID:         i,
			Complexity: complexity,
			Weight:     "Medium",
		}
	}
	return tasks
}

// generateImbalancedCPUTasks creates tasks with varying complexity
func generateImbalancedCPUTasks(count int, baseComplexity int) []CPUTask {
	tasks := make([]CPUTask, count)
	heavyCount := count * 10 / 100
	mediumCount := count * 20 / 100

	for i := range count {
		task := CPUTask{ID: i}

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

// generatePriorityCPUTasks creates imbalanced tasks in reversed order
func generatePriorityCPUTasks(count int, baseComplexity int) []CPUTask {
	tasks := generateImbalancedCPUTasks(count, baseComplexity)
	slices.Reverse(tasks)
	return tasks
}

// processCPUTask performs CPU-intensive work using XOR shift and trigonometric calculations
func processCPUTask(_ context.Context, task CPUTask) (CPUTaskResult, error) {
	start := time.Now()
	state := max(1, uint32(task.ID))
	_ = xorShift(task.Complexity, state)

	return CPUTaskResult{
		ID:          task.ID,
		ProcessTime: time.Since(start),
	}, nil
}

// CPURunner implements the benchmark runner for CPU workloads
type CPURunner struct {
	strategy   string
	numWorkers int
	tasks      []CPUTask
}

func (r *CPURunner) Run() runner.StrategyResult {
	ctx := context.Background()

	wPool := SelectStrategy(r.strategy, StrategyConfig[CPUTask, CPUTaskResult]{
		NumWorkers: r.numWorkers,
		Comparator: func(a, b CPUTask) bool {
			return a.Complexity > b.Complexity
		},
	})

	start := time.Now()
	results, err := wPool.Process(ctx, r.tasks, processCPUTask)
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

	p50, p95, p99 := CalculatePercentiles(latencies)

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

func printCPUConfiguration(numWorkers int, numTasks int, workload string, complexity int) {
	cp := ConfigPrinter{
		Title:      "Configuration:",
		NumWorkers: numWorkers,
		NumTasks:   numTasks,
		Workload:   workload,
		CustomParams: map[string]string{
			"Complexity": fmt.Sprintf("%s iterations per task", runner.FormatNumber(complexity)),
		},
		WorkloadDesc: map[string]string{
			WorkloadBalanced: fmt.Sprintf("  • All %s tasks have equal complexity (%s iterations)\n",
				runner.FormatNumber(numTasks), runner.FormatNumber(complexity)),
			WorkloadImbalanced: fmt.Sprintf("  • 10%% Heavy tasks (%s tasks × %s iterations)\n  • 20%% Medium tasks (%s tasks × %s iterations)\n  • 70%% Light tasks (%s tasks × %s iterations)\n",
				runner.FormatNumber(numTasks*10/100), runner.FormatNumber(complexity*10),
				runner.FormatNumber(numTasks*20/100), runner.FormatNumber(complexity*5),
				runner.FormatNumber(numTasks*70/100), runner.FormatNumber(complexity)),
			WorkloadPriority: "  • Imbalanced workload submitted in reversed order (light → heavy)\n  • Tests if priority schedulers reorder tasks effectively\n",
		},
	}
	cp.Print()
}

// printCPUResults prints the CPU benchmark results
func printCPUResults(results []runner.StrategyResult) {
	rr := ResultsRenderer{
		Title:         "THROUGHPUT COMPARISON",
		ShowVsFastest: true,
	}
	rr.PrintComparisonTable(results)
	printCPULatencyTable(results)
}

// printCPULatencyTable prints a separate latency comparison table for CPU benchmarks
func printCPULatencyTable(results []runner.StrategyResult) {
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")
	_, _ = bold.Println("⚡ LATENCY COMPARISON")
	_, _ = bold.Println("═══════════════════════════════════════════════════════════")

	latencyTable := tablewriter.NewWriter(os.Stdout)
	latencyTable.Header("Rank", "Scheduler", "P50 (median)", "P95", "P99")

	for _, r := range results {
		_ = latencyTable.Append([]string{
			RankIcon(r.Rank),
			r.Name,
			runner.FormatLatency(r.P50Latency),
			runner.FormatLatency(r.P95Latency),
			runner.FormatLatency(r.P99Latency),
		})
	}

	_ = latencyTable.Render()
	fmt.Println()
}
