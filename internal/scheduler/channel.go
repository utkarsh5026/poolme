package scheduler

import (
	"context"
	"sync"

	"github.com/utkarsh5026/poolme/internal/types"
)

type channelStrategy[T any, R any] struct {
	config   *ProcessorConfig[T, R]
	taskChan chan *types.SubmittedTask[T, R]
	quit     chan struct{}
	wg       sync.WaitGroup
}

func newChannelStrategy[T any, R any](conf *ProcessorConfig[T, R]) *channelStrategy[T, R] {
	return &channelStrategy[T, R]{
		config:   conf,
		taskChan: make(chan *types.SubmittedTask[T, R], conf.TaskBuffer),
		quit:     make(chan struct{}),
	}
}

func (s *channelStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	s.taskChan <- task
	return nil
}

func (s *channelStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for _, task := range tasks {
			select {
			case s.taskChan <- task:
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
	close(s.taskChan)
}

func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	for {
		select {
		case <-ctx.Done():
			s.drain(ctx, executor, h)
			return ctx.Err()
		case t, ok := <-s.taskChan:
			if !ok {
				return nil
			}
			err := executeSubmitted(ctx, t, s.config, executor, h)
			if err := handleExecutionError(err, s.config.ContinueOnErr, func() {
				s.drain(ctx, executor, h)
			}); err != nil {
				return err
			}
		}
	}
}

// drain processes any remaining tasks in the channel during context cancellation.
// This ensures all submitted tasks are processed even when context is cancelled.
func (s *channelStrategy[T, R]) drain(ctx context.Context, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	for {
		select {
		case t, ok := <-s.taskChan:
			if !ok {
				return
			}
			_ = executeSubmitted(ctx, t, s.config, executor, h)
		default:
			return
		}
	}
}
