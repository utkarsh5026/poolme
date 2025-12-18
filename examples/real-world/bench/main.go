package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/schollz/progressbar/v3"
	"github.com/utkarsh5026/poolme/pool"
)

var (
	allStrategies = []string{
		"Channel",
		"Work-Stealing",
		"MPMC Queue",
		"LMAX Disruptor",
		"Priority Queue",
		"Skip List",
		"Bitmask",
	}
)

// Task represents a unit of work with configurable CPU complexity
type Task struct {
	ID         int    // Task identifier
	Complexity int    // CPU work units (iterations of math operations)
	Weight     string // "Light", "Medium", "Heavy" (for display only)
}

// TaskResult represents the result of processing a task
type TaskResult struct {
	ID          int
	ProcessTime time.Duration
}

// StrategyResult holds the results for a strategy
type StrategyResult struct {
	Name             string
	TotalTime        time.Duration
	ThroughputMBps   float64
	ThroughputRowsPS float64
	Rank             int
}

var (
	bold = color.New(color.Bold)
	red  = color.New(color.FgRed)
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

// generateBalancedTasks creates tasks with uniform complexity.
// All tasks have the same CPU work, creating a perfectly balanced workload.
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

// generateImbalancedTasks creates tasks with varying complexity.
// This creates a realistic workload with different task sizes:
// - 10% Heavy tasks (10x base complexity)
// - 20% Medium tasks (5x base complexity)
// - 70% Light tasks (1x base complexity)
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

// generatePriorityTasks creates imbalanced tasks in reversed order.
// Light tasks are submitted first, heavy tasks last.
// This tests if priority-based schedulers can reorder tasks for better performance.
func generatePriorityTasks(count int, baseComplexity int) []Task {
	tasks := generateImbalancedTasks(count, baseComplexity)
	slices.Reverse(tasks) // Submit light first, heavy last
	return tasks
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

type Runner struct {
	strategy   string
	numWorkers int
	tasks      []Task
}

func newRunner(strategyName string, tasks []Task, numWorkers int) *Runner {
	return &Runner{
		strategy:   strategyName,
		numWorkers: numWorkers,
		tasks:      tasks,
	}
}

func (r *Runner) Run(bar *progressbar.ProgressBar) StrategyResult {
	ctx := context.Background()
	wPool := r.selectStrategy()
	start := time.Now()

	// Process all tasks using the batch Process method
	// This handles submission and result collection internally, avoiding deadlocks
	_, err := wPool.Process(ctx, r.tasks, processTask)
	if err != nil {
		_, _ = red.Printf("Error processing %s: %v\n", r.strategy, err)
		return StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)

	if bar != nil {
		_ = bar.Add(1)
	}

	taskCount := len(r.tasks)
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()

	return StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputMBps:   0,
		ThroughputRowsPS: throughputTasksPS,
	}
}

func (r *Runner) selectStrategy() *pool.WorkerPool[Task, TaskResult] {
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

// calculateStats computes median time from multiple iterations
func calculateStats(strategyName string, results []StrategyResult) StrategyResult {
	if len(results) == 0 {
		return StrategyResult{Name: strategyName}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	medianIdx := len(results) / 2
	median := results[medianIdx]

	return StrategyResult{
		Name:             strategyName,
		TotalTime:        median.TotalTime,
		ThroughputMBps:   median.ThroughputMBps,
		ThroughputRowsPS: median.ThroughputRowsPS,
	}
}

// printIterationStats prints detailed statistics for multiple iterations
func printIterationStats(results []StrategyResult) {
	if len(results) <= 1 {
		return
	}

	times := make([]time.Duration, len(results))
	for i, r := range results {
		times[i] = r.TotalTime
	}

	// Sort for percentile calculations
	slices.Sort(times)

	mini := times[0]
	maxi := times[len(times)-1]
	median := times[len(times)/2]

	// Calculate mean
	var sum time.Duration
	for _, t := range times {
		sum += t
	}
	mean := sum / time.Duration(len(times))

	// Calculate standard deviation
	var variance float64
	for _, t := range times {
		diff := float64(t - mean)
		variance += diff * diff
	}
	stddev := time.Duration(math.Sqrt(variance / float64(len(times))))

	fmt.Printf("    Min: %v | Median: %v | Mean: %v | Max: %v | StdDev: %v\n",
		mini.Round(time.Millisecond),
		median.Round(time.Millisecond),
		mean.Round(time.Millisecond),
		maxi.Round(time.Millisecond),
		stddev.Round(time.Millisecond))
}

func printResults(results []StrategyResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalTime < results[j].TotalTime
	})

	for i := range results {
		results[i].Rank = i + 1
	}

	printComparisonTable(results)
}

func printComparisonTable(results []StrategyResult) {
	fmt.Println()
	_, _ = bold.Println("📊 SCHEDULER PERFORMANCE - Tasks/Second")
	fmt.Println()

	fastestTime := results[0].TotalTime

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Scheduler", "Time", "Tasks/sec", "vs Fastest")

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

		vsFastest := float64(r.TotalTime) / float64(fastestTime)
		vsFastestStr := fmt.Sprintf("%.2fx", vsFastest)
		if r.Rank == 1 {
			vsFastestStr = "baseline"
		}

		_ = table.Append(
			rankIcon,
			r.Name,
			r.TotalTime.Round(time.Millisecond).String(),
			formatNumber(int(r.ThroughputRowsPS)),
			vsFastestStr,
		)
	}

	_ = table.Render()
}

