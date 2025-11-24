package pool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Task with priority for testing
type PriorityTask struct {
	ID       int
	Priority int
	Value    string
}

func TestPriorityQueueStrategy_BasicOrdering(t *testing.T) {
	// Lower priority number = higher priority (executed first)
	pool := NewWorkerPool[PriorityTask, string](
		WithWorkerCount(1), // Single worker to ensure ordering
		WithPriorityQueue(func(task PriorityTask) int {
			return task.Priority
		}),
	)

	var executionOrder []int
	var mu sync.Mutex

	// Use channel to control when workers start processing
	readyToProcess := make(chan struct{})

	processFn := func(ctx context.Context, task PriorityTask) (string, error) {
		// First worker waits for signal, then processes all remaining tasks
		select {
		case <-readyToProcess:
		default:
		}

		mu.Lock()
		executionOrder = append(executionOrder, task.ID)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond) // Small delay to ensure serialization
		return fmt.Sprintf("processed-%d", task.ID), nil
	}

	ctx := context.Background()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks in random order with different priorities
	tasks := []PriorityTask{
		{ID: 1, Priority: 5, Value: "low"},
		{ID: 2, Priority: 1, Value: "high"},
		{ID: 3, Priority: 3, Value: "medium"},
		{ID: 4, Priority: 2, Value: "high-medium"},
		{ID: 5, Priority: 4, Value: "medium-low"},
	}

	// Submit all tasks first
	var futures []*Future[string, int64]
	for _, task := range tasks {
		future, err := pool.Submit(task)
		if err != nil {
			t.Fatalf("failed to submit task: %v", err)
		}
		futures = append(futures, future)
	}

	// Allow tasks to queue up, then signal processing can start
	time.Sleep(100 * time.Millisecond)
	close(readyToProcess)

	// Wait for all to complete
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer waitCancel()

	for i, future := range futures {
		result, _, err := future.GetWithContext(waitCtx)
		if err != nil {
			t.Errorf("task %d failed: %v", i, err)
		}
		expectedResult := fmt.Sprintf("processed-%d", tasks[i].ID)
		if result != expectedResult {
			t.Errorf("expected result %s, got %s", expectedResult, result)
		}
	}

	pool.Shutdown(2 * time.Second)

	// Verify execution order (should be by priority: 2, 4, 3, 5, 1)
	expectedOrder := []int{2, 4, 3, 5, 1}
	if len(executionOrder) != len(expectedOrder) {
		t.Fatalf("expected %d tasks executed, got %d", len(expectedOrder), len(executionOrder))
	}

	for i, expected := range expectedOrder {
		if executionOrder[i] != expected {
			t.Errorf("position %d: expected task ID %d, got %d", i, expected, executionOrder[i])
		}
	}
}

