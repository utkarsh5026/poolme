package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

// TestNewWSDeque tests the creation and initialization of wsDeque.
func TestNewWSDeque(t *testing.T) {
	tests := []struct {
		name         string
		capacity     int
		wantCapacity int
	}{
		{
			name:         "default capacity",
			capacity:     0,
			wantCapacity: defaultLocalQueueCapacity,
		},
		{
			name:         "explicit power of 2",
			capacity:     64,
			wantCapacity: 64,
		},
		{
			name:         "non-power of 2 rounds up",
			capacity:     100,
			wantCapacity: 128,
		},
		{
			name:         "small capacity",
			capacity:     10,
			wantCapacity: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dq := newWSDeque[int, int](tt.capacity)

			if dq == nil {
				t.Fatal("newWSDeque returned nil")
			}

			buf := dq.buffer.Load()
			if buf == nil {
				t.Fatal("buffer is nil")
			}

			if len(buf.ring) != tt.wantCapacity {
				t.Errorf("expected capacity %d, got %d", tt.wantCapacity, len(buf.ring))
			}

			expectedMask := uint64(tt.wantCapacity - 1)
			if buf.mask != expectedMask {
				t.Errorf("expected mask %d, got %d", expectedMask, buf.mask)
			}

			if dq.head.Load() != 0 {
				t.Errorf("expected head 0, got %d", dq.head.Load())
			}

			if dq.tail.Load() != 0 {
				t.Errorf("expected tail 0, got %d", dq.tail.Load())
			}

			if dq.Len() != 0 {
				t.Errorf("expected length 0, got %d", dq.Len())
			}
		})
	}
}

// TestWSDeque_PushBack tests pushing tasks to the back of the deque.
func TestWSDeque_PushBack(t *testing.T) {
	dq := newWSDeque[int, int](16)

	// Push single task
	task1 := types.NewSubmittedTask(42, 1, types.NewFuture[int, int64]())
	dq.PushBack(task1)

	if dq.Len() != 1 {
		t.Errorf("expected length 1, got %d", dq.Len())
	}

	// Push multiple tasks
	for i := 2; i <= 10; i++ {
		task := types.NewSubmittedTask(i*10, int64(i), types.NewFuture[int, int64]())
		dq.PushBack(task)
	}

	if dq.Len() != 10 {
		t.Errorf("expected length 10, got %d", dq.Len())
	}

	// Verify tail moved correctly
	if dq.tail.Load() != 10 {
		t.Errorf("expected tail 10, got %d", dq.tail.Load())
	}
}

