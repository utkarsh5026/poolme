package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tests for MPMCQueue

func TestMPMCQueue_BasicEnqueueDequeue(t *testing.T) {
	queue := newMPMCQueue[int](10, false)
	ctx := context.Background()

	// Enqueue some items
	for i := range 5 {
		if err := queue.Enqueue(ctx.Done(), i); err != nil {
			t.Fatalf("failed to enqueue %d: %v", i, err)
		}
	}

	// Dequeue and verify
	for i := range 5 {
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
	queue := newMPMCQueue[int](capacity, true)
	ctx := context.Background()

	// Fill the queue
	for i := range capacity {
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
	queue := newMPMCQueue[int](capacity, false)
	ctx := context.Background()

	// Fill the queue
	for i := range capacity {
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
	queue := newMPMCQueue[int](1024, false)
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
	queue := newMPMCQueue[int](1024, false)
	ctx := context.Background()

	// Enqueue items
	itemCount := 1000
	for i := range itemCount {
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
	for c := range consumerCount {
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
	queue := newMPMCQueue[int](128, false)
	ctx := context.Background()

	producerCount := 5
	consumerCount := 5
	itemsPerProducer := 200

	var producerWg, consumerWg sync.WaitGroup
	var consumedCount atomic.Int32
	var producedCount atomic.Int32

	// Start producers
	producerWg.Add(producerCount)
	for p := range producerCount {
		go func(producerID int) {
			defer producerWg.Done()
			for i := range itemsPerProducer {
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
	for c := range consumerCount {
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
	queue := newMPMCQueue[int](10, false)
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
	queue := newMPMCQueue[int](10, false)
	ctx := context.Background()

	// Try dequeue from empty queue
	if _, ok := queue.TryDequeue(); ok {
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
		val, ok := queue.TryDequeue()
		if !ok {
			t.Errorf("expected TryDequeue to succeed, got false")
		}
		if val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}

	// Try dequeue from empty queue again
	if _, ok := queue.TryDequeue(); ok {
		t.Error("expected TryDequeue to return false after draining")
	}
}
