package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/gopool/internal/types"
)

// TestNewMPMCQueue tests the creation and initialization of MPMC queue.
func TestNewMPMCQueue(t *testing.T) {
	tests := []struct {
		name         string
		capacity     int
		bounded      bool
		wantCapacity int
		wantBounded  bool
	}{
		{
			name:         "unbounded with zero capacity",
			capacity:     0,
			bounded:      false,
			wantCapacity: defaultInitialCapacity,
			wantBounded:  false,
		},
		{
			name:         "bounded with small capacity",
			capacity:     10,
			bounded:      true,
			wantCapacity: 16, // next power of 2
			wantBounded:  true,
		},
		{
			name:         "bounded with power of 2 capacity",
			capacity:     128,
			bounded:      true,
			wantCapacity: 128,
			wantBounded:  true,
		},
		{
			name:         "unbounded with explicit capacity",
			capacity:     100,
			bounded:      false,
			wantCapacity: 128, // next power of 2
			wantBounded:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newMPMCQueue[int](tt.capacity, tt.bounded)

			if q == nil {
				t.Fatal("newMPMCQueue returned nil")
			}

			if q.Cap() != tt.wantCapacity {
				t.Errorf("capacity = %d, want %d", q.Cap(), tt.wantCapacity)
			}

			if q.IsBounded() != tt.wantBounded {
				t.Errorf("bounded = %v, want %v", q.IsBounded(), tt.wantBounded)
			}

			if q.Len() != 0 {
				t.Errorf("initial length = %d, want 0", q.Len())
			}
		})
	}
}

// TestMPMCQueue_EnqueueDequeue tests basic enqueue and dequeue operations.
func TestMPMCQueue_EnqueueDequeue(t *testing.T) {
	q := newMPMCQueue[int](16, false)

	// Enqueue single item
	err := q.Enqueue(42)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if q.Len() != 1 {
		t.Errorf("length after enqueue = %d, want 1", q.Len())
	}

	// Dequeue item
	val, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if val != 42 {
		t.Errorf("dequeued value = %d, want 42", val)
	}

	if q.Len() != 0 {
		t.Errorf("length after dequeue = %d, want 0", q.Len())
	}
}

// TestMPMCQueue_EnqueueDequeueMultiple tests multiple enqueue/dequeue operations.
func TestMPMCQueue_EnqueueDequeueMultiple(t *testing.T) {
	q := newMPMCQueue[int](32, false)

	// Enqueue multiple items
	numItems := 10
	for i := 0; i < numItems; i++ {
		err := q.Enqueue(i)
		if err != nil {
			t.Fatalf("Enqueue(%d) failed: %v", i, err)
		}
	}

	if q.Len() != numItems {
		t.Errorf("length = %d, want %d", q.Len(), numItems)
	}

	// Dequeue all items in order
	for i := 0; i < numItems; i++ {
		val, err := q.Dequeue()
		if err != nil {
			t.Fatalf("Dequeue failed: %v", err)
		}
		if val != i {
			t.Errorf("dequeued value = %d, want %d", val, i)
		}
	}

	if q.Len() != 0 {
		t.Errorf("length after dequeue all = %d, want 0", q.Len())
	}
}

// TestMPMCQueue_BoundedQueueFull tests bounded queue full behavior.
func TestMPMCQueue_BoundedQueueFull(t *testing.T) {
	capacity := 8
	q := newMPMCQueue[int](capacity, true)

	// Fill the queue
	for i := 0; i < capacity; i++ {
		err := q.Enqueue(i)
		if err != nil {
			t.Fatalf("Enqueue(%d) failed: %v", i, err)
		}
	}

	// Next enqueue should fail with ErrQueueFull
	err := q.Enqueue(999)
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}

	// Dequeue one item to make space
	_, err = q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Should be able to enqueue again
	err = q.Enqueue(1000)
	if err != nil {
		t.Errorf("Enqueue after dequeue failed: %v", err)
	}
}

