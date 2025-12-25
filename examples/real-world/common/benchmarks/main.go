package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/utkarsh5026/gopool/examples/real-world/common/benchmarks/bench"
	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
)

func main() {
	runner.EnableWindowsANSI()

	benchType, err := detectBenchmarkType()
	if err != nil {
		log.Fatalf("Failed to detect benchmark type: %v", err)
	}

	commonFlags := runner.DefineCommonFlags()

	switch benchType {
	case "cpu":
		framework := bench.NewCPUBenchmarkFramework()
		flag.Parse()
		framework.Run(commonFlags)
	case "io":
		framework := bench.NewIOBenchmarkFramework()
		flag.Parse()
		framework.Run(commonFlags)
	case "pipeline":
		framework := bench.NewPipelineBenchmarkFramework()
		flag.Parse()
		framework.Run(commonFlags)
	default:
		log.Fatalf("Unknown benchmark type: %s", benchType)
	}
}

// detectBenchmarkType detects which benchmark based on parent directory
func detectBenchmarkType() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	parent := filepath.Base(cwd)
	if parent == "runner" {
		cwd = filepath.Dir(cwd)
		parent = filepath.Base(cwd)
	}

	switch {
	case strings.Contains(parent, "bench-io"):
		return "io", nil
	case strings.Contains(parent, "bench-pipeline"):
		return "pipeline", nil
	case parent == "bench":
		return "cpu", nil
	default:
		return "", fmt.Errorf("unknown benchmark directory: %s", parent)
	}
}