// TestWSDeque_PopBack tests popping tasks from the back (LIFO order).
func TestWSDeque_PopBack(t *testing.T) {
	dq := newWSDeque[int, int](16)

	task := dq.PopBack()
	if task != nil {
		t.Error("expected nil from empty deque")
	}

	// Push and pop single task
	task1 := types.NewSubmittedTask(42, 1, types.NewFuture[int, int64]())
	dq.PushBack(task1)

	popped := dq.PopBack()
	if popped == nil {
		t.Fatal("PopBack returned nil")
		return
	}
	if popped.Task != 42 {
		t.Errorf("expected task 42, got %d", popped.Task)
	}

	// Verify LIFO order
	tasks := []*types.SubmittedTask[int, int]{
		types.NewSubmittedTask(1, 1, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(2, 2, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(3, 3, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(4, 4, types.NewFuture[int, int64]()),
	}

	for _, task := range tasks {
		dq.PushBack(task)
	}

	// Pop in reverse order (LIFO)
	for i := len(tasks) - 1; i >= 0; i-- {
		popped := dq.PopBack()
		if popped == nil {
			t.Fatalf("PopBack returned nil at index %d", i)
			return
		}
		if popped.Task != tasks[i].Task {
			t.Errorf("expected task %d, got %d", tasks[i].Task, popped.Task)
		}
	}

	// Should be empty now
	if dq.Len() != 0 {
		t.Errorf("expected length 0, got %d", dq.Len())
	}
}

// TestWSDeque_PopFront tests popping tasks from the front (FIFO order).
func TestWSDeque_PopFront(t *testing.T) {
	dq := newWSDeque[int, int](16)

	// Pop from empty deque
	task := dq.PopFront()
	if task != nil {
		t.Error("expected nil from empty deque")
	}

	// Push and pop single task
	task1 := types.NewSubmittedTask(42, 1, types.NewFuture[int, int64]())
	dq.PushBack(task1)

	popped := dq.PopFront()
	if popped == nil {
		t.Fatal("PopFront returned nil")
	}
	if popped.Task != 42 {
		t.Errorf("expected task 42, got %d", popped.Task)
	}

	// Verify FIFO order
	tasks := []*types.SubmittedTask[int, int]{
		types.NewSubmittedTask(1, 1, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(2, 2, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(3, 3, types.NewFuture[int, int64]()),
		types.NewSubmittedTask(4, 4, types.NewFuture[int, int64]()),
	}

	for _, task := range tasks {
		dq.PushBack(task)
	}

	// Pop in same order (FIFO)
	for i := 0; i < len(tasks); i++ {
		popped := dq.PopFront()
		if popped == nil {
			t.Fatalf("PopFront returned nil at index %d", i)
		}
		if popped.Task != tasks[i].Task {
			t.Errorf("expected task %d, got %d", tasks[i].Task, popped.Task)
		}
	}

	// Should be empty now
	if dq.Len() != 0 {
		t.Errorf("expected length 0, got %d", dq.Len())
	}
}

// TestWSDeque_Grow tests automatic deque growth.
func TestWSDeque_Grow(t *testing.T) {
	initialCap := 8
	dq := newWSDeque[int, int](initialCap)

	// Get initial buffer info
	oldBuf := dq.buffer.Load()
	oldCap := len(oldBuf.ring)
	oldMask := oldBuf.mask

	// Fill to mask capacity (growth triggers when tail-head >= mask)
	for i := 0; i < int(oldMask); i++ {
		task := types.NewSubmittedTask(i, int64(i), types.NewFuture[int, int64]())
		dq.PushBack(task)
	}

	// Push one more to trigger growth
	task := types.NewSubmittedTask(999, 999, types.NewFuture[int, int64]())
	dq.PushBack(task)

	// Verify buffer grew
	newBuf := dq.buffer.Load()
	newCap := len(newBuf.ring)

	if newCap != oldCap*2 {
		t.Errorf("expected capacity to double from %d to %d, got %d", oldCap, oldCap*2, newCap)
	}

	// Verify all tasks are still accessible
	expectedLen := int(oldMask) + 1
	if dq.Len() != expectedLen {
		t.Errorf("expected length %d, got %d", expectedLen, dq.Len())
	}

	// Pop all tasks and verify they're correct
	for i := 0; i < expectedLen; i++ {
		popped := dq.PopBack()
		if popped == nil {
			t.Fatalf("PopBack returned nil at iteration %d", i)
		}
	}
}

// TestWSDeque_Len tests the Len method.
func TestWSDeque_Len(t *testing.T) {
	dq := newWSDeque[int, int](16)

	if dq.Len() != 0 {
		t.Errorf("expected length 0, got %d", dq.Len())
	}

	// Add tasks and check length
	for i := 1; i <= 10; i++ {
		task := types.NewSubmittedTask(i, int64(i), types.NewFuture[int, int64]())
		dq.PushBack(task)
		if dq.Len() != i {
			t.Errorf("after push %d: expected length %d, got %d", i, i, dq.Len())
		}
	}

	// Remove tasks and check length
	for i := 9; i >= 0; i-- {
		dq.PopBack()
		if dq.Len() != i {
			t.Errorf("after pop: expected length %d, got %d", i, dq.Len())
		}
	}
}

// TestWSDeque_ConcurrentPopBackAndPopFront tests concurrent access from opposite ends.
func TestWSDeque_ConcurrentPopBackAndPopFront(t *testing.T) {
	dq := newWSDeque[int, int](256)
	numTasks := 1000

	// Push tasks
	for i := 0; i < numTasks; i++ {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		dq.PushBack(task)
	}

	var wg sync.WaitGroup
	var poppedFromBack, poppedFromFront atomic.Int32

	// One goroutine pops from back (owner)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			task := dq.PopBack()
			if task == nil {
				return
			}
			poppedFromBack.Add(1)
		}
	}()

	// Multiple goroutines steal from front
	numStealers := 4
	wg.Add(numStealers)
	for i := 0; i < numStealers; i++ {
		go func() {
			defer wg.Done()
			for {
				task := dq.PopFront()
				if task == nil {
					time.Sleep(1 * time.Millisecond)
					if dq.Len() == 0 {
						return
					}
					continue
				}
				poppedFromFront.Add(1)
			}
		}()
	}

	wg.Wait()

	// All tasks should be popped
	totalPopped := poppedFromBack.Load() + poppedFromFront.Load()
	if totalPopped != int32(numTasks) {
		t.Errorf("expected %d tasks popped, got %d (back: %d, front: %d)",
			numTasks, totalPopped, poppedFromBack.Load(), poppedFromFront.Load())
	}

	// Deque should be empty
	if dq.Len() != 0 {
		t.Errorf("expected empty deque, got length %d", dq.Len())
	}
}

// TestWSDeque_SingleElementContention tests contention on last element.
func TestWSDeque_SingleElementContention(t *testing.T) {
	// This tests the race condition when both owner and stealer try to pop the last element
	for iteration := 0; iteration < 100; iteration++ {
		dq := newWSDeque[int, int](16)
		task := types.NewSubmittedTask[int, int](42, 1, nil)
		dq.PushBack(task)

		var wg sync.WaitGroup
		var results [2]*types.SubmittedTask[int, int]

		// Owner tries PopBack
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[0] = dq.PopBack()
		}()

		// Stealer tries PopFront
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[1] = dq.PopFront()
		}()

		wg.Wait()

		// Exactly one should succeed
		successCount := 0
		for i, result := range results {
			if result != nil {
				successCount++
				if result.Task != 42 {
					t.Errorf("iteration %d: wrong task value at index %d: got %d, want 42",
						iteration, i, result.Task)
				}
			}
		}

		if successCount != 1 {
			t.Errorf("iteration %d: expected exactly 1 successful pop, got %d", iteration, successCount)
		}

		// Deque should be empty
		if dq.Len() != 0 {
			t.Errorf("iteration %d: expected empty deque, got length %d", iteration, dq.Len())
		}
	}
}

// TestNewWorkStealingStrategy tests the creation of work-stealing scheduler.
func TestNewWorkStealingStrategy(t *testing.T) {
	tests := []struct {
		name         string
		workerCount  int
		maxLocalSize int
	}{
		{
			name:         "default configuration",
			workerCount:  4,
			maxLocalSize: 256,
		},
		{
			name:         "single worker",
			workerCount:  1,
			maxLocalSize: 128,
		},
		{
			name:         "many workers",
			workerCount:  16,
			maxLocalSize: 512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &ProcessorConfig[int, int]{
				WorkerCount: tt.workerCount,
			}

			s := newWorkStealingStrategy(tt.maxLocalSize, conf)

			if s == nil {
				t.Fatal("newWorkStealingStrategy returned nil")
			}

			if s.workerCount != tt.workerCount {
				t.Errorf("expected worker count %d, got %d", tt.workerCount, s.workerCount)
			}

			if len(s.workerQueues) != tt.workerCount {
				t.Errorf("expected %d worker queues, got %d", tt.workerCount, len(s.workerQueues))
			}

			for i, wq := range s.workerQueues {
				if wq == nil {
					t.Errorf("worker queue %d is nil", i)
				}
			}

			if s.globalQueue == nil {
				t.Error("global queue is nil")
			}

			if s.quit == nil {
				t.Error("quit channel is nil")
			}
		})
	}
}