// TestMPMCQueue_UnboundedQueueGrowth tests unbounded queue with concurrent enqueue/dequeue.
func TestMPMCQueue_UnboundedQueueGrowth(t *testing.T) {
	initialCapacity := 16
	q := newMPMCQueue[int](initialCapacity, false)

	// For an unbounded queue, we need to dequeue concurrently to test wrapping
	// because the ring buffer is fixed size and will spin-wait when full
	numItems := initialCapacity * 2

	var wg sync.WaitGroup
	wg.Add(2)

	// Producer
	go func() {
		defer wg.Done()
		for i := range numItems {
			err := q.Enqueue(i)
			if err != nil {
				t.Errorf("Enqueue(%d) failed: %v", i, err)
			}
		}
	}()

	// Consumer
	received := make([]int, 0, numItems)
	var mu sync.Mutex
	go func() {
		defer wg.Done()
		for i := 0; i < numItems; i++ {
			val, err := q.Dequeue()
			if err != nil {
				t.Errorf("Dequeue failed at %d: %v", i, err)
				return
			}
			mu.Lock()
			received = append(received, val)
			mu.Unlock()
		}
	}()

	wg.Wait()

	// Verify all items were received
	mu.Lock()
	defer mu.Unlock()
	if len(received) != numItems {
		t.Errorf("received %d items, want %d", len(received), numItems)
	}

	// Verify items are in order
	for i, val := range received {
		if val != i {
			t.Errorf("item %d: got value %d, want %d", i, val, i)
		}
	}
}

// TestMPMCQueue_TryDequeue tests non-blocking dequeue.
func TestMPMCQueue_TryDequeue(t *testing.T) {
	q := newMPMCQueue[int](16, false)

	// TryDequeue on empty queue should return false
	val, ok := q.TryDequeue()
	if ok {
		t.Errorf("TryDequeue on empty queue returned value %d", val)
	}

	// Enqueue items
	q.Enqueue(10)
	q.Enqueue(20)

	// TryDequeue should succeed
	val, ok = q.TryDequeue()
	if !ok {
		t.Error("TryDequeue failed on non-empty queue")
	}
	if val != 10 {
		t.Errorf("TryDequeue value = %d, want 10", val)
	}

	// Second TryDequeue should succeed
	val, ok = q.TryDequeue()
	if !ok {
		t.Error("TryDequeue failed on non-empty queue")
	}
	if val != 20 {
		t.Errorf("TryDequeue value = %d, want 20", val)
	}

	// Queue is empty again
	_, ok = q.TryDequeue()
	if ok {
		t.Error("TryDequeue succeeded on empty queue")
	}
}

// TestMPMCQueue_Close tests queue close behavior.
func TestMPMCQueue_Close(t *testing.T) {
	q := newMPMCQueue[int](16, false)

	// Enqueue items
	q.Enqueue(1)
	q.Enqueue(2)

	// Close the queue
	q.Close()

	// Dequeue should still work for remaining items
	val, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed after close: %v", err)
	}
	if val != 1 {
		t.Errorf("dequeued value = %d, want 1", val)
	}

	val, err = q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed after close: %v", err)
	}
	if val != 2 {
		t.Errorf("dequeued value = %d, want 2", val)
	}

	// Dequeue on empty closed queue should return error
	_, err = q.Dequeue()
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}

	// TryDequeue should return false
	_, ok := q.TryDequeue()
	if ok {
		t.Error("TryDequeue succeeded on closed empty queue")
	}
}

