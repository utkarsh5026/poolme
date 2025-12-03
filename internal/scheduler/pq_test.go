package scheduler

import (
	"container/heap"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

// Test helpers

// intTask represents a simple integer task with a priority value.
type intTask struct {
	value    int
	priority int
}

// createPriorityTask creates a submitted task with a priority.
func createPriorityTask(value, priority int, id int64) *types.SubmittedTask[intTask, int] {
	future := types.NewFuture[int, int64]()
	return types.NewSubmittedTask(intTask{value: value, priority: priority}, id, future)
}

// lessFuncInt is a comparison function for intTask - lower priority value = higher priority.
func lessFuncInt(a, b intTask) bool {
	return a.priority < b.priority
}

// greaterFuncInt is a comparison function for intTask - higher priority value = higher priority.
func greaterFuncInt(a, b intTask) bool {
	return a.priority > b.priority
}

// Tests for priorityQueue

func TestPriorityQueue_NewPriorityQueue(t *testing.T) {
	t.Run("creates empty queue with comparison function", func(t *testing.T) {
		queue := make([]*types.SubmittedTask[intTask, int], 0)
		pq := newPriorityQueue(queue, lessFuncInt)

		if pq == nil {
			t.Fatal("expected non-nil priority queue")
		}
		if pq.Len() != 0 {
			t.Errorf("expected empty queue, got length %d", pq.Len())
		}
	})

	t.Run("creates queue with initial tasks", func(t *testing.T) {
		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1),
			createPriorityTask(2, 3, 2),
			createPriorityTask(3, 7, 3),
		}
		pq := newPriorityQueue(tasks, lessFuncInt)

		if pq.Len() != 3 {
			t.Errorf("expected queue length 3, got %d", pq.Len())
		}
	})
}

func TestPriorityQueue_Len(t *testing.T) {
	pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)

	if pq.Len() != 0 {
		t.Errorf("expected length 0, got %d", pq.Len())
	}

	heap.Push(pq, createPriorityTask(1, 1, 1))
	if pq.Len() != 1 {
		t.Errorf("expected length 1, got %d", pq.Len())
	}

	heap.Push(pq, createPriorityTask(2, 2, 2))
	if pq.Len() != 2 {
		t.Errorf("expected length 2, got %d", pq.Len())
	}
}

func TestPriorityQueue_Less(t *testing.T) {
	t.Run("compares tasks correctly with min-heap", func(t *testing.T) {
		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1), // index 0
			createPriorityTask(2, 3, 2), // index 1
		}
		pq := newPriorityQueue(tasks, lessFuncInt)

		// Priority 3 < 5, so task at index 1 should have higher priority
		if !pq.Less(1, 0) {
			t.Error("expected task with priority 3 to be less than task with priority 5")
		}
		if pq.Less(0, 1) {
			t.Error("expected task with priority 5 to NOT be less than task with priority 3")
		}
	})

	t.Run("compares tasks correctly with max-heap", func(t *testing.T) {
		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1), // index 0
			createPriorityTask(2, 3, 2), // index 1
		}
		pq := newPriorityQueue(tasks, greaterFuncInt)

		// Priority 5 > 3, so task at index 0 should have higher priority
		if !pq.Less(0, 1) {
			t.Error("expected task with priority 5 to be greater than task with priority 3")
		}
		if pq.Less(1, 0) {
			t.Error("expected task with priority 3 to NOT be greater than task with priority 5")
		}
	})
}

func TestPriorityQueue_Swap(t *testing.T) {
	tasks := []*types.SubmittedTask[intTask, int]{
		createPriorityTask(1, 5, 1),
		createPriorityTask(2, 3, 2),
	}
	pq := newPriorityQueue(tasks, lessFuncInt)

	// Verify initial state
	if pq.queue[0].Task.value != 1 {
		t.Errorf("expected queue[0].value = 1, got %d", pq.queue[0].Task.value)
	}
	if pq.queue[1].Task.value != 2 {
		t.Errorf("expected queue[1].value = 2, got %d", pq.queue[1].Task.value)
	}

	// Swap
	pq.Swap(0, 1)

	// Verify swapped state
	if pq.queue[0].Task.value != 2 {
		t.Errorf("expected queue[0].value = 2 after swap, got %d", pq.queue[0].Task.value)
	}
	if pq.queue[1].Task.value != 1 {
		t.Errorf("expected queue[1].value = 1 after swap, got %d", pq.queue[1].Task.value)
	}
}