func printConfiguration(numWorkers int, numTasks int, complexity int, workload string) {
	_, _ = bold.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:    %d (using %d CPU cores)\n", numWorkers, runtime.NumCPU())
	fmt.Printf("  Tasks:      %s concurrent tasks\n", formatNumber(numTasks))
	fmt.Printf("  Complexity: %s iterations per task\n", formatNumber(complexity))
	fmt.Printf("  Workload:   %s\n", workload)
	fmt.Printf("  Strategies: 7 schedulers\n")
	fmt.Println()

	_, _ = bold.Println("📊 Workload Distribution:")
	switch workload {
	case "balanced":
		fmt.Printf("  • All %s tasks have equal complexity (%s iterations)\n", formatNumber(numTasks), formatNumber(complexity))
	case "imbalanced":
		fmt.Printf("  • 10%% Heavy tasks (%s tasks × %s iterations)\n", formatNumber(numTasks*10/100), formatNumber(complexity*10))
		fmt.Printf("  • 20%% Medium tasks (%s tasks × %s iterations)\n", formatNumber(numTasks*20/100), formatNumber(complexity*5))
		fmt.Printf("  • 70%% Light tasks (%s tasks × %s iterations)\n", formatNumber(numTasks*70/100), formatNumber(complexity))
	case "priority":
		fmt.Printf("  • Imbalanced workload submitted in reversed order (light → heavy)\n")
		fmt.Printf("  • Tests if priority schedulers reorder tasks effectively\n")
	}
	fmt.Println()
}

func getStrategiesToRun(isolated string, iterations int, warmup int) []string {
	if isolated != "" {
		found := slices.Contains(allStrategies, isolated)
		if !found {
			_, _ = red.Printf("Error: Unknown strategy '%s'\n", isolated)
			fmt.Println("Available strategies:", allStrategies)
			os.Exit(1)
		}
		fmt.Printf("🔬 SINGLE STRATEGY MODE: Testing '%s' scheduler\n", isolated)
		if iterations > 1 {
			fmt.Printf("  Running %d iterations with %d warmup runs\n", iterations, warmup)
		}
		fmt.Println()
		return []string{isolated}
	}
	return allStrategies
}

func makeProgressBar(strategies []string) *progressbar.ProgressBar {
	return progressbar.NewOptions(len(strategies),
		progressbar.OptionSetDescription("Testing strategies"),
		progressbar.OptionSetWidth(50),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: "░",
			BarStart:      "│",
			BarEnd:        "│",
		}),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWriter(os.Stderr),          // Use stderr for better terminal support
		progressbar.OptionThrottle(65*time.Millisecond), // Reduce update frequency
		progressbar.OptionClearOnFinish(),               // Clear progress bar when done
	)
}