// TestMPMCQueue_ConcurrentEnqueueDequeue tests concurrent producers and consumers.
func TestMPMCQueue_ConcurrentEnqueueDequeue(t *testing.T) {
	q := newMPMCQueue[int](1024, false)

	numProducers := 4
	numConsumers := 4
	itemsPerProducer := 100

	var wg sync.WaitGroup
	var consumedCount atomic.Int32

	// Start consumers
	wg.Add(numConsumers)
	for c := 0; c < numConsumers; c++ {
		go func() {
			defer wg.Done()
			for {
				_, err := q.Dequeue()
				if errors.Is(err, ErrQueueClosed) {
					return
				}
				if err == nil {
					consumedCount.Add(1)
				}
			}
		}()
	}

	// Start producers
	var producerWg sync.WaitGroup
	producerWg.Add(numProducers)
	for p := 0; p < numProducers; p++ {
		go func(producerID int) {
			defer producerWg.Done()
			for i := 0; i < itemsPerProducer; i++ {
				q.Enqueue(producerID*itemsPerProducer + i)
			}
		}(p)
	}

	// Wait for producers to finish
	producerWg.Wait()

	// Give consumers time to drain the queue
	time.Sleep(200 * time.Millisecond)

	// Close queue to stop consumers
	q.Close()
	wg.Wait()

	expectedTotal := numProducers * itemsPerProducer
	consumed := int(consumedCount.Load())
	if consumed != expectedTotal {
		t.Errorf("consumed %d items, want %d", consumed, expectedTotal)
	}

	t.Logf("Successfully processed %d items with %d producers and %d consumers",
		consumed, numProducers, numConsumers)
}

// TestMPMCQueue_MultipleCloses tests that multiple close calls are safe.
func TestMPMCQueue_MultipleCloses(t *testing.T) {
	q := newMPMCQueue[int](16, false)

	// Close multiple times should not panic
	q.Close()
	q.Close()
	q.Close()

	// Verify queue is closed
	_, err := q.Dequeue()
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

// TestNewMPMCStrategy tests the creation of MPMC strategy.
func TestNewMPMCStrategy(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		bounded  bool
	}{
		{
			name:     "unbounded strategy",
			capacity: 0,
			bounded:  false,
		},
		{
			name:     "bounded strategy",
			capacity: 128,
			bounded:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &ProcessorConfig[int, int]{
				WorkerCount:   4,
				TaskBuffer:    tt.capacity,
				MaxAttempts:   1,
				ContinueOnErr: true,
			}

			s := newMPMCStrategy(conf, tt.bounded, tt.capacity)

			if s == nil {
				t.Fatal("newMPMCStrategy returned nil")
			}

			if s.queue == nil {
				t.Error("queue is nil")
			}

			if s.conf == nil {
				t.Error("config is nil")
			}

			if s.runner == nil {
				t.Error("runner is nil")
			}

			if s.quitter == nil {
				t.Error("quitter is nil")
			}

			if s.queue.IsBounded() != tt.bounded {
				t.Errorf("queue bounded = %v, want %v", s.queue.IsBounded(), tt.bounded)
			}
		})
	}
}

// TestMPMCStrategy_Submit tests single task submission.
func TestMPMCStrategy_Submit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		TaskBuffer:    16,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 16)
	defer s.Shutdown()

	// Create and submit task
	future := types.NewFuture[int, int64]()
	task := types.NewSubmittedTask(42, 1, future)

	err := s.Submit(task)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if s.queue.Len() != 1 {
		t.Errorf("queue length = %d, want 1", s.queue.Len())
	}
}

// TestMPMCStrategy_SubmitBatch tests batch task submission.
func TestMPMCStrategy_SubmitBatch(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		TaskBuffer:    64,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 64)
	defer s.Shutdown()

	// Create batch of tasks
	batchSize := 20
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
		t.Errorf("submitted count = %d, want %d", count, batchSize)
	}

	// Queue should have all tasks
	time.Sleep(50 * time.Millisecond)
	qLen := s.queue.Len()
	if qLen != batchSize {
		t.Errorf("queue length = %d, want %d", qLen, batchSize)
	}
}

// TestMPMCStrategy_SubmitAfterShutdown tests that submit fails after shutdown.
func TestMPMCStrategy_SubmitAfterShutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  16,
	}

	s := newMPMCStrategy(conf, false, 16)
	s.Shutdown()

	// Submit should fail
	future := types.NewFuture[int, int64]()
	task := types.NewSubmittedTask(42, 1, future)
	err := s.Submit(task)

	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

