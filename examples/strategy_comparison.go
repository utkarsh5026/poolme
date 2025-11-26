package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

// ANSI color codes for beautiful output
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
)

type BenchmarkResult struct {
	Name           string
	Duration       time.Duration
	TasksPerSecond float64
	TotalTasks     int
	Workers        int
}

func main() {
	printHeader()

	// Run different scenarios
	scenarios := []struct {
		name        string
		taskCount   int
		workers     int
		workFunc    func(ctx context.Context, task int) (int, error)
		description string
	}{
		{
			name:        "CPU-Bound (Light)",
			taskCount:   10000,
			workers:     8,
			workFunc:    cpuBoundLight,
			description: "10,000 light CPU tasks with 8 workers",
		},
		{
			name:        "CPU-Bound (Heavy)",
			taskCount:   5000,
			workers:     8,
			workFunc:    cpuBoundHeavy,
			description: "5,000 heavy CPU tasks with 8 workers",
		},
		{
			name:        "I/O-Bound",
			taskCount:   1000,
			workers:     8,
			workFunc:    ioBoundWork,
			description: "1,000 I/O-bound tasks with variable delays",
		},
		{
			name:        "High Contention",
			taskCount:   50000,
			workers:     16,
			workFunc:    cpuBoundLight,
			description: "50,000 tasks with 16 workers (oversubscribed)",
		},
		{
			name:        "Variable Complexity",
			taskCount:   10000,
			workers:     8,
			workFunc:    variableComplexity,
			description: "10,000 tasks with variable processing time",
		},
		{
			name:        "Data Pipeline",
			taskCount:   5000,
			workers:     8,
			workFunc:    dataProcessingPipeline,
			description: "5,000 tasks simulating Extract-Transform-Load pipeline",
		},
	}

	for i, scenario := range scenarios {
		if i > 0 {
			fmt.Println()
		}
		runScenario(scenario.name, scenario.description, scenario.taskCount, scenario.workers, scenario.workFunc)
	}

	// Run concurrent submitters demo
	runConcurrentSubmittersDemo()

	printFooter()
}

func runScenario(name, description string, taskCount, workers int, workFunc func(ctx context.Context, task int) (int, error)) {
	printScenarioHeader(name, description)

	results := []BenchmarkResult{
		runBenchmark("Channel (Default)", taskCount, workers, workFunc, pool.SchedulingChannel),
		runBenchmark("Work Stealing", taskCount, workers, workFunc, pool.SchedulingWorkStealing),
		runBenchmark("MPMC Bounded", taskCount, workers, workFunc, pool.SchedulingMPMC),
	}

	displayResults(results)
}

func runBenchmark(strategyName string, taskCount, workers int, workFunc func(ctx context.Context, task int) (int, error), strategy pool.SchedulingStrategyType) BenchmarkResult {
	tasks := make([]int, taskCount)
	for i := range tasks {
		tasks[i] = i
	}

	opts := []pool.WorkerPoolOption{
		pool.WithWorkerCount(workers),
		pool.WithSchedulingStrategy(strategy),
	}

	if strategy == pool.SchedulingMPMC {
		opts = append(opts, pool.WithMPMCQueue(pool.WithBoundedQueue(taskCount*2)))
	}

	wp := pool.NewWorkerPool[int, int](opts...)

	start := time.Now()
	_, err := wp.Process(context.Background(), tasks, workFunc)
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("%s Error: %v%s\n", colorRed, err, colorReset)
	}

	tasksPerSecond := float64(taskCount) / duration.Seconds()

	return BenchmarkResult{
		Name:           strategyName,
		Duration:       duration,
		TasksPerSecond: tasksPerSecond,
		TotalTasks:     taskCount,
		Workers:        workers,
	}
}

