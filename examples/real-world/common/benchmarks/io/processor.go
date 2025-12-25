package io

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// simulateAPICall simulates an HTTP API call with variable latency
func simulateAPICall(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.3)
}

// simulateDatabaseQuery simulates a database query with connection pool contention
func simulateDatabaseQuery(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.15)
}

// simulateFileIO simulates file system I/O with seek times
func simulateFileIO(ctx context.Context, baseLatencyMs int) time.Duration {
	return simulateWithJitter(ctx, baseLatencyMs, 0.4)
}

func simulateWithJitter(ctx context.Context, baseLatencyMs int, jitterFactor float64) time.Duration {
	start := time.Now()
	jitter := float64(baseLatencyMs) * jitterFactor
	jitterRange := max(int(jitter*2), 1)
	actualLatency := max(baseLatencyMs+int(float64(rand.Intn(jitterRange))-jitter), 1)

	timer := time.NewTimer(time.Duration(actualLatency) * time.Millisecond)
	select {
	case <-timer.C:
		totalDBQueries.Add(1)
	case <-ctx.Done():
		timer.Stop()
		return time.Since(start)
	}

	return time.Since(start)
}

// performCPUWork simulates CPU computation
func performCPUWork(iterations int) time.Duration {
	start := time.Now()
	state := uint64(1)
	result := 0.0

	for i := range iterations {
		x := state
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		state = x

		if i%50 == 0 {
			result += math.Sin(float64(x)) * math.Cos(float64(i))
		}
	}
	_ = result
	return time.Since(start)
}

func processIOTask(ctx context.Context, task IOTask) (IOTaskResult, error) {
	start := time.Now()
	result := IOTaskResult{
		ID:       task.ID,
		TaskType: task.TaskType,
	}

	var ioTime time.Duration

	switch task.TaskType {
	case "api":
		ioTime = simulateAPICall(ctx, task.IOLatencyMs)
	case "database":
		ioTime = simulateDatabaseQuery(ctx, task.IOLatencyMs)
	case "file":
		ioTime = simulateFileIO(ctx, task.IOLatencyMs)
	case "mixed":
		ioTime = simulateAPICall(ctx, task.IOLatencyMs)
		cpuTime := performCPUWork(task.CPUWork)
		result.CPUTime = cpuTime
	}

	result.IOTime = ioTime
	result.ProcessTime = time.Since(start)
	return result, nil
}
