package cpu

import (
	"context"
	"slices"
	"time"

	"github.com/utkarsh5026/gopool/examples/real-world/common/runner"
	"github.com/utkarsh5026/gopool/pool"
)

// CPURunner implements the benchmark runner for CPU workloads
type CPURunner struct {
	strategy   string
	numWorkers int
	tasks      []Task
}

func (r *CPURunner) Run() runner.StrategyResult {
	ctx := context.Background()
	wPool := r.selectStrategy()
	start := time.Now()

	results, err := wPool.Process(ctx, r.tasks, processTask)
	if err != nil {
		_, _ = runner.Red.Printf("Error processing %s: %v\n", r.strategy, err)
		return runner.StrategyResult{Name: r.strategy}
	}

	elapsed := time.Since(start)

	taskCount := len(r.tasks)
	throughputTasksPS := float64(taskCount) / elapsed.Seconds()

	latencies := make([]time.Duration, 0, len(results))
	for _, result := range results {
		latencies = append(latencies, result.ProcessTime)
	}
	slices.Sort(latencies)

	p50, p95, p99 := calculatePercentiles(latencies)

	return runner.StrategyResult{
		Name:             r.strategy,
		TotalTime:        elapsed,
		ThroughputMBPS:   0,
		ThroughputRowsPS: throughputTasksPS,
		P50Latency:       p50,
		P95Latency:       p95,
		P99Latency:       p99,
	}
}

func (r *CPURunner) selectStrategy() *pool.WorkerPool[Task, TaskResult] {
	switch r.strategy {
	case "Work-Stealing":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithWorkStealing(),
		)
	case "MPMC Queue":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithMPMCQueue(),
		)
	case "Priority Queue":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithPriorityQueue(func(a, b Task) bool {
				return a.Complexity > b.Complexity
			}),
		)
	case "Skip List":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithSkipList(func(a, b Task) bool {
				return a.Complexity > b.Complexity
			}),
		)
	case "Bitmask":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithBitmask(),
		)
	case "LMAX Disruptor":
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
			pool.WithLmax(),
		)
	default:
		return pool.NewWorkerPool[Task, TaskResult](
			pool.WithWorkerCount(r.numWorkers),
		)
	}
}
