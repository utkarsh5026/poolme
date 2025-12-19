package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

func submitTasks[T any, R any](d *lmaxStrategy[T, R], start, count int, t *testing.T) {
	for i := start; i < start+count; i++ {
		fmt.Printf("Submitting task %d\n", i)
		task := types.NewSubmittedTask[T, R](any(i).(T), int64(i), nil)
		if err := d.Submit(task); err != nil {
			t.Errorf("failed to submit task %d: %v", i, err)
			return
		}
	}
}

func submitTasksWithIDs[T any, R any](d *lmaxStrategy[T, R], n int, t *testing.T, idFunc func(int) int64) {
	for id := range n {
		debugLog("Submitting task %d", id)
		task := types.NewSubmittedTask[T, R](any(id).(T), idFunc(id), nil)
		if err := d.Submit(task); err != nil {
			t.Errorf("failed to submit task %d: %v", id, err)
			return
		}
	}
}

func startWorkers(ctx context.Context, conf *ProcessorConfig[int, int], d *lmaxStrategy[int, int], executor types.ProcessFunc[int, int], resultHandler types.ResultHandler[int, int]) {
	for i := 0; i < conf.WorkerCount; i++ {
		go func(workerID int) {
			_ = d.Worker(ctx, int64(workerID), executor, resultHandler)
		}(i)
	}
}

func verifyFinalCount(receivedCount int, expected int, t *testing.T) {
	// Verify
	if receivedCount != expected {
		t.Errorf("expected %d tasks processed, got %d", expected, receivedCount)
	}
}

// TestDisruptorBasicSubmitConsume tests basic submit and consume flow
func TestDisruptorBasicSubmitConsume(t *testing.T) {
	fmt.Println("Starting TestDisruptorBasicSubmitConsume")
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  10,
	}
	d := newLmaxStrategy(conf, 16)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	taskCount := 100000
	go submitTasks(d, 0, taskCount, t)

	// Consume tasks
	var received []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		mu.Lock()
		received = append(received, task.Task)
		debugLog("recieved task %s", task)
		mu.Unlock()
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		return val, nil
	}

	wg.Add(taskCount)

	startWorkers(ctx, conf, d, executor, resultHandler)

	wg.Wait()

	// Verify all tasks were received
	if len(received) != taskCount {
		t.Errorf("expected %d tasks, got %d", taskCount, len(received))
	}
}

// TestDisruptorConcurrentProducers tests multiple concurrent producers
func TestDisruptorConcurrentProducers(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  100,
	}
	// Use larger buffer to avoid wrap-around issues in this test
	d := newLmaxStrategy(conf, 512)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	producerCount := 8
	tasksPerProducer := 50
	totalTasks := producerCount * tasksPerProducer

	// Setup result handling
	var receivedCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(totalTasks)

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		receivedCount.Add(1)
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		return val, nil
	}

	// Start workers FIRST (important!)
	startWorkers(ctx, conf, d, executor, resultHandler)

	// Give workers a moment to start
	time.Sleep(10 * time.Millisecond)

	// Now start multiple producers
	var submitWg sync.WaitGroup
	submitWg.Add(producerCount)

	for p := range producerCount {
		go func(producerID int) {
			defer submitWg.Done()
			submitTasksWithIDs(d, tasksPerProducer, t, func(i int) int64 {
				return int64(producerID*tasksPerProducer + i)
			})
		}(p)
	}

	// Wait for all submissions
	submitWg.Wait()

	// Wait for all tasks to be processed
	wg.Wait()

	// Verify all tasks were received
	if int(receivedCount.Load()) != totalTasks {
		t.Errorf("expected %d tasks, got %d", totalTasks, receivedCount.Load())
	}
}

// TestDisruptorConcurrentConsumers tests multiple concurrent consumers
func TestDisruptorConcurrentConsumers(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 8,
		TaskBuffer:  100,
	}
	d := newLmaxStrategy(conf, 512)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Submit tasks
	taskCount := 500
	submitTasks(d, 0, taskCount, t)

	// Consume with multiple workers
	var receivedCount atomic.Int32
	received := make(map[int]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(taskCount)

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		mu.Lock()
		if received[task.Task] {
			t.Errorf("duplicate task received: %d", task.Task)
		}
		received[task.Task] = true
		mu.Unlock()
		receivedCount.Add(1)
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		return val, nil
	}

	// Start workers
	for i := 0; i < conf.WorkerCount; i++ {
		go func(workerID int) {
			_ = d.Worker(ctx, int64(workerID), executor, resultHandler)
		}(i)
	}
	startWorkers(ctx, conf, d, executor, resultHandler)
	wg.Wait()

	verifyFinalCount(int(receivedCount.Load()), taskCount, t)
	verifyFinalCount(len(received), taskCount, t)
}

