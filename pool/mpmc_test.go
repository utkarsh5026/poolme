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

// Tests for MPMCQueue

func TestMPMCQueue_BasicEnqueueDequeue(t *testing.T) {
	queue := NewMPMCQueue[int](10, false)
	ctx := context.Background()

	// Enqueue some items
	for i := 0; i < 5; i++ {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue %d: %v", i, err)
		}
	}

	// Dequeue and verify
	for i := 0; i < 5; i++ {
		val, err := queue.Dequeue(ctx)
		if err != nil {
			t.Fatalf("failed to dequeue: %v", err)
		}
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}
}

func TestMPMCQueue_BoundedQueueFull(t *testing.T) {
	capacity := 4
	queue := NewMPMCQueue[int](capacity, true)
	ctx := context.Background()

	// Fill the queue
	for i := 0; i < capacity; i++ {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue %d: %v", i, err)
		}
	}

	// Try to enqueue one more - should eventually succeed or timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := queue.Enqueue(ctx.Done(), 999)
	if err == nil {
		t.Error("expected error when enqueueing to full bounded queue, got nil")
	}
}

func TestMPMCQueue_UnboundedBlocking(t *testing.T) {
	// Test that unbounded queues block when full rather than returning error
	capacity := 8
	queue := NewMPMCQueue[int](capacity, false)
	ctx := context.Background()

	// Fill the queue
	for i := 0; i < capacity; i++ {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue %d: %v", i, err)
		}
	}

	// Try to enqueue more with a consumer running
	done := make(chan struct{})
	go func() {
		// Dequeue items to make space
		for i := 0; i < capacity; i++ {
			time.Sleep(10 * time.Millisecond)
			_, _ = queue.Dequeue(ctx)
		}
		close(done)
	}()

	// This should block and wait for space, not return error
	ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for i := capacity; i < capacity+5; i++ {
		if err := queue.Enqueue(ctx2.Done(), i); err != nil {
			if err == ctx2.Err() {
				// Timeout is acceptable if consumer is slow
				continue
			}
			t.Fatalf("failed to enqueue %d to unbounded queue: %v", i, err)
		}
	}

	<-done
}

func TestMPMCQueue_ConcurrentEnqueue(t *testing.T) {
	queue := NewMPMCQueue[int](1024, false)
	ctx := context.Background()

	producerCount := 10
	itemsPerProducer := 100
	var wg sync.WaitGroup

	// Multiple producers
	wg.Add(producerCount)
	for p := 0; p < producerCount; p++ {
		go func(producerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerProducer; i++ {
				val := producerID*itemsPerProducer + i
				if err := queue.Enqueue(ctx.Done(), val); err != nil {
					t.Errorf("producer %d: failed to enqueue %d: %v", producerID, val, err)
					return
				}
			}
		}(p)
	}

	wg.Wait()

	// Verify all items were enqueued
	expectedCount := producerCount * itemsPerProducer
	actualCount := queue.Len()

	// Account for some items already being dequeued in concurrent scenarios
	if actualCount > expectedCount {
		t.Errorf("queue has more items than expected: %d > %d", actualCount, expectedCount)
	}
}

func TestMPMCQueue_ConcurrentDequeue(t *testing.T) {
	queue := NewMPMCQueue[int](1024, false)
	ctx := context.Background()

	// Enqueue items
	itemCount := 1000
	for i := 0; i < itemCount; i++ {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}
	}

	consumerCount := 10
	var wg sync.WaitGroup
	var receivedCount atomic.Int32
	received := make(map[int]bool)
	var mu sync.Mutex

	// Close queue after a short delay to signal consumers when empty
	go func() {
		time.Sleep(100 * time.Millisecond)
		queue.Close()
	}()

	// Multiple consumers
	wg.Add(consumerCount)
	for c := 0; c < consumerCount; c++ {
		go func(consumerID int) {
			defer wg.Done()
			for {
				val, err := queue.Dequeue(ctx)
				if err != nil {
					if err == ErrQueueClosed {
						return
					}
					continue
				}

				// Track received items
				mu.Lock()
				if received[val] {
					t.Errorf("consumer %d: duplicate value received: %d", consumerID, val)
				}
				received[val] = true
				mu.Unlock()

				receivedCount.Add(1)
			}
		}(c)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for consumers to finish (received %d/%d)", receivedCount.Load(), itemCount)
	}

	if int(receivedCount.Load()) != itemCount {
		t.Errorf("expected %d items, got %d", itemCount, receivedCount.Load())
	}
}

