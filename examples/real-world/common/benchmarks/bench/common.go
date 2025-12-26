package bench

import (
	"math"
	"time"

	"github.com/fatih/color"
)

// Workload type constants used across all benchmarks
const (
	WorkloadBalanced   = "balanced"
	WorkloadImbalanced = "imbalanced"
	WorkloadPriority   = "priority"

	WorkloadAPI      = "api"
	WorkloadDatabase = "database"
	WorkloadFile     = "file"
	WorkloadMixed    = "mixed"

	WorkloadETL       = "etl"
	WorkloadStreaming = "streaming"
	WorkloadBatch     = "batch"
)

// Common color definitions used for printing
var (
	bold = color.New(color.Bold)
)

// CalculatePercentiles computes P50, P95, P99 percentiles from sorted latencies.
// The input slice must be sorted in ascending order.
// Returns 0 for all percentiles if the input slice is empty.
func CalculatePercentiles(sortedLatencies []time.Duration) (p50, p95, p99 time.Duration) {
	if len(sortedLatencies) == 0 {
		return 0, 0, 0
	}

	n := len(sortedLatencies)
	p50Idx := n * 50 / 100
	p95Idx := n * 95 / 100
	p99Idx := n * 99 / 100

	if p50Idx >= n {
		p50Idx = n - 1
	}
	if p95Idx >= n {
		p95Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}

	return sortedLatencies[p50Idx], sortedLatencies[p95Idx], sortedLatencies[p99Idx]
}

func xorShift(count int, state uint32) uint32 {
	result := 0.0

	for i := range count {
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		if i%50 == 0 {
			result += math.Sin(float64(x)) * math.Cos(float64(i))
		}
	}

	return uint32(result)
}
