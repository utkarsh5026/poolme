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
		"MPMC Queue",
		"LMAX Disruptor",
		"Priority Queue",
		"Skip List",
		"Bitmask",
	}
)

func parseStrategyOutput(output string) (time.Duration, float64, float64, error) {
	lines := strings.Split(output, "\n")

	var totalTime time.Duration
	var rowsPerSec, mbPerSec float64

	// Look for completion message first (most reliable)
	for _, line := range lines {
		if strings.Contains(line, "completed in") && strings.Contains(line, "M tasks/s") {
			// Format: "✓ Channel completed in 2ms (62.7 M tasks/s)"
			// Extract the duration and throughput

			// Find "completed in" and parse what follows
			if idx := strings.Index(line, "completed in "); idx >= 0 {
				rest := line[idx+len("completed in "):]

				// Parse duration (e.g., "2ms" or "1.5s")
				var durationStr string
				var throughput float64
				n, _ := fmt.Sscanf(rest, "%s (%f M tasks/s)", &durationStr, &throughput)
				if n == 2 {
					if d, err := time.ParseDuration(durationStr); err == nil {
						totalTime = d
						rowsPerSec = throughput * 1_000_000
						break
					}
				}
			}
		}
	}

	// If not found, try parsing the table
	if totalTime == 0 {
		for _, line := range lines {
			if strings.Contains(line, "│") && !strings.Contains(line, "RANK") && !strings.Contains(line, "─") {
				parts := strings.Split(line, "│")
				if len(parts) >= 6 {
					// Try to parse time (column 2)
					timeStr := strings.TrimSpace(parts[2])
					if d, err := time.ParseDuration(timeStr); err == nil {
						totalTime = d
					}

					// Try to parse M rows/sec (column 3)
					rowsStr := strings.TrimSpace(parts[3])
					fmt.Sscanf(rowsStr, "%f", &rowsPerSec)
					rowsPerSec *= 1_000_000 // Convert from M rows/sec to rows/sec

					// Try to parse MB/sec (column 4)
					mbStr := strings.TrimSpace(parts[4])
					fmt.Sscanf(mbStr, "%f", &mbPerSec)

					if totalTime > 0 && rowsPerSec > 0 {
						break
					}
				}
			}
		}
	}

	if totalTime == 0 {
		return 0, 0, 0, fmt.Errorf("could not parse strategy output")
	}

	return totalTime, rowsPerSec, mbPerSec, nil
}

func runSingleStrategy(strategy string, args []string) RunResult {
	result := RunResult{
		Strategy: strategy,
		Success:  false,
	}

	// Build command arguments - use "." to include all package files (main.go, terminal_*.go)
	cmdArgs := append([]string{"run", ".", "-strategy=" + strategy}, args...)

	runnerYellow.Printf("  Running: %s\n", strategy)

	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = ".." // Run in parent directory where main.go is located

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output))
		runnerRed.Printf("    ✗ Failed: %v\n", err)
		return result
	}

	// Parse the output to extract metrics
	totalTime, rowsPerSec, mbPerSec, parseErr := parseStrategyOutput(string(output))
	if parseErr != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to parse output: %v", parseErr)
		runnerRed.Printf("    ✗ Parse error: %v\n", parseErr)
		return result
	}

	result.TotalTime = totalTime
	result.ThroughputRowsPS = rowsPerSec
	result.ThroughputMBps = mbPerSec
	result.Success = true

	runnerGreen.Printf("    ✓ Completed in %v (%.1f M tasks/s)\n",
		totalTime.Round(time.Millisecond),
		rowsPerSec/1_000_000)

	return result
}

