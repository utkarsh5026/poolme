package pool

import (
	"container/heap"
	"context"
	"sync"
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

type priorityQueueStrategy[T any, R any] struct {
	pq            *priorityQueue[T, R]
	mu            sync.Mutex
	availableChan chan struct{}
}

func newPriorityQueueStrategy[T any, R any](taskBuffer int, checkPrior func(a T) int) *priorityQueueStrategy[T, R] {
	queue := make([]*submittedTask[T, R], 0, taskBuffer)
	return &priorityQueueStrategy[T, R]{
		pq:            newPriorityQueue(queue, checkPrior),
		availableChan: make(chan struct{}, taskBuffer),
	}
}

func (s *priorityQueueStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	s.mu.Lock()
	heap.Push(s.pq, task)
	s.mu.Unlock()

	s.availableChan <- struct{}{}
	return nil
}

func (s *priorityQueueStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-s.availableChan:
			if !ok {
				return
			}

			for {
				s.mu.Lock()
				if s.pq.Len() == 0 {
					s.mu.Unlock()
					break
				}
				item := heap.Pop(s.pq).(*submittedTask[T, R])
				s.mu.Unlock()
				executeSubmitted(ctx, item, pool, executor)
			}
		}
	}
}

func (s *priorityQueueStrategy[T, R]) Shutdown() {
	close(s.availableChan)
}