func TestPriorityQueue_PushPop(t *testing.T) {
	t.Run("push increases length", func(t *testing.T) {
		pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)

		heap.Push(pq, createPriorityTask(1, 5, 1))
		if pq.Len() != 1 {
			t.Errorf("expected length 1 after push, got %d", pq.Len())
		}

		heap.Push(pq, createPriorityTask(2, 3, 2))
		if pq.Len() != 2 {
			t.Errorf("expected length 2 after push, got %d", pq.Len())
		}
	})

	t.Run("pop decreases length and returns correct item", func(t *testing.T) {
		pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)
		heap.Push(pq, createPriorityTask(1, 5, 1))
		heap.Push(pq, createPriorityTask(2, 3, 2))

		item := heap.Pop(pq).(*types.SubmittedTask[intTask, int])
		if pq.Len() != 1 {
			t.Errorf("expected length 1 after pop, got %d", pq.Len())
		}

		// Should pop the task with priority 3 (highest priority in min-heap)
		if item.Task.priority != 3 {
			t.Errorf("expected popped task priority 3, got %d", item.Task.priority)
		}
	})

	t.Run("maintains heap property with multiple operations", func(t *testing.T) {
		pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)
		priorities := []int{5, 2, 8, 1, 9, 3}

		// Push all tasks
		for i, p := range priorities {
			heap.Push(pq, createPriorityTask(i, p, int64(i)))
		}

		// Pop all tasks and verify they come out in priority order
		expectedOrder := []int{1, 2, 3, 5, 8, 9}
		for i, expected := range expectedOrder {
			if pq.Len() != len(priorities)-i {
				t.Errorf("unexpected length at iteration %d", i)
			}

			item := heap.Pop(pq).(*types.SubmittedTask[intTask, int])
			if item.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, item.Task.priority)
			}
		}

		if pq.Len() != 0 {
			t.Errorf("expected empty queue, got length %d", pq.Len())
		}
	})
}

func TestPriorityQueue_HeapInvariant(t *testing.T) {
	t.Run("heap property maintained after random insertions", func(t *testing.T) {
		pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)
		priorities := []int{15, 3, 9, 5, 21, 1, 12, 7, 18, 4}

		for i, p := range priorities {
			heap.Push(pq, createPriorityTask(i, p, int64(i)))

			// Verify heap invariant after each push
			if !isValidHeap(pq) {
				t.Errorf("heap invariant violated after pushing priority %d", p)
			}
		}
	})
}

// isValidHeap checks if the heap property is maintained.
func isValidHeap[T any, R any](pq *priorityQueue[T, R]) bool {
	n := pq.Len()
	for i := 0; i < n; i++ {
		left := 2*i + 1
		right := 2*i + 2

		if left < n && pq.Less(left, i) {
			return false
		}
		if right < n && pq.Less(right, i) {
			return false
		}
	}
	return true
}

// Tests for priorityQueueStrategy

func TestPriorityQueueStrategy_New(t *testing.T) {
	conf := &ProcessorConfig[intTask, int]{
		TaskBuffer: 10,
		LessFunc:   lessFuncInt,
	}

	strategy := newPriorityQueueStrategy(conf, nil)

	if strategy == nil {
		t.Fatal("expected non-nil strategy")
		return
	}
	if strategy.pq == nil {
		t.Fatal("expected non-nil priority queue")
		return
	}
	if strategy.pq.Len() != 0 {
		t.Errorf("expected empty queue, got length %d", strategy.pq.Len())
	}
}

