package benchmarks

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/utkarsh5026/gopool/pool"
)

// strategyConfig defines a benchmark configuration for a scheduling strategy
type strategyConfig struct {
	name string
	opts []pool.WorkerPoolOption
}

// getAllStrategies returns all scheduling strategies for benchmarking
func getAllStrategies(workerCount int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithMPMCQueue(pool.WithBoundedQueue(20000)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
		{
			name: "Bitmask",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithBitmask(),
			},
		},
		{
			name: "Lmax",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithLmax(),
			},
		},
	}
}

// getBasicStrategies returns basic strategies (without MPMC unbounded)
func getBasicStrategies(workerCount int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithMPMCQueue(pool.WithBoundedQueue(20000)),
			},
		},
		{
			name: "Bitmask",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithBitmask(),
			},
		},
		{
			name: "Lmax",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithLmax(),
			},
		},
	}
}

// getCacheLocalityStrategies returns strategies best for cache locality tests
func getCacheLocalityStrategies(workerCount int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithWorkStealing(),
			},
		},
	}
}

// getPriorityStrategies returns strategies for priority queue comparison
func getPriorityStrategies(workerCount int, lessFunc func(a, b int) bool) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel_NoPriority",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "PriorityQueue",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithPriorityQueue(lessFunc),
			},
		},
		{
			name: "SkipList",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSkipList(lessFunc),
			},
		},
	}
}

// getAllStrategiesWithQueueSize returns all strategies with custom queue size for bounded queues
func getAllStrategiesWithQueueSize(workerCount, queueSize int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithSchedulingStrategy(pool.SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithWorkStealing(),
			},
		},
		{
			name: "MPMC_Bounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithMPMCQueue(pool.WithBoundedQueue(queueSize)),
			},
		},
		{
			name: "MPMC_Unbounded",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithMPMCQueue(pool.WithUnboundedQueue()),
			},
		},
		{
			name: "Bitmask",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithBitmask(),
			},
		},
		{
			name: "Lmax",
			opts: []pool.WorkerPoolOption{
				pool.WithWorkerCount(workerCount),
				pool.WithLmax(),
			},
		},
	}
}

// runStrategyBenchmark runs a benchmark function for all strategies
func runStrategyBenchmark(b *testing.B, strategies []strategyConfig, benchFunc func(b *testing.B, s strategyConfig)) {
	for _, strategy := range strategies {
		b.Run(strategy.name, func(b *testing.B) {
			benchFunc(b, strategy)
		})
	}
}

// =============================================================================
// Benchmark Workload Generators
// =============================================================================

// cpuBoundWork simulates a CPU-intensive operation
func cpuBoundWork(iterations int) func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		result := 0
		for i := range iterations {
			result += i * task
		}
		return result, nil
	}
}

// ioBoundWork simulates an I/O operation with a delay
func ioBoundWork(delay time.Duration) func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		select {
		case <-time.After(delay):
			return task * 2, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// mixedWork simulates a realistic workload with variable processing time
func mixedWork() func(ctx context.Context, task int) (int, error) {
	return func(ctx context.Context, task int) (int, error) {
		// Simulate variable processing time (0-10ms)
		delay := time.Duration(task%10) * time.Millisecond
		time.Sleep(delay)

		// Do some computation
		result := 0
		for i := range 1000 {
			result += i
		}
		return result + task, nil
	}
}

func percentile(latencies []time.Duration, p float64) time.Duration {
	if len(latencies) == 0 {
		return 0
	}

	// Create a copy and sort
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Simple bubble sort (fine for benchmark data)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i] > sorted[j] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Calculate index using the nearest-rank method
	// For p=0.50 with 100 elements, we want the 50th element (index 49)
	index := max(int(math.Round(p*float64(len(sorted)-1))), 0)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}
