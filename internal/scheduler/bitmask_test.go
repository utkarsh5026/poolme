package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

// Test helpers for bitmask strategy

type simpleTask struct {
	value int
}

func createSimpleTask(value int, id int64) *types.SubmittedTask[simpleTask, int] {
	future := types.NewFuture[int, int64]()
	return types.NewSubmittedTask(simpleTask{value: value}, id, future)
}

// Tests for bitmaskStrategy

func TestBitmaskStrategy_New(t *testing.T) {
	t.Run("creates strategy with correct number of workers", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  10,
		}

		strategy := newBitmaskStrategy(conf)

		if strategy == nil {
			t.Fatal("expected non-nil strategy")
		}
		if len(strategy.workerChans) != 4 {
			t.Errorf("expected 4 worker channels, got %d", len(strategy.workerChans))
		}
		// Global queue size is max(TaskBuffer, WorkerCount*10)
		expectedQueueSize := max(10, 4*10) // max(10, 40) = 40
		if cap(strategy.globalQueue) != expectedQueueSize {
			t.Errorf("expected global queue capacity %d, got %d", expectedQueueSize, cap(strategy.globalQueue))
		}
	})

	t.Run("caps workers at maxWorkersBitmask", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 100, // Exceeds maxWorkersBitmask (64)
			TaskBuffer:  10,
		}

		strategy := newBitmaskStrategy(conf)

		if len(strategy.workerChans) != maxWorkersBitmask {
			t.Errorf("expected %d worker channels, got %d", maxWorkersBitmask, len(strategy.workerChans))
		}
	})

	t.Run("initializes with all workers idle", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}

		strategy := newBitmaskStrategy(conf)

		// All workers should start as idle (all bits set for 8 workers)
		expectedMask := uint64((1 << 8) - 1) // 0xFF (all 8 bits set)
		mask := strategy.idleMask.Load()
		if mask != expectedMask {
			t.Errorf("expected initial idle mask %d (0x%X), got %d (0x%X)", expectedMask, expectedMask, mask, mask)
		}
	})
}

func TestBitmaskStrategy_Submit(t *testing.T) {
	t.Run("submits task to idle worker successfully", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		// Mark worker 0 as idle
		strategy.idleMask.Store(1) // bit 0 set = worker 0 idle

		task := createSimpleTask(42, 1)
		err := strategy.Submit(task)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Worker 0 should have received the task
		select {
		case receivedTask := <-strategy.workerChans[0]:
			if receivedTask.Task.value != 42 {
				t.Errorf("expected task value 42, got %d", receivedTask.Task.value)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("expected task in worker channel 0")
		}

		// Worker 0 should now be marked busy
		mask := strategy.idleMask.Load()
		if mask&1 != 0 {
			t.Error("expected worker 0 to be marked busy")
		}
	})

	t.Run("submits to global queue when all workers busy", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		// All workers busy (mask = 0)
		strategy.idleMask.Store(0)

		task := createSimpleTask(42, 1)
		err := strategy.Submit(task)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Task should be in global queue
		select {
		case receivedTask := <-strategy.globalQueue:
			if receivedTask.Task.value != 42 {
				t.Errorf("expected task value 42, got %d", receivedTask.Task.value)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("expected task in global queue")
		}
	})

	t.Run("returns error when closed", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  1, // Small buffer
		}
		strategy := newBitmaskStrategy(conf)

		// All workers busy, so it will try global queue
		strategy.idleMask.Store(0)

		// Fill the global queue
		strategy.globalQueue <- createSimpleTask(1, 1)

		// Close the strategy
		strategy.Shutdown()

		task := createSimpleTask(42, 1)
		err := strategy.Submit(task)

		if err != ErrSchedulerClosed {
			t.Errorf("expected ErrSchedulerClosed, got %v", err)
		}
	})

	t.Run("handles concurrent submissions", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  100,
		}
		strategy := newBitmaskStrategy(conf)

		// Mark all workers as idle
		strategy.idleMask.Store((1 << 8) - 1) // All 8 workers idle

		const goroutines = 10
		const tasksPerGoroutine = 10
		var wg sync.WaitGroup
		wg.Add(goroutines)

		// Submit tasks concurrently
		for i := 0; i < goroutines; i++ {
			go func(start int) {
				defer wg.Done()
				for j := 0; j < tasksPerGoroutine; j++ {
					task := createSimpleTask(start*tasksPerGoroutine+j, int64(start*tasksPerGoroutine+j))
					_ = strategy.Submit(task)
				}
			}(i)
		}

		wg.Wait()

		// Count tasks in worker channels and global queue
		totalTasks := 0
		for _, ch := range strategy.workerChans {
			totalTasks += len(ch)
		}
		totalTasks += len(strategy.globalQueue)

		expectedTotal := goroutines * tasksPerGoroutine
		if totalTasks != expectedTotal {
			t.Errorf("expected %d tasks total, got %d", expectedTotal, totalTasks)
		}
	})
}