// TestMPMCStrategy_SubmitBatchAfterShutdown tests that batch submit fails after shutdown.
func TestMPMCStrategy_SubmitBatchAfterShutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  16,
	}

	s := newMPMCStrategy(conf, false, 16)
	s.Shutdown()

	// Create batch
	tasks := make([]*types.SubmittedTask[int, int], 5)
	for i := 0; i < 5; i++ {
		future := types.NewFuture[int, int64]()
		tasks[i] = types.NewSubmittedTask(i, int64(i), future)
	}

	// SubmitBatch should fail
	count, err := s.SubmitBatch(tasks)
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

// TestMPMCStrategy_Worker tests worker execution.
func TestMPMCStrategy_Worker(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		TaskBuffer:    32,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 32)

	ctx := context.Background()
	workerID := int64(0)

	// Executor that doubles the input
	executor := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	// Track results
	var results []int
	var resultsMu sync.Mutex
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		if result.Error == nil {
			results = append(results, result.Value)
		}
		task.Future.AddResult(*result)
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Submit tasks
	numTasks := 10
	for i := 0; i < numTasks; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Shutdown to stop worker
	s.Shutdown()

	// Wait for worker to finish
	select {
	case err := <-workerDone:
		if err != nil {
			t.Errorf("worker returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish in time")
	}

	// Verify results
	resultsMu.Lock()
	defer resultsMu.Unlock()
	if len(results) != numTasks {
		t.Errorf("processed %d tasks, want %d", len(results), numTasks)
	}

	// Verify the results are correct (doubled values)
	expectedResults := make(map[int]bool)
	for i := 0; i < numTasks; i++ {
		expectedResults[i*2] = true
	}
	for _, result := range results {
		if !expectedResults[result] {
			t.Errorf("unexpected result: %d", result)
		}
	}

	t.Logf("Successfully processed %d tasks", len(results))
}

// TestMPMCStrategy_WorkerWithError tests worker behavior with errors.
func TestMPMCStrategy_WorkerWithError(t *testing.T) {
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
				TaskBuffer:    16,
				MaxAttempts:   1,
				ContinueOnErr: tt.continueOnErr,
			}

			s := newMPMCStrategy(conf, false, 16)

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
				task.Future.AddResult(*result)
			}

			// Start worker
			workerDone := make(chan error, 1)
			go func() {
				workerDone <- s.Worker(ctx, workerID, executor, handler)
			}()

			// Submit a task
			future := types.NewFuture[int, int64]()
			task := types.NewSubmittedTask(1, 1, future)
			s.Submit(task)

			// Wait for processing
			time.Sleep(100 * time.Millisecond)

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

// TestMPMCStrategy_MultipleWorkers tests multiple workers processing concurrently.
func TestMPMCStrategy_MultipleWorkers(t *testing.T) {
	numWorkers := 4
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   numWorkers,
		TaskBuffer:    128,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 128)

	ctx := context.Background()

	executor := func(ctx context.Context, task int) (int, error) {
		time.Sleep(5 * time.Millisecond)
		return task * 2, nil
	}

	var processedCount atomic.Int32
	var resultsMu sync.Mutex
	results := make(map[int64]int)

	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		if result.Error == nil {
			results[result.Key] = result.Value
			processedCount.Add(1)
		}
		task.Future.AddResult(*result)
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

	// Submit many tasks
	numTasks := 100
	for i := 0; i < numTasks; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Wait for processing
	time.Sleep(1 * time.Second)

	// Shutdown to stop workers
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