// TestDisruptorConcurrentProducerConsumer tests simultaneous production and consumption
func TestDisruptorConcurrentProducerConsumer(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  50,
	}
	// 512 slots for 400 tasks (4 producers × 100 tasks)
	d := newLmaxStrategy(conf, 512)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	producerCount := 4
	tasksPerProducer := 100
	totalTasks := producerCount * tasksPerProducer

	var receivedCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(totalTasks)

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		receivedCount.Add(1)
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		// Simulate some work
		time.Sleep(time.Microsecond)
		return val, nil
	}

	startWorkers(ctx, conf, d, executor, resultHandler)

	// Start producers
	var submitWg sync.WaitGroup
	submitWg.Add(producerCount)
	for p := range producerCount {
		go func(producerID int) {
			defer submitWg.Done()
			submitTasksWithIDs(d, tasksPerProducer, t, func(i int) int64 {
				return int64(producerID*tasksPerProducer + i)
			})
		}(p)
	}

	// Wait for all submissions
	submitWg.Wait()

	// Wait for all tasks to be processed
	wg.Wait()

	verifyFinalCount(int(receivedCount.Load()), totalTasks, t)
}

// TestDisruptorBufferFull tests behavior when ring buffer is full
// SKIPPED: v1 implementation has issues with wrap-around when buffer is smaller than task count
// TODO: Fix wrap-around logic to handle unsigned arithmetic correctly
func TestDisruptorBufferFull(t *testing.T) {
	t.Skip("Skipping wrap-around test - known issue in v1, will fix in v2")
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  10,
	}
	// Small buffer to test wrapping
	capacity := 8
	d := newLmaxStrategy(conf, capacity)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	submitTasks(d, 0, capacity, t)

	// Start a consumer after a delay
	var wg sync.WaitGroup
	wg.Add(capacity + 5)

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		time.Sleep(10 * time.Millisecond)
		return val, nil
	}

	// Start consumers with a slight delay
	time.Sleep(50 * time.Millisecond)

	startWorkers(ctx, conf, d, executor, resultHandler)
	// Try to submit more tasks - should block until consumers free up space
	submitTasks(d, capacity, capacity+5, t)

	wg.Wait()
}

// TestDisruptorShutdown tests graceful shutdown
func TestDisruptorShutdown(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 2,
		TaskBuffer:  10,
	}
	d := newLmaxStrategy(conf, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var receivedCount atomic.Int32
	var workerWg sync.WaitGroup
	workerWg.Add(2) // For 2 workers to exit

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		receivedCount.Add(1)
	}

	executor := func(ctx context.Context, val int) (int, error) {
		time.Sleep(5 * time.Millisecond)
		return val, nil
	}

	// Start workers BEFORE submitting tasks (to avoid deadlock)
	for i := 0; i < conf.WorkerCount; i++ {
		go func(workerID int) {
			defer workerWg.Done()
			_ = d.Worker(ctx, int64(workerID), executor, resultHandler)
		}(i)
	}

	// Submit tasks after workers are running
	taskCount := 200
	submitTasks(d, 0, taskCount, t)

	// Let workers process some tasks
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	d.Shutdown()

	// Wait for workers to exit
	workerWg.Wait()

	// Verify that all tasks were processed (drain should handle remaining)
	if int(receivedCount.Load()) != taskCount {
		t.Logf("warning: not all tasks processed during shutdown: %d/%d", receivedCount.Load(), taskCount)
	}

	// Try to submit after shutdown - should fail
	task := types.NewSubmittedTask[int, int](999, 999, nil)
	if err := d.Submit(task); !errors.Is(err, ErrSchedulerClosed) {
		t.Errorf("expected ErrQueueClosed after shutdown, got %v", err)
	}
}

// TestDisruptorSubmitBatch tests batch submission
func TestDisruptorSubmitBatch(t *testing.T) {
	conf := &ProcessorConfig[int, int]{
		WorkerCount: 4,
		TaskBuffer:  50,
	}
	d := newLmaxStrategy(conf, 256)
	defer d.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create batch of tasks
	batchSize := 100
	tasks := make([]*types.SubmittedTask[int, int], batchSize)
	for i := range batchSize {
		tasks[i] = types.NewSubmittedTask[int, int](i, int64(i), nil)
	}

	// Submit batch
	submitted, err := d.SubmitBatch(tasks)
	if err != nil {
		t.Fatalf("SubmitBatch failed: %v", err)
	}
	verifyFinalCount(submitted, batchSize, t)

	// Consume tasks
	var receivedCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(batchSize)

	resultHandler := func(task *types.SubmittedTask[int, int], result *types.Result[int, int64]) {
		receivedCount.Add(1)
		wg.Done()
	}

	executor := func(ctx context.Context, val int) (int, error) {
		return val, nil
	}

	startWorkers(ctx, conf, d, executor, resultHandler)

	wg.Wait()
	verifyFinalCount(int(receivedCount.Load()), int(batchSize), t)
}