func TestBitmaskStrategy_SubmitBatch(t *testing.T) {
	t.Run("submits batch successfully", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  20,
		}
		strategy := newBitmaskStrategy(conf)

		// Mark all workers as idle
		strategy.idleMask.Store((1 << 4) - 1)

		tasks := []*types.SubmittedTask[simpleTask, int]{
			createSimpleTask(1, 1),
			createSimpleTask(2, 2),
			createSimpleTask(3, 3),
		}

		count, err := strategy.SubmitBatch(tasks)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}
	})

	t.Run("handles empty batch", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		tasks := []*types.SubmittedTask[simpleTask, int]{}
		count, err := strategy.SubmitBatch(tasks)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if count != 0 {
			t.Errorf("expected count 0, got %d", count)
		}
	})

	t.Run("returns error on first failure", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 2,
			TaskBuffer:  0, // Zero buffer to force blocking
		}
		strategy := newBitmaskStrategy(conf)

		// Close the strategy and mark all workers as busy
		strategy.Shutdown()
		strategy.idleMask.Store(0)

		tasks := []*types.SubmittedTask[simpleTask, int]{
			createSimpleTask(1, 1),
			createSimpleTask(2, 2),
		}

		count, err := strategy.SubmitBatch(tasks)

		if err == nil {
			t.Error("expected error, got nil")
		}
		// Should return count of successfully submitted tasks
		if count != 0 {
			t.Logf("submitted %d tasks before error", count)
		}
	})
}

func TestBitmaskStrategy_Worker(t *testing.T) {
	t.Run("processes tasks from worker channel", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		var processedValue atomic.Int32

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			processedValue.Store(int32(task.value))
			return task.value * 2, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start worker 0
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 0, executor, handler)
		}()

		// Wait for worker to be ready (it will announce idle)
		time.Sleep(50 * time.Millisecond)

		// Submit a task directly to worker 0's channel
		task := createSimpleTask(42, 1)
		strategy.workerChans[0] <- task

		// Wait for processing
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done

		if processedValue.Load() != 42 {
			t.Errorf("expected processed value 42, got %d", processedValue.Load())
		}
	})

	t.Run("processes tasks from global queue", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		var processedValue atomic.Int32

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			processedValue.Store(int32(task.value))
			return task.value * 2, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start worker 0
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 0, executor, handler)
		}()

		// Wait for worker to be ready
		time.Sleep(50 * time.Millisecond)

		// Submit a task to global queue
		task := createSimpleTask(99, 1)
		strategy.globalQueue <- task

		// Wait for processing
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done

		if processedValue.Load() != 99 {
			t.Errorf("expected processed value 99, got %d", processedValue.Load())
		}
	})

	t.Run("worker announces idle after processing", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start worker 1
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 1, executor, handler)
		}()

		// Wait for worker to announce idle
		time.Sleep(100 * time.Millisecond)

		// Check that bit 1 is set (worker 1 is idle)
		mask := strategy.idleMask.Load()
		if mask&(1<<1) == 0 {
			t.Error("expected worker 1 to be marked idle")
		}

		cancel()
		<-done
	})

	t.Run("multiple workers process tasks concurrently", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    100,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		// Mark all workers as idle
		strategy.idleMask.Store((1 << 4) - 1)

		var processedCount atomic.Int32

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			processedCount.Add(1)
			time.Sleep(10 * time.Millisecond) // Simulate work
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start all workers
		var wg sync.WaitGroup
		wg.Add(4)
		for i := 0; i < 4; i++ {
			go func(id int) {
				defer wg.Done()
				strategy.Worker(ctx, int64(id), executor, handler)
			}(i)
		}

		// Submit tasks
		taskCount := 20
		for i := 0; i < taskCount; i++ {
			_ = strategy.Submit(createSimpleTask(i, int64(i)))
		}

		// Wait for processing
		time.Sleep(500 * time.Millisecond)
		cancel()
		wg.Wait()

		// All tasks should be processed
		if processedCount.Load() != int32(taskCount) {
			t.Errorf("expected %d tasks processed, got %d", taskCount, processedCount.Load())
		}
	})

	t.Run("worker exits on context cancellation", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx, cancel := context.WithCancel(context.Background())

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 0, executor, handler)
		}()

		// Cancel immediately
		cancel()

		// Worker should exit quickly
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Error("worker did not exit after context cancellation")
		}
	})

	t.Run("worker exits on quit signal", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx := context.Background()

		// Start worker
		done := make(chan error, 1)
		go func() {
			done <- strategy.Worker(ctx, 0, executor, handler)
		}()

		// Wait for worker to start
		time.Sleep(50 * time.Millisecond)

		// Signal quit
		strategy.Shutdown()

		// Worker should exit
		select {
		case <-done:
			// Success
		case <-time.After(500 * time.Millisecond):
			t.Error("worker did not exit after quit signal")
		}
	})
}

