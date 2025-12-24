package runner

import (
	"fmt"
	"os/exec"
	"time"
)

// Run is the main entry point for the unified runner
func Run() error {
	EnableWindowsANSI()

	benchType, err := DetectBenchmarkType()
	if err != nil {
		return fmt.Errorf("failed to detect benchmark type: %w", err)
	}

	config, err := NewBenchmarkConfig(benchType)
	if err != nil {
		return fmt.Errorf("failed to create benchmark config: %w", err)
	}

	args, err := config.ParseFlags()
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	printHeader(config.GetTitle())
	config.PrintConfiguration()

	strategies := config.GetStrategies()

	colorPrintf(Bold, "🚀 Testing %d scheduler strategies in isolated mode\n", len(strategies))
	colorPrintLn(Yellow, "   (Each strategy runs in a separate process for fair comparison)")
	fmt.Println()

	// 7. Run all strategies
	results := make([]RunResult, 0, len(strategies))
	for i, strategy := range strategies {
		Blue.Printf("[%d/%d] ", i+1, len(strategies))
		result := runSingleStrategy(strategy, args, config)
		results = append(results, result)
		fmt.Println()

		if i < len(strategies)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// 8. Render results using benchmark-specific renderer
	config.RenderResults(results)

	return nil
}

func printHeader(title string) {
	colorPrintLn(Bold, "╔════════════════════════════════════════════════════════════╗")
	colorPrintf(Bold, "║       %-52s ║\n", title)
	colorPrintLn(Bold, "╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// runSingleStrategy runs a single strategy and returns the result
func runSingleStrategy(strategy string, args []string, config BenchmarkConfig) RunResult {
	result := RunResult{
		Strategy: strategy,
		Success:  false,
	}

	cmdArgs := append([]string{"run", ".", "-strategy=" + strategy}, args...)
	colorPrintf(Yellow, "  Running: %s\n", strategy)

	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = ".."

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output))
		Red.Printf("    ✗ Failed: %v\n", err)
		return result
	}

	parsed, parseErr := config.ParseOutput(string(output))
	if parseErr != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to parse output: %v", parseErr)
		colorPrintf(Red, "    ✗ Parse error: %v\n", parseErr)
		return result
	}

	parsed.Strategy = result.Strategy
	result = parsed
	printSuccessMessage(result, config)
	return result
}

func printSuccessMessage(result RunResult, config BenchmarkConfig) {
	benchName := config.GetName()

	completedTime := result.TotalTime.Round(time.Millisecond)
	tasksPerSec := FormatNumber(int(result.ThroughputRowsPS))
	p95Latency := FormatLatency(result.P95Latency)
	p99Latency := FormatLatency(result.P99Latency)

	switch benchName {
	case "CPU":
		colorPrintf(Green, "    ✓ Completed in %v (%s tasks/s, p95: %s, p99: %s)\n",
			completedTime,
			tasksPerSec,
			p95Latency,
			p99Latency)

	case "I/O":
		colorPrintf(Green, "    ✓ Completed in %v (%s tasks/s, P95: %s, P99: %s)\n",
			completedTime,
			tasksPerSec,
			p95Latency,
			p99Latency)

	case "Pipeline":
		colorPrintf(Green, "    ✓ Completed in %v (%s tasks/s, %.1f MB/s, P95: %s)\n",
			completedTime,
			tasksPerSec,
			result.ThroughputMBPS,
			p95Latency)
	}
}
