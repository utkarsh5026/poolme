package io

import (
	"context"
	"slices"
	"time"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
	"github.com/utkarsh5026/gopool/pool"
)

// IORunner implements the benchmark runner for I/O workloads
type IORunner struct {
	strategy   string
	numWorkers int
	tasks      []IOTask
	results    []IOTaskResult
}

func (r *IORunner) Run() runner.StrategyResult {
	ctx := context.Background()
	wPool := r.selectStrategy()
	start := time.Now()

	results, err := wPool.Process(ctx, r.tasks, processIOTask)
	if err != nil {
		runner.Red.Printf("Error processing %s: %v\n", r.strategy, err)
		return runner.StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)
	r.results = results

	latencies := make([]time.Duration, len(results))
	for i, res := range results {
		latencies[i] = res.ProcessTime
	}
	slices.Sort(latencies)

	avgLatency := elapsed / time.Duration(len(results))
	p95Latency := latencies[int(float64(len(latencies))*0.95)]
	p99Latency := latencies[int(float64(len(latencies))*0.99)]

	taskCount := len(r.tasks)
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()

	return runner.StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputRowsPS: throughputTasksPS,
		AvgLatency:       avgLatency,
		P95Latency:       p95Latency,
		P99Latency:       p99Latency,
	}
}

func (r *IORunner) selectStrategy() *pool.WorkerPool[IOTask, IOTaskResult] {
	switch r.strategy {
	case "Work-Stealing":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithPriorityQueue(func(a, b IOTask) bool {
				return a.IOLatencyMs < b.IOLatencyMs
			}),
		)
	case "Skip List":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithSkipList(func(a, b IOTask) bool {
				return a.IOLatencyMs < b.IOLatencyMs
			}),
		)
	case "Bitmask":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithLmax(),
		)
	default:
		return pool.NewWorkerPool[IOTask, IOTaskResult](
			pool.WithWorkerCount(r.numWorkers),
		)
	}
}
