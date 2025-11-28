package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

// TestNewChannelStrategy tests the creation and initialization of channelStrategy.
func TestNewChannelStrategy(t *testing.T) {
	tests := []struct {
		name        string
		workerCount int
		taskBuffer  int
		wantChans   int
	}{
		{
			name:        "default worker count",
			workerCount: 0,
			taskBuffer:  10,
			wantChans:   runtime.NumCPU(),
		},
		{
			name:        "explicit worker count less than NumCPU",
			workerCount: 2,
			taskBuffer:  5,
			wantChans:   max(runtime.NumCPU(), 2),
		},
		{
			name:        "explicit worker count greater than NumCPU",
			workerCount: runtime.NumCPU() + 5,
			taskBuffer:  20,
			wantChans:   runtime.NumCPU() + 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &ProcessorConfig[int, int]{
				WorkerCount: tt.workerCount,
				TaskBuffer:  tt.taskBuffer,
			}

			s := newChannelStrategy(conf)

			if s == nil {
				t.Fatal("newChannelStrategy returned nil")
			}

			if len(s.taskChans) != tt.wantChans {
				t.Errorf("expected %d channels, got %d", tt.wantChans, len(s.taskChans))
			}

			// Verify all channels are initialized with correct buffer size
			for i, ch := range s.taskChans {
				if ch == nil {
					t.Errorf("channel %d is nil", i)
				}
				if cap(ch) != tt.taskBuffer {
					t.Errorf("channel %d has capacity %d, want %d", i, cap(ch), tt.taskBuffer)
				}
			}

			if s.quit == nil {
				t.Error("quit channel is nil")
			}
		})
	}
}

// TestChannelStrategy_Submit tests single task submission.
func TestChannelStrategy_Submit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  10,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	// Create a test task
	task := types.NewSubmittedTask[int, int](42, 1, nil)

	// Submit task
	err := s.Submit(task)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Verify task was added to one of the channels
	taskReceived := false
	for _, ch := range s.taskChans {
		select {
		case receivedTask := <-ch:
			if receivedTask.Task == 42 && receivedTask.Id == 1 {
				taskReceived = true
			}
		default:
		}
	}

	if !taskReceived {
		t.Error("task was not received in any channel")
	}
}

// TestChannelStrategy_SubmitRoundRobin tests round-robin distribution of tasks.
func TestChannelStrategy_SubmitRoundRobin(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  100,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	numTasks := 100
	channelCounts := make([]int, len(s.taskChans))

	// Submit tasks
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		err := s.Submit(task)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Count tasks in each channel
	for i, ch := range s.taskChans {
		for {
			select {
			case <-ch:
				channelCounts[i]++
			default:
				goto nextChannel
			}
		}
	nextChannel:
	}

	totalReceived := 0
	for i, count := range channelCounts {
		t.Logf("Channel %d received %d tasks", i, count)
		totalReceived += count
		if count == 0 {
			t.Errorf("Channel %d received no tasks", i)
		}
	}

	if totalReceived != numTasks {
		t.Errorf("expected %d tasks received, got %d", numTasks, totalReceived)
	}
}

// TestChannelStrategy_SubmitBatch tests batch task submission.
func TestChannelStrategy_SubmitBatch(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  50,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	// Create batch of tasks
	batchSize := 20
	tasks := make([]*types.SubmittedTask[int, int], batchSize)
	for i := range batchSize {
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

	// Wait a bit for async submission to complete
	time.Sleep(100 * time.Millisecond)

	// Count received tasks
	totalReceived := 0
	for _, ch := range s.taskChans {
		for {
			select {
			case <-ch:
				totalReceived++
			default:
				goto nextChannel
			}
		}
	nextChannel:
	}

	if totalReceived != batchSize {
		t.Errorf("expected %d tasks received, got %d", batchSize, totalReceived)
	}
}

// TestChannelStrategy_SubmitBatchWithShutdown tests batch submission with early shutdown.
func TestChannelStrategy_SubmitBatchWithShutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  5,
	}

	s := newChannelStrategy(conf)

	// Create a large batch
	batchSize := 100
	tasks := make([]*types.SubmittedTask[int, int], batchSize)
	for i := 0; i < batchSize; i++ {
		future := types.NewFuture[int, int64]()
		tasks[i] = types.NewSubmittedTask(i, int64(i), future)
	}

	// Submit batch
	_, err := s.SubmitBatch(tasks)
	if err != nil {
		t.Fatalf("SubmitBatch failed: %v", err)
	}

	// Shutdown immediately (should stop batch goroutine early)
	s.Shutdown()

	// Verify all channels are closed by checking if we can read from them
	// Closed channels will return immediately with ok=false
	for i, ch := range s.taskChans {
		// Drain any remaining tasks
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					// Channel is closed, which is what we expect
					goto channelClosed
				}
				// Continue draining
			default:
				// No more tasks, but channel might not be closed yet
				// Try reading one more time with blocking to confirm closure
				_, ok := <-ch
				if !ok {
					goto channelClosed
				}
				t.Errorf("channel %d is not closed after shutdown", i)
				goto nextChan
			}
		}
	channelClosed:
		// Good - channel is closed as expected
	nextChan:
	}
}

