package pool_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/pool"
)

func TestWorkerPool_Submit_BasicFunctionality(t *testing.T) {
	pool := pool.NewScheduler[int, string](pool.WithWorkerCount(2))

	processFn := func(ctx context.Context, task int) (string, error) {
		return fmt.Sprintf("result-%d", task), nil
	}

	err := pool.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(time.Second)

	future, err := pool.Submit(42)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	value, key, err := future.Get()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if value != "result-42" {
		t.Errorf("expected 'result-42', got %v", value)
	}
	if key != 1 {
		t.Errorf("expected key 1, got %v", key)
	}
}

func TestWorkerPool_Submit_MultipleSubmissions(t *testing.T) {
	p := pool.NewScheduler[int, int](pool.WithWorkerCount(4))

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(2 * time.Second)
	// Submit multiple tasks
	numTasks := 100
	futures := make([]*pool.Future[int, int64], numTasks)

	for i := range numTasks {
		future, err := p.Submit(i)
		if err != nil {
			t.Fatalf("failed to submit task %d: %v", i, err)
		}
		futures[i] = future
	}

	// Collect results
	for i, future := range futures {
		value, _, err := future.Get()
		if err != nil {
			t.Errorf("task %d failed: %v", i, err)
		}
		expected := i * 2
		if value != expected {
			t.Errorf("task %d: expected %d, got %d", i, expected, value)
		}
	}
}

func TestWorkerPool_Submit_ErrorHandling(t *testing.T) {
	p := pool.NewScheduler[int, string](pool.WithWorkerCount(2))

	processFn := func(ctx context.Context, task int) (string, error) {
		if task%2 == 0 {
			return "", errors.New("even number error")
		}
		return fmt.Sprintf("success-%d", task), nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)

	// Submit task that will fail
	future1, err := p.Submit(2)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	value, _, err := future1.Get()
	if err == nil {
		t.Error("expected error for even number")
	}
	if value != "" {
		t.Errorf("expected empty value, got %v", value)
	}

	// Submit task that will succeed
	future2, err := p.Submit(3)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	value, _, err = future2.Get()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if value != "success-3" {
		t.Errorf("expected 'success-3', got %v", value)
	}
}

func TestWorkerPool_Submit_BeforeStart(t *testing.T) {
	p := pool.NewScheduler[int, string]()

	_, err := p.Submit(42)
	if err == nil {
		t.Error("expected error when submitting before Start")
	}
	if err.Error() != "pool not started" {
		t.Errorf("expected 'pool not started', got %v", err)
	}
}

