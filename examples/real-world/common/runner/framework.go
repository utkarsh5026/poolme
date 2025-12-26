package runner

import (
	"flag"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

// BenchmarkFramework provides a generic benchmark execution framework
type BenchmarkFramework[T any, R any] struct {
	Name          string
	AllStrategies []string

	GenerateTasks func(workload string, count int) []T
	NewRunner     func(strategy string, tasks []T, workers int) BenchmarkRunner[T, R]
	PrintConfig   func(workers int, taskCount int, workload string)
	PrintResults  func(results []StrategyResult)

	CalculateStats func(strategyName string, results []StrategyResult) StrategyResult
}

// BenchmarkRunner interface that each benchmark's runner must implement
type BenchmarkRunner[T any, R any] interface {
	Run() StrategyResult
}

// CommonFlags holds common command-line flags
type CommonFlags struct {
	Workers         int
	Tasks           int
	Strategy        string
	Iterations      int
	Warmup          int
	Workload        string
	CPUProfile      string
	MemProfile      string
	OutputFormat    string
	ProfileAnalysis bool
	TopN            int
}

// DefineCommonFlags defines common flags (but doesn't parse yet)
// Call this before defining benchmark-specific flags
func DefineCommonFlags() *CommonFlags {
	flags := &CommonFlags{}

	flag.IntVar(&flags.Workers, "workers", 0, "Number of workers (0 = NumCPU)")
	flag.IntVar(&flags.Tasks, "tasks", 100000, "Number of tasks")
	flag.StringVar(&flags.Strategy, "strategy", "", "Run single strategy (isolated mode)")
	flag.IntVar(&flags.Iterations, "iterations", 1, "Number of iterations")
	flag.IntVar(&flags.Warmup, "warmup", 0, "Number of warmup runs")
	flag.StringVar(&flags.Workload, "workload", "", "Workload type")
	flag.StringVar(&flags.CPUProfile, "cpuprofile", "", "Write CPU profile to file")
	flag.StringVar(&flags.MemProfile, "memprofile", "", "Write memory profile to file")
	flag.StringVar(&flags.OutputFormat, "output-format", "table", "Output format: 'table' or 'json'")
	flag.BoolVar(&flags.ProfileAnalysis, "profile-analysis", false, "Enable CPU profile analysis after benchmarks")
	flag.IntVar(&flags.TopN, "top-n", 5, "Number of top functions to show in profile analysis")

	return flags
}

// Run executes the complete benchmark suite
// Flags should already be defined and parsed before calling this
func (f *BenchmarkFramework[T, R]) Run(flags *CommonFlags) {
	silent := flags.OutputFormat == "json"
	cleanup := setupProfilingSilent(flags.CPUProfile, flags.MemProfile, silent)
	defer cleanup()

	numWorkers := flags.Workers
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	tasks := f.GenerateTasks(flags.Workload, flags.Tasks)

	if flags.OutputFormat != "json" {
		printBenchmarkHeader(f.Name)
		f.PrintConfig(numWorkers, len(tasks), flags.Workload)
	}

	strategies := GetStrategiesToRun(f.AllStrategies, flags.Strategy, flags.Iterations, flags.Warmup, silent)
	results := f.runStrategies(strategies, tasks, numWorkers, flags)
	if flags.OutputFormat == "json" {
		_ = OutputJSON(f.Name, results)
	} else {
		f.PrintResults(results)
	}

	// Profile analysis phase - only run in parent process, not in subprocess mode
	// Subprocess mode is when a specific strategy is selected
	if flags.ProfileAnalysis && flags.CPUProfile != "" && flags.Strategy == "" {
		profileDir := filepath.Dir(flags.CPUProfile)

		if flags.OutputFormat != "json" {
			fmt.Println()
			colorPrintLn(Bold, "🔍 Starting CPU Profile Analysis...")
			fmt.Println()
		}

		analysisResult, err := AnalyzeStrategies(profileDir, f.AllStrategies, flags.TopN)
		if err != nil {
			if flags.OutputFormat != "json" {
				colorPrintf(Red, "⚠️  Profile analysis failed: %v\n", err)
			}
			return
		}

		if flags.OutputFormat != "json" {
			renderProfileAnalysis(analysisResult)
		}
	}
}

// runStrategies executes all strategies with warmup and iterations
func (f *BenchmarkFramework[T, R]) runStrategies(
	strategies []string,
	tasks []T,
	workers int,
	flags *CommonFlags,
) []StrategyResult {
	results := make([]StrategyResult, 0, len(strategies))
	if flags.OutputFormat != "json" {
		colorPrintLn(Bold, "Running Benchmarks...")
		fmt.Println()
	}

	for _, strategy := range strategies {
		if flags.Warmup > 0 {
			for w := 0; w < flags.Warmup; w++ {
				runner := f.NewRunner(strategy, tasks, workers)
				_ = runner.Run()
				runtime.GC()
				time.Sleep(100 * time.Millisecond)
			}
		}

		res := make([]StrategyResult, 0, flags.Iterations)
		for i := 0; i < flags.Iterations; i++ {
			runner := f.NewRunner(strategy, tasks, workers)
			res = append(res, runner.Run())

			if i < flags.Iterations-1 {
				runtime.GC()
				time.Sleep(100 * time.Millisecond)
			}
		}

		var finalResult StrategyResult
		if flags.Iterations == 1 {
			finalResult = res[0]
		} else {
			if f.CalculateStats != nil {
				finalResult = f.CalculateStats(strategy, res)
			} else {
				finalResult = DefaultCalculateStats(strategy, res)
			}
			if flags.OutputFormat != "json" {
				PrintIterationStats(res)
			}
		}

		results = append(results, finalResult)
		time.Sleep(300 * time.Millisecond)
	}

	return results
}

func printBenchmarkHeader(name string) {
	colorPrintLn(Bold, "╔════════════════════════════════════════════════════════════╗")
	colorPrintf(Bold, "║       %-52s ║\n", name)
	colorPrintLn(Bold, "╚════════════════════════════════════════════════════════════╝")
	fmt.Println()
}