func TestBitmaskStrategy_AnnounceIdle(t *testing.T) {
	t.Run("sets worker bit in idle mask", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		// Initially all workers busy
		strategy.idleMask.Store(0)
		myBit := uint64(1) << 3
		strategy.announceIdle(3)

		// Check that bit 3 is set
		mask := strategy.idleMask.Load()
		if mask&myBit == 0 {
			t.Error("expected worker 3 bit to be set in idle mask")
		}
	})

	t.Run("handles already idle worker", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		myBit := uint64(1) << 2
		strategy.idleMask.Store(myBit) // Worker 2 already idle

		// Announce idle again (should not panic)
		strategy.announceIdle(2)

		// Bit should still be set
		mask := strategy.idleMask.Load()
		if mask&myBit == 0 {
			t.Error("expected worker 2 bit to remain set")
		}
	})

	t.Run("handles concurrent announcements", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		strategy.idleMask.Store(0)

		var wg sync.WaitGroup
		wg.Add(8)

		// All workers announce idle concurrently
		for i := 0; i < 8; i++ {
			go func(workerID int) {
				defer wg.Done()
				strategy.announceIdle(workerID)
			}(i)
		}

		wg.Wait()

		// All bits should be set
		mask := strategy.idleMask.Load()
		expectedMask := uint64((1 << 8) - 1)
		if mask != expectedMask {
			t.Errorf("expected mask %064b, got %064b", expectedMask, mask)
		}
	})
}

func TestBitmaskStrategy_MarkBusy(t *testing.T) {
	t.Run("clears worker bit in idle mask", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		// All workers idle
		strategy.idleMask.Store((1 << 8) - 1)

		// Worker 5 marks busy
		myBit := uint64(1) << 5
		strategy.markBusy(myBit)

		// Check that bit 5 is cleared
		mask := strategy.idleMask.Load()
		if mask&myBit != 0 {
			t.Error("expected worker 5 bit to be cleared in idle mask")
		}
	})

	t.Run("handles already busy worker", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 8,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		myBit := uint64(1) << 4
		strategy.idleMask.Store(0) // All workers busy

		// Mark busy again (should not panic)
		strategy.markBusy(myBit)

		// Bit should still be cleared
		mask := strategy.idleMask.Load()
		if mask&myBit != 0 {
			t.Error("expected worker 4 bit to remain cleared")
		}
	})
}

func TestBitmaskStrategy_Shutdown(t *testing.T) {
	t.Run("closes quit channel", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 4,
			TaskBuffer:  10,
		}
		strategy := newBitmaskStrategy(conf)

		strategy.Shutdown()

		// quit channel should be closed
		select {
		case _, ok := <-strategy.quit.Wait():
			if ok {
				t.Error("expected quit channel to be closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("timeout waiting for quit channel close")
		}
	})

	t.Run("workers exit after shutdown", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   4,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		executor := func(ctx context.Context, task simpleTask) (int, error) {
			return task.value, nil
		}

		handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
			task.Future.AddResult(*result)
		}

		ctx := context.Background()

		// Start multiple workers
		done := make(chan error, 4)
		for i := 0; i < 4; i++ {
			go func(id int) {
				done <- strategy.Worker(ctx, int64(id), executor, handler)
			}(i)
		}

		// Shutdown
		strategy.Shutdown()

		// All workers should exit
		for i := 0; i < 4; i++ {
			select {
			case <-done:
				// Success
			case <-time.After(500 * time.Millisecond):
				t.Errorf("worker %d did not exit after shutdown", i)
			}
		}
	})
}

