package runner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTopNFunctions = 5
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

	// 9. Profile analysis if enabled
	if hasProfileAnalysis(args) && hasCPUProfile(args) {
		profilePath := getCPUProfilePath(args)
		profileDir := filepath.Join("..", filepath.Dir(profilePath))
		topN := getTopN(args)

		fmt.Println()
		colorPrintLn(Bold, "🔍 Starting CPU Profile Analysis...")
		fmt.Println()

		analysisResult, err := AnalyzeStrategies(profileDir, strategies, topN)
		if err != nil {
			colorPrintf(Red, "⚠️  Profile analysis failed: %v\n", err)
			return nil
		}

		renderProfileAnalysis(analysisResult)
	}

	return nil
}

func printHeader(title string) {
	colorPrintLn(Bold, "╔════════════════════════════════════════════════════════════╗")
	colorPrintf(Bold, "║       %-52s ║\n", title)
	colorPrintLn(Bold, "╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// injectStrategyProfile replaces {strategy} placeholder in profile paths with actual strategy filename
func injectStrategyProfile(args []string, strategy string) []string {
	result := make([]string, 0, len(args))
	strategyFilename := StrategyNameToFilename(strategy)

	for _, arg := range args {
		if after, ok := strings.CutPrefix(arg, "-cpuprofile="); ok {
			path := after
			strategyPath := strings.ReplaceAll(path, "{strategy}", strategyFilename)
			result = append(result, "-cpuprofile="+strategyPath)
			continue
		}

		if after0, ok0 := strings.CutPrefix(arg, "-memprofile="); ok0 {
			path := after0
			strategyPath := strings.ReplaceAll(path, "{strategy}", strategyFilename)
			result = append(result, "-memprofile="+strategyPath)
		} else {
			result = append(result, arg)
		}
	}
	return result
}

// runSingleStrategy runs a single strategy and returns the result
func runSingleStrategy(strategy string, args []string, config BenchmarkConfig) RunResult {
	result := RunResult{
		Strategy: strategy,
		Success:  false,
	}

	profileArgs := injectStrategyProfile(args, strategy)
	cmdArgs := append([]string{"run", ".", "-strategy=" + strategy, "--output-format=json"}, profileArgs...)
	colorPrintf(Yellow, "  Running: %s\n", strategy)

	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = ".."

	output, err := cmd.CombinedOutput()
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("Command failed: %v\nOutput: %s", err, string(output))
		Red.Printf("    ✗ Failed: %v\n", err)
		if len(output) > 0 {
			fmt.Printf("      Output: %s\n", string(output))
		}
		return result
	}

	parsed, parseErr := config.ParseOutput(string(output))
	if parseErr != nil {
		result.ErrorMsg = fmt.Sprintf("Failed to parse output: %v", parseErr)
		colorPrintf(Red, "    ✗ Parse error: %v\n", parseErr)
		if len(output) > 0 {
			debugOut := string(output)
			if len(debugOut) > 500 {
				debugOut = debugOut[:500] + "..."
			}
			fmt.Printf("      Output preview: %q\n", debugOut)
		}
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

// hasProfileAnalysis checks if profile analysis is enabled in args
func hasProfileAnalysis(args []string) bool {
	for _, arg := range args {
		if arg == "-profile-analysis=true" || arg == "-profile-analysis" {
			return true
		}
	}
	return false
}

// hasCPUProfile checks if CPU profile is enabled in args
func hasCPUProfile(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-cpuprofile=") && strings.TrimPrefix(arg, "-cpuprofile=") != "" {
			return true
		}
	}
	return false
}

// getCPUProfilePath extracts the CPU profile path from args
func getCPUProfilePath(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-cpuprofile=") {
			path := strings.TrimPrefix(arg, "-cpuprofile=")
			return strings.ReplaceAll(path, "{strategy}", "channel")
		}
	}
	return ""
}

// getTopN extracts the top-n value from args
func getTopN(args []string) int {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-top-n=") {
			nStr := strings.TrimPrefix(arg, "-top-n=")
			if n, err := strconv.Atoi(nStr); err == nil {
				return n
			}
		}
	}
	return defaultTopNFunctions
}
