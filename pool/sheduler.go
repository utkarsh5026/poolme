package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrShutdownTimeout = errors.New("error in shutting down: timeout reached")
)

// SchedulingStrategy defines the behavior for distributing tasks to workers.
// Any new algorithm (Work Stealing, Priority Queue, Ring Buffer) must implement this.
type SchedulingStrategy[T any, R any] interface {
	// Start initializes the algorithm's resources and spawns worker goroutines.
	// The 'executor' function provided by the pool already handles hooks, retries, and rate limiting.
	Start(ctx context.Context, workers int) error

	// Submit accepts a task into the scheduling system.
	// It handles the logic of where the task goes (Global Queue vs Local Queue).
	Submit(ctx context.Context, task *submittedTask[T, R]) error

	// Shutdown gracefully stops the workers and waits for them to finish.
	Shutdown() <-chan struct{}
}

type ChannelStrategy[T any, R any] struct {
	pool     *WorkerPool[T, R]
	executor ProcessFunc[T, R]
	wg       sync.WaitGroup
	taskChan chan *submittedTask[T, R]
}

func NewChannelStrategy[T any, R any](pool *WorkerPool[T, R], executor ProcessFunc[T, R]) *ChannelStrategy[T, R] {
	return &ChannelStrategy[T, R]{
		pool:     pool,
		executor: executor,
		taskChan: make(chan *submittedTask[T, R], pool.taskBuffer),
	}
}

func (s *ChannelStrategy[T, R]) Submit(ctx context.Context, task *submittedTask[T, R]) error {
	select {
	case s.taskChan <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ChannelStrategy[T, R]) Shutdown() <-chan struct{} {
	close(s.taskChan)

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	return done
}

func (s *ChannelStrategy[T, R]) Start(ctx context.Context, workers int) error {
	s.wg.Add(workers)

	for range workers {
		go func() {
			defer s.wg.Done()
			s.worker(ctx)
		}()
	}

	return nil
}

func (s *ChannelStrategy[T, R]) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case t, ok := <-s.taskChan:
			if !ok {
				return
			}

			result, err := executeTask(ctx, s.pool, t.task, s.executor)
			t.future.result <- Result[R, int64]{
				Value: result,
				Error: err,
				Key:   t.id,
			}
		}
	}
}

func waitUntil(d <-chan struct{}, timeout time.Duration) error {
	select {
	case <-d:
		return nil
	case <-time.After(timeout):
		return ErrShutdownTimeout
	}
}
