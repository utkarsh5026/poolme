package scheduler

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

type channelStrategy[T any, R any] struct {
	config    *ProcessorConfig[T, R]
	taskChans []chan *types.SubmittedTask[T, R]
	counter   atomic.Int64
	quit      chan struct{}
	wg        sync.WaitGroup
}

func newChannelStrategy[T any, R any](conf *ProcessorConfig[T, R]) *channelStrategy[T, R] {
	c := &channelStrategy[T, R]{
		config:    conf,
		taskChans: make([]chan *types.SubmittedTask[T, R], conf.WorkerCount),
		quit:      make(chan struct{}),
	}

	for i := 0; i < conf.WorkerCount; i++ {
		c.taskChans[i] = make(chan *types.SubmittedTask[T, R], conf.TaskBuffer)
	}

	return c
}

func (s *channelStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	index := s.counter.Add(1) % int64(s.config.WorkerCount)
	s.taskChans[index] <- task
	return nil
}

func (s *channelStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for _, task := range tasks {
			index := s.counter.Add(1) % int64(s.config.WorkerCount)
			select {
			case s.taskChans[index] <- task:
			case <-s.quit:
				return
			}
		}
	}()
	return len(tasks), nil
}

func (s *channelStrategy[T, R]) Shutdown() {
	close(s.quit) // Signal all SubmitBatch goroutines to stop
	s.wg.Wait()   // Wait for all SubmitBatch goroutines to finish
	for _, ch := range s.taskChans {
		close(ch)
	}
}

func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	for {
		select {
		case <-ctx.Done():
			s.drain(ctx, executor, h, workerID)
			return ctx.Err()
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return nil
			}
			err := executeSubmitted(ctx, t, s.config, executor, h)
			if err := handleExecutionError(err, s.config.ContinueOnErr, func() {
				s.drain(ctx, executor, h, workerID)
			}); err != nil {
				return err
			}
		}
	}
}

// drain processes any remaining tasks in the channel during context cancellation.
// This ensures all submitted tasks are processed even when context is cancelled.
func (s *channelStrategy[T, R]) drain(ctx context.Context, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R], workerID int64) {
	for {
		select {
		case t, ok := <-s.taskChans[workerID]:
			if !ok {
				return
			}
			_ = executeSubmitted(ctx, t, s.config, executor, h)
		default:
			return
		}
	}
}