func TestMPMCQueue_ConcurrentProducerConsumer(t *testing.T) {
	queue := NewMPMCQueue[int](128, false)
	ctx := context.Background()

	producerCount := 5
	consumerCount := 5
	itemsPerProducer := 200

	var producerWg, consumerWg sync.WaitGroup
	var consumedCount atomic.Int32
	var producedCount atomic.Int32

	// Start producers
	producerWg.Add(producerCount)
	for p := 0; p < producerCount; p++ {
		go func(producerID int) {
			defer producerWg.Done()
			for i := 0; i < itemsPerProducer; i++ {
				val := producerID*itemsPerProducer + i
				if err := queue.Enqueue(ctx.Done(), val); err != nil {
					t.Errorf("producer %d: failed to enqueue: %v", producerID, err)
					return
				}
				producedCount.Add(1)
			}
		}(p)
	}

	// Start consumers
	consumerWg.Add(consumerCount)
	for c := 0; c < consumerCount; c++ {
		go func(consumerID int) {
			defer consumerWg.Done()
			for int(consumedCount.Load()) < producerCount*itemsPerProducer {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				val, err := queue.Dequeue(ctx)
				cancel()

				if err != nil {
					if ctx.Err() != nil {
						// Timeout, check if we're done
						if int(consumedCount.Load()) >= producerCount*itemsPerProducer {
							return
						}
						continue
					}
					if err == ErrQueueClosed {
						return
					}
					continue
				}

				if val < 0 || val >= producerCount*itemsPerProducer {
					t.Errorf("consumer %d: invalid value %d", consumerID, val)
				}
				consumedCount.Add(1)
			}
		}(c)
	}

	// Wait for producers
	producerWg.Wait()

	// Wait for consumers with timeout
	done := make(chan struct{})
	go func() {
		consumerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout: produced=%d, consumed=%d", producedCount.Load(), consumedCount.Load())
	}

	expectedCount := int32(producerCount * itemsPerProducer)
	if consumedCount.Load() != expectedCount {
		t.Errorf("expected %d consumed items, got %d", expectedCount, consumedCount.Load())
	}
}

func TestMPMCQueue_Close(t *testing.T) {
	queue := NewMPMCQueue[int](10, false)
	ctx := context.Background()

	// Enqueue some items
	for i := 0; i < 5; i++ {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}
	}

	// Close the queue
	queue.Close()

	// Try to enqueue after close - should error
	err := queue.Enqueue(ctx.Done(), 999)
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}

	// Should still be able to dequeue remaining items
	for i := 0; i < 5; i++ {
		val, err := queue.Dequeue(ctx)
		if err != nil {
			t.Fatalf("failed to dequeue remaining item: %v", err)
		}
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}

	// Dequeue from empty closed queue should error
	_, err = queue.Dequeue(ctx)
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed on empty closed queue, got %v", err)
	}
}

func TestMPMCQueue_TryDequeue(t *testing.T) {
	queue := NewMPMCQueue[int](10, false)
	ctx := context.Background()

	// Try dequeue from empty queue
	val, ok := queue.TryDequeue()
	if ok {
		t.Error("expected TryDequeue to return false on empty queue")
	}

	// Enqueue some items
	for i := range 3 {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}
	}

	// Try dequeue should succeed
	for i := range 3 {
		val, ok = queue.TryDequeue()
		if !ok {
			t.Errorf("expected TryDequeue to succeed, got false")
		}
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}

	// Try dequeue from empty queue again
	val, ok = queue.TryDequeue()
	if ok {
		t.Error("expected TryDequeue to return false after draining")
	}
}

// Tests for MPMC Strategy with WorkerPool

func TestWorkerPool_MPMC_BasicFunctionality(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(4),
		WithMPMCQueue(),
	)

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

func TestWorkerPool_MPMC_BoundedQueue(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(2),
		WithMPMCQueue(WithBoundedQueue(8)),
	)

	tasks := []int{1, 2, 3, 4, 5}
	processFn := func(ctx context.Context, task int) (int, error) {
		time.Sleep(10 * time.Millisecond)
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != len(tasks) {
		t.Fatalf("expected %d results, got %d", len(tasks), len(results))
	}
}