func displayResults(results []BenchmarkResult) {
	// Find the best result
	bestIdx := 0
	for i, r := range results {
		if r.TasksPerSecond > results[bestIdx].TasksPerSecond {
			bestIdx = i
		}
	}

	// Print table header
	fmt.Printf("\n%s┌─────────────────────────┬──────────────┬────────────────┬─────────────┬────────────┐%s\n", colorCyan, colorReset)
	fmt.Printf("%s│ %-23s │ %-12s │ %-14s │ %-11s │ %-10s │%s\n", colorCyan, "Strategy", "Duration", "Tasks/sec", "vs Best", "Performance", colorReset)
	fmt.Printf("%s├─────────────────────────┼──────────────┼────────────────┼─────────────┼────────────┤%s\n", colorCyan, colorReset)

	// Print results
	for i, r := range results {
		pct := (r.TasksPerSecond / results[bestIdx].TasksPerSecond) * 100
		diff := pct - 100

		var diffStr string
		var color string
		if i == bestIdx {
			diffStr = "BEST ✓"
			color = colorGreen + colorBold
		} else if diff >= -5 {
			diffStr = fmt.Sprintf("%.1f%%", diff)
			color = colorYellow
		} else {
			diffStr = fmt.Sprintf("%.1f%%", diff)
			color = colorRed
		}

		bar := createProgressBar(pct, 10)

		fmt.Printf("%s│ %-23s │ %12s │ %14s │ %s%-11s%s │ %s%s\n",
			colorCyan,
			r.Name,
			formatDuration(r.Duration),
			formatNumber(r.TasksPerSecond)+" /s",
			color, diffStr, colorCyan,
			bar,
			colorReset,
		)
	}

	fmt.Printf("%s└─────────────────────────┴──────────────┴────────────────┴─────────────┴────────────┘%s\n", colorCyan, colorReset)

	// Print detailed stats
	fmt.Printf("\n%sDetailed Statistics:%s\n", colorBold, colorReset)
	for i, r := range results {
		var icon string
		if i == bestIdx {
			icon = "🏆"
		} else {
			icon = "  "
		}

		tasksPerWorker := r.TasksPerSecond / float64(r.Workers)
		fmt.Printf("%s %s%-15s%s: %s%.2f tasks/sec/worker%s\n",
			icon,
			colorBlue,
			r.Name,
			colorReset,
			colorWhite,
			tasksPerWorker,
			colorReset,
		)
	}
}

func createProgressBar(percentage float64, width int) string {
	filled := int((percentage / 100.0) * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled)
	empty := strings.Repeat("░", width-filled)

	var color string
	if percentage >= 95 {
		color = colorGreen
	} else if percentage >= 80 {
		color = colorYellow
	} else {
		color = colorRed
	}

	return fmt.Sprintf("%s%s%s%s%s", color, bar, empty, colorReset, colorCyan+"│"+colorReset)
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%d μs", d.Microseconds())
	} else if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

func formatNumber(n float64) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.2fM", n/1000000)
	} else if n >= 1000 {
		return fmt.Sprintf("%.2fK", n/1000)
	}
	return fmt.Sprintf("%.0f", n)
}

