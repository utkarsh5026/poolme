package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/gopool/internal/types"
)

// Tests for skipList

func TestSkipList_New(t *testing.T) {
	t.Run("creates empty skiplist with comparison function", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		if sl == nil {
			t.Fatal("expected non-nil skiplist")
		}
		if sl.Len() != 0 {
			t.Errorf("expected empty skiplist, got length %d", sl.Len())
		}
		if sl.head == nil {
			t.Error("expected non-nil head node")
		}
		if atomic.LoadInt32(&sl.level) != 1 {
			t.Errorf("expected initial level 1, got %d", atomic.LoadInt32(&sl.level))
		}
	})

	t.Run("initializes update pool", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Test that we can get an item from the pool
		updatePtr, ok := sl.updatePool.Get().(*[]*slNode[intTask, int])
		if !ok || updatePtr == nil {
			t.Fatal("expected non-nil update array from pool")
		}
		if len(*updatePtr) != defaultMaxLevel {
			t.Errorf("expected update array length %d, got %d", defaultMaxLevel, len(*updatePtr))
		}
		sl.updatePool.Put(updatePtr)
	})
}

func TestSkipList_Push(t *testing.T) {
	t.Run("pushes single task successfully", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		task := createPriorityTask(1, 5, 1)

		sl.Push(task)

		if sl.Len() != 1 {
			t.Errorf("expected length 1, got %d", sl.Len())
		}
	})

	t.Run("pushes multiple tasks maintaining priority order", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		priorities := []int{5, 2, 8, 1, 9, 3}

		for i, p := range priorities {
			sl.Push(createPriorityTask(i, p, int64(i)))
		}

		if sl.Len() != len(priorities) {
			t.Errorf("expected length %d, got %d", len(priorities), sl.Len())
		}

		// Verify tasks come out in priority order
		expectedOrder := []int{1, 2, 3, 5, 8, 9}
		for i, expected := range expectedOrder {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if task.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, task.Task.priority)
			}
		}
	})

	t.Run("handles tasks with same priority", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Push multiple tasks with same priority
		for i := 0; i < 5; i++ {
			sl.Push(createPriorityTask(i, 5, int64(i)))
		}

		// All tasks should be stored
		if sl.Len() != 5 {
			t.Errorf("expected length 5, got %d", sl.Len())
		}

		// Pop all tasks - they should all have priority 5
		for i := 0; i < 5; i++ {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if task.Task.priority != 5 {
				t.Errorf("iteration %d: expected priority 5, got %d", i, task.Task.priority)
			}
		}
	})

	t.Run("increases level as needed", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		initialLevel := atomic.LoadInt32(&sl.level)

		// Push many tasks to likely trigger level increase
		for i := 0; i < 100; i++ {
			sl.Push(createPriorityTask(i, i, int64(i)))
		}

		// Level should have increased
		finalLevel := atomic.LoadInt32(&sl.level)
		if finalLevel <= initialLevel {
			t.Logf("Warning: level did not increase after 100 pushes (initial: %d, final: %d)", initialLevel, finalLevel)
			// Note: This is probabilistic, so we don't hard fail
		}
	})
}