// TestChannelStrategy_Worker tests worker execution.
func TestChannelStrategy_Worker(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   2,
		TaskBuffer:    10,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newChannelStrategy(conf)

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

	// Submit tasks directly to the worker's channel
	numTasks := 5
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.taskChans[workerID] <- task
	}

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	// Close the channel to stop worker
	close(s.taskChans[workerID])

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
		t.Errorf("expected %d results, got %d", numTasks, len(results))
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

	t.Logf("Processed %d tasks", len(results))
}

// TestChannelStrategy_WorkerWithError tests worker behavior with errors.
func TestChannelStrategy_WorkerWithError(t *testing.T) {
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
				TaskBuffer:    10,
				MaxAttempts:   1,
				ContinueOnErr: tt.continueOnErr,
			}

			s := newChannelStrategy(conf)

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

			// Submit a task directly to worker's channel
			future := types.NewFuture[int, int64]()
			task := types.NewSubmittedTask(1, 1, future)
			s.taskChans[workerID] <- task

			// Wait a bit for processing
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
					// Stop worker
					close(s.taskChans[workerID])
					<-workerDone
				}
			}

			if !handlerCalled.Load() {
				t.Error("handler was not called")
			}
		})
	}
}

// TestChannelStrategy_WorkerContextCancellation tests worker behavior on context cancellation.
func TestChannelStrategy_WorkerContextCancellation(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  10,
		MaxAttempts: 1,
	}

	s := newChannelStrategy(conf)

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

	// Submit some tasks directly to worker's channel
	for i := 0; i < 5; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.taskChans[workerID] <- task
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

// TestChannelStrategy_Shutdown tests graceful shutdown.
func TestChannelStrategy_Shutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  10,
	}

	s := newChannelStrategy(conf)

	// Submit some tasks
	for i := 0; i < 10; i++ {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Submit batch
	tasks := make([]*types.SubmittedTask[int, int], 5)
	for i := range 5 {
		future := types.NewFuture[int, int64]()
		tasks[i] = types.NewSubmittedTask(i+100, int64(i+100), future)
	}
	s.SubmitBatch(tasks)

	// Shutdown
	s.Shutdown()

	// Verify quit channel is closed
	select {
	case <-s.quit:
		// Good - quit channel is closed
	default:
		t.Error("quit channel was not closed")
	}

	// Verify all task channels are closed
	for i, ch := range s.taskChans {
		select {
		case <-ch:
		default:
			t.Errorf("channel %d appears to be open and empty", i)
		}
	}
}

// TestChannelStrategy_Drain tests the drain functionality.
func TestChannelStrategy_Drain(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  20,
		MaxAttempts: 1,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	workerID := int64(0)

	executor := func(ctx context.Context, task int) (int, error) {
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		return task, nil
	}

	var processedCount atomic.Int32
	handler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		processedCount.Add(1)
		task.Future.AddResult(*result)
	}

	// Submit tasks to worker's channel
	numTasks := 10
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.taskChans[workerID] <- task
	}

	// Start worker
	workerDone := make(chan error)
	go func() {
		workerDone <- s.Worker(ctx, workerID, executor, handler)
	}()

	// Wait a bit for some processing
	time.Sleep(50 * time.Millisecond)

	// Cancel context (should trigger drain)
	cancel()

	// Wait for worker to finish
	select {
	case <-workerDone:
		// Worker finished
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish draining")
	}

	// All tasks should have been processed (including drain)
	processed := processedCount.Load()
	t.Logf("Processed %d out of %d tasks", processed, numTasks)

	if processed < 1 {
		t.Error("no tasks were processed")
	}
}

// TestChannelStrategy_AffinityRouting tests affinity-based task routing.
func TestChannelStrategy_AffinityRouting(t *testing.T) {
	// Affinity function that routes based on task modulo
	affinityFunc := func(task int) string {
		return fmt.Sprintf("key-%d", task%3)
	}

	conf := &ProcessorConfig[int, int]{
		WorkerCount:  6,
		TaskBuffer:   20,
		AffinityFunc: affinityFunc,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	// Submit tasks with different affinity keys
	numTasks := 30
	tasksByChannel := make(map[int64][]int)

	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)

		// Calculate expected channel
		expectedChannel := s.next(task)
		tasksByChannel[expectedChannel] = append(tasksByChannel[expectedChannel], i)

		s.Submit(task)
	}

	// Verify tasks with same affinity key go to same channel
	time.Sleep(50 * time.Millisecond)

	// Group tasks by affinity key
	tasksByKey := make(map[string][]int)
	for i := range numTasks {
		key := affinityFunc(i)
		tasksByKey[key] = append(tasksByKey[key], i)
	}

	// All tasks with the same affinity key should go to the same channel
	for key, tasks := range tasksByKey {
		if len(tasks) == 0 {
			continue
		}

		// Calculate channel for first task with this key
		firstTask := tasks[0]
		future := types.NewFuture[int, int64]()
		testTask := types.NewSubmittedTask(firstTask, int64(firstTask), future)
		expectedChannel := s.next(testTask)

		// All tasks with same key should route to same channel
		for _, task := range tasks {
			future := types.NewFuture[int, int64]()
			testTask := types.NewSubmittedTask(task, int64(task), future)
			channel := s.next(testTask)
			if channel != expectedChannel {
				t.Errorf("task %d with key %s routed to channel %d, expected %d",
					task, key, channel, expectedChannel)
			}
		}
	}
}

