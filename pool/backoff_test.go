package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWorkerPool_Backoff_ExponentialBackoff tests that exponential backoff
// strategy is correctly applied during retries.
func TestWorkerPool_Backoff_ExponentialBackoff(t *testing.T) {
	testFunc := func(t *testing.T, s strategyConfig) {
		initialDelay := 100 * time.Millisecond
		maxDelay := 5 * time.Second

		opts := append(s.opts,
			WithRetryPolicy(4, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptTimes []time.Time
		var mu sync.Mutex

		processFn := func(ctx context.Context, task int) (int, error) {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
			return 0, errors.New("failure")
		}

		pool.Process(context.Background(), []int{1}, processFn)

		if len(attemptTimes) != 4 {
			t.Fatalf("expected 4 attempts, got %d", len(attemptTimes))
		}

		// Verify exponential backoff delays
		// Attempt 1: immediate
		// Attempt 2: wait 100ms (2^0 * 100ms)
		// Attempt 3: wait 200ms (2^1 * 100ms)
		// Attempt 4: wait 400ms (2^2 * 100ms)
		delays := []time.Duration{
			attemptTimes[1].Sub(attemptTimes[0]),
			attemptTimes[2].Sub(attemptTimes[1]),
			attemptTimes[3].Sub(attemptTimes[2]),
		}

		expectedDelays := []time.Duration{
			100 * time.Millisecond,
			200 * time.Millisecond,
			400 * time.Millisecond,
		}

		tolerance := 50 * time.Millisecond
		for i, delay := range delays {
			if delay < expectedDelays[i]-tolerance || delay > expectedDelays[i]+tolerance {
				t.Errorf("attempt %d: expected delay ~%v, got %v", i+2, expectedDelays[i], delay)
			}
		}
	}

	runStrategyTest(t, testFunc, 1)
}

// TestWorkerPool_Backoff_JitteredBackoff tests that jittered backoff
// adds randomization to prevent thundering herd.
func TestWorkerPool_Backoff_JitteredBackoff(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 100 * time.Millisecond
		maxDelay := 5 * time.Second
		jitterFactor := 0.2 // ±20% jitter

		opts := append(s.opts,
			WithRetryPolicy(3, initialDelay),
			WithJitteredBackoff(initialDelay, maxDelay, jitterFactor),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptTimes []time.Time
		var mu sync.Mutex

		processFn := func(ctx context.Context, task int) (int, error) {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
			return 0, errors.New("failure")
		}

		pool.Process(context.Background(), []int{1}, processFn)

		if len(attemptTimes) != 3 {
			t.Fatalf("expected 3 attempts, got %d", len(attemptTimes))
		}

		// Verify jittered backoff delays are within bounds
		// First delay should be around 100ms ± 20% (80ms to 120ms)
		firstDelay := attemptTimes[1].Sub(attemptTimes[0])
		expectedMin := 80 * time.Millisecond
		expectedMax := 120 * time.Millisecond
		tolerance := 30 * time.Millisecond

		if firstDelay < expectedMin-tolerance || firstDelay > expectedMax+tolerance {
			t.Errorf("first delay = %v, expected between %v and %v (with tolerance)",
				firstDelay, expectedMin, expectedMax)
		}

		// Second delay should be around 200ms ± 20% (160ms to 240ms)
		secondDelay := attemptTimes[2].Sub(attemptTimes[1])
		expectedMin = 160 * time.Millisecond
		expectedMax = 240 * time.Millisecond

		if secondDelay < expectedMin-tolerance || secondDelay > expectedMax+tolerance {
			t.Errorf("second delay = %v, expected between %v and %v (with tolerance)",
				secondDelay, expectedMin, expectedMax)
		}
	}, 1)
}

// TestWorkerPool_Backoff_DecorrelatedJitter tests AWS-style decorrelated jitter.
func TestWorkerPool_Backoff_DecorrelatedJitter(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 100 * time.Millisecond
		maxDelay := 5 * time.Second

		opts := append(s.opts,
			WithRetryPolicy(5, initialDelay),
			WithDecorrelatedJitter(initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptTimes []time.Time
		var mu sync.Mutex

		processFn := func(ctx context.Context, task int) (int, error) {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
			return 0, errors.New("failure")
		}

		pool.Process(context.Background(), []int{1}, processFn)

		if len(attemptTimes) != 5 {
			t.Fatalf("expected 5 attempts, got %d", len(attemptTimes))
		}

		// Verify decorrelated jitter delays are within bounds
		// First delay should be exactly initialDelay
		firstDelay := attemptTimes[1].Sub(attemptTimes[0])
		tolerance := 30 * time.Millisecond

		if firstDelay < initialDelay-tolerance || firstDelay > initialDelay+tolerance {
			t.Errorf("first delay = %v, expected %v", firstDelay, initialDelay)
		}

		// Subsequent delays should be random but within bounds
		for i := 2; i < len(attemptTimes); i++ {
			delay := attemptTimes[i].Sub(attemptTimes[i-1])
			if delay < initialDelay-tolerance || delay > maxDelay+tolerance {
				t.Errorf("attempt %d: delay = %v, expected between %v and %v",
					i+1, delay, initialDelay, maxDelay)
			}
		}
	}, 1)
}

// TestWorkerPool_Backoff_MaxDelayRespected tests that maxDelay is enforced.
func TestWorkerPool_Backoff_MaxDelayRespected(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 100 * time.Millisecond
		maxDelay := 300 * time.Millisecond // Low max to test capping

		opts := append(s.opts,
			WithRetryPolicy(5, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptTimes []time.Time
		var mu sync.Mutex

		processFn := func(ctx context.Context, task int) (int, error) {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
			return 0, errors.New("failure")
		}

		pool.Process(context.Background(), []int{1}, processFn)

		if len(attemptTimes) != 5 {
			t.Fatalf("expected 5 attempts, got %d", len(attemptTimes))
		}

		// Verify all delays respect maxDelay
		tolerance := 50 * time.Millisecond
		for i := 1; i < len(attemptTimes); i++ {
			delay := attemptTimes[i].Sub(attemptTimes[i-1])
			if delay > maxDelay+tolerance {
				t.Errorf("attempt %d: delay = %v exceeds maxDelay %v", i+1, delay, maxDelay)
			}
		}
	}, 1)
}

// TestWorkerPool_Backoff_SuccessAfterRetry verifies that backoff works
// correctly when a task eventually succeeds after retries.
func TestWorkerPool_Backoff_SuccessAfterRetry(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 50 * time.Millisecond
		maxDelay := 5 * time.Second

		opts := append(s.opts,
			WithRetryPolicy(5, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptCount atomic.Int32

		processFn := func(ctx context.Context, task int) (int, error) {
			count := attemptCount.Add(1)
			if count < 3 {
				return 0, errors.New("temporary failure")
			}
			return task * 2, nil
		}

		start := time.Now()
		results, err := pool.Process(context.Background(), []int{5}, processFn)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if results[0] != 10 {
			t.Errorf("expected result 10, got %d", results[0])
		}

		// Should have attempted 3 times (fail, fail, succeed)
		if attemptCount.Load() != 3 {
			t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
		}

		// Verify backoff delays were applied
		// First attempt: immediate
		// Second attempt: wait 50ms
		// Third attempt: wait 100ms
		// Total: ~150ms
		expectedMinDelay := 150 * time.Millisecond
		if elapsed < expectedMinDelay {
			t.Errorf("expected at least %v elapsed time, got %v", expectedMinDelay, elapsed)
		}
	}, 1)
}

// TestWorkerPool_Backoff_WithHooks tests that backoff works correctly
// with retry hooks.
func TestWorkerPool_Backoff_WithHooks(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 50 * time.Millisecond
		maxDelay := 5 * time.Second

		var retryAttempts []int
		var retryErrors []error
		var mu sync.Mutex

		opts := append(s.opts,
			WithRetryPolicy(4, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
			WithOnEachAttempt(func(task int, attempt int, err error) {
				mu.Lock()
				retryAttempts = append(retryAttempts, attempt)
				retryErrors = append(retryErrors, err)
				mu.Unlock()
			}),
		)

		pool := NewWorkerPool[int, int](opts...)

		expectedErr := errors.New("persistent failure")
		processFn := func(ctx context.Context, task int) (int, error) {
			return 0, expectedErr
		}

		_, err := pool.Process(context.Background(), []int{1}, processFn)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Verify hooks were called for each retry (but not for the last attempt)
		if len(retryAttempts) != 3 {
			t.Errorf("expected 3 retry hook calls, got %d", len(retryAttempts))
		}

		// Verify hook was called with correct attempt numbers
		expectedAttempts := []int{1, 2, 3}
		for i, attempt := range retryAttempts {
			if attempt != expectedAttempts[i] {
				t.Errorf("retry %d: expected attempt %d, got %d", i, expectedAttempts[i], attempt)
			}
		}

		// Verify all errors are the same
		for i, err := range retryErrors {
			if !errors.Is(err, expectedErr) {
				t.Errorf("retry %d: expected error %v, got %v", i, expectedErr, err)
			}
		}
	}, 1)
}

