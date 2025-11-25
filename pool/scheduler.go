package pool

import (
	"context"
)

type channelStrategy[T any, R any] struct {
	taskChan chan *submittedTask[T, R]
}

func newChannelStrategy[T any, R any](taskBuffer int) *channelStrategy[T, R] {
	return &channelStrategy[T, R]{
		taskChan: make(chan *submittedTask[T, R], taskBuffer),
	}
}

func (s *channelStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	s.taskChan <- task
	return nil
}

func (s *channelStrategy[T, R]) Shutdown() {
	close(s.taskChan)
}

func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		select {
		case <-ctx.Done():
			return

		case t, ok := <-s.taskChan:
			if !ok {
				return
			}
			executeSubmitted(ctx, t, pool, executor)
		}
	}
}