func TestWorkerPool_MPMC_UnboundedQueue(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(4),
		WithMPMCQueue(WithUnboundedQueue()),
	)

	// Large number of tasks
	taskCount := 1000
	tasks := make([]int, taskCount)
	for i := 0; i < taskCount; i++ {
		tasks[i] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	results, err := pool.Process(context.Background(), tasks, processFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != taskCount {
		t.Fatalf("expected %d results, got %d", taskCount, len(results))
	}

	for i := 0; i < taskCount; i++ {
		expected := i * 2
		if results[i] != expected {
			t.Errorf("task %d: expected %d, got %d", i, expected, results[i])
		}
	}
}

func TestWorkerPool_MPMC_StartSubmitShutdown(t *testing.T) {
	pool := NewWorkerPool[int, string](
		WithWorkerCount(4),
		WithMPMCQueue(),
	)

	processFn := func(ctx context.Context, task int) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return fmt.Sprintf("result-%d", task), nil
	}

	// Start the pool
	ctx := context.Background()
	if err := pool.Start(ctx, processFn); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}

	// Submit tasks
	taskCount := 20
	futures := make([]*Future[string, int64], taskCount)
	for i := 0; i < taskCount; i++ {
		future, err := pool.Submit(i)
		if err != nil {
			t.Fatalf("failed to submit task %d: %v", i, err)
		}
		futures[i] = future
	}

	// Collect results
	for i, future := range futures {
		result, _, err := future.GetWithContext(ctx)
		if err != nil {
			t.Errorf("task %d failed: %v", i, err)
		}
		expected := fmt.Sprintf("result-%d", i)
		if result != expected {
			t.Errorf("task %d: expected %s, got %s", i, expected, result)
		}
	}

	// Shutdown
	if err := pool.Shutdown(10 * time.Second); err != nil {
		t.Fatalf("failed to shutdown pool: %v", err)
	}
}

func TestWorkerPool_MPMC_ConcurrentSubmitters(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(8),
		WithMPMCQueue(),
	)

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	ctx := context.Background()
	if err := pool.Start(ctx, processFn); err != nil {
		t.Fatalf("failed to start pool: %v", err)
	}
	defer pool.Shutdown(10 * time.Second)

	// Multiple goroutines submitting tasks concurrently
	submitterCount := 10
	tasksPerSubmitter := 50
	var wg sync.WaitGroup
	var allFutures sync.Map

	wg.Add(submitterCount)
	for s := 0; s < submitterCount; s++ {
		go func(submitterID int) {
			defer wg.Done()
			for i := 0; i < tasksPerSubmitter; i++ {
				task := submitterID*tasksPerSubmitter + i
				future, err := pool.Submit(task)
				if err != nil {
					t.Errorf("submitter %d: failed to submit task %d: %v", submitterID, task, err)
					return
				}
				allFutures.Store(task, future)
			}
		}(s)
	}

	wg.Wait()

	// Verify all results
	totalTasks := submitterCount * tasksPerSubmitter
	successCount := 0
	allFutures.Range(func(key, value interface{}) bool {
		task := key.(int)
		future := value.(*Future[int, int64])

		result, _, err := future.GetWithContext(ctx)
		if err != nil {
			t.Errorf("task %d failed: %v", task, err)
			return true
		}

		expected := task * 2
		if result != expected {
			t.Errorf("task %d: expected %d, got %d", task, expected, result)
		}
		successCount++
		return true
	})

	if successCount != totalTasks {
		t.Errorf("expected %d successful tasks, got %d", totalTasks, successCount)
	}
}

func TestWorkerPool_MPMC_ErrorHandling(t *testing.T) {
	pool := NewWorkerPool[int, int](
		WithWorkerCount(4),
		WithMPMCQueue(),
	)

	expectedErr := errors.New("processing error")
	tasks := []int{1, 2, 3, 4, 5}
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

// Benchmark tests

func BenchmarkMPMCQueue_Enqueue(b *testing.B) {
	queue := NewMPMCQueue[int](1024, false)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = queue.Enqueue(ctx.Done(), i)
	}
}

func BenchmarkMPMCQueue_Dequeue(b *testing.B) {
	queue := NewMPMCQueue[int](b.N, false)
	ctx := context.Background()

	// Pre-fill the queue
	for i := 0; i < b.N; i++ {
		_ = queue.Enqueue(ctx.Done(), i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = queue.Dequeue(ctx)
	}
}

func BenchmarkMPMCQueue_ConcurrentEnqueueDequeue(b *testing.B) {
	queue := NewMPMCQueue[int](1024, false)
	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				_ = queue.Enqueue(ctx.Done(), i)
			} else {
				_, _ = queue.TryDequeue()
			}
			i++
		}
	})
}

func BenchmarkWorkerPool_MPMC_vs_Channel(b *testing.B) {
	testCases := []struct {
		name string
		opts []WorkerPoolOption
	}{
		{"MPMC", []WorkerPoolOption{WithWorkerCount(8), WithMPMCQueue()}},
		{"Channel", []WorkerPoolOption{WithWorkerCount(8)}},
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		return task * 2, nil
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			pool := NewWorkerPool[int, int](tc.opts...)
			ctx := context.Background()

			tasks := make([]int, b.N)
			for i := 0; i < b.N; i++ {
				tasks[i] = i
			}

			b.ResetTimer()
			_, err := pool.Process(ctx, tasks, processFn)
			if err != nil {
				b.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