func printRunnerResults(results []RunResult) {
	fmt.Println()
	runnerBold.Println("═══════════════════════════════════════════════════════════")
	runnerBold.Println("📊 TASK THROUGHPUT RESULTS (Isolated Strategy Runs)")
	runnerBold.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Filter successful results and sort by time
	successfulResults := make([]RunResult, 0)
	for _, r := range results {
		if r.Success {
			successfulResults = append(successfulResults, r)
		}
	}

	if len(successfulResults) == 0 {
		runnerRed.Println("No strategies completed successfully!")
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
	table.Header("Rank", "Strategy", "Time", "M tasks/sec", "MB/sec", "vs Fastest")

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

		table.Append(
			rankIcon,
			r.Strategy,
			r.TotalTime.Round(time.Millisecond).String(),
			fmt.Sprintf("%.1f", r.ThroughputRowsPS/1_000_000),
			fmt.Sprintf("%.1f", r.ThroughputMBps),
			vsFastestStr,
		)
	}

	table.Render()

	// Print failed strategies if any
	failedResults := make([]RunResult, 0)
	for _, r := range results {
		if !r.Success {
			failedResults = append(failedResults, r)
		}
	}

	if len(failedResults) > 0 {
		fmt.Println()
		runnerRed.Println("⚠️  Failed Strategies:")
		for _, r := range failedResults {
			runnerRed.Printf("  • %s: %s\n", r.Strategy, r.ErrorMsg)
		}
	}

	fmt.Println()
	runnerGreen.Printf("✅ Successfully tested %d/%d strategies\n", len(successfulResults), len(results))
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
	totalRowsFlag := flag.Int("rows", 65_000_000, "Total number of tasks to process")
	workersFlag := flag.Int("workers", 0, "Number of workers (0 = auto-detect, max 8)")
	chunkSizeFlag := flag.Int("chunk", 500, "Items per task chunk")
	balancedFlag := flag.Bool("balanced", true, "Balanced mode: uniform-sized chunks")
	iterationsFlag := flag.Int("iterations", 1, "Number of iterations per strategy")
	warmupFlag := flag.Int("warmup", 0, "Number of warmup iterations")
	priorityFlag := flag.Bool("priority", false, "Priority mode: reverse task order")
	flag.Parse()

	// Build arguments to pass to each strategy run
	args := []string{
		fmt.Sprintf("-rows=%d", *totalRowsFlag),
		fmt.Sprintf("-workers=%d", *workersFlag),
		fmt.Sprintf("-chunk=%d", *chunkSizeFlag),
		fmt.Sprintf("-balanced=%t", *balancedFlag),
		fmt.Sprintf("-iterations=%d", *iterationsFlag),
		fmt.Sprintf("-warmup=%d", *warmupFlag),
		fmt.Sprintf("-priority=%t", *priorityFlag),
	}

	runnerBold.Println("╔════════════════════════════════════════════════════════════╗")
	runnerBold.Println("║       Task Throughput Benchmark - Strategy Comparison      ║")
	runnerBold.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	runnerBlue.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d", *workersFlag)
	if *workersFlag == 0 {
		fmt.Printf(" (auto-detect, max 8 cores out of %d)\n", runtime.NumCPU())
	} else {
		fmt.Println()
	}
	fmt.Printf("  Total Tasks:      %s tasks to process\n", formatNumber(*totalRowsFlag))
	fmt.Printf("  Chunk Size:       %s items per task\n", formatNumber(*chunkSizeFlag))
	fmt.Printf("  Balanced Mode:    %t\n", *balancedFlag)
	fmt.Printf("  Iterations:       %d\n", *iterationsFlag)
	if *warmupFlag > 0 {
		fmt.Printf("  Warmup Runs:      %d\n", *warmupFlag)
	}
	fmt.Println()

	strategies := allStrategies

	runnerBold.Printf("🚀 Testing %d scheduler strategies in isolated mode\n", len(strategies))
	runnerYellow.Println("   (Each strategy runs in a separate process for fair comparison)")
	fmt.Println()

	results := make([]RunResult, 0, len(strategies))

	for i, strategy := range strategies {
		runnerBlue.Printf("[%d/%d] ", i+1, len(strategies))
		result := runSingleStrategy(strategy, args)
		results = append(results, result)
		fmt.Println()

		// Small delay between runs to let system stabilize
		if i < len(strategies)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	printRunnerResults(results)
}