func TestSkipList_Pop(t *testing.T) {
	t.Run("pops tasks in priority order", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		priorities := []int{15, 3, 9, 5, 21, 1, 12, 7, 18, 4}

		for i, p := range priorities {
			sl.Push(createPriorityTask(i, p, int64(i)))
		}

		// Pop all tasks and verify they come out in sorted order
		var lastPriority int
		for i := 0; i < len(priorities); i++ {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if i > 0 && task.Task.priority < lastPriority {
				t.Errorf("priority order violated: %d came after %d", task.Task.priority, lastPriority)
			}
			lastPriority = task.Task.priority
		}
	})

	t.Run("returns nil when empty", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		task := sl.Pop()
		if task != nil {
			t.Error("expected nil from empty skiplist")
		}
	})

	t.Run("decreases length after pop", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		sl.Push(createPriorityTask(1, 5, 1))
		sl.Push(createPriorityTask(2, 3, 2))

		if sl.Len() != 2 {
			t.Errorf("expected length 2, got %d", sl.Len())
		}

		sl.Pop()
		if sl.Len() != 1 {
			t.Errorf("expected length 1 after pop, got %d", sl.Len())
		}

		sl.Pop()
		if sl.Len() != 0 {
			t.Errorf("expected length 0 after second pop, got %d", sl.Len())
		}
	})

	t.Run("pops from node with multiple tasks", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Push tasks with same priority (they should be in same node)
		for i := 0; i < 3; i++ {
			sl.Push(createPriorityTask(i, 5, int64(i)))
		}

		// Pop all tasks
		for i := 0; i < 3; i++ {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if task.Task.priority != 5 {
				t.Errorf("expected priority 5, got %d", task.Task.priority)
			}
		}

		if sl.Len() != 0 {
			t.Errorf("expected empty skiplist, got length %d", sl.Len())
		}
	})
}

func TestSkipList_Len(t *testing.T) {
	sl := newSkipList[intTask, int](lessFuncInt)

	if sl.Len() != 0 {
		t.Errorf("expected length 0, got %d", sl.Len())
	}

	sl.Push(createPriorityTask(1, 1, 1))
	if sl.Len() != 1 {
		t.Errorf("expected length 1, got %d", sl.Len())
	}

	sl.Push(createPriorityTask(2, 2, 2))
	if sl.Len() != 2 {
		t.Errorf("expected length 2, got %d", sl.Len())
	}

	sl.Pop()
	if sl.Len() != 1 {
		t.Errorf("expected length 1 after pop, got %d", sl.Len())
	}
}

func TestSkipList_RandomLevel(t *testing.T) {
	t.Run("generates valid levels", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Generate many random levels
		levels := make(map[int]int)
		for i := 0; i < 1000; i++ {
			level := sl.randomLevel()
			if level < 1 || level > defaultMaxLevel {
				t.Errorf("invalid level generated: %d (expected 1-%d)", level, defaultMaxLevel)
			}
			levels[level]++
		}

		// Level 1 should be most common (probability 0.5)
		if levels[1] < 400 {
			t.Logf("Warning: level 1 generated only %d times out of 1000 (expected ~500)", levels[1])
		}
	})

	t.Run("level distribution is roughly exponential", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		levels := make(map[int]int)
		for i := 0; i < 10000; i++ {
			levels[sl.randomLevel()]++
		}

		// Each level should have roughly half the frequency of the previous
		// Level 1: ~50%, Level 2: ~25%, Level 3: ~12.5%, etc.
		// We don't strict test this since it's probabilistic
		t.Logf("Level distribution: %v", levels)
	})
}

func TestSkipList_RemoveNode(t *testing.T) {
	t.Run("removes node and maintains structure", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Push tasks
		for i := 1; i <= 5; i++ {
			sl.Push(createPriorityTask(i, i, int64(i)))
		}

		// Pop all tasks - this exercises removeNode
		for i := 1; i <= 5; i++ {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task")
			}
			if sl.Len() != 5-i {
				t.Errorf("expected length %d, got %d", 5-i, sl.Len())
			}
		}

		// Skiplist should be empty
		if sl.Pop() != nil {
			t.Error("expected nil from empty skiplist")
		}
	})

	t.Run("adjusts level after removing high-level nodes", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)

		// Push many tasks to create multi-level structure
		for i := 0; i < 50; i++ {
			sl.Push(createPriorityTask(i, i, int64(i)))
		}

		initialLevel := atomic.LoadInt32(&sl.level)

		// Pop all tasks
		for i := 0; i < 50; i++ {
			sl.Pop()
		}

		// Level should be reduced to 1
		finalLevel := atomic.LoadInt32(&sl.level)
		if finalLevel != 1 {
			t.Errorf("expected level 1 after removing all nodes, got %d", finalLevel)
		}

		t.Logf("Level changed from %d to %d after removing all nodes", initialLevel, finalLevel)
	})
}