// TestWorkerPool_Backoff_ContextCancellationDuringBackoff tests that
// context cancellation is respected during backoff delays.
func TestWorkerPool_Backoff_ContextCancellationDuringBackoff(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 1 * time.Second // Long delay
		maxDelay := 10 * time.Second

		opts := append(s.opts,
			WithRetryPolicy(5, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		ctx, cancel := context.WithCancel(context.Background())
		var attemptCount atomic.Int32

		processFn := func(ctx context.Context, task int) (int, error) {
			count := attemptCount.Add(1)
			if count == 1 {
				// Cancel context after first failure
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()
			}
			return 0, errors.New("failure")
		}

		start := time.Now()
		_, err := pool.Process(ctx, []int{1}, processFn)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}

		// Should stop retrying when context is cancelled
		if attemptCount.Load() > 2 {
			t.Errorf("expected at most 2 attempts before cancellation, got %d", attemptCount.Load())
		}

		// Should cancel quickly, not wait for full backoff delay
		if elapsed > 500*time.Millisecond {
			t.Errorf("expected fast cancellation, but took %v", elapsed)
		}
	}, 1)
}

// TestWorkerPool_Backoff_MultipleTasksIndependentBackoff tests that
// multiple tasks have independent backoff sequences.
func TestWorkerPool_Backoff_MultipleTasksIndependentBackoff(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		initialDelay := 50 * time.Millisecond
		maxDelay := 5 * time.Second

		opts := append(s.opts,
			WithRetryPolicy(3, initialDelay),
			WithBackoff(BackoffExponential, initialDelay, maxDelay),
		)

		pool := NewWorkerPool[int, int](opts...)

		var attemptCounts sync.Map // map[int]int32

		processFn := func(ctx context.Context, task int) (int, error) {
			val, _ := attemptCounts.LoadOrStore(task, new(atomic.Int32))
			count := val.(*atomic.Int32).Add(1)

			// Task 1 succeeds on first attempt
			if task == 1 {
				return task * 2, nil
			}
			// Task 2 succeeds on second attempt
			if task == 2 && count >= 2 {
				return task * 2, nil
			}
			// Task 3 always fails
			if task == 3 {
				return 0, errors.New("always fails")
			}

			return 0, errors.New("temporary failure")
		}

		_, err := pool.Process(context.Background(), []int{1, 2, 3}, processFn)
		if err == nil {
			t.Fatal("expected error from task 3, got nil")
		}

		// Verify each task had independent retry sequences
		val1, _ := attemptCounts.Load(1)
		if count1 := val1.(*atomic.Int32).Load(); count1 != 1 {
			t.Errorf("task 1: expected 1 attempt, got %d", count1)
		}

		val2, _ := attemptCounts.Load(2)
		if count2 := val2.(*atomic.Int32).Load(); count2 != 2 {
			t.Errorf("task 2: expected 2 attempts, got %d", count2)
		}

		val3, _ := attemptCounts.Load(3)
		if count3 := val3.(*atomic.Int32).Load(); count3 != 3 {
			t.Errorf("task 3: expected 3 attempts, got %d", count3)
		}
	}, 3)
}