func printHeader() {
	fmt.Printf("\n%s%s", colorBold+colorCyan, strings.Repeat("═", 88))
	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                                                                                    ║\n")
	fmt.Printf("║                    🚀 WORKER POOL SCHEDULING STRATEGY COMPARISON 🚀                ║\n")
	fmt.Printf("║                                                                                    ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("%s%s\n", strings.Repeat("═", 88), colorReset)

	fmt.Printf("%sSystem Info:%s CPU Cores: %d, GOMAXPROCS: %d\n",
		colorBold, colorReset,
		runtime.NumCPU(),
		runtime.GOMAXPROCS(0),
	)
	fmt.Printf("%s%s%s\n", colorCyan, strings.Repeat("─", 88), colorReset)
}

func printScenarioHeader(name, description string) {
	fmt.Printf("\n%s╔═══ %s%s %s%s═══╗%s\n",
		colorPurple+colorBold,
		colorWhite,
		name,
		colorPurple,
		strings.Repeat("═", 88-len(name)-8),
		colorReset,
	)
	fmt.Printf("%s║ %s%s%s%-82s%s║%s\n",
		colorPurple,
		colorWhite,
		description,
		colorPurple,
		"",
		colorPurple,
		colorReset,
	)
	fmt.Printf("%s╚%s╝%s\n",
		colorPurple,
		strings.Repeat("═", 86),
		colorReset,
	)
}

func printFooter() {
	fmt.Printf("\n%s%s", colorCyan+colorBold, strings.Repeat("═", 88))
	fmt.Printf("\n")
	fmt.Printf("╔════════════════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                                 📊 SUMMARY 📊                                      ║\n")
	fmt.Printf("╚════════════════════════════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("%s%s\n", strings.Repeat("═", 88), colorReset)

	fmt.Printf("\n%sStrategy Recommendations (based on results):%s\n", colorBold, colorReset)
	fmt.Printf("  %s▸ Channel%s:        Best for CPU-bound workloads and general use (simple & fast)\n", colorGreen, colorReset)
	fmt.Printf("  %s▸ MPMC%s:           Best for variable complexity tasks and concurrent submitters\n", colorBlue, colorReset)
	fmt.Printf("  %s▸ Work Stealing%s:  Best for I/O-bound workloads (workers can steal while others wait)\n", colorYellow, colorReset)

	fmt.Printf("\n%sKey Insights:%s\n", colorBold, colorReset)
	fmt.Printf("  • Channel strategy performs best for lightweight CPU tasks\n")
	fmt.Printf("  • Work-stealing has overhead that's noticeable with very fast tasks\n")
	fmt.Printf("  • Optimal worker count ≈ number of CPU cores (%d on this system)\n", runtime.NumCPU())
	fmt.Printf("  • Beyond CPU core count, efficiency drops due to contention\n")

	fmt.Printf("\n%s%s%s\n", colorCyan, strings.Repeat("═", 88), colorReset)
	fmt.Printf("%s✨ Benchmark completed! ✨%s\n\n", colorGreen+colorBold, colorReset)
}

// Workload functions
func cpuBoundLight(ctx context.Context, task int) (int, error) {
	result := 0
	for i := range 1000 {
		result += i * task
	}
	return result, nil
}

func cpuBoundHeavy(ctx context.Context, task int) (int, error) {
	result := 0
	for i := range 10000 {
		result += i * task
	}
	return result, nil
}

func variableComplexity(ctx context.Context, task int) (int, error) {
	// Every 10th task is 10x more expensive
	iterations := 1000
	if task%10 == 0 {
		iterations = 10000
	}

	result := 0
	for i := 0; i < iterations; i++ {
		result += i * task
	}
	return result, nil
}

func ioBoundWork(ctx context.Context, task int) (int, error) {
	delay := time.Duration(1+task%5) * time.Millisecond
	select {
	case <-time.After(delay):
		return task * 2, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Advanced example: Real-world data processing simulation
func dataProcessingPipeline(ctx context.Context, task int) (int, error) {
	// Simulate: Extract -> Transform -> Load

	// Extract (read from source)
	time.Sleep(100 * time.Microsecond)

	// Transform (CPU work)
	result := task
	for i := 0; i < 5000; i++ {
		result = (result*31 + i) % 1000000
	}

	// Load (write to destination)
	time.Sleep(100 * time.Microsecond)

	return result, nil
}

// Example with concurrent submitters (shows MPMC advantage)
func runConcurrentSubmittersDemo() {
	fmt.Printf("\n%s╔═══ Concurrent Submitters Test ═══╗%s\n", colorPurple+colorBold, colorReset)
	fmt.Printf("%sThis test simulates multiple goroutines submitting tasks concurrently%s\n", colorWhite, colorReset)

	submitters := 10
	tasksPerSubmitter := 1000
	workers := 8

	strategies := []struct {
		name     string
		strategy pool.SchedulingStrategyType
	}{
		{"Channel", pool.SchedulingChannel},
		{"MPMC", pool.SchedulingMPMC},
	}

	for _, s := range strategies {
		opts := []pool.WorkerPoolOption{
			pool.WithWorkerCount(workers),
			pool.WithSchedulingStrategy(s.strategy),
		}

		if s.strategy == pool.SchedulingMPMC {
			opts = append(opts, pool.WithMPMCQueue(pool.WithBoundedQueue(tasksPerSubmitter*submitters)))
		}

		wp := pool.NewWorkerPool[int, int](opts...)

		var completed atomic.Int64
		start := time.Now()

		// Spawn multiple submitters
		done := make(chan bool, submitters)
		for i := 0; i < submitters; i++ {
			go func(id int) {
				tasks := make([]int, tasksPerSubmitter)
				for j := range tasks {
					tasks[j] = id*tasksPerSubmitter + j
				}

				_, _ = wp.Process(context.Background(), tasks, func(ctx context.Context, task int) (int, error) {
					completed.Add(1)
					result := 0
					for i := 0; i < 100; i++ {
						result += i
					}
					return result, nil
				})

				done <- true
			}(i)
		}

		// Wait for all submitters
		for i := 0; i < submitters; i++ {
			<-done
		}

		duration := time.Since(start)
		total := completed.Load()
		throughput := float64(total) / duration.Seconds()

		fmt.Printf("  %s%-15s%s: %s%8.2f tasks/sec%s (%d submitters, %d total tasks)\n",
			colorBlue, s.name, colorReset,
			colorGreen, throughput, colorReset,
			submitters, total,
		)
	}
}