// TestChannelStrategy_FNVHash tests the FNV hash function.
func TestChannelStrategy_FNVHash(t *testing.T) {
	conf := &ProcessorConfig[string, string]{
		WorkerCount: 4,
		TaskBuffer:  10,
	}

	s := newChannelStrategy(conf)

	// Test empty string returns FNV offset
	emptyHash := s.fnvHash("")
	if emptyHash != 2166136261 {
		t.Errorf("fnvHash(\"\") = %d, want 2166136261", emptyHash)
	}

	// Test hash consistency
	testCases := []string{"hello", "world", "test", "affinity", "consistent"}
	for _, key := range testCases {
		hash1 := s.fnvHash(key)
		hash2 := s.fnvHash(key)
		if hash1 != hash2 {
			t.Errorf("hash not consistent for %q: %d != %d", key, hash1, hash2)
		}
	}

	// Test different strings produce different hashes
	hash1 := s.fnvHash("hello")
	hash2 := s.fnvHash("world")
	if hash1 == hash2 {
		t.Error("different strings produced same hash")
	}

	// Test hash distribution (different inputs should produce different hashes)
	hashes := make(map[uint32]bool)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		hash := s.fnvHash(key)
		if hashes[hash] {
			// Collision is possible but unlikely for sequential keys
			t.Logf("hash collision detected for key %s", key)
		}
		hashes[hash] = true
	}

	// Should have good distribution (most keys should hash to unique values)
	if len(hashes) < 90 {
		t.Errorf("poor hash distribution: only %d unique hashes for 100 keys", len(hashes))
	}
}

// TestChannelStrategy_ConcurrentSubmit tests concurrent task submission.
func TestChannelStrategy_ConcurrentSubmit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  100,
	}

	s := newChannelStrategy(conf)
	defer s.Shutdown()

	numGoroutines := 10
	tasksPerGoroutine := 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for i := range tasksPerGoroutine {
				future := types.NewFuture[int, int64]()
				taskID := int64(goroutineID*tasksPerGoroutine + i)
				task := types.NewSubmittedTask(int(taskID), taskID, future)
				if err := s.Submit(task); err != nil {
					t.Errorf("goroutine %d: Submit failed: %v", goroutineID, err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Count total tasks in channels
	time.Sleep(100 * time.Millisecond)
	totalTasks := 0
	for _, ch := range s.taskChans {
		totalTasks += len(ch)
	}

	expectedTasks := numGoroutines * tasksPerGoroutine
	if totalTasks != expectedTasks {
		t.Errorf("expected %d tasks in channels, got %d", expectedTasks, totalTasks)
	}
}

// TestChannelStrategy_ConcurrentBatchSubmit tests concurrent batch submission.
func TestChannelStrategy_ConcurrentBatchSubmit(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  50,
	}

	s := newChannelStrategy(conf)

	numGoroutines := 3
	batchSize := 10

	var wg sync.WaitGroup
	var submittedCount atomic.Int32

	wg.Add(numGoroutines)
	for g := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			tasks := make([]*types.SubmittedTask[int, int], batchSize)
			for i := range batchSize {
				future := types.NewFuture[int, int64]()
				taskID := int64(goroutineID*batchSize + i)
				tasks[i] = types.NewSubmittedTask(int(taskID), taskID, future)
			}
			count, err := s.SubmitBatch(tasks)
			if err != nil {
				t.Errorf("goroutine %d: SubmitBatch failed: %v", goroutineID, err)
				return
			}
			submittedCount.Add(int32(count))
		}(g)
	}

	wg.Wait()

	// Wait a bit for batch goroutines to process
	time.Sleep(100 * time.Millisecond)

	// Shutdown to ensure all batch processing is complete
	s.Shutdown()

	expectedSubmitted := int32(numGoroutines * batchSize)
	if submittedCount.Load() != expectedSubmitted {
		t.Errorf("expected %d tasks submitted, got %d", expectedSubmitted, submittedCount.Load())
	}

	t.Logf("Successfully submitted %d tasks concurrently", submittedCount.Load())
}

// TestChannelStrategy_MultipleWorkers tests multiple workers processing concurrently.
func TestChannelStrategy_MultipleWorkers(t *testing.T) {
	numWorkers := 4
	conf := &ProcessorConfig[int, int]{
		WorkerCount:   numWorkers,
		TaskBuffer:    50,
		MaxAttempts:   1,
		ContinueOnErr: true,
	}

	s := newChannelStrategy(conf)

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
	for i := range numTasks {
		future := types.NewFuture[int, int64]()
		task := types.NewSubmittedTask(i, int64(i), future)
		s.Submit(task)
	}

	// Wait for processing
	time.Sleep(2 * time.Second)

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
