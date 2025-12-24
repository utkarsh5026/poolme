package scheduler

import (
	"container/heap"
	"context"
	"sync"

	"github.com/utkarsh5026/gopool/internal/types"
)

// priorityQueue is a generic min-heap based priority queue used within the worker pool.
// It orders submitted tasks based on a user-defined comparison function.
//
// Type Parameters:
//   - T: The input task type.
//   - R: The result type.
//
// Fields:
//   - queue: The underlying slice of submitted tasks (heap structure).
//   - lessFunc: Function to compare two tasks, returns true if the first task has higher priority.
type priorityQueue[T any, R any] struct {
	queue    []*types.SubmittedTask[T, R]
	lessFunc func(a, b T) bool
}

// newPriorityQueue creates a new priorityQueue with the initial set of tasks and a comparison function.
// The lessFunc should return true if task 'a' has higher priority than task 'b'.
func newPriorityQueue[T any, R any](queue []*types.SubmittedTask[T, R], lessFunc func(a, b T) bool) *priorityQueue[T, R] {
	return &priorityQueue[T, R]{
		queue:    queue,
		lessFunc: lessFunc,
	}
}

// Len returns the current number of tasks in the priority queue.
func (pq *priorityQueue[T, R]) Len() int {
	return len(pq.queue)
}

// Less compares the priorities of two tasks at index i and j.
// Returns true if the task at i has higher priority than task at j.
func (pq *priorityQueue[T, R]) Less(i, j int) bool {
	return pq.lessFunc(pq.queue[i].Task, pq.queue[j].Task)
}

// Swap swaps the position of two tasks in the queue.
func (pq *priorityQueue[T, R]) Swap(i, j int) {
	pq.queue[i], pq.queue[j] = pq.queue[j], pq.queue[i]
}

// Push inserts a new task into the priority queue.
// This is intended to meet the heap.Interface contract.
func (pq *priorityQueue[T, R]) Push(x any) {
	task, ok := x.(*types.SubmittedTask[T, R])
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
type priorityQueueStrategy[T any, R any] struct {
	pq        *priorityQueue[T, R]
	conf      *ProcessorConfig[T, R]
	mu        sync.Mutex
	available *workerSignal
	runner    *workerRunner[T, R]
}

// newPriorityQueueStrategy creates a new priority queue-based scheduling strategy.
func newPriorityQueueStrategy[T any, R any](conf *ProcessorConfig[T, R], tasks []*types.SubmittedTask[T, R]) *priorityQueueStrategy[T, R] {
	if tasks == nil {
		tasks = make([]*types.SubmittedTask[T, R], 0, conf.TaskBuffer)
	}
	p := &priorityQueueStrategy[T, R]{
		pq:        newPriorityQueue(tasks, conf.LessFunc),
		conf:      conf,
		available: newWorkerSignal(),
	}

	p.runner = newWorkerRunner(conf, p)
	return p
}

// Submit adds a task to the priority queue and signals waiting workers.
// The task is inserted into the heap based on its priority value.
func (s *priorityQueueStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.available.IsClosed() {
		return context.Canceled
	}
	heap.Push(s.pq, task)
	s.available.Signal()
	return nil
}

// SubmitBatch adds multiple tasks to the priority queue efficiently.
// Builds the heap in O(n) time rather than O(n log n) for individual inserts.
func (s *priorityQueueStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.available.IsClosed() {
		return 0, context.Canceled
	}
	s.pq.queue = append(s.pq.queue, tasks...)
	heap.Init(s.pq)

	for range tasks {
		s.available.Signal()
	}
	return len(tasks), nil
}

// Worker is the main loop for worker goroutines using priority queue scheduling.
// Workers process all available tasks in priority order, then wait for new task signals.
func (s *priorityQueueStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	drainFunc := func() {
		s.drain(ctx, executor, h)
	}

	// Process any tasks that were submitted before worker started
	if err := s.runInLoop(ctx, executor, h, drainFunc, false); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			drainFunc()
			return ctx.Err()

		case _, ok := <-s.available.Wait():
			if !ok {
				return nil
			}

			if err := s.runInLoop(ctx, executor, h, drainFunc, false); err != nil {
				return err
			}
		}
	}
}

// Shutdown initiates graceful shutdown of the priority queue strategy.
// Closes the signal channel, causing all workers to exit after processing remaining tasks.
func (s *priorityQueueStrategy[T, R]) Shutdown() {
	s.available.Close()
}

// drain processes all remaining tasks in the priority queue during shutdown.
// This ensures that no tasks are left unprocessed when the strategy is shut down.
func (s *priorityQueueStrategy[T, R]) drain(ctx context.Context, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	_ = s.runInLoop(ctx, executor, h, nil, true)
}

// runInLoop processes tasks in a loop until the queue is empty or an error occurs.
// It uses the provided executor and result handler for task processing.
// The ignoreErr flag determines whether to continue processing on errors.
func (s *priorityQueueStrategy[T, R]) runInLoop(ctx context.Context, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R], drainFunc func(), ignoreErr bool) error {
	for {
		t := s.dequeueTask()
		if t == nil {
			break
		}

		if err := s.runner.Execute(ctx, t, executor, h, drainFunc); err != nil && !ignoreErr {
			return err
		}
	}
	return nil
}

// dequeueTask removes and returns the highest priority task from the queue.
// Returns nil if the queue is empty.
func (s *priorityQueueStrategy[T, R]) dequeueTask() *types.SubmittedTask[T, R] {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pq.Len() == 0 {
		return nil
	}

	task, ok := heap.Pop(s.pq).(*types.SubmittedTask[T, R])
	if !ok {
		panic("priorityQueueStrategy.dequeueTask: invalid type assertion")
	}

	return task
}