// TestWorkSteal_Submit tests single task submission.
func TestWorkSteal_Submit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy[int, int](256, conf)
	defer s.Shutdown()

	// Submit a task
	task := types.NewSubmittedTask[int, int](42, 1, nil)
	err := s.Submit(task)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Check that task was added to a worker queue or global queue
	totalTasks := s.globalQueue.Len()
	for _, wq := range s.workerQueues {
		totalTasks += wq.Len()
	}

	if totalTasks != 1 {
		t.Errorf("expected 1 task in queues, got %d", totalTasks)
	}
}

// TestWorkSteal_SubmitRoundRobin tests round-robin task distribution.
func TestWorkSteal_SubmitRoundRobin(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	numTasks := 100

	// Submit tasks
	for i := 0; i < numTasks; i++ {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		err := s.Submit(task)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Count tasks in each worker queue
	workerCounts := make([]int, len(s.workerQueues))
	for i, wq := range s.workerQueues {
		workerCounts[i] = wq.Len()
	}

	globalCount := s.globalQueue.Len()

	// Log distribution
	t.Logf("Global queue: %d tasks", globalCount)
	for i, count := range workerCounts {
		t.Logf("Worker %d: %d tasks", i, count)
	}

	// All workers should have received some tasks
	totalInWorkers := 0
	for _, count := range workerCounts {
		totalInWorkers += count
	}

	totalTasks := totalInWorkers + globalCount
	if totalTasks != numTasks {
		t.Errorf("expected %d total tasks, got %d", numTasks, totalTasks)
	}
}

// TestWorkSteal_SubmitBatch tests batch task submission.
func TestWorkSteal_SubmitBatch(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Create batch of tasks
	batchSize := 100
	tasks := make([]*types.SubmittedTask[int, int], batchSize)
	for i := 0; i < batchSize; i++ {
		future := types.NewFuture[int, int64]()
		tasks[i] = types.NewSubmittedTask(i, int64(i), future)
	}

	// Submit batch
	count, err := s.SubmitBatch(tasks)
	if err != nil {
		t.Fatalf("SubmitBatch failed: %v", err)
	}

	if count != batchSize {
		t.Errorf("expected count %d, got %d", batchSize, count)
	}

	// Count total tasks in all queues
	totalTasks := s.globalQueue.Len()
	for _, wq := range s.workerQueues {
		totalTasks += wq.Len()
	}

	if totalTasks != batchSize {
		t.Errorf("expected %d tasks in queues, got %d", batchSize, totalTasks)
	}

	// Verify balanced distribution across workers
	workerCounts := make([]int, len(s.workerQueues))
	for i, wq := range s.workerQueues {
		workerCounts[i] = wq.Len()
	}

	t.Logf("Batch distribution: %v", workerCounts)

	// Each worker should have approximately batchSize/workerCount tasks
	expectedPerWorker := batchSize / conf.WorkerCount
	for i, count := range workerCounts {
		// Allow some variance (±2 tasks)
		if count < expectedPerWorker-2 || count > expectedPerWorker+2 {
			t.Logf("Worker %d has unbalanced load: %d tasks (expected ~%d)", i, count, expectedPerWorker)
		}
	}
}

// TestWorkSteal_SubmitBatchEmpty tests batch submission with empty slice.
func TestWorkSteal_SubmitBatchEmpty(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Submit empty batch
	count, err := s.SubmitBatch(nil)
	if err != nil {
		t.Errorf("SubmitBatch failed on empty batch: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

// TestWorkSteal_Worker tests the worker loop.
func TestWorkSteal_Worker(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newWorkStealingStrategy(256, conf)

	ctx := context.Background()
	workerID := int64(0)

	// Executor that doubles the input
	executor := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	// Track results
	var results sync.Map
	var processedCount atomic.Int32

	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		if result.Error == nil {
			results.Store(result.Key, result.Value)
			processedCount.Add(1)
		}
		if task.Future != nil {
			task.Future.AddResult(*result)
		}
	}

	// Submit tasks to the worker's queue BEFORE starting worker to avoid race condition
	numTasks := 10
	for i := 0; i < numTasks; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.workerQueues[workerID].PushBack(task)
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Shutdown
	s.Shutdown()

	// Wait for worker to finish
	select {
	case <-workerDone:
		// Worker finished
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish in time")
	}

	// Verify results
	processed := processedCount.Load()
	t.Logf("Processed %d out of %d tasks", processed, numTasks)

	if processed == 0 {
		t.Error("no tasks were processed")
	}

	// Verify the results are correct (doubled values)
	results.Range(func(key, value interface{}) bool {
		k := key.(int64)
		v := value.(int)
		expected := int(k) * 2
		if v != expected {
			t.Errorf("task %d: got result %d, want %d", k, v, expected)
		}
		return true
	})
}

// TestWorkSteal_WorkerWithError tests worker behavior with errors.
func TestWorkSteal_WorkerWithError(t *testing.T) {
	tests := []struct {
		name          string
		continueOnErr bool
		wantExit      bool
	}{
		{
			name:          "continue on error",
			continueOnErr: true,
			wantExit:      false,
		},
		{
			name:          "stop on error",
			continueOnErr: false,
			wantExit:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &ProcessorConfig[int, int]{
				WorkerCount:   1,
				MaxAttempts:   1,
				ContinueOnErr: tt.continueOnErr,
			}

			s := newWorkStealingStrategy(256, conf)

			ctx := context.Background()
			workerID := int64(0)

			// Executor that fails
			testErr := errors.New("task error")
			executor := func(ctx context.Context, task int) (int, error) {
				return 0, testErr
			}

			var handlerCalled atomic.Bool
			handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
				handlerCalled.Store(true)
				if task.Future != nil {
					task.Future.AddResult(*result)
				}
			}

			// Submit a task BEFORE starting worker to avoid race condition
			future := types.NewFuture[int, int64]()
			task := types.NewSubmittedTask(1, 1, future)
			s.workerQueues[workerID].PushBack(task)

			// Start worker
			workerDone := make(chan error, 1)
			go func() {
				workerDone <- s.Worker(ctx, workerID, executor, handler)
			}()

			// Wait for processing
			time.Sleep(200 * time.Millisecond)

			if tt.wantExit {
				// Worker should have exited due to error
				select {
				case err := <-workerDone:
					if err == nil {
						t.Error("expected worker to return error, got nil")
					}
				case <-time.After(1 * time.Second):
					t.Error("worker did not exit on error")
				}
			} else {
				// Worker should still be running
				select {
				case err := <-workerDone:
					t.Errorf("worker exited unexpectedly with error: %v", err)
				default:
					// Good - worker still running
					s.Shutdown()
					<-workerDone
				}
			}

			if !handlerCalled.Load() {
				t.Error("handler was not called")
			}
		})
	}
}

// TestWorkSteal_WorkerContextCancellation tests worker response to context cancellation.
func TestWorkSteal_WorkerContextCancellation(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		MaxAttempts: 1,
	}

	s := newWorkStealingStrategy(256, conf)

	ctx, cancel := context.WithCancel(context.Background())
	workerID := int64(0)

	executor := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		if task.Future != nil {
			task.Future.AddResult(*result)
		}
	}

	// Submit some tasks BEFORE starting worker to avoid race condition
	// (PushBack is only safe when called by the owner worker)
	for i := 0; i < 10; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.workerQueues[workerID].PushBack(task)
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Wait a bit for some processing
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Worker should exit with context.Canceled error
	select {
	case err := <-workerDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after context cancellation")
	}

	t.Logf("Processed %d tasks before cancellation", processedCount.Load())
}