func main() {
	// Enable ANSI escape sequences on Windows for progress bar support
	enableWindowsANSI()

	tasksFlag := flag.Int("tasks", 100_000, "Number of tasks to process (default: 100,000)")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect, max 8)")
	complexityFlag := flag.Int("complexity", 10_000, "Base CPU work per task in iterations (default: 10,000)")
	workloadFlag := flag.String("workload", "balanced", "Workload mode: 'balanced' (all tasks equal), 'imbalanced' (varied task sizes), or 'priority' (reversed order)")
	strategyFlag := flag.String("strategy", "", "Run a specific scheduler strategy (e.g., 'Work-Stealing', 'MPMC Queue'). If empty, runs all strategies")
	iterationsFlag := flag.Int("iterations", 1, "Number of iterations to run per strategy (for statistical analysis)")
	warmupFlag := flag.Int("warmup", 0, "Number of warmup iterations before measurement")
	cpuProfileFlag := flag.String("cpuprofile", "", "Write CPU profile to file")
	memProfileFlag := flag.String("memprofile", "", "Write memory profile to file")
	flag.Parse()

	if *cpuProfileFlag != "" {
		f, err := os.Create(*cpuProfileFlag)
		if err != nil {
			_, _ = red.Printf("Error creating CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				_, _ = red.Printf("Error closing memory file: %v\n", err)
			}
		}(f)

		if err := pprof.StartCPUProfile(f); err != nil {
			_, _ = red.Printf("Error starting CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
		fmt.Printf("CPU profiling enabled, writing to: %s\n", *cpuProfileFlag)
	}

	var memProfileFile *os.File
	if *memProfileFlag != "" {
		var err error
		memProfileFile, err = os.Create(*memProfileFlag)
		if err != nil {
			_, _ = red.Printf("Error creating memory profile: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			runtime.GC()
			if err := pprof.WriteHeapProfile(memProfileFile); err != nil {
				_, _ = red.Printf("Error writing memory profile: %v\n", err)
			}
			if err := memProfileFile.Close(); err != nil {
				_, _ = red.Printf("Error closing memory file: %v\n", err)
			}
		}()
		fmt.Printf("Memory profiling enabled, writing to: %s\n", *memProfileFlag)
	}

	numWorkers := min(runtime.NumCPU(), *workersFlag)

	// Generate tasks based on workload mode
	var tasks []Task
	switch *workloadFlag {
	case "priority":
		tasks = generatePriorityTasks(*tasksFlag, *complexityFlag)
	case "imbalanced":
		tasks = generateImbalancedTasks(*tasksFlag, *complexityFlag)
	default: // "balanced"
		tasks = generateBalancedTasks(*tasksFlag, *complexityFlag)
	}

	printConfiguration(numWorkers, len(tasks), *complexityFlag, *workloadFlag)

	strategies := getStrategiesToRun(*strategyFlag, *iterationsFlag, *warmupFlag)

	results := make([]StrategyResult, 0, len(strategies))

	_, _ = bold.Println("Running Benchmarks...")
	fmt.Println()

	bar := makeProgressBar(strategies)

	for _, strategy := range strategies {
		if *warmupFlag > 0 {
			for w := 0; w < *warmupFlag; w++ {
				r := newRunner(strategy, tasks, numWorkers)
				_ = r.Run(nil)
				runtime.GC() // Force GC between warmup runs
				time.Sleep(100 * time.Millisecond)
			}
		}

		iterationResults := make([]StrategyResult, 0, *iterationsFlag)
		for iter := 0; iter < *iterationsFlag; iter++ {
			bar.Describe(fmt.Sprintf("Testing: %s", strategy))

			r := newRunner(strategy, tasks, numWorkers)
			iterationResults = append(iterationResults, r.Run(bar))

			if iter < *iterationsFlag-1 {
				runtime.GC()
				time.Sleep(100 * time.Millisecond)
			}
		}

		var finalResult StrategyResult
		if *iterationsFlag == 1 {
			finalResult = iterationResults[0]
		} else {
			finalResult = calculateStats(strategy, iterationResults)
			printIterationStats(iterationResults)
		}

		results = append(results, finalResult)
		time.Sleep(time.Millisecond * 300)
	}

	fmt.Println()
	fmt.Println()

	printResults(results)
}