func TestBitmaskStrategy_EdgeCases(t *testing.T) {
	t.Run("handles single worker correctly", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount:   1,
			TaskBuffer:    10,
			MaxAttempts:   1,
			ContinueOnErr: true,
		}
		strategy := newBitmaskStrategy(conf)

		// Mark worker as idle
		strategy.idleMask.Store(1)

		task := createSimpleTask(42, 1)
		err := strategy.Submit(task)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Task should be in worker 0's channel
		select {
		case receivedTask := <-strategy.workerChans[0]:
			if receivedTask.Task.value != 42 {
				t.Errorf("expected task value 42, got %d", receivedTask.Task.value)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("expected task in worker channel")
		}
	})

	t.Run("handles maximum workers (64)", func(t *testing.T) {
		conf := &ProcessorConfig[simpleTask, int]{
			WorkerCount: 64,
			TaskBuffer:  100,
		}
		strategy := newBitmaskStrategy(conf)

		if len(strategy.workerChans) != 64 {
			t.Errorf("expected 64 worker channels, got %d", len(strategy.workerChans))
		}

		// Mark all workers as idle
		strategy.idleMask.Store(^uint64(0)) // All bits set

		// Submit 64 tasks
		for i := range 64 {
			err := strategy.Submit(createSimpleTask(i, int64(i)))
			if err != nil {
				t.Errorf("task %d: unexpected error: %v", i, err)
			}
		}

		// Each worker should have received one task
		totalTasks := 0
		for i, ch := range strategy.workerChans {
			if len(ch) > 0 {
				totalTasks++
			} else {
				t.Logf("worker %d has no tasks", i)
			}
		}

		if totalTasks < 1 {
			t.Error("expected at least some workers to receive tasks")
		}
	})
}

// Benchmarks

func BenchmarkBitmaskStrategy_Submit(b *testing.B) {
	conf := &ProcessorConfig[simpleTask, int]{
		WorkerCount: 8,
		TaskBuffer:  b.N + 1000,
	}
	strategy := newBitmaskStrategy(conf)

	// Mark all workers as idle
	strategy.idleMask.Store((1 << 8) - 1)

	task := createSimpleTask(1, 1)

	// Drain channels in background
	ctx := b.Context()

	for i := range 8 {
		go func(ch chan *types.SubmittedTask[simpleTask, int]) {
			for {
				select {
				case <-ctx.Done():
					return
				case <-ch:
				}
			}
		}(strategy.workerChans[i])
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-strategy.globalQueue:
			}
		}
	}()

	for i := 0; b.Loop(); i++ {
		_ = strategy.Submit(task)
		// Reset some workers to idle to keep benchmark realistic
		if i%100 == 0 {
			strategy.idleMask.Store((1 << 8) - 1)
		}
	}
	b.StopTimer()

	strategy.Shutdown()
}

func BenchmarkBitmaskStrategy_SubmitBatch(b *testing.B) {
	conf := &ProcessorConfig[simpleTask, int]{
		WorkerCount: 8,
		TaskBuffer:  b.N*100 + 10000,
	}
	strategy := newBitmaskStrategy(conf)

	tasks := make([]*types.SubmittedTask[simpleTask, int], 100)
	for i := range tasks {
		tasks[i] = createSimpleTask(i, int64(i))
	}

	// Drain channels in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 8; i++ {
		go func(ch chan *types.SubmittedTask[simpleTask, int]) {
			for {
				select {
				case <-ctx.Done():
					return
				case <-ch:
				}
			}
		}(strategy.workerChans[i])
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-strategy.globalQueue:
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = strategy.SubmitBatch(tasks)
		// Reset workers to idle periodically
		if i%10 == 0 {
			strategy.idleMask.Store((1 << 8) - 1)
		}
	}
	b.StopTimer()

	strategy.Shutdown()
}

func BenchmarkBitmaskStrategy_WorkerProcessing(b *testing.B) {
	conf := &ProcessorConfig[simpleTask, int]{
		WorkerCount:   8,
		TaskBuffer:    10000,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}
	strategy := newBitmaskStrategy(conf)

	executor := func(ctx context.Context, task simpleTask) (int, error) {
		return task.value, nil
	}

	handler := func(task *types.SubmittedTask[simpleTask, int], result *types.Result[int, int64]) {
		task.Future.AddResult(*result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start workers
	for i := 0; i < 8; i++ {
		go func(id int) {
			strategy.Worker(ctx, int64(id), executor, handler)
		}(i)
	}

	// Wait for workers to start
	time.Sleep(50 * time.Millisecond)

	// Mark all workers as idle
	strategy.idleMask.Store((1 << 8) - 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strategy.Submit(createSimpleTask(i, int64(i)))
	}
	b.StopTimer()

	// Wait for processing to complete
	time.Sleep(100 * time.Millisecond)
	strategy.Shutdown()
}