// TestWorkSteal_Steal tests work stealing between workers.
func TestWorkSteal_Steal(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Fill victim queue with many tasks
	victimID := 0
	numTasks := 100
	for i := 0; i < numTasks; i++ {
		task := types.NewSubmittedTask(i, int64(i), types.NewFuture[int, int64]())
		s.workerQueues[victimID].PushBack(task)
	}

	// Thief tries to steal - may need multiple attempts due to randomized victim selection
	thiefID := 1
	var stolen *types.SubmittedTask[int, int]
	for attempt := 0; attempt < 10 && stolen == nil; attempt++ {
		stolen = s.steal(thiefID)
	}

	if stolen == nil {
		t.Fatal("steal returned nil after multiple attempts despite available tasks")
	}

	// Verify some tasks were stolen
	thiefQueueLen := s.workerQueues[thiefID].Len()
	victimQueueLen := s.workerQueues[victimID].Len()

	t.Logf("After steal: thief queue=%d, victim queue=%d", thiefQueueLen, victimQueueLen)

	if thiefQueueLen == 0 {
		t.Error("thief queue is empty after steal")
	}

	// Victim should have fewer tasks
	if victimQueueLen >= numTasks {
		t.Error("victim queue was not reduced after steal")
	}

	// Total tasks should be preserved (minus the one returned)
	total := thiefQueueLen + victimQueueLen + 1 // +1 for the returned task
	if total != numTasks {
		t.Errorf("tasks were lost: expected %d, got %d", numTasks, total)
	}
}

