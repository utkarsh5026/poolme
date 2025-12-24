package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectBenchmarkType detects which benchmark type we're running based on the current directory
func DetectBenchmarkType() (BenchmarkType, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("failed to get current directory: %w", err)
	}

	parentDir := filepath.Base(filepath.Dir(cwd))

	switch {
	case strings.Contains(parentDir, "bench-io"):
		return BenchmarkIO, nil
	case strings.Contains(parentDir, "bench-pipeline"):
		return BenchmarkPipeline, nil
	case parentDir == "bench":
		return BenchmarkCPU, nil
	default:
		return 0, fmt.Errorf("unknown benchmark type in directory: %s", parentDir)
	}
}

// NewBenchmarkConfig creates the appropriate BenchmarkConfig based on the type
func NewBenchmarkConfig(benchType BenchmarkType) (BenchmarkConfig, error) {
	switch benchType {
	case BenchmarkCPU:
		return &CPUBenchmark{}, nil
	case BenchmarkIO:
		return &IOBenchmark{}, nil
	case BenchmarkPipeline:
		return &PipelineBenchmark{}, nil
	default:
		return nil, fmt.Errorf("unsupported benchmark type: %v", benchType)
	}
}
