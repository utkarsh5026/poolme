package runner

import (
	"time"

	"github.com/fatih/color"
)

// BenchmarkType identifies which benchmark we're running
type BenchmarkType int

const (
	BenchmarkCPU BenchmarkType = iota
	BenchmarkIO
	BenchmarkPipeline
)

// RunResult captures the result of running a single strategy (superset of all fields)
type RunResult struct {
	Strategy  string
	TotalTime time.Duration
	Rank      int
	Success   bool
	ErrorMsg  string

	ThroughputRowsPS float64 // Used by all benchmarks
	ThroughputMBPS   float64 // Used by CPU and Pipeline

	AvgLatency time.Duration // Used by I/O and Pipeline
	P50Latency time.Duration // Used by CPU only
	P95Latency time.Duration // Used by all benchmarks
	P99Latency time.Duration // Used by all benchmarks
}

// BenchmarkConfig defines the interface for benchmark-specific behavior
type BenchmarkConfig interface {
	// GetName returns the benchmark type name
	GetName() string

	// GetTitle returns the header title for the benchmark
	GetTitle() string

	// GetStrategies returns the list of strategies to test
	GetStrategies() []string

	// ParseFlags parses command-line flags and returns args for subprocess
	ParseFlags() ([]string, error)

	// ParseOutput parses the output from a single strategy run
	ParseOutput(output string) (RunResult, error)

	// RenderResults renders the final comparison table(s)
	RenderResults(results []RunResult)

	// PrintConfiguration prints the benchmark configuration
	PrintConfiguration()
}

// Color helpers (shared across all benchmarks)
var (
	Bold   = color.New(color.Bold)
	Green  = color.New(color.FgGreen)
	Red    = color.New(color.FgRed)
	Yellow = color.New(color.FgYellow)
	Blue   = color.New(color.FgBlue)
)

// AllStrategies Standard strategy list (used by all benchmarks)
var AllStrategies = []string{
	"Channel",
	"Work-Stealing",
	"LMAX Disruptor",
	"MPMC Queue",
	"Priority Queue",
	"Skip List",
	"Bitmask",
}