func TestPriorityQueueStrategy_Submit(t *testing.T) {
	t.Run("submits single task successfully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		task := createPriorityTask(1, 5, 1)
		err := strategy.Submit(task)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		strategy.mu.Lock()
		if strategy.pq.Len() != 1 {
			t.Errorf("expected 1 task in queue, got %d", strategy.pq.Len())
		}
		strategy.mu.Unlock()
	})

	t.Run("signals available channel on submit", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		task := createPriorityTask(1, 5, 1)
		strategy.Submit(task)

		// Should receive signal from availableChan
		select {
		case <-strategy.available.Wait():
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Error("expected signal on availableChan")
		}
	})

	t.Run("maintains priority order with multiple submissions", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit tasks in random order
		priorities := []int{5, 2, 8, 1, 9}
		for i, p := range priorities {
			strategy.Submit(createPriorityTask(i, p, int64(i)))
		}

		// Verify priority order
		strategy.mu.Lock()
		defer strategy.mu.Unlock()

		expectedOrder := []int{1, 2, 5, 8, 9}
		for i, expected := range expectedOrder {
			if strategy.pq.Len() == 0 {
				t.Fatal("queue is empty before all tasks popped")
			}

			task := heap.Pop(strategy.pq).(*types.SubmittedTask[intTask, int])
			if task.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, task.Task.priority)
			}
		}
	})
}

func TestPriorityQueueStrategy_SubmitBatch(t *testing.T) {
	t.Run("submits batch successfully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 20,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1),
			createPriorityTask(2, 3, 2),
			createPriorityTask(3, 7, 3),
		}

		count, err := strategy.SubmitBatch(tasks)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}

		strategy.mu.Lock()
		if strategy.pq.Len() != 3 {
			t.Errorf("expected 3 tasks in queue, got %d", strategy.pq.Len())
		}
		strategy.mu.Unlock()
	})

	t.Run("rebuilds heap efficiently", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 20,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit batch of tasks
		tasks := make([]*types.SubmittedTask[intTask, int], 10)
		priorities := []int{15, 3, 9, 5, 21, 1, 12, 7, 18, 4}
		for i, p := range priorities {
			tasks[i] = createPriorityTask(i, p, int64(i))
		}

		strategy.SubmitBatch(tasks)

		// Verify heap property is maintained
		strategy.mu.Lock()
		if !isValidHeap(strategy.pq) {
			t.Error("heap invariant violated after SubmitBatch")
		}
		strategy.mu.Unlock()
	})

	t.Run("maintains priority order after batch submit", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 20,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1),
			createPriorityTask(2, 2, 2),
			createPriorityTask(3, 8, 3),
			createPriorityTask(4, 1, 4),
		}

		strategy.SubmitBatch(tasks)

		// Verify tasks come out in priority order
		strategy.mu.Lock()
		defer strategy.mu.Unlock()

		expectedOrder := []int{1, 2, 5, 8}
		for i, expected := range expectedOrder {
			task := heap.Pop(strategy.pq).(*types.SubmittedTask[intTask, int])
			if task.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, task.Task.priority)
			}
		}
	})
}

func TestPriorityQueueStrategy_Worker(t *testing.T) {
	t.Run("processes tasks in priority order", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    20,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit tasks with different priorities
		priorities := []int{5, 2, 8, 1, 9}
		for i, p := range priorities {
			strategy.Submit(createPriorityTask(i, p, int64(i)))
		}

		// Track execution order
		var executionOrder []int
		var mu sync.Mutex

		executor := func(ctx context.Context, task intTask) (int, error) {
			mu.Lock()
			executionOrder = append(executionOrder, task.priority)
			mu.Unlock()
			return task.value * 2, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		// Wait for tasks to be processed
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done

		// Verify execution order matches priority order
		expectedOrder := []int{1, 2, 5, 8, 9}
		mu.Lock()
		defer mu.Unlock()

		if len(executionOrder) != len(expectedOrder) {
			t.Fatalf("expected %d tasks executed, got %d", len(expectedOrder), len(executionOrder))
		}

		for i, expected := range expectedOrder {
			if executionOrder[i] != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, executionOrder[i])
			}
		}
	})

	t.Run("worker drains queue on context cancellation", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    20,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit tasks first, before starting the worker
		for i := 0; i < 5; i++ {
			strategy.Submit(createPriorityTask(i, i, int64(i)))
		}

		var processedCount atomic.Int32
		var startedProcessing atomic.Bool

		executor := func(ctx context.Context, task intTask) (int, error) {
			startedProcessing.Store(true)
			processedCount.Add(1)
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		// Wait until at least one task starts processing
		for !startedProcessing.Load() {
			time.Sleep(1 * time.Millisecond)
		}

		// Cancel context - this should trigger drain
		cancel()

		// Wait for worker to finish
		select {
		case <-done:
			// Success - worker should drain and finish
		case <-time.After(1 * time.Second):
			t.Error("worker did not finish draining in time")
		}

		// At least some tasks should be processed (drain is called but context is cancelled)
		// Note: Not all tasks may complete due to context cancellation during drain
		if processedCount.Load() == 0 {
			t.Error("expected at least some tasks to be processed")
		}

		t.Logf("Processed %d out of 5 tasks before/during drain", processedCount.Load())
	})

	t.Run("multiple workers process tasks concurrently", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    100,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit many tasks
		taskCount := 50
		for i := 0; i < taskCount; i++ {
			strategy.Submit(createPriorityTask(i, i%10, int64(i)))
		}

		var processedCount atomic.Int32
		var mu sync.Mutex
		processedPriorities := make(map[int]bool)

		executor := func(ctx context.Context, task intTask) (int, error) {
			processedCount.Add(1)
			mu.Lock()
			processedPriorities[task.priority] = true
			mu.Unlock()
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start multiple workers
		workerCount := 3
		var wg sync.WaitGroup
		wg.Add(workerCount)

		for i := 0; i < workerCount; i++ {
			go func(id int) {
				defer wg.Done()
				strategy.Worker(ctx, int64(id), executor, handler)
			}(i)
		}

		// Wait for all tasks to be processed
		time.Sleep(500 * time.Millisecond)
		cancel()
		wg.Wait()

		// All tasks should be processed
		if processedCount.Load() != int32(taskCount) {
			t.Errorf("expected %d tasks processed, got %d", taskCount, processedCount.Load())
		}
	})
}