// TestMPMCStrategy_ConcurrentSubmit tests concurrent task submission with consumers.
func TestMPMCStrategy_ConcurrentSubmit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   4,
		TaskBuffer:    256,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 256)

	ctx := context.Background()
	executor := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		task.Future.AddResult(*result)
	}

	// Start workers to consume tasks
	var workerWg sync.WaitGroup
	workerWg.Add(conf.WorkerCount)
	for i := 0; i < conf.WorkerCount; i++ {
		go func(workerID int64) {
			defer workerWg.Done()
			s.Worker(ctx, workerID, executor, handler)
		}(int64(i))
	}

	// Submit tasks concurrently
	numGoroutines := 10
	tasksPerGoroutine := 50
	totalTasks := numGoroutines * tasksPerGoroutine

	var submitWg sync.WaitGroup
	var submitErrors atomic.Int32

	submitWg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer submitWg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				taskID := int64(gid*tasksPerGoroutine + i)
				future := types.NewFuture[int, int64]()
				task := types.NewSubmittedTask(int(taskID), taskID, future)
				if err := s.Submit(task); err != nil {
					submitErrors.Add(1)
					t.Errorf("goroutine %d: Submit failed: %v", gid, err)
				}
			}
		}(g)
	}

	submitWg.Wait()

	if submitErrors.Load() > 0 {
		t.Errorf("encountered %d submit errors", submitErrors.Load())
	}

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Shutdown workers
	s.Shutdown()
	workerWg.Wait()

	processed := processedCount.Load()
	t.Logf("Processed %d out of %d tasks", processed, totalTasks)

	if processed < int32(totalTasks/2) {
		t.Errorf("processed too few tasks: %d out of %d", processed, totalTasks)
	}
}

// TestMPMCStrategy_BoundedQueueFull tests bounded strategy behavior when queue is full.
func TestMPMCStrategy_BoundedQueueFull(t *testing.T) {
	capacity := 16
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 1,
		TaskBuffer:  capacity,
	}

	s := newMPMCStrategy(conf, true, capacity)
	defer s.Shutdown()

	// Fill the queue
	for i := 0; i < capacity; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		err := s.Submit(task)
		if err != nil {
			t.Fatalf("Submit(%d) failed: %v", i, err)
		}
	}

	// Next submit should fail
	future := types.NewFuture[int, int64]()
	task := types.NewSubmittedTask(999, 999, future)
	err := s.Submit(task)
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

// TestMPMCStrategy_Shutdown tests graceful shutdown.
func TestMPMCStrategy_Shutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  32,
	}

	s := newMPMCStrategy(conf, false, 32)

	// Submit some tasks
	for i := 0; i < 10; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Shutdown
	s.Shutdown()

	// Verify quit signal is closed
	select {
	case <-s.quitter.Wait():
		// Good - quit signal is closed
	default:
		t.Error("quit signal was not closed")
	}

	// Queue should eventually be drained by workers or closed
	time.Sleep(100 * time.Millisecond)

	// Submit after shutdown should fail
	future := types.NewFuture[int, int64]()
	task := types.NewSubmittedTask(999, 999, future)
	err := s.Submit(task)
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("expected ErrQueueClosed after shutdown, got %v", err)
	}
}

// TestMPMCStrategy_WorkerContextCancellation tests worker behavior on context cancellation.
func TestMPMCStrategy_WorkerContextCancellation(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		TaskBuffer:    32,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 32)
	defer s.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	workerID := int64(0)

	executor := func(ctx context.Context, task int) (int, error) {
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		task.Future.AddResult(*result)
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Submit some tasks
	for i := 0; i < 5; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

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

// TestMPMCStrategy_DrainQueue tests that workers drain remaining tasks on shutdown.
func TestMPMCStrategy_DrainQueue(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   1,
		TaskBuffer:    32,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newMPMCStrategy(conf, false, 32)

	ctx := context.Background()
	workerID := int64(0)

	executor := func(ctx context.Context, task int) (int, error) {
		// Simulate slow processing
		time.Sleep(50 * time.Millisecond)
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		task.Future.AddResult(*result)
	}

	// Submit tasks
	numTasks := 10
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Wait a bit for some processing
	time.Sleep(100 * time.Millisecond)

	// Shutdown (should trigger drain)
	s.Shutdown()

	// Wait for worker to finish
	select {
	case <-workerDone:
		// Worker finished
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish draining")
	}

	// Some tasks should have been processed
	processed := processedCount.Load()
	t.Logf("Processed %d out of %d tasks", processed, numTasks)

	if processed < 1 {
		t.Error("no tasks were processed")
	}
}
