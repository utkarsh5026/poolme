package pool

import (
	"context"
	"runtime"
)

// mpmc implements a lock-free multi-producer multi-consumer queue strategy.
//
// This strategy is optimized for high-throughput scenarios where many goroutines
// submit tasks concurrently to the pool.
type mpmc[T any, R any] struct {
	queue *MPMCQueue[*submittedTask[T, R]]
	quit  chan struct{}
}

// newMPMCStrategy creates a new MPMC queue strategy with the given configuration
func newMPMCStrategy[T any, R any](bounded bool, capacity int) *mpmc[T, R] {
	return &mpmc[T, R]{
		queue: NewMPMCQueue[*submittedTask[T, R]](capacity, bounded),
		quit:  make(chan struct{}),
	}
}

// Submit enqueues a task into the MPMC queue
func (s *mpmc[T, R]) Submit(task *submittedTask[T, R]) error {
	return s.queue.Enqueue(s.quit, task)
}

// worker is the main worker loop that dequeues and executes tasks
func (s *mpmc[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		select {
		case <-s.quit:
			s.drainQueue(ctx, executor, pool)
			return
		case <-ctx.Done():
			s.drainQueue(ctx, executor, pool)
			return
		default:
			task, err := s.queue.Dequeue(ctx)
			if err != nil {
				if err == ErrQueueClosed {
					s.drainQueue(ctx, executor, pool)
					return
				}
				if ctx.Err() != nil {
					return
				}
				runtime.Gosched()
				continue
			}

			executeSubmitted(ctx, task, pool, executor)
		}
	}
}

// drainQueue attempts to process any remaining tasks in the queue during shutdown
func (s *mpmc[T, R]) drainQueue(ctx context.Context, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		task, ok := s.queue.TryDequeue()
		if !ok {
			return
		}
		executeSubmitted(ctx, task, pool, executor)
	}
}

// Shutdown gracefully stops the workers and closes the queue
func (s *mpmc[T, R]) Shutdown() {
	s.queue.Close()
	close(s.quit)
}