func TestPriorityQueueStrategy_Shutdown(t *testing.T) {
	t.Run("closes available channel", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		strategy.Shutdown()

		// availableChan should be closed
		select {
		case _, ok := <-strategy.available.Wait():
			if ok {
				t.Error("expected availableChan to be closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for availableChan close")
		}
	})

	t.Run("worker exits after shutdown", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    10,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		executor := func(ctx context.Context, task intTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx := context.Background()

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		// Shutdown
		strategy.Shutdown()

		// Worker should exit
		select {
		case <-done:
			// Success
		case <-time.After(500 * time.Millisecond):
			t.Error("worker did not exit after shutdown")
		}
	})
}

func TestPriorityQueueStrategy_Drain(t *testing.T) {
	t.Run("drains all remaining tasks", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:  10,
			LessFunc:    lessFuncInt,
			MaxAttempts: 1,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		// Submit tasks
		taskCount := 5
		for i := 0; i < taskCount; i++ {
			strategy.Submit(createPriorityTask(i, i, int64(i)))
		}

		var processedCount atomic.Int32

		executor := func(ctx context.Context, task intTask) (int, error) {
			processedCount.Add(1)
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx := context.Background()

		// Drain
		strategy.drain(ctx, executor, handler)

		// Verify all tasks were processed
		if processedCount.Load() != int32(taskCount) {
			t.Errorf("expected %d tasks processed, got %d", taskCount, processedCount.Load())
		}

		// Queue should be empty
		strategy.mu.Lock()
		if strategy.pq.Len() != 0 {
			t.Errorf("expected empty queue after drain, got %d", strategy.pq.Len())
		}
		strategy.mu.Unlock()
	})
}