// TestWorkerPool_Backoff_WithContinueOnError tests that backoff works
// correctly when continueOnError is enabled.
func TestWorkerPool_Backoff_WithContinueOnError(t *testing.T) {
	initialDelay := 20 * time.Millisecond
	maxDelay := 5 * time.Second

	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithContinueOnError(true),
		WithRetryPolicy(3, initialDelay),
		WithBackoff(BackoffExponential, initialDelay, maxDelay),
	)

	var attemptCounts sync.Map

	processFn := func(ctx context.Context, task int) (int, error) {
		val, _ := attemptCounts.LoadOrStore(task, new(atomic.Int32))
		count := val.(*atomic.Int32).Add(1)

		// Task 1 succeeds immediately
		if task == 1 {
			return task * 2, nil
		}
		// Task 2 fails after all retries
		if task == 2 {
			return 0, errors.New("always fails")
		}
		// Task 3 succeeds on second attempt
		if task == 3 && count >= 2 {
			return task * 2, nil
		}

		return 0, errors.New("temporary failure")
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), []int{1, 2, 3}, processFn)
	elapsed := time.Since(start)

	// Should return an error but process all tasks
	if err == nil {
		t.Error("expected error from task 2")
	}

	// Verify results
	if results[0] != 2 {
		t.Errorf("task 1: expected 2, got %d", results[0])
	}
	if results[2] != 6 {
		t.Errorf("task 3: expected 6, got %d", results[2])
	}

	// Verify retry counts
	val1, _ := attemptCounts.Load(1)
	if count1 := val1.(*atomic.Int32).Load(); count1 != 1 {
		t.Errorf("task 1: expected 1 attempt, got %d", count1)
	}

	val2, _ := attemptCounts.Load(2)
	if count2 := val2.(*atomic.Int32).Load(); count2 != 3 {
		t.Errorf("task 2: expected 3 attempts, got %d", count2)
	}

	val3, _ := attemptCounts.Load(3)
	if count3 := val3.(*atomic.Int32).Load(); count3 != 2 {
		t.Errorf("task 3: expected 2 attempts, got %d", count3)
	}

	// Verify backoff delays were applied
	// Task 2: 3 attempts with delays of 20ms, 40ms = 60ms total
	// Task 3: 2 attempts with delay of 20ms = 20ms total
	// Total minimum: ~60ms (parallel execution)
	expectedMinDelay := 60 * time.Millisecond
	if elapsed < expectedMinDelay {
		t.Errorf("expected at least %v elapsed time, got %v", expectedMinDelay, elapsed)
	}
}

