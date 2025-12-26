package bench

import (
	"github.com/utkarsh5026/gopool/pool"
)

// StrategyConfig holds configuration for strategy selection
type StrategyConfig[T any, R any] struct {
	NumWorkers int
	Comparator func(a, b T) bool // Optional, for priority-based strategies
}

// SelectStrategy creates a worker pool based on the strategy name.
// This function consolidates identical strategy selection logic from all benchmarks.
func SelectStrategy[T any, R any](
	strategyName string,
	config StrategyConfig[T, R],
) *pool.WorkerPool[T, R] {
	switch strategyName {
	case "Work-Stealing":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithPriorityQueue(config.Comparator),
		)
	case "Skip List":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithSkipList(config.Comparator),
		)
	case "Bitmask":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
			pool.WithLmax(),
		)
	default: // "Channel"
		return pool.NewWorkerPool[T, R](
			pool.WithWorkerCount(config.NumWorkers),
		)
	}
}