func TestPriorityQueueStrategy_Concurrency(t *testing.T) {
	t.Run("handles concurrent submissions safely", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 1000,
			LessFunc:   lessFuncInt,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		const goroutines = 10
		const tasksPerGoroutine = 50
		var wg sync.WaitGroup
		wg.Add(goroutines)

		// Submit tasks concurrently
		for i := 0; i < goroutines; i++ {
			go func(start int) {
				defer wg.Done()
				for j := 0; j < tasksPerGoroutine; j++ {
					priority := (start*tasksPerGoroutine + j) % 100
					strategy.Submit(createPriorityTask(start*tasksPerGoroutine+j, priority, int64(start*tasksPerGoroutine+j)))
				}
			}(i)
		}

		wg.Wait()

		// Verify all tasks were submitted
		strategy.mu.Lock()
		totalTasks := strategy.pq.Len()
		strategy.mu.Unlock()

		expectedTotal := goroutines * tasksPerGoroutine
		if totalTasks != expectedTotal {
			t.Errorf("expected %d tasks, got %d", expectedTotal, totalTasks)
		}

		// Verify heap invariant is maintained
		strategy.mu.Lock()
		if !isValidHeap(strategy.pq) {
			t.Error("heap invariant violated after concurrent submissions")
		}
		strategy.mu.Unlock()
	})

	t.Run("concurrent submit and pop operations", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    1000,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		var submitWg, workerWg sync.WaitGroup
		var processedCount atomic.Int32

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start workers
		workerCount := 3
		executor := func(ctx context.Context, task intTask) (int, error) {
			processedCount.Add(1)
			time.Sleep(1 * time.Millisecond)
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		workerWg.Add(workerCount)
		for i := 0; i < workerCount; i++ {
			go func(id int) {
				defer workerWg.Done()
				strategy.Worker(ctx, int64(id), executor, handler)
			}(i)
		}

		// Submit tasks concurrently while workers are processing
		submitters := 5
		tasksPerSubmitter := 40
		submitWg.Add(submitters)

		for i := 0; i < submitters; i++ {
			go func(start int) {
				defer submitWg.Done()
				for j := 0; j < tasksPerSubmitter; j++ {
					priority := (start*tasksPerSubmitter + j) % 50
					strategy.Submit(createPriorityTask(start*tasksPerSubmitter+j, priority, int64(start*tasksPerSubmitter+j)))
					time.Sleep(1 * time.Millisecond)
				}
			}(i)
		}

		submitWg.Wait()
		time.Sleep(500 * time.Millisecond) // Allow workers to process
		cancel()
		workerWg.Wait()

		// All tasks should be processed
		expectedTotal := int32(submitters * tasksPerSubmitter)
		if processedCount.Load() != expectedTotal {
			t.Errorf("expected %d tasks processed, got %d", expectedTotal, processedCount.Load())
		}
	})
}

func TestPriorityQueueStrategy_EdgeCases(t *testing.T) {
	t.Run("handles empty queue gracefully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    10,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		executor := func(ctx context.Context, task intTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		// Start worker with empty queue
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		// Should timeout gracefully
		select {
		case <-done:
			// Success - worker exits when context times out
		case <-time.After(500 * time.Millisecond):
			t.Error("worker did not handle empty queue gracefully")
		}
	})

	t.Run("handles single task correctly", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    10,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newPriorityQueueStrategy(conf, nil)

		strategy.Submit(createPriorityTask(42, 1, 1))

		var processedValue int

		executor := func(ctx context.Context, task intTask) (int, error) {
			processedValue = task.value
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[intTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done

		if processedValue != 42 {
			t.Errorf("expected processed value 42, got %d", processedValue)
		}
	})
}

// Benchmarks

func BenchmarkPriorityQueue_Push(b *testing.B) {
	pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)
	task := createPriorityTask(1, 5, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		heap.Push(pq, task)
	}
}

func BenchmarkPriorityQueue_Pop(b *testing.B) {
	pq := newPriorityQueue([]*types.SubmittedTask[intTask, int]{}, lessFuncInt)

	// Pre-fill the queue
	for i := 0; i < b.N; i++ {
		heap.Push(pq, createPriorityTask(i, i, int64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		heap.Pop(pq)
	}
}

func BenchmarkPriorityQueueStrategy_Submit(b *testing.B) {
	conf := &ProcessorConfig[intTask, int]{
		TaskBuffer: b.N + 1000,
		LessFunc:   lessFuncInt,
	}
	strategy := newPriorityQueueStrategy(conf, nil)
	task := createPriorityTask(1, 5, 1)

	// Drain the channel in background to prevent deadlock
	go func() {
		for range strategy.available.Wait() {
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.Submit(task)
	}
	b.StopTimer()

	strategy.Shutdown()
}

func BenchmarkPriorityQueueStrategy_SubmitBatch(b *testing.B) {
	conf := &ProcessorConfig[intTask, int]{
		TaskBuffer: b.N*100 + 10000,
		LessFunc:   lessFuncInt,
	}
	strategy := newPriorityQueueStrategy(conf, nil)

	tasks := make([]*types.SubmittedTask[intTask, int], 100)
	for i := range tasks {
		tasks[i] = createPriorityTask(i, i, int64(i))
	}

	// Drain the channel in background to prevent deadlock
	go func() {
		for range strategy.available.Wait() {
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = strategy.SubmitBatch(tasks)
	}
	b.StopTimer()

	strategy.Shutdown()
}