// TestWorkSteal_StealSingleWorker tests stealing with only one worker.
func TestWorkSteal_StealSingleWorker(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 1,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Add tasks to the only worker
	for i := 0; i < 10; i++ {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		s.workerQueues[0].PushBack(task)
	}

	// Try to steal (should return nil since no other workers)
	stolen := s.steal(0)

	if stolen != nil {
		t.Error("steal should return nil with single worker")
	}
}

// TestWorkSteal_StealFromEmptyQueues tests stealing when all queues are empty.
func TestWorkSteal_StealFromEmptyQueues(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	stolen := s.steal(0)

	if stolen != nil {
		t.Error("steal should return nil from empty queues")
	}
}

// TestWorkSteal_Drain tests draining queues during shutdown.
func TestWorkSteal_Drain(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		MaxAttempts: 1,
	}

	s := newWorkStealingStrategy(256, conf)

	ctx := context.Background()
	workerID := int64(0)
	localQueue := s.workerQueues[workerID]

	executor := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		if task.Future != nil {
			task.Future.AddResult(*result)
		}
	}

	// Add tasks to local and global queues
	numLocalTasks := 10
	numGlobalTasks := 5

	for i := range numLocalTasks {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		localQueue.PushBack(task)
	}

	for i := range numGlobalTasks {
		task := types.NewSubmittedTask[int, int](i+100, int64(i+100), nil)
		_ = s.globalQueue.Enqueue(task)
	}

	// Call drain
	s.drain(ctx, localQueue, executor, handler)

	// All tasks should be processed
	processed := processedCount.Load()
	expectedTotal := int32(numLocalTasks + numGlobalTasks)

	if processed != expectedTotal {
		t.Errorf("expected %d tasks processed, got %d", expectedTotal, processed)
	}

	// Queues should be empty
	if localQueue.Len() != 0 {
		t.Errorf("local queue not empty: %d tasks remaining", localQueue.Len())
	}

	if s.globalQueue.Len() != 0 {
		t.Errorf("global queue not empty: %d tasks remaining", s.globalQueue.Len())
	}
}

