package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// RunResult captures the result of running a single strategy
type RunResult struct {
	Strategy         string
	TotalTime        time.Duration
	ThroughputMBps   float64
	ThroughputRowsPS float64
	Rank             int
	Success          bool
	ErrorMsg         string
}

var (
	runnerBold   = color.New(color.Bold)
	runnerGreen  = color.New(color.FgGreen)
	runnerRed    = color.New(color.FgRed)
	runnerYellow = color.New(color.FgYellow)
	runnerBlue   = color.New(color.FgBlue)

	allStrategies = []string{
		"Channel",
		"Work-Stealing",

		"LMAX Disruptor",
		"MPMC Queue",
		"Priority Queue",
		"Skip List",
		"Bitmask",
	}
)

func parseStrategyOutput(output string) (time.Duration, float64, error) {
	lines := strings.Split(output, "\n")

	var totalTime time.Duration
	var tasksPerSec float64

	// Parse the table output - new format has: Rank | Scheduler | Time | Tasks/sec | vs Fastest
	for _, line := range lines {
		if strings.Contains(line, "│") && !strings.Contains(line, "RANK") &&
			!strings.Contains(line, "SCHEDULER") && !strings.Contains(line, "─") {
			parts := strings.Split(line, "│")
			if len(parts) >= 5 {
				// Try to parse time (column 3, index 2 after split)
				timeStr := strings.TrimSpace(parts[3])
				if d, err := time.ParseDuration(timeStr); err == nil {
					totalTime = d
				}

				// Try to parse Tasks/sec (column 4, index 3) - this is now a formatted number with commas
				tasksStr := strings.TrimSpace(parts[4])
				// Remove commas from the number
				tasksStr = strings.ReplaceAll(tasksStr, ",", "")
				if _, err := fmt.Sscanf(tasksStr, "%f", &tasksPerSec); err == nil {
					if totalTime > 0 && tasksPerSec > 0 {
						break
					}
				}
			}
		}
	}

	if totalTime == 0 {
		return 0, 0, fmt.Errorf("could not parse strategy output")
	}

	return totalTime, tasksPerSec, nil
}

func runSingleStrategy(strategy string, args []string) RunResult {
	result := RunResult{
		Strategy: strategy,
		Success:  false,
	}

	// Build command arguments - use "." to include all package files (main.go, terminal_*.go)
	cmdArgs := append([]string{"run", ".", "-strategy=" + strategy}, args...)

	_, _ = runnerYellow.Printf("  Running: %s\n", strategy)

	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = ".." // Run in parent directory where main.go is located

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output))
		_, _ = runnerRed.Printf("    ✗ Failed: %v\n", err)
		return result
	}

	totalTime, tasksPerSec, parseErr := parseStrategyOutput(string(output))
	if parseErr != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to parse output: %v", parseErr)
		_, _ = runnerRed.Printf("    ✗ Parse error: %v\n", parseErr)
		return result
	}

	result.TotalTime = totalTime
	result.ThroughputRowsPS = tasksPerSec
	result.Success = true

	_, _ = runnerGreen.Printf("    ✓ Completed in %v (%s tasks/s)\n",
		totalTime.Round(time.Millisecond),
		formatNumber(int(tasksPerSec)))

	return result
}