// TestWorkerPool_Backoff_DifferentBackoffTypes tests that different
// backoff types can be configured and behave differently.
func TestWorkerPool_Backoff_DifferentBackoffTypes(t *testing.T) {
	tests := []struct {
		name        string
		setupPool   func() *WorkerPool[int, int]
		description string
	}{
		{
			name: "exponential backoff",
			setupPool: func() *WorkerPool[int, int] {
				return NewWorkerPool[int, int](
					WithWorkerCount(1),
					WithRetryPolicy(3, 50*time.Millisecond),
					WithBackoff(BackoffExponential, 50*time.Millisecond, 5*time.Second),
				)
			},
			description: "should use exponential backoff",
		},
		{
			name: "jittered backoff",
			setupPool: func() *WorkerPool[int, int] {
				return NewWorkerPool[int, int](
					WithWorkerCount(1),
					WithRetryPolicy(3, 50*time.Millisecond),
					WithJitteredBackoff(50*time.Millisecond, 5*time.Second, 0.2),
				)
			},
			description: "should use jittered backoff with ±20% randomization",
		},
		{
			name: "decorrelated jitter",
			setupPool: func() *WorkerPool[int, int] {
				return NewWorkerPool[int, int](
					WithWorkerCount(1),
					WithRetryPolicy(3, 50*time.Millisecond),
					WithDecorrelatedJitter(50*time.Millisecond, 5*time.Second),
				)
			},
			description: "should use AWS-style decorrelated jitter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := tt.setupPool()

			var attemptCount atomic.Int32
			processFn := func(ctx context.Context, task int) (int, error) {
				attemptCount.Add(1)
				return 0, errors.New("failure")
			}

			start := time.Now()
			_, err := pool.Process(context.Background(), []int{1}, processFn)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Should attempt 3 times
			if attemptCount.Load() != 3 {
				t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
			}

			// Should have some delay from backoff
			minExpectedDelay := 50 * time.Millisecond
			if elapsed < minExpectedDelay {
				t.Errorf("expected at least %v elapsed time, got %v", minExpectedDelay, elapsed)
			}
		})
	}
}

