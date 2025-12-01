package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Retry_SuccessOnFirstAttempt(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

		var attemptCount atomic.Int32
		processFn := func(ctx context.Context, task int) (int, error) {
			attemptCount.Add(1)
			return task * 2, nil
		}

		results, err := pool.Process(context.Background(), []int{1}, processFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if results[0] != 2 {
			t.Errorf("expected result 2, got %d", results[0])
		}

		// Should only execute once since it succeeded on first attempt
		if attemptCount.Load() != 1 {
			t.Errorf("expected 1 attempt, got %d", attemptCount.Load())
		}
	}, 2, WithRetryPolicy(3, 100*time.Millisecond))
}

func TestWorkerPool_Retry_SuccessAfterRetries(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

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

		// Should execute 3 times (fail, fail, succeed)
		if attemptCount.Load() != 3 {
			t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
		}

		// Verify exponential backoff delays were applied
		// First attempt: immediate
		// Second attempt: wait 50ms (2^0 * 50ms = 50ms)
		// Third attempt: wait 100ms (2^1 * 50ms = 100ms)
		// Total wait: ~150ms
		expectedMinDelay := 150 * time.Millisecond
		if elapsed < expectedMinDelay {
			t.Errorf("expected at least %v elapsed time for backoff, got %v", expectedMinDelay, elapsed)
		}
	}, 2, WithRetryPolicy(3, 50*time.Millisecond))
}

func TestWorkerPool_Retry_AllAttemptsFail(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

		var attemptCount atomic.Int32
		expectedErr := errors.New("persistent failure")

		processFn := func(ctx context.Context, task int) (int, error) {
			attemptCount.Add(1)
			return 0, expectedErr
		}

		_, err := pool.Process(context.Background(), []int{1}, processFn)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}

		// Should attempt exactly 3 times
		if attemptCount.Load() != 3 {
			t.Errorf("expected 3 attempts, got %d", attemptCount.Load())
		}
	}, 2, WithRetryPolicy(3, 10*time.Millisecond))
}

func TestWorkerPool_Retry_NoRetryWhenMaxAttemptsIsOne(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

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

		// Should only attempt once
		if attemptCount.Load() != 1 {
			t.Errorf("expected 1 attempt, got %d", attemptCount.Load())
		}

		// Should not wait at all
		if elapsed > 50*time.Millisecond {
			t.Errorf("expected no backoff delay, but took %v", elapsed)
		}
	}, 2, WithRetryPolicy(1, 100*time.Millisecond))
}

func TestWorkerPool_Retry_ExponentialBackoffTiming(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

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

		// Verify delays between attempts follow exponential backoff
		// Delay before attempt 1: 0 (immediate)
		// Delay before attempt 2: 100ms (2^0 * 100ms)
		// Delay before attempt 3: 200ms (2^1 * 100ms)
		// Delay before attempt 4: 400ms (2^2 * 100ms)

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
	}, 1, WithRetryPolicy(4, initialDelay))
}

func TestWorkerPool_Retry_ContextCancellationDuringBackoff(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

		ctx, cancel := context.WithCancel(context.Background())
		var attemptCount atomic.Int32

		processFn := func(ctx context.Context, task int) (int, error) {
			count := attemptCount.Add(1)
			if count == 1 {
				// Cancel context after first failure
				go func() {
					time.Sleep(50 * time.Millisecond)
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
	}, 1, WithRetryPolicy(5, 1*time.Second))
}

func TestWorkerPool_Retry_MultipleTasksIndependentRetries(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

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

		// Verify attempt counts for each task
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
	}, 3, WithRetryPolicy(3, 20*time.Millisecond))
}

func TestWorkerPool_Retry_NoDelayWhenInitialDelayIsZero(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

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
	}, 1, WithRetryPolicy(3, 0))
}

func TestWorkerPool_Retry_PanicRecoveryDoesNotRetry(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewWorkerPool[int, int](s.opts...)

		var attemptCount atomic.Int32
		processFn := func(ctx context.Context, task int) (int, error) {
			attemptCount.Add(1)
			panic("intentional panic")
		}

		_, err := pool.Process(context.Background(), []int{1}, processFn)
		if err == nil {
			t.Fatal("expected panic recovery error, got nil")
		}

		// Panic should be caught and returned as error without retries
		// Note: The panic is caught by defer before retry logic runs,
		// so it should only attempt once
		if attemptCount.Load() != 1 {
			t.Errorf("expected 1 attempt (panic should not retry), got %d", attemptCount.Load())
		}

		if !contains(err.Error(), "worker panic") {
			t.Errorf("expected panic recovery error, got: %v", err)
		}
	}, 1, WithRetryPolicy(3, 10*time.Millisecond))
}
