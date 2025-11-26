package pool

import (
	"container/heap"
	"context"
	"sync"
)

// priorityQueue is a generic min-heap based priority queue used within the worker pool.
// It orders submitted tasks based on a user-defined priority function.
//
// Type Parameters:
//   - T: The input task type.
//   - R: The result type.
//
// Fields:
//   - queue: The underlying slice of submitted tasks (heap structure).
//   - checkPrior: Function to compute the priority of a task, lower value means higher priority.
type priorityQueue[T any, R any] struct {
	queue      []*submittedTask[T, R]
	checkPrior func(a T) int
}

// newPriorityQueue creates a new priorityQueue with the initial set of tasks and a priority function.
func newPriorityQueue[T any, R any](queue []*submittedTask[T, R], checkPrior func(a T) int) *priorityQueue[T, R] {
	return &priorityQueue[T, R]{
		queue:      queue,
		checkPrior: checkPrior,
	}
}

// Len returns the current number of tasks in the priority queue.
func (pq *priorityQueue[T, R]) Len() int {
	return len(pq.queue)
}

// Less compares the priorities of two tasks at index i and j.
// Returns true if the task at i has higher priority (less is considered higher).
func (pq *priorityQueue[T, R]) Less(i, j int) bool {
	return pq.checkPrior(pq.queue[i].task) < pq.checkPrior(pq.queue[j].task)
}

// Swap swaps the position of two tasks in the queue.
func (pq *priorityQueue[T, R]) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
}

// Push inserts a new task into the priority queue.
// This is intended to meet the heap.Interface contract.
func (pq *priorityQueue[T, R]) Push(x any) {
	task, ok := x.(*submittedTask[T, R])
	if !ok {
		panic("priorityQueue.Push: invalid type assertion")
	}
	pq.queue = append(pq.queue, task)
}

// Pop removes and returns the task with the highest priority (lowest value).
// This is intended to meet the heap.Interface contract.
func (pq *priorityQueue[T, R]) Pop() any {
	old := pq.queue
	n := len(old)
	item := old[n-1]
	pq.queue = old[0 : n-1]
	return item
}

// priorityQueueStrategy implements a priority-based task scheduling strategy for the worker pool.
// Tasks are executed in order of their priority rather than submission order.
//
// Architecture:
//   - Uses a min-heap (container/heap) to maintain tasks sorted by priority
//   - Lower priority values indicate higher priority (tasks are executed first)
//   - Thread-safe: Protected by a mutex for concurrent access
//   - Signal-based: Uses a buffered channel to notify workers of available tasks
//
// Performance characteristics:
//   - Submit: O(log n) - heap insertion
//   - Pop: O(log n) - heap extraction
//   - Space: O(n) where n is the number of queued tasks
//
// Use cases:
//   - Task prioritization (e.g., user-facing tasks over background jobs)
//   - Deadline-driven scheduling (shorter deadlines = higher priority)
//   - Critical path optimization (prioritize blocking operations)
//
// Note: This strategy introduces some overhead compared to FIFO due to heap operations
// and mutex contention. Use only when task prioritization is necessary.
type priorityQueueStrategy[T any, R any] struct {
	pq            *priorityQueue[T, R]
	mu            sync.Mutex
	availableChan chan struct{}
}

// newPriorityQueueStrategy creates a new priority queue-based scheduling strategy.
func newPriorityQueueStrategy[T any, R any](taskBuffer int, checkPrior func(a T) int) *priorityQueueStrategy[T, R] {
	queue := make([]*submittedTask[T, R], 0, taskBuffer)
	return &priorityQueueStrategy[T, R]{
		pq:            newPriorityQueue(queue, checkPrior),
		availableChan: make(chan struct{}, taskBuffer),
	}
}

// Submit adds a task to the priority queue and signals waiting workers.
// The task is inserted into the heap based on its priority value.
func (s *priorityQueueStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	s.mu.Lock()
	heap.Push(s.pq, task)
	s.mu.Unlock()

	s.availableChan <- struct{}{}
	return nil
}

// Worker is the main loop for worker goroutines using priority queue scheduling.
// Workers wait for task signals and then batch-process all available tasks in priority order.
func (s *priorityQueueStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		select {
		case <-ctx.Done():
			s.drain(ctx, executor, pool)
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
				task, ok := heap.Pop(s.pq).(*submittedTask[T, R])
				s.mu.Unlock()
				if !ok {
					panic("priorityQueueStrategy.Worker: invalid type assertion")
				}
				executeSubmitted(ctx, task, pool, executor)
			}
		}
	}
}

// Shutdown initiates graceful shutdown of the priority queue strategy.
// Closes the signal channel, causing all workers to exit after processing remaining tasks.
//
// Note: This does not drain the queue. Any tasks remaining in the queue when workers
// exit will not be processed. Consider implementing drain logic if task completion
// guarantees are required.
func (s *priorityQueueStrategy[T, R]) Shutdown() {
	close(s.availableChan)
}

// drain processes all remaining tasks in the priority queue during shutdown.
// This ensures that no tasks are left unprocessed when the strategy is shut down.
func (s *priorityQueueStrategy[T, R]) drain(ctx context.Context, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		s.mu.Lock()
		if s.pq.Len() == 0 {
			s.mu.Unlock()
			return
		}

		task, ok := heap.Pop(s.pq).(*submittedTask[T, R])
		s.mu.Unlock()
		if !ok {
			panic("priorityQueueStrategy.drain: invalid type assertion")
		}
		executeSubmitted(ctx, task, pool, executor)
	}
}