// TestWorkerPool_Backoff_WithRateLimit tests that backoff and rate limiting
// work together correctly.
func TestWorkerPool_Backoff_WithRateLimit(t *testing.T) {
	initialDelay := 50 * time.Millisecond
	maxDelay := 5 * time.Second

	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithRetryPolicy(3, initialDelay),
		WithBackoff(BackoffExponential, initialDelay, maxDelay),
		WithRateLimit(10, 5), // 10 tasks/sec, burst 5
	)

	var attemptCount atomic.Int32
	processFn := func(ctx context.Context, task int) (int, error) {
		count := attemptCount.Add(1)
		if count <= 2 {
			return 0, errors.New("temporary failure")
		}
		return task * 2, nil
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), []int{1}, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0] != 2 {
		t.Errorf("expected result 2, got %d", results[0])
	}

	// Should have attempted 3 times with backoff delays
	if attemptCount.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
	}

	// Verify both rate limiting and backoff delays were applied
	// Rate limit: ~200ms for 3 attempts at 10 tasks/sec
	// Backoff: 50ms + 100ms = 150ms
	// Total should be sum of both
	expectedMinDelay := 150 * time.Millisecond
	if elapsed < expectedMinDelay {
		t.Errorf("expected at least %v elapsed time, got %v", expectedMinDelay, elapsed)
	}
}

// TestWorkerPool_Backoff_ZeroInitialDelay tests that backoff works
// correctly when initial delay is zero.
func TestWorkerPool_Backoff_ZeroInitialDelay(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(1),
		WithRetryPolicy(3, 0), // Zero initial delay
		WithBackoff(BackoffExponential, 0, 5*time.Second),
	)

	var attemptCount atomic.Int32
	processFn := func(ctx context.Context, task int) (int, error) {
		attemptCount.Add(1)
		return 0, errors.New("failure")
	}

	start := time.Now()
	pool.Process(context.Background(), []int{1}, processFn)
	elapsed := time.Since(start)

	// Should attempt 3 times
	if attemptCount.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
	}

	// Should complete very quickly with no backoff
	if elapsed > 50*time.Millisecond {
		t.Errorf("expected fast execution with no backoff, but took %v", elapsed)
	}
}

// TestWorkerPool_Backoff_HighConcurrency tests that backoff works
// correctly under high concurrency.
func TestWorkerPool_Backoff_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high concurrency test in short mode")
	}

	initialDelay := 10 * time.Millisecond
	maxDelay := 1 * time.Second

	pool := NewWorkerPool[int, int](
		WithWorkerCount(10),
		WithRetryPolicy(3, initialDelay),
		WithBackoff(BackoffDecorrelated, initialDelay, maxDelay),
	)

	taskCount := 100
	tasks := make([]int, taskCount)
	for i := range tasks {
		tasks[i] = i
	}

	var attemptCounts sync.Map
	var successCount atomic.Int32

	processFn := func(ctx context.Context, task int) (int, error) {
		val, _ := attemptCounts.LoadOrStore(task, new(atomic.Int32))
		count := val.(*atomic.Int32).Add(1)

		// 50% of tasks fail once before succeeding
		if task%2 == 0 && count < 2 {
			return 0, errors.New("temporary failure")
		}

		successCount.Add(1)
		return task * 2, nil
	}

	start := time.Now()
	results, err := pool.Process(context.Background(), tasks, processFn)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all tasks completed
	if len(results) != taskCount {
		t.Errorf("expected %d results, got %d", taskCount, len(results))
	}

	// Verify all tasks eventually succeeded
	if successCount.Load() != int32(taskCount) {
		t.Errorf("expected %d successes, got %d", taskCount, successCount.Load())
	}

	// Should complete reasonably fast despite retries
	maxExpectedTime := 5 * time.Second
	if elapsed > maxExpectedTime {
		t.Errorf("expected completion within %v, took %v", maxExpectedTime, elapsed)
	}
}