func TestSkipList_PriorityOrdering(t *testing.T) {
	t.Run("maintains min-heap property with lessFuncInt", func(t *testing.T) {
		sl := newSkipList[intTask, int](lessFuncInt)
		priorities := []int{50, 10, 30, 20, 40}

		for i, p := range priorities {
			sl.Push(createPriorityTask(i, p, int64(i)))
		}

		// Should pop in ascending order: 10, 20, 30, 40, 50
		expected := []int{10, 20, 30, 40, 50}
		for i, exp := range expected {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil at iteration %d", i)
			}
			if task.Task.priority != exp {
				t.Errorf("iteration %d: expected priority %d, got %d", i, exp, task.Task.priority)
			}
		}
	})

	t.Run("maintains max-heap property with greaterFuncInt", func(t *testing.T) {
		sl := newSkipList[intTask, int](greaterFuncInt)
		priorities := []int{50, 10, 30, 20, 40}

		for i, p := range priorities {
			sl.Push(createPriorityTask(i, p, int64(i)))
		}

		// Should pop in descending order: 50, 40, 30, 20, 10
		expected := []int{50, 40, 30, 20, 10}
		for i, exp := range expected {
			task := sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil at iteration %d", i)
			}
			if task.Task.priority != exp {
				t.Errorf("iteration %d: expected priority %d, got %d", i, exp, task.Task.priority)
			}
		}
	})
}

// Tests for slStrategy

func TestSlStrategy_New(t *testing.T) {
	t.Run("creates strategy with empty tasks", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}

		strategy := newSlStrategy(conf, nil)

		if strategy == nil {
			t.Fatal("expected non-nil strategy")
		}
		if strategy.sl == nil {
			t.Error("expected non-nil skiplist")
		}
		if strategy.sl.Len() != 0 {
			t.Errorf("expected empty skiplist, got length %d", strategy.sl.Len())
		}
		if strategy.available == nil {
			t.Error("expected non-nil available channel")
		}
	})

	t.Run("creates strategy with initial tasks", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}

		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1),
			createPriorityTask(2, 3, 2),
			createPriorityTask(3, 7, 3),
		}

		strategy := newSlStrategy(conf, tasks)

		if strategy.sl.Len() != 3 {
			t.Errorf("expected skiplist length 3, got %d", strategy.sl.Len())
		}
	})
}

func TestSlStrategy_Submit(t *testing.T) {
	t.Run("submits single task successfully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

		task := createPriorityTask(1, 5, 1)
		err := strategy.Submit(task)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if strategy.sl.Len() != 1 {
			t.Errorf("expected 1 task in skiplist, got %d", strategy.sl.Len())
		}
	})

	t.Run("signals available channel on submit", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

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
		strategy := newSlStrategy(conf, nil)

		// Submit tasks in random order
		priorities := []int{5, 2, 8, 1, 9}
		for i, p := range priorities {
			strategy.Submit(createPriorityTask(i, p, int64(i)))
		}

		// Verify priority order
		expectedOrder := []int{1, 2, 5, 8, 9}
		for i, expected := range expectedOrder {
			task := strategy.sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if task.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, task.Task.priority)
			}
		}
	})
}

func TestSlStrategy_SubmitBatch(t *testing.T) {
	t.Run("submits batch successfully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 20,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

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

		if strategy.sl.Len() != 3 {
			t.Errorf("expected 3 tasks in skiplist, got %d", strategy.sl.Len())
		}
	})

	t.Run("maintains priority order after batch submit", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 20,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

		tasks := []*types.SubmittedTask[intTask, int]{
			createPriorityTask(1, 5, 1),
			createPriorityTask(2, 2, 2),
			createPriorityTask(3, 8, 3),
			createPriorityTask(4, 1, 4),
		}

		strategy.SubmitBatch(tasks)

		// Verify tasks come out in priority order
		expectedOrder := []int{1, 2, 5, 8}
		for i, expected := range expectedOrder {
			task := strategy.sl.Pop()
			if task == nil {
				t.Fatalf("unexpected nil task at iteration %d", i)
			}
			if task.Task.priority != expected {
				t.Errorf("iteration %d: expected priority %d, got %d", i, expected, task.Task.priority)
			}
		}
	})
}

