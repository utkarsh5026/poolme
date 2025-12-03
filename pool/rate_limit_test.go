package pool

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_RateLimit_BasicThroughput(t *testing.T) {
	// Test that rate limiting actually limits throughput
	tasksPerSecond := 10.0
	burst := 5
	numTasks := 25

	pool := NewWorkerPool[int, int](
		WithWorkerCount(10),
		WithRateLimit(tasksPerSecond, burst),
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	tasks := make([]int, numTasks)
	for i := range tasks {
		tasks[i] = i
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// With 25 tasks at 10 tasks/sec (burst 5), we expect:
	// - First 5 tasks process immediately (burst)
	// - Remaining 20 tasks at 10/sec = 2 seconds
	// Total: ~2 seconds minimum
	expectedMinDuration := 2 * time.Second

	if elapsed < expectedMinDuration {
		t.Errorf("expected at least %v, got %v (rate limiting not working properly)", expectedMinDuration, elapsed)
	}

	// Should not take more than 3 seconds (allowing some overhead)
	expectedMaxDuration := 3 * time.Second
	if elapsed > expectedMaxDuration {
		t.Errorf("took too long: %v (expected less than %v)", elapsed, expectedMaxDuration)
	}
}

func TestWorkerPool_RateLimit_BurstBehavior(t *testing.T) {
	// Test that burst allows initial quick processing
	tasksPerSecond := 5.0
	burst := 10
	numTasks := 10

	pool := NewWorkerPool[int, int](
		WithWorkerCount(10),
		WithRateLimit(tasksPerSecond, burst),
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	tasks := make([]int, numTasks)
	for i := range tasks {
		tasks[i] = i
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// With burst=10 and 10 tasks, all should process quickly via burst
	expectedMaxDuration := 500 * time.Millisecond
	if elapsed > expectedMaxDuration {
		t.Errorf("burst should allow fast processing, took %v (expected less than %v)", elapsed, expectedMaxDuration)
	}
}

func TestWorkerPool_RateLimit_WithContextCancellation(t *testing.T) {
	// Test that rate limiting respects context cancellation
	pool := NewWorkerPool[int, int](
		WithWorkerCount(5),
		WithRateLimit(2, 1), // Very slow: 2 tasks/sec
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	tasks := make([]int, 100)
	for i := range tasks {
		tasks[i] = i
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := pool.Process(ctx, tasks, processFn)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}

	// Should stop around 500ms due to context timeout
	if elapsed > time.Second {
		t.Errorf("should have stopped due to context, but took %v", elapsed)
	}
}

func TestWorkerPool_RateLimit_WithoutRateLimiting(t *testing.T) {
	// Test that pool works normally without rate limiting (backwards compatibility)
	pool := NewWorkerPool[int, int](
		WithWorkerCount(10),
		// No rate limit configured
	)

	numTasks := 100
	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	tasks := make([]int, numTasks)
	for i := range tasks {
		tasks[i] = i
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// Without rate limiting, 100 tasks should complete very quickly
	expectedMaxDuration := 100 * time.Millisecond
	if elapsed > expectedMaxDuration {
		t.Errorf("without rate limiting, tasks should complete quickly; took %v", elapsed)
	}

	// Verify results
	for i, task := range tasks {
		expected := task * 2
		if results[i] != expected {
			t.Errorf("task %d: expected %d, got %d", i, expected, results[i])
		}
	}
}

func TestWorkerPool_RateLimit_ProcessMap(t *testing.T) {
	// Test rate limiting works with ProcessMap
	tasksPerSecond := 10.0
	burst := 3
	numTasks := 20

	pool := NewWorkerPool[int, int](
		WithWorkerCount(5),
		WithRateLimit(tasksPerSecond, burst),
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 3, nil
	}

	tasks := make(map[string]int)
	for i := range numTasks {
		tasks[fmt.Sprintf("task-%d", i)] = i
	}

	start := time.Now()
	results, err := pool.ProcessMap(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// With 20 tasks at 10 tasks/sec (burst 3), minimum time should be ~1.7 seconds
	expectedMinDuration := 1500 * time.Millisecond
	if elapsed < expectedMinDuration {
		t.Errorf("rate limiting should slow down processing; took %v (expected at least %v)", elapsed, expectedMinDuration)
	}
}

func TestWorkerPool_RateLimit_InvalidParameters(t *testing.T) {
	// Test that invalid rate limit parameters don't crash
	tests := []struct {
		name           string
		tasksPerSecond float64
		burst          int
		shouldLimit    bool
	}{
		{"zero rate", 0, 10, false},
		{"negative rate", -5, 10, false},
		{"zero burst", 10, 0, false},
		{"negative burst", 10, -5, false},
		{"both zero", 0, 0, false},
		{"valid params", 10, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](
				WithWorkerCount(2),
				WithRateLimit(tt.tasksPerSecond, tt.burst),
			)

			tasks := []int{1, 2, 3}
			processFn := func(ctx context.Context, task int) (int, error) {
				return task, nil
			}

			start := time.Now()
			results, err := pool.Process(context.Background(), tasks, processFn)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != len(tasks) {
				t.Fatalf("expected %d results, got %d", len(tasks), len(results))
			}

			// Invalid parameters should not apply rate limiting
			if !tt.shouldLimit && elapsed > 100*time.Millisecond {
				t.Errorf("should not have rate limiting with invalid params, but took %v", elapsed)
			}
		})
	}
}

func TestWorkerPool_RateLimit_ConcurrentWorkers(t *testing.T) {
	// Test that rate limiting works correctly with multiple concurrent workers
	tasksPerSecond := 50.0
	burst := 10
	numTasks := 100
	numWorkers := 20

	pool := NewWorkerPool[int, int](
		WithWorkerCount(numWorkers),
		WithRateLimit(tasksPerSecond, burst),
	)

	var processedCount atomic.Int32
	processFn := func(ctx context.Context, task int) (int, error) {
		processedCount.Add(1)
		return task, nil
	}

	tasks := make([]int, numTasks)
	for i := range tasks {
		tasks[i] = i
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	if processedCount.Load() != int32(numTasks) {
		t.Errorf("expected %d tasks processed, got %d", numTasks, processedCount.Load())
	}

	// With 100 tasks at 50/sec (burst 10), minimum time ~1.8 seconds
	expectedMinDuration := 1700 * time.Millisecond
	if elapsed < expectedMinDuration {
		t.Errorf("rate limiting should apply across all workers; took %v (expected at least %v)", elapsed, expectedMinDuration)
	}
}