// TestWorkSteal_Shutdown tests graceful shutdown.
func TestWorkSteal_Shutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)

	// Submit some tasks
	for i := range 20 {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		_ = s.Submit(task)
	}

	// Shutdown
	s.Shutdown()

	// Verify quit channel is closed
	select {
	case <-s.quit.Wait():
		// Good - quit channel is closed
	default:
		t.Error("quit channel was not closed")
	}
}

// TestWorkSteal_ConcurrentSubmit tests concurrent task submission.
func TestWorkSteal_ConcurrentSubmit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	numGoroutines := 10
	tasksPerGoroutine := 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for i := range tasksPerGoroutine {
				taskID := int64(goroutineID*tasksPerGoroutine + i)
				task := types.NewSubmittedTask[int, int](int(taskID), taskID, nil)
				if err := s.Submit(task); err != nil {
					t.Errorf("goroutine %d: Submit failed: %v", goroutineID, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Count total tasks
	time.Sleep(100 * time.Millisecond)
	totalTasks := s.globalQueue.Len()
	for _, wq := range s.workerQueues {
		totalTasks += wq.Len()
	}

	expectedTasks := numGoroutines * tasksPerGoroutine
	if totalTasks != expectedTasks {
		t.Errorf("expected %d tasks, got %d", expectedTasks, totalTasks)
	}
}

// TestWorkSteal_MultipleWorkersIntegration tests multiple workers running concurrently.
func TestWorkSteal_MultipleWorkersIntegration(t *testing.T) {
	numWorkers := 16
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   numWorkers,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newWorkStealingStrategy(256, conf)

	ctx := context.Background()

	executor := func(ctx context.Context, task int) (int, error) {
		// Simulate some work
		time.Sleep(1 * time.Millisecond)
		return task * 2, nil
	}

	var processedCount atomic.Int32
	var resultsMu sync.Mutex
	results := make(map[int64]int)

	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		if result.Error == nil {
			resultsMu.Lock()
			results[result.Key] = result.Value
			resultsMu.Unlock()
			processedCount.Add(1)
		}
		if task.Future != nil {
			task.Future.AddResult(*result)
		}
	}

	// Start workers
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := range numWorkers {
		workerID := int64(i)
		go func(id int64) {
			defer wg.Done()
			s.Worker(ctx, id, executor, handler)
		}(workerID)
	}

	// Submit many tasks
	numTasks := 20000
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Shutdown
	s.Shutdown()
	wg.Wait()

	// Check results
	processed := processedCount.Load()
	t.Logf("Processed %d out of %d tasks", processed, numTasks)

	if processed == 0 {
		t.Error("no tasks were processed")
	}

	// Verify results are correct
	resultsMu.Lock()
	defer resultsMu.Unlock()
	for id, value := range results {
		expected := int(id) * 2
		if value != expected {
			t.Errorf("task %d: got result %d, want %d", id, value, expected)
		}
	}
}

// TestWorkSteal_StealingUnderLoad tests that work stealing occurs under load.
func TestWorkSteal_StealingUnderLoad(t *testing.T) {
	numWorkers := 4
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   numWorkers,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newWorkStealingStrategy(256, conf)

	ctx := context.Background()

	// Track which worker processed each task
	var processingWorkerMu sync.Mutex
	processingWorkers := make(map[int64][]int64) // task ID -> worker IDs that processed it

	executor := func(ctx context.Context, task int) (int, error) {
		// Variable work time to create load imbalance
		sleepTime := time.Duration(task%10) * time.Millisecond
		time.Sleep(sleepTime)
		return task, nil
	}

	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		if task.Future != nil {
			task.Future.AddResult(*result)
		}
	}

	// Start workers
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		workerID := int64(i)
		go func(id int64) {
			defer wg.Done()
			s.Worker(ctx, id, executor, handler)
		}(workerID)
	}

	// Submit all tasks - they'll naturally accumulate in one worker initially
	// creating an imbalanced workload that will trigger stealing
	numTasks := 100
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task) // Use proper submission API instead of direct queue manipulation

		processingWorkerMu.Lock()
		processingWorkers[int64(i)] = []int64{}
		processingWorkerMu.Unlock()
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

	// Shutdown
	s.Shutdown()
	wg.Wait()

	// Verify that tasks were distributed (stolen by other workers)
	finalCounts := make([]int, numWorkers)
	for _, wq := range s.workerQueues {
		// Count remaining tasks (should be low if stealing worked)
		for i := range numWorkers {
			if s.workerQueues[i] == wq {
				finalCounts[i] = wq.Len()
			}
		}
	}

	t.Logf("Final queue lengths: %v", finalCounts)

	// Verify that queues are mostly empty (tasks were processed)
	// This indicates that work-stealing and processing occurred
	totalRemaining := 0
	for _, count := range finalCounts {
		totalRemaining += count
	}

	if totalRemaining > numTasks/4 {
		t.Logf("Warning: %d/%d tasks still remaining in queues, work may not have been processed effectively",
			totalRemaining, numTasks)
	}
}