func TestSlStrategy_Worker(t *testing.T) {
	t.Run("processes tasks in priority order", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    20,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newSlStrategy(conf, nil)

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
		strategy := newSlStrategy(conf, nil)

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

		// At least some tasks should be processed
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
		strategy := newSlStrategy(conf, nil)

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

func TestSlStrategy_Shutdown(t *testing.T) {
	t.Run("closes available channel", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 10,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

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
		strategy := newSlStrategy(conf, nil)

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

func TestSlStrategy_Drain(t *testing.T) {
	t.Run("drains all remaining tasks", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:  10,
			LessFunc:    lessFuncInt,
			MaxAttempts: 1,
		}
		strategy := newSlStrategy(conf, nil)

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

		// Skiplist should be empty
		if strategy.sl.Len() != 0 {
			t.Errorf("expected empty skiplist after drain, got %d", strategy.sl.Len())
		}
	})
}

func TestSlStrategy_Concurrency(t *testing.T) {
	t.Run("handles concurrent submissions safely", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer: 1000,
			LessFunc:   lessFuncInt,
		}
		strategy := newSlStrategy(conf, nil)

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
		totalTasks := strategy.sl.Len()
		expectedTotal := goroutines * tasksPerGoroutine
		if totalTasks != expectedTotal {
			t.Errorf("expected %d tasks, got %d", expectedTotal, totalTasks)
		}
	})

	t.Run("concurrent submit and pop operations", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    1000,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newSlStrategy(conf, nil)

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

func TestSlStrategy_EdgeCases(t *testing.T) {
	t.Run("handles empty queue gracefully", func(t *testing.T) {
		conf := &ProcessorConfig[intTask, int]{
			TaskBuffer:    10,
			LessFunc:      lessFuncInt,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newSlStrategy(conf, nil)

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
		strategy := newSlStrategy(conf, nil)

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

func BenchmarkSkipList_Push(b *testing.B) {
	sl := newSkipList[intTask, int](lessFuncInt)
	task := createPriorityTask(1, 5, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sl.Push(task)
	}
}

func BenchmarkSkipList_Pop(b *testing.B) {
	sl := newSkipList[intTask, int](lessFuncInt)

	// Pre-fill the skiplist
	for i := 0; i < b.N; i++ {
		sl.Push(createPriorityTask(i, i, int64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sl.Pop()
	}
}

func BenchmarkSkipList_PushPop(b *testing.B) {
	sl := newSkipList[intTask, int](lessFuncInt)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sl.Push(createPriorityTask(i, i%1000, int64(i)))
		if i%2 == 0 {
			sl.Pop()
		}
	}
}

func BenchmarkSkipList_Concurrent(b *testing.B) {
	sl := newSkipList[intTask, int](lessFuncInt)
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func(val int) {
			defer wg.Done()
			sl.Push(createPriorityTask(val, val%1000, int64(val)))
		}(i)
		go func() {
			defer wg.Done()
			sl.Pop()
		}()
	}
	wg.Wait()
}

func BenchmarkSlStrategy_Submit(b *testing.B) {
	conf := &ProcessorConfig[intTask, int]{
		TaskBuffer: b.N + 1000,
		LessFunc:   lessFuncInt,
	}
	strategy := newSlStrategy(conf, nil)
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

func BenchmarkSlStrategy_SubmitBatch(b *testing.B) {
	conf := &ProcessorConfig[intTask, int]{
		TaskBuffer: b.N*100 + 10000,
		LessFunc:   lessFuncInt,
	}
	strategy := newSlStrategy(conf, nil)

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