func TestWorkerPool_Submit_AfterShutdown(t *testing.T) {
	p := pool.NewScheduler[int, string](pool.WithWorkerCount(2))
	processFn := func(ctx context.Context, task int) (string, error) {
		return "result", nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	err = p.Shutdown(time.Second)
	if err != nil {
		t.Fatalf("failed to shutdown pool: %v", err)
	}

	_, err = p.Submit(42)
	if err == nil {
		t.Error("expected error when submitting after shutdown")
	}
	if err.Error() != "pool shut down" {
		t.Errorf("expected 'pool shut down', got %v", err)
	}
}

func TestWorkerPool_Submit_WithRetry(t *testing.T) {
	var attemptCount atomic.Int32

	p := pool.NewScheduler[int, string](
		pool.WithWorkerCount(1),
		pool.WithRetryPolicy(3, 10*time.Millisecond),
	)

	processFn := func(ctx context.Context, task int) (string, error) {
		count := attemptCount.Add(1)
		if count < 3 {
			return "", errors.New("temporary failure")
		}
		return "success", nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)

	future, err := p.Submit(1)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	value, _, err := future.Get()
	if err != nil {
		t.Errorf("expected no error after retries, got %v", err)
	}
	if value != "success" {
		t.Errorf("expected 'success', got %v", value)
	}

	finalCount := attemptCount.Load()
	if finalCount != 3 {
		t.Errorf("expected 3 attempts, got %d", finalCount)
	}
}

func TestWorkerPool_Submit_WithHooks(t *testing.T) {
	var beforeCount atomic.Int32
	var endCount atomic.Int32
	var retryCount atomic.Int32

	p := pool.NewScheduler[int, string](
		pool.WithWorkerCount(1),
		pool.WithRetryPolicy(2, 10*time.Millisecond),
		pool.WithBeforeTaskStart(func(task int) {
			beforeCount.Add(1)
		}),
		pool.WithOnTaskEnd(func(task int, result string, err error) {
			endCount.Add(1)
		}),
		pool.WithOnEachAttempt(func(task int, attempt int, err error) {
			retryCount.Add(1)
		}),
	)

	var firstAttempt atomic.Bool
	processFn := func(ctx context.Context, task int) (string, error) {
		if !firstAttempt.Swap(true) {
			return "", errors.New("first attempt fails")
		}
		return "success", nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)
	future, err := p.Submit(42)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	value, _, err := future.Get()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if value != "success" {
		t.Errorf("expected 'success', got %v", value)
	}

	// Give hooks time to execute
	time.Sleep(100 * time.Millisecond)

	if beforeCount.Load() != 1 {
		t.Errorf("expected beforeTaskStart called 1 time, got %d", beforeCount.Load())
	}
	if endCount.Load() != 1 {
		t.Errorf("expected onTaskEnd called 1 time, got %d", endCount.Load())
	}
	if retryCount.Load() != 1 {
		t.Errorf("expected onRetry called 1 time, got %d", retryCount.Load())
	}
}

func TestWorkerPool_Submit_ConcurrentSubmissions(t *testing.T) {
	p := pool.NewScheduler[int, int](pool.WithWorkerCount(8))

	processFn := func(ctx context.Context, task int) (int, error) {
		time.Sleep(10 * time.Millisecond)
		return task * task, nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(5 * time.Second)

	numGoroutines := 50
	tasksPerGoroutine := 10
	var wg sync.WaitGroup

	results := make(chan int, numGoroutines*tasksPerGoroutine)
	errors := make(chan error, numGoroutines*tasksPerGoroutine)

	for g := range numGoroutines {
		wg.Add(1)
		go func(gID int) {
			defer wg.Done()

			for i := range tasksPerGoroutine {
				task := gID*tasksPerGoroutine + i
				future, err := p.Submit(task)
				if err != nil {
					errors <- err
					continue
				}

				value, _, err := future.Get()
				if err != nil {
					errors <- err
					continue
				}

				results <- value
			}
		}(g)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("got error: %v", err)
		errorCount++
	}

	// Check results
	resultCount := 0
	for range results {
		resultCount++
	}

	expectedCount := numGoroutines * tasksPerGoroutine
	if resultCount+errorCount != expectedCount {
		t.Errorf("expected %d total results, got %d results and %d errors",
			expectedCount, resultCount, errorCount)
	}
}

func TestWorkerPool_Submit_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	p := pool.NewScheduler[int, string](pool.WithWorkerCount(2))

	processFn := func(ctx context.Context, task int) (string, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return "completed", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	err := p.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)

	future, err := p.Submit(1)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	// Cancel context after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// The task should fail with context cancellation
	_, _, err = future.Get()
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestWorkerPool_Submit_LongRunningStability(t *testing.T) {
	p := pool.NewScheduler[int, int](pool.WithWorkerCount(4))

	processFn := func(ctx context.Context, task int) (int, error) {
		time.Sleep(time.Millisecond)
		return task + 1, nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks in waves
	numWaves := 10
	tasksPerWave := 50

	for wave := range numWaves {
		futures := make([]*pool.Future[int, int64], tasksPerWave)

		// Submit wave
		for i := range tasksPerWave {
			future, err := p.Submit(i)
			if err != nil {
				t.Fatalf("wave %d, task %d: failed to submit: %v", wave, i, err)
			}
			futures[i] = future
		}

		// Collect results
		for i, future := range futures {
			value, _, err := future.Get()
			if err != nil {
				t.Errorf("wave %d, task %d: failed: %v", wave, i, err)
			}
			if value != i+1 {
				t.Errorf("wave %d, task %d: expected %d, got %d", wave, i, i+1, value)
			}
		}
	}

	err = p.Shutdown(5 * time.Second)
	if err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestWorkerPool_Submit_WithGetWithContext(t *testing.T) {
	p := pool.NewScheduler[int, string](pool.WithWorkerCount(2))

	processFn := func(ctx context.Context, task int) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return fmt.Sprintf("result-%d", task), nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)
	future, err := p.Submit(42)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	// Use GetWithContext with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	value, key, err := future.GetWithContext(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if value != "result-42" {
		t.Errorf("expected 'result-42', got %v", value)
	}
	if key != 1 {
		t.Errorf("expected key 1, got %v", key)
	}
}

func TestWorkerPool_Submit_TaskIDIncrement(t *testing.T) {
	p := pool.NewScheduler[int, int](pool.WithWorkerCount(1))

	processFn := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	err := p.Start(context.Background(), processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer p.Shutdown(time.Second)
	// Submit multiple tasks and check IDs increment
	numTasks := 10
	for i := range numTasks {
		future, err := p.Submit(i)
		if err != nil {
			t.Fatalf("failed to submit task %d: %v", i, err)
		}

		_, key, err := future.Get()
		if err != nil {
			t.Errorf("task %d failed: %v", i, err)
		}

		expectedKey := int64(i + 1)
		if key != expectedKey {
			t.Errorf("task %d: expected key %d, got %d", i, expectedKey, key)
		}
	}
}