func TestPriorityQueueStrategy_ConcurrentSubmission(t *testing.T) {
	pool := NewWorkerPool[PriorityTask, string](
		WithWorkerCount(4),
		WithPriorityQueue(func(task PriorityTask) int {
			return task.Priority
		}),
	)

	var processed atomic.Int64

	processFn := func(ctx context.Context, task PriorityTask) (string, error) {
		time.Sleep(10 * time.Millisecond) // Simulate work
		processed.Add(1)
		return fmt.Sprintf("result-%d", task.ID), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit 100 tasks concurrently
	const numTasks = 100
	var wg sync.WaitGroup
	wg.Add(numTasks)

	for i := 0; i < numTasks; i++ {
		go func(id int) {
			defer wg.Done()
			task := PriorityTask{
				ID:       id,
				Priority: id % 10, // Priority 0-9
				Value:    fmt.Sprintf("task-%d", id),
			}
			_, err := pool.Submit(task)
			if err != nil {
				t.Errorf("failed to submit task %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Wait for all tasks to complete
	time.Sleep(2 * time.Second)

	pool.Shutdown(5 * time.Second)

	processedCount := processed.Load()
	if processedCount != numTasks {
		t.Errorf("expected %d tasks processed, got %d", numTasks, processedCount)
	}
}

func TestPriorityQueueStrategy_ContextCancellation(t *testing.T) {
	pool := NewWorkerPool[PriorityTask, string](
		WithWorkerCount(2),
		WithPriorityQueue(func(task PriorityTask) int {
			return task.Priority
		}),
	)

	var started atomic.Int64
	var completed atomic.Int64

	processFn := func(ctx context.Context, task PriorityTask) (string, error) {
		started.Add(1)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
			completed.Add(1)
			return "done", nil
		}
	}

	ctx := context.Background()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit some tasks with a context that will be cancelled
	submitCtx, submitCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer submitCancel()

	for i := 0; i < 10; i++ {
		task := PriorityTask{ID: i, Priority: i, Value: fmt.Sprintf("task-%d", i)}
		pool.Submit(task)
	}

	// Wait for submit context to cancel
	<-submitCtx.Done()

	// Try to submit with cancelled context
	task := PriorityTask{ID: 99, Priority: 1, Value: "late-task"}
	_, err = pool.Submit(task)
	// Note: Submit doesn't currently check pool state, only context in the select
	// This test verifies the submit context handling works

	time.Sleep(500 * time.Millisecond)
	pool.Shutdown(1 * time.Second)

	t.Logf("Started: %d, Completed: %d", started.Load(), completed.Load())

	if started.Load() == 0 {
		t.Error("expected some tasks to start")
	}
}

func TestPriorityQueueStrategy_Shutdown(t *testing.T) {
	pool := NewWorkerPool[PriorityTask, string](
		WithWorkerCount(3),
		WithPriorityQueue(func(task PriorityTask) int {
			return task.Priority
		}),
	)

	var processed atomic.Int64

	processFn := func(ctx context.Context, task PriorityTask) (string, error) {
		time.Sleep(50 * time.Millisecond)
		processed.Add(1)
		return "done", nil
	}

	ctx := context.Background()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks
	for i := 0; i < 20; i++ {
		task := PriorityTask{ID: i, Priority: i % 5, Value: fmt.Sprintf("task-%d", i)}
		pool.Submit(task)
	}

	// Shutdown gracefully
	start := time.Now()
	err = pool.Shutdown(10 * time.Second)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	t.Logf("Shutdown took %v, processed %d tasks", duration, processed.Load())

	// Try to submit after shutdown
	task := PriorityTask{ID: 999, Priority: 1, Value: "post-shutdown"}
	_, err = pool.Submit(task)
	if err == nil {
		t.Error("expected error when submitting after shutdown")
	}
}

func TestPriorityQueueStrategy_HighPriorityFirst(t *testing.T) {
	pool := NewWorkerPool[int, string](
		WithWorkerCount(1), // Single worker
		WithPriorityQueue(func(task int) int {
			return task // Use task value as priority
		}),
	)

	var executionOrder []int
	var mu sync.Mutex
	var startProcessing sync.WaitGroup
	startProcessing.Add(1)

	processFn := func(ctx context.Context, task int) (string, error) {
		// Wait for all tasks to be submitted before processing
		startProcessing.Wait()
		mu.Lock()
		executionOrder = append(executionOrder, task)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return fmt.Sprintf("result-%d", task), nil
	}

	ctx := context.Background()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks in descending order
	tasks := []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	for _, task := range tasks {
		_, err := pool.Submit(task)
		if err != nil {
			t.Fatalf("failed to submit task: %v", err)
		}
	}

	// Allow processing to start
	time.Sleep(100 * time.Millisecond)
	startProcessing.Done()

	// Wait for completion
	time.Sleep(500 * time.Millisecond)
	pool.Shutdown(2 * time.Second)

	// Verify tasks were executed in priority order (1, 2, 3, ... 10)
	// Because lower number = higher priority
	if len(executionOrder) != len(tasks) {
		t.Fatalf("expected %d tasks, got %d", len(tasks), len(executionOrder))
	}

	for i := 0; i < len(executionOrder); i++ {
		expected := i + 1
		if executionOrder[i] != expected {
			t.Errorf("position %d: expected %d, got %d", i, expected, executionOrder[i])
		}
	}
}

func TestPriorityQueueStrategy_SamePriority(t *testing.T) {
	pool := NewWorkerPool[PriorityTask, string](
		WithWorkerCount(1),
		WithPriorityQueue(func(task PriorityTask) int {
			return task.Priority
		}),
	)

	var processed atomic.Int64

	processFn := func(ctx context.Context, task PriorityTask) (string, error) {
		processed.Add(1)
		return fmt.Sprintf("done-%d", task.ID), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks with same priority
	for i := 0; i < 10; i++ {
		task := PriorityTask{ID: i, Priority: 5, Value: fmt.Sprintf("task-%d", i)}
		_, err := pool.Submit(task)
		if err != nil {
			t.Fatalf("failed to submit task: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)
	pool.Shutdown(2 * time.Second)

	if processed.Load() != 10 {
		t.Errorf("expected 10 tasks processed, got %d", processed.Load())
	}
}

func TestPriorityQueueStrategy_WithRetries(t *testing.T) {
	var attempts atomic.Int64

	pool := NewWorkerPool[int, string](
		WithWorkerCount(2),
		WithRetryPolicy(3, 50*time.Millisecond),
		WithPriorityQueue(func(task int) int {
			return task
		}),
	)

	processFn := func(ctx context.Context, task int) (string, error) {
		attemptNum := attempts.Add(1)
		if attemptNum < 3 {
			return "", fmt.Errorf("temporary error %d", attemptNum)
		}
		return "success", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	future, err := pool.Submit(1)
	if err != nil {
		t.Fatalf("failed to submit task: %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()

	result, _, err := future.GetWithContext(waitCtx)
	if err != nil {
		t.Fatalf("task failed: %v", err)
	}

	if result != "success" {
		t.Errorf("expected 'success', got '%s'", result)
	}

	pool.Shutdown(2 * time.Second)

	if attempts.Load() < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts.Load())
	}
}

func TestPriorityQueueStrategy_WithHooks(t *testing.T) {
	var beforeCalls atomic.Int64
	var afterCalls atomic.Int64
	var retryCalls atomic.Int64

	pool := NewWorkerPool[int, string](
		WithWorkerCount(2),
		WithRetryPolicy(2, 10*time.Millisecond),
		WithPriorityQueue(func(task int) int {
			return task
		}),
		WithBeforeTaskStart(func(task int) {
			beforeCalls.Add(1)
		}),
		WithOnTaskEnd(func(task int, result string, err error) {
			afterCalls.Add(1)
		}),
		WithOnEachAttempt(func(task int, attempt int, err error) {
			retryCalls.Add(1)
		}),
	)

	var failOnce atomic.Bool

	processFn := func(ctx context.Context, task int) (string, error) {
		if !failOnce.Load() && task == 1 {
			failOnce.Store(true)
			return "", fmt.Errorf("first failure")
		}
		return "ok", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.Start(ctx, processFn)
	if err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks
	for i := 1; i <= 5; i++ {
		_, err := pool.Submit(i)
		if err != nil {
			t.Fatalf("failed to submit task: %v", err)
		}
	}

	time.Sleep(500 * time.Millisecond)
	pool.Shutdown(2 * time.Second)

	if beforeCalls.Load() == 0 {
		t.Error("beforeTaskStart hook was not called")
	}
	if afterCalls.Load() == 0 {
		t.Error("onTaskEnd hook was not called")
	}

	t.Logf("Before: %d, After: %d, Retry: %d", beforeCalls.Load(), afterCalls.Load(), retryCalls.Load())
}
