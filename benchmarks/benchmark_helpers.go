package benchmarks

import (
	"testing"

	"github.com/utkarsh5026/poolme/pool"
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