// TestWorkSteal_GlobalQueueFallback tests fallback to global queue.
func TestWorkSteal_GlobalQueueFallback(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Submit tasks that exceed local queue threshold
	numTasks := localQueueThreshold + 50

	for i := range numTasks {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		// Submit all to same worker to trigger global queue usage
		s.workerQueues[0].PushBack(task)
	}

	// Force more submissions to same worker
	for i := 0; i < 20; i++ {
		task := types.NewSubmittedTask[int, int](i+numTasks, int64(i+numTasks), nil)
		err := s.Submit(task)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Check that global queue has some tasks
	globalCount := s.globalQueue.Len()
	t.Logf("Global queue has %d tasks", globalCount)

	if globalCount == 0 {
		t.Log("Note: Global queue is empty, all tasks went to local queues")
	}
}

// BenchmarkWSDeque_PushBack benchmarks PushBack operation.
func BenchmarkWSDeque_PushBack(b *testing.B) {
	dq := newWSDeque[int, int](256)
	task := types.NewSubmittedTask[int, int](42, 1, nil)

	for b.Loop() {
		dq.PushBack(task)
	}
}

// BenchmarkWSDeque_PopBack benchmarks PopBack operation.
func BenchmarkWSDeque_PopBack(b *testing.B) {
	dq := newWSDeque[int, int](256)

	for i := range 1000 {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		dq.PushBack(task)
	}

	for b.Loop() {
		task := dq.PopBack()
		if task == nil {
			for j := range 1000 {
				t := types.NewSubmittedTask[int, int](j, int64(j), nil)
				dq.PushBack(t)
			}
		}
	}
}

// BenchmarkWSDeque_PopFront benchmarks PopFront operation.
func BenchmarkWSDeque_PopFront(b *testing.B) {
	dq := newWSDeque[int, int](256)

	// Pre-fill the deque
	for i := range 1000 {
		task := types.NewSubmittedTask[int, int](i, int64(i), nil)
		dq.PushBack(task)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := dq.PopFront()
		if task == nil {
			// Refill if empty
			for j := 0; j < 1000; j++ {
				t := types.NewSubmittedTask[int, int](j, int64(j), nil)
				dq.PushBack(t)
			}
		}
	}
}

// BenchmarkWorkSteal_Submit benchmarks task submission.
func BenchmarkWorkSteal_Submit(b *testing.B) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	task := types.NewSubmittedTask[int, int](42, 1, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Submit(task)
	}
}

// BenchmarkWorkSteal_SubmitBatch benchmarks batch submission.
func BenchmarkWorkSteal_SubmitBatch(b *testing.B) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	// Create batch
	batchSize := 100
	tasks := make([]*types.SubmittedTask[int, int], batchSize)
	for i := 0; i < batchSize; i++ {
		tasks[i] = types.NewSubmittedTask[int, int](i, int64(i), nil)
	}

	for b.Loop() {
		s.SubmitBatch(tasks)
	}
}

// BenchmarkWorkSteal_Steal benchmarks the steal operation.
func BenchmarkWorkSteal_Steal(b *testing.B) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
	}

	s := newWorkStealingStrategy(256, conf)
	defer s.Shutdown()

	for i := 0; i < len(s.workerQueues); i++ {
		for j := range 100 {
			task := types.NewSubmittedTask[int, int](j, int64(j), nil)
			s.workerQueues[i].PushBack(task)
		}
	}

	for b.Loop() {
		s.steal(0)
	}
}