func printRunnerResults(results []RunResult) {
	fmt.Println()
	_, _ = runnerBold.Println("═══════════════════════════════════════════════════════════")
	_, _ = runnerBold.Println("📊 TASK THROUGHPUT RESULTS (Isolated Strategy Runs)")
	_, _ = runnerBold.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Filter successful results and sort by time
	successfulResults := make([]RunResult, 0)
	for _, r := range results {
		if r.Success {
			successfulResults = append(successfulResults, r)
		}
	}

	if len(successfulResults) == 0 {
		_, _ = runnerRed.Println("No strategies completed successfully!")
		return
	}

	sort.Slice(successfulResults, func(i, j int) bool {
		return successfulResults[i].TotalTime < successfulResults[j].TotalTime
	})

	// Assign ranks
	for i := range successfulResults {
		successfulResults[i].Rank = i + 1
	}

	fastestTime := successfulResults[0].TotalTime

	table := tablewriter.NewWriter(os.Stdout)
	table.Header("Rank", "Strategy", "Time", "Tasks/sec", "vs Fastest")

	for _, r := range successfulResults {
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
			r.Strategy,
			r.TotalTime.Round(time.Millisecond).String(),
			formatNumber(int(r.ThroughputRowsPS)),
			vsFastestStr,
		)
	}

	_ = table.Render()

	// Print failed strategies if any
	failedResults := make([]RunResult, 0)
	for _, r := range results {
		if !r.Success {
			failedResults = append(failedResults, r)
		}
	}

	if len(failedResults) > 0 {
		fmt.Println()
		_, _ = runnerRed.Println("⚠️  Failed Strategies:")
		for _, r := range failedResults {
			_, _ = runnerRed.Printf("  • %s: %s\n", r.Strategy, r.ErrorMsg)
		}
	}

	fmt.Println()
	_, _ = runnerGreen.Printf("✅ Successfully tested %d/%d strategies\n", len(successfulResults), len(results))
	fmt.Println()
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

func enableWindowsANSI() {
	// No-op for runner - colors work fine without it in most terminals
}

func main() {
	enableWindowsANSI()

	// Parse flags that we want to pass through to the main program
	tasksFlag := flag.Int("tasks", 100_000, "Number of tasks to process")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect)")
	complexityFlag := flag.Int("complexity", 10_000, "Base CPU work per task in iterations")
	workloadFlag := flag.String("workload", "balanced", "Workload mode: 'balanced', 'imbalanced', or 'priority'")
	iterationsFlag := flag.Int("iterations", 1, "Number of iterations per strategy")
	warmupFlag := flag.Int("warmup", 0, "Number of warmup iterations")
	flag.Parse()

	// Build arguments to pass to each strategy run
	args := []string{
		fmt.Sprintf("-tasks=%d", *tasksFlag),
		fmt.Sprintf("-workers=%d", *workersFlag),
		fmt.Sprintf("-complexity=%d", *complexityFlag),
		fmt.Sprintf("-workload=%s", *workloadFlag),
		fmt.Sprintf("-iterations=%d", *iterationsFlag),
		fmt.Sprintf("-warmup=%d", *warmupFlag),
	}

	_, _ = runnerBold.Println("╔════════════════════════════════════════════════════════════╗")
	_, _ = runnerBold.Println("║       Task Throughput Benchmark - Strategy Comparison      ║")
	_, _ = runnerBold.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	_, _ = runnerBlue.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d", *workersFlag)
	if *workersFlag == 0 {
		fmt.Printf(" (auto-detect, using %d CPU cores)\n", runtime.NumCPU())
	} else {
		fmt.Println()
	}
	fmt.Printf("  Tasks:            %s concurrent tasks\n", formatNumber(*tasksFlag))
	fmt.Printf("  Complexity:       %s iterations per task\n", formatNumber(*complexityFlag))
	fmt.Printf("  Workload:         %s\n", *workloadFlag)
	fmt.Printf("  Iterations:       %d\n", *iterationsFlag)
	if *warmupFlag > 0 {
		fmt.Printf("  Warmup Runs:      %d\n", *warmupFlag)
	}
	fmt.Println()

	strategies := allStrategies

	_, _ = runnerBold.Printf("🚀 Testing %d scheduler strategies in isolated mode\n", len(strategies))
	_, _ = runnerYellow.Println("   (Each strategy runs in a separate process for fair comparison)")
	fmt.Println()

	results := make([]RunResult, 0, len(strategies))

	for i, strategy := range strategies {
		_, _ = runnerBlue.Printf("[%d/%d] ", i+1, len(strategies))
		result := runSingleStrategy(strategy, args)
		results = append(results, result)
		fmt.Println()

		if i < len(strategies)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	printRunnerResults(results)
}
