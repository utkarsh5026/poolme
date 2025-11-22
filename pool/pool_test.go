package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Process_BasicFunctionality(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	for i, task := range tasks {
		expected := task * 2
		if results[i] != expected {
			t.Errorf("task %d: expected %d, got %d", i, expected, results[i])
		}
	}
}

func TestWorkerPool_Process_EmptyTasks(t *testing.T) {
	pool := NewWorkerPool[int, int]()

	tasks := []int{}
	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestWorkerPool_Process_SingleTask(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := []int{42}
	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0] != 84 {
		t.Errorf("expected 84, got %d", results[0])
	}
}

func TestWorkerPool_Process_ErrorHandling(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := []int{1, 2, 3, 4, 5}
	expectedErr := errors.New("processing error")

	processFn := func(ctx context.Context, task int) (int, error) {
		if task == 3 {
			return 0, expectedErr
		}
		return task * 2, nil
	}

	_, err := pool.Process(context.Background(), tasks, processFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWorkerPool_Process_ContextCancellation(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	ctx, cancel := context.WithCancel(context.Background())
	tasks := make([]int, 100)
	for i := range tasks {
		tasks[i] = i
	}

	var processedCount atomic.Int32
	processFn := func(ctx context.Context, task int) (int, error) {
		// Cancel after processing a few tasks
		if processedCount.Add(1) == 5 {
			cancel()
		}
		time.Sleep(10 * time.Millisecond) // Simulate work
		return task * 2, nil
	}

	_, err := pool.Process(ctx, tasks, processFn)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestWorkerPool_Process_ContextTimeout(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(2))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tasks := []int{1, 2, 3, 4, 5}
	processFn := func(ctx context.Context, task int) (int, error) {
		time.Sleep(100 * time.Millisecond) // Exceed timeout
		return task * 2, nil
	}

	_, err := pool.Process(ctx, tasks, processFn)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestWorkerPool_Process_PanicRecovery(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := []int{1, 2, 3, 4, 5}
	processFn := func(ctx context.Context, task int) (int, error) {
		if task == 3 {
			panic("intentional panic")
		}
		return task * 2, nil
	}

	_, err := pool.Process(context.Background(), tasks, processFn)
	if err == nil {
		t.Fatal("expected panic recovery error, got nil")
	}

	errStr := err.Error()
	if !contains(errStr, "worker panic") || !contains(errStr, "intentional panic") {
		t.Errorf("expected panic recovery error message, got: %v", err)
	}
}

func TestWorkerPool_Process_Concurrency(t *testing.T) {
	workerCount := 4
	pool := NewWorkerPool[int, int](WithWorkerCount(workerCount))

	tasks := make([]int, 100)
	for i := range tasks {
		tasks[i] = i
	}

	var activeWorkers atomic.Int32
	var maxConcurrent atomic.Int32

	processFn := func(ctx context.Context, task int) (int, error) {
		current := activeWorkers.Add(1)
		defer activeWorkers.Add(-1)

		// Track max concurrent workers
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond) // Simulate work
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	// Verify we actually used concurrent workers
	if maxConcurrent.Load() < int32(workerCount) {
		t.Errorf("expected at least %d concurrent workers, got %d", workerCount, maxConcurrent.Load())
	}
}

func TestWorkerPool_ProcessMap_BasicFunctionality(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
		"d": 4,
		"e": 5,
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.ProcessMap(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}

	for key, task := range tasks {
		expected := task * 2
		if result, ok := results[key]; !ok {
			t.Errorf("missing result for key %s", key)
		} else if result != expected {
			t.Errorf("key %s: expected %d, got %d", key, expected, result)
		}
	}
}

func TestWorkerPool_ProcessMap_EmptyMap(t *testing.T) {
	pool := NewWorkerPool[int, int]()

	tasks := map[string]int{}
	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.ProcessMap(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestWorkerPool_ProcessMap_ErrorHandling(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	expectedErr := errors.New("processing error")
	processFn := func(ctx context.Context, task int) (int, error) {
		if task == 2 {
			return 0, expectedErr
		}
		return task * 2, nil
	}

	_, err := pool.ProcessMap(context.Background(), tasks, processFn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWorkerPool_ProcessStream_BasicFunctionality(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	// Create task channel
	taskChan := make(chan int, 10)
	go func() {
		defer close(taskChan)
		for i := 1; i <= 10; i++ {
			taskChan <- i
		}
	}()

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	resultChan, errChan := pool.ProcessStream(context.Background(), taskChan, processFn)

	// Collect results
	var results []int
	for result := range resultChan {
		results = append(results, result)
	}

	// Check for errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	default:
	}

	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
}

func TestWorkerPool_ProcessStream_ErrorHandling(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	taskChan := make(chan int, 5)
	go func() {
		defer close(taskChan)
		for i := 1; i <= 5; i++ {
			taskChan <- i
		}
	}()

	expectedErr := errors.New("processing error")
	processFn := func(ctx context.Context, task int) (int, error) {
		if task == 3 {
			return 0, expectedErr
		}
		return task * 2, nil
	}

	_, errChan := pool.ProcessStream(context.Background(), taskChan, processFn)

	// Wait for error
	err := <-errChan
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWorkerPool_WithOptions(t *testing.T) {
	tests := []struct {
		name        string
		opts        []WorkerPoolOption
		wantWorkers int
		wantBuffer  int
	}{
		{
			name:        "default options",
			opts:        nil,
			wantWorkers: -1, // Will use runtime.GOMAXPROCS(0)
			wantBuffer:  -1, // Will equal worker count
		},
		{
			name:        "custom worker count",
			opts:        []WorkerPoolOption{WithWorkerCount(8)},
			wantWorkers: 8,
			wantBuffer:  8,
		},
		{
			name:        "custom buffer size",
			opts:        []WorkerPoolOption{WithTaskBuffer(16)},
			wantWorkers: -1,
			wantBuffer:  16,
		},
		{
			name:        "custom worker and buffer",
			opts:        []WorkerPoolOption{WithWorkerCount(4), WithTaskBuffer(32)},
			wantWorkers: 4,
			wantBuffer:  32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](tt.opts...)

			if tt.wantWorkers > 0 && pool.workerCount != tt.wantWorkers {
				t.Errorf("expected %d workers, got %d", tt.wantWorkers, pool.workerCount)
			}

			if tt.wantBuffer > 0 && pool.taskBuffer != tt.wantBuffer {
				t.Errorf("expected buffer size %d, got %d", tt.wantBuffer, pool.taskBuffer)
			}
		})
	}
}

func TestWorkerPool_Process_OrderPreservation(t *testing.T) {
	pool := NewWorkerPool[int, int](WithWorkerCount(4))

	tasks := make([]int, 100)
	for i := range tasks {
		tasks[i] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		// Add variable delay to test order preservation
		time.Sleep(time.Duration(100-task) * time.Microsecond)
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify order is preserved
	for i, task := range tasks {
		expected := task * 2
		if results[i] != expected {
			t.Errorf("index %d: expected %d, got %d (order not preserved)", i, expected, results[i])
		}
	}
}

func TestWorkerPool_Process_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high concurrency test in short mode")
	}

	pool := NewWorkerPool[int, int](WithWorkerCount(16))

	taskCount := 10000
	tasks := make([]int, taskCount)
	for i := range tasks {
		tasks[i] = i
	}

	var counter atomic.Int64
	processFn := func(ctx context.Context, task int) (int, error) {
		counter.Add(1)
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != taskCount {
		t.Fatalf("expected %d results, got %d", taskCount, len(results))
	}

	if counter.Load() != int64(taskCount) {
		t.Errorf("expected %d tasks processed, got %d", taskCount, counter.Load())
	}
}

// Benchmark tests
func BenchmarkWorkerPool_Process(b *testing.B) {
	pool := NewWorkerPool[int, int](WithWorkerCount(8))

	tasks := make([]int, 1000)
	for i := range tasks {
		tasks[i] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		// Simulate some work
		sum := 0
		for i := 0; i < 100; i++ {
			sum += i
		}
		return task * 2, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.Process(context.Background(), tasks, processFn)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkWorkerPool_ProcessMap(b *testing.B) {
	pool := NewWorkerPool[int, int](WithWorkerCount(8))

	tasks := make(map[string]int, 1000)
	for i := 0; i < 1000; i++ {
		tasks[fmt.Sprintf("key_%d", i)] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		sum := 0
		for i := 0; i < 100; i++ {
			sum += i
		}
		return task * 2, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.ProcessMap(context.Background(), tasks, processFn)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// Retry logic tests

func TestWorkerPool_Retry_SuccessOnFirstAttempt(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithRetryPolicy(3, 100*time.Millisecond),
	)

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
}

func TestWorkerPool_Retry_SuccessAfterRetries(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithRetryPolicy(3, 50*time.Millisecond),
	)

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
}

func TestWorkerPool_Retry_AllAttemptsFail(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithRetryPolicy(3, 10*time.Millisecond),
	)

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
}

func TestWorkerPool_Retry_NoRetryWhenMaxAttemptsIsOne(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithRetryPolicy(1, 100*time.Millisecond),
	)

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
}

func TestWorkerPool_Retry_ExponentialBackoffTiming(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	pool := NewWorkerPool[int, int](
		WithWorkerCount(1),
		WithRetryPolicy(4, initialDelay),
	)

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
}

func TestWorkerPool_Retry_ContextCancellationDuringBackoff(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(1),
		WithRetryPolicy(5, 1*time.Second), // Long delay
	)

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
}

func TestWorkerPool_Retry_MultipleTasksIndependentRetries(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(3),
		WithRetryPolicy(3, 20*time.Millisecond),
	)

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
}

func TestWorkerPool_Retry_NoDelayWhenInitialDelayIsZero(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(1),
		WithRetryPolicy(3, 0), // No delay
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

func TestWorkerPool_Retry_PanicRecoveryDoesNotRetry(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(1),
		WithRetryPolicy(3, 10*time.Millisecond),
	)

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
}

func TestCalcBackoffDelay(t *testing.T) {
	tests := []struct {
		name          string
		initialDelay  time.Duration
		attemptNumber int
		expected      time.Duration
	}{
		{
			name:          "first retry (attempt 0)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 0,
			expected:      100 * time.Millisecond, // 2^0 = 1
		},
		{
			name:          "second retry (attempt 1)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 1,
			expected:      200 * time.Millisecond, // 2^1 = 2
		},
		{
			name:          "third retry (attempt 2)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 2,
			expected:      400 * time.Millisecond, // 2^2 = 4
		},
		{
			name:          "fourth retry (attempt 3)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 3,
			expected:      800 * time.Millisecond, // 2^3 = 8
		},
		{
			name:          "negative attempt number",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: -1,
			expected:      0,
		},
		{
			name:          "zero initial delay",
			initialDelay:  0,
			attemptNumber: 2,
			expected:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calcBackoffDelay(tt.initialDelay, tt.attemptNumber)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Rate Limiting Tests

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

	// Both context.DeadlineExceeded and rate limiter's context error are acceptable
	if !errors.Is(err, context.DeadlineExceeded) && !contains(err.Error(), "context deadline") {
		t.Errorf("expected context deadline exceeded, got: %v", err)
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
	for i := 0; i < numTasks; i++ {
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

func TestWorkerPool_RateLimit_ProcessStream(t *testing.T) {
	// Test rate limiting works with ProcessStream
	tasksPerSecond := 20.0
	burst := 5
	numTasks := 30

	pool := NewWorkerPool[int, int](
		WithWorkerCount(5),
		WithRateLimit(tasksPerSecond, burst),
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task + 100, nil
	}

	taskChan := make(chan int, numTasks)
	for i := 0; i < numTasks; i++ {
		taskChan <- i
	}
	close(taskChan)

	start := time.Now()
	resultChan, errChan := pool.ProcessStream(context.Background(), taskChan, processFn)

	results := []int{}
	for result := range resultChan {
		results = append(results, result)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	elapsed := time.Since(start)

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	// With 30 tasks at 20 tasks/sec (burst 5), minimum time should be ~1.25 seconds
	expectedMinDuration := 1200 * time.Millisecond
	if elapsed < expectedMinDuration {
		t.Errorf("rate limiting should slow down processing; took %v (expected at least %v)", elapsed, expectedMinDuration)
	}
}

func TestWorkerPool_RateLimit_InvalidParameters(t *testing.T) {
	// Test that invalid rate limit parameters don't crash
	tests := []struct {
		name            string
		tasksPerSecond  float64
		burst           int
		shouldLimit     bool
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

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
