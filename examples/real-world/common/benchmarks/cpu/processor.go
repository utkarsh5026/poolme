package cpu

import (
	"context"
	"math"
	"time"
)

func processTask(_ context.Context, task Task) (TaskResult, error) {
	start := time.Now()
	state := uint64(task.ID)
	if state == 0 {
		state = 1
	}

	result := 0.0
	for i := 0; i < task.Complexity; i++ {
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		if i%100 == 0 {
			result += math.Sin(float64(x)) * math.Cos(float64(i))
		}
	}
	_ = result
	return TaskResult{
		ID:          task.ID,
		ProcessTime: time.Since(start),
	}, nil
}

func calculatePercentiles(sortedLatencies []time.Duration) (p50, p95, p99 time.Duration) {
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
