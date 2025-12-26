package runner

import (
	"encoding/json"
	"flag"
	"fmt"
	"runtime"
)

// parseJSONOutput parses JSON output from benchmarks and extracts the first result
func parseJSONOutput(output string) (RunResult, error) {
	var jsonOutput JSONBenchmarkOutput

	if err := json.Unmarshal([]byte(output), &jsonOutput); err != nil {
		return RunResult{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if len(jsonOutput.Results) == 0 {
		return RunResult{}, fmt.Errorf("no results in JSON output")
	}

	sr := jsonOutput.Results[0]
	return RunResult{
		Success:          true,
		TotalTime:        sr.TotalTime,
		ThroughputRowsPS: sr.ThroughputRowsPS,
		ThroughputMBPS:   sr.ThroughputMBPS,
		AvgLatency:       sr.AvgLatency,
		P50Latency:       sr.P50Latency,
		P95Latency:       sr.P95Latency,
		P99Latency:       sr.P99Latency,
	}, nil
}

type CPUBenchmark struct {
	tasksFlag           *int
	workersFlag         *int
	complexityFlag      *int
	workloadFlag        *string
	iterationsFlag      *int
	warmupFlag          *int
	cpuProfileFlag      *string
	memProfileFlag      *string
	profileAnalysisFlag *bool
	topNFlag            *int
}

func (c *CPUBenchmark) GetName() string {
	return "CPU"
}

func (c *CPUBenchmark) GetTitle() string {
	return "Task Throughput Benchmark - Strategy Comparison"
}

func (c *CPUBenchmark) GetStrategies() []string {
	return AllStrategies
}

func (c *CPUBenchmark) ParseFlags() ([]string, error) {
	c.tasksFlag = flag.Int("tasks", 100_000, "Number of tasks to process")
	c.workersFlag = flag.Int("workers", 0, "Number of workers (0 = auto-detect)")
	c.complexityFlag = flag.Int("complexity", 10_000, "Base CPU work per task in iterations")
	c.workloadFlag = flag.String("workload", "balanced", "Workload mode: 'balanced', 'imbalanced', or 'priority'")
	c.iterationsFlag = flag.Int("iterations", 1, "Number of iterations per strategy")
	c.warmupFlag = flag.Int("warmup", 0, "Number of warmup iterations")
	c.cpuProfileFlag = flag.String("cpuprofile", "", "Write CPU profile to file")
	c.memProfileFlag = flag.String("memprofile", "", "Write memory profile to file")
	c.profileAnalysisFlag = flag.Bool("profile-analysis", false, "Enable CPU profile analysis")
	c.topNFlag = flag.Int("top-n", 5, "Number of top functions to show")
	flag.Parse()

	args := []string{
		fmt.Sprintf("-tasks=%d", *c.tasksFlag),
		fmt.Sprintf("-workers=%d", *c.workersFlag),
		fmt.Sprintf("-complexity=%d", *c.complexityFlag),
		fmt.Sprintf("-workload=%s", *c.workloadFlag),
		fmt.Sprintf("-iterations=%d", *c.iterationsFlag),
		fmt.Sprintf("-warmup=%d", *c.warmupFlag),
	}

	// Add profiling flags if specified
	if *c.cpuProfileFlag != "" {
		args = append(args, fmt.Sprintf("-cpuprofile=%s", *c.cpuProfileFlag))
	}
	if *c.memProfileFlag != "" {
		args = append(args, fmt.Sprintf("-memprofile=%s", *c.memProfileFlag))
	}
	if *c.profileAnalysisFlag {
		args = append(args, fmt.Sprintf("-profile-analysis=%v", *c.profileAnalysisFlag))
	}
	if *c.topNFlag != 5 {
		args = append(args, fmt.Sprintf("-top-n=%d", *c.topNFlag))
	}

	return args, nil
}

func (c *CPUBenchmark) ParseOutput(output string) (RunResult, error) {
	return parseJSONOutput(output)
}

func (c *CPUBenchmark) PrintConfiguration() {
	colorPrintLn(Blue, "⚙️  Configuration:")
	fmt.Printf("  Workers:          %d", *c.workersFlag)
	if *c.workersFlag == 0 {
		fmt.Printf(" (auto-detect, using %d CPU cores)\n", runtime.NumCPU())
	} else {
		fmt.Println()
	}
	fmt.Printf("  Tasks:            %s concurrent tasks\n", FormatNumber(*c.tasksFlag))
	fmt.Printf("  Complexity:       %s iterations per task\n", FormatNumber(*c.complexityFlag))
	fmt.Printf("  Workload:         %s\n", *c.workloadFlag)
	fmt.Printf("  Iterations:       %d\n", *c.iterationsFlag)
	if *c.warmupFlag > 0 {
		fmt.Printf("  Warmup Runs:      %d\n", *c.warmupFlag)
	}
	fmt.Println()
}

type IOBenchmark struct {
	tasksFlag      *int
	workersFlag    *int
	latencyFlag    *int
	cpuWorkFlag    *int
	workloadFlag   *string
	iterationsFlag *int
	warmupFlag     *int
}

func (i *IOBenchmark) GetName() string {
	return "I/O"
}

func (i *IOBenchmark) GetTitle() string {
	return "I/O Benchmark - Strategy Comparison (Isolated)"
}

func (i *IOBenchmark) GetStrategies() []string {
	return AllStrategies
}

func (i *IOBenchmark) ParseFlags() ([]string, error) {
	i.tasksFlag = flag.Int("tasks", 50_000, "Number of I/O tasks to process")
	i.workersFlag = flag.Int("workers", 0, "Number of workers (0 = auto-detect)")
	i.latencyFlag = flag.Int("latency", 50, "Average I/O latency in milliseconds")
	i.cpuWorkFlag = flag.Int("cpuwork", 1000, "CPU work iterations for mixed workload")
	i.workloadFlag = flag.String("workload", "api", "Workload type: 'api', 'database', 'file', or 'mixed'")
	i.iterationsFlag = flag.Int("iterations", 1, "Number of iterations per strategy")
	i.warmupFlag = flag.Int("warmup", 0, "Number of warmup iterations")
	flag.Parse()

	return []string{
		fmt.Sprintf("-tasks=%d", *i.tasksFlag),
		fmt.Sprintf("-workers=%d", *i.workersFlag),
		fmt.Sprintf("-latency=%d", *i.latencyFlag),
		fmt.Sprintf("-cpuwork=%d", *i.cpuWorkFlag),
		fmt.Sprintf("-workload=%s", *i.workloadFlag),
		fmt.Sprintf("-iterations=%d", *i.iterationsFlag),
		fmt.Sprintf("-warmup=%d", *i.warmupFlag),
	}, nil
}

func (i *IOBenchmark) ParseOutput(output string) (RunResult, error) {
	return parseJSONOutput(output)
}

func (i *IOBenchmark) PrintConfiguration() {
	_, _ = Blue.Println("⚙️  Configuration:")
	fmt.Printf("  Workers:          %d", *i.workersFlag)
	if *i.workersFlag == 0 {
		fmt.Printf(" (auto-detect, using %d CPU cores)\n", runtime.NumCPU())
	} else {
		fmt.Println()
	}
	fmt.Printf("  Tasks:            %s I/O operations\n", FormatNumber(*i.tasksFlag))
	fmt.Printf("  Workload:         %s\n", *i.workloadFlag)
	fmt.Printf("  Avg Latency:      %dms\n", *i.latencyFlag)
	if *i.workloadFlag == "mixed" {
		fmt.Printf("  CPU Work:         %s iterations\n", FormatNumber(*i.cpuWorkFlag))
	}
	fmt.Printf("  Iterations:       %d\n", *i.iterationsFlag)
	if *i.warmupFlag > 0 {
		fmt.Printf("  Warmup Runs:      %d\n", *i.warmupFlag)
	}
	fmt.Println()
}

type PipelineBenchmark struct {
	tasksFlag        *int
	workersFlag      *int
	recordSizeFlag   *int
	readLatencyFlag  *int
	writeLatencyFlag *int
	cpuWorkFlag      *int
	workloadFlag     *string
	iterationsFlag   *int
	warmupFlag       *int
}

func (p *PipelineBenchmark) GetName() string {
	return "Pipeline"
}

func (p *PipelineBenchmark) GetTitle() string {
	return "Pipeline Benchmark - Strategy Comparison (Isolated)"
}

func (p *PipelineBenchmark) GetStrategies() []string {
	return AllStrategies
}

func (p *PipelineBenchmark) ParseFlags() ([]string, error) {
	p.tasksFlag = flag.Int("tasks", 20_000, "Number of data records to process")
	p.workersFlag = flag.Int("workers", 0, "Number of workers (0 = auto-detect)")
	p.recordSizeFlag = flag.Int("recordsize", 10, "Average record size in KB")
	p.readLatencyFlag = flag.Int("readlatency", 10, "Base read latency in milliseconds")
	p.writeLatencyFlag = flag.Int("writelatency", 15, "Base write latency in milliseconds")
	p.cpuWorkFlag = flag.Int("cpuwork", 2000, "CPU work iterations for transformation")
	p.workloadFlag = flag.String("workload", "etl", "Workload type: 'etl', 'streaming', or 'batch'")
	p.iterationsFlag = flag.Int("iterations", 1, "Number of iterations per strategy")
	p.warmupFlag = flag.Int("warmup", 0, "Number of warmup iterations")
	flag.Parse()

	return []string{
		fmt.Sprintf("-tasks=%d", *p.tasksFlag),
		fmt.Sprintf("-workers=%d", *p.workersFlag),
		fmt.Sprintf("-recordsize=%d", *p.recordSizeFlag),
		fmt.Sprintf("-readlatency=%d", *p.readLatencyFlag),
		fmt.Sprintf("-writelatency=%d", *p.writeLatencyFlag),
		fmt.Sprintf("-cpuwork=%d", *p.cpuWorkFlag),
		fmt.Sprintf("-workload=%s", *p.workloadFlag),
		fmt.Sprintf("-iterations=%d", *p.iterationsFlag),
		fmt.Sprintf("-warmup=%d", *p.warmupFlag),
	}, nil
}

func (p *PipelineBenchmark) ParseOutput(output string) (RunResult, error) {
	return parseJSONOutput(output)
}

func (p *PipelineBenchmark) PrintConfiguration() {
	colorPrintLn(Blue, "⚙️  Configuration:")
	fmt.Printf("  Workers:          %d", *p.workersFlag)
	if *p.workersFlag == 0 {
		fmt.Printf(" (auto-detect, using %d CPU cores)\n", runtime.NumCPU())
	} else {
		fmt.Println()
	}
	fmt.Printf("  Tasks:            %s data records\n", FormatNumber(*p.tasksFlag))
	fmt.Printf("  Workload:         %s\n", *p.workloadFlag)
	fmt.Printf("  Record Size:      ~%d KB\n", *p.recordSizeFlag)
	fmt.Printf("  Read Latency:     %dms\n", *p.readLatencyFlag)
	fmt.Printf("  Write Latency:    %dms\n", *p.writeLatencyFlag)
	fmt.Printf("  CPU Work:         %s iterations\n", FormatNumber(*p.cpuWorkFlag))
	fmt.Printf("  Iterations:       %d\n", *p.iterationsFlag)
	if *p.warmupFlag > 0 {
		fmt.Printf("  Warmup Runs:      %d\n", *p.warmupFlag)
	}
	fmt.Println()
}
