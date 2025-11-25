package pool

import (
	"container/heap"
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SchedulingStrategy defines the behavior for distributing tasks to workers.
// Any new algorithm (Work Stealing, Priority Queue, Ring Buffer) must implement this.
type SchedulingStrategy[T any, R any] interface {
	// Submit accepts a task into the scheduling system.
	// It handles the logic of where the task goes (Global Queue vs Local Queue).
	Submit(task *submittedTask[T, R]) error

	// Shutdown gracefully stops the workers and waits for them to finish.
	Shutdown()

	//
	Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R])
}

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

// workStealingStrategy implements a work-stealing scheduler for efficient load balancing.
//
// Architecture:
//   - Each worker has its own local double-ended queue (dequeue)
//   - A global queue serves as overflow and initial task distribution point
//   - Workers process their local queue in LIFO order (for cache locality)
//   - When idle, workers steal from other workers' queues in FIFO order
//
// This is based on the design used in Go's runtime scheduler and other
// modern work-stealing schedulers (Java ForkJoinPool, .NET TPL, etc.)
type workStealingStrategy[T any, R any] struct {
	globalQueue               *dequeue[T, R]
	workerQueues              []*dequeue[T, R]
	nextWorker, stealSeed     atomic.Uint64 // Round-robin counter for task distribution
	workerCount, maxLocalSize int
	quit                      chan struct{}
}

func newWorkStealingStrategy[T any, R any](maxLocalSize int, workers int) *workStealingStrategy[T, R] {
	w := &workStealingStrategy[T, R]{
		globalQueue:  newDequeue[T, R](),
		maxLocalSize: maxLocalSize,
		quit:         make(chan struct{}),
		workerQueues: make([]*dequeue[T, R], workers),
		workerCount:  workers,
	}

	for i := range workers {
		w.workerQueues[i] = newDequeue[T, R]()
	}

	return w
}

// Submit distributes tasks to worker queues using round-robin for load balancing.
// If a worker's local queue is full, the task goes to the global queue instead.
func (s *workStealingStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	workerID := int(s.nextWorker.Add(1) % uint64(s.workerCount))
	local := s.workerQueues[workerID]

	if local.Len() < s.maxLocalSize {
		local.PushBack(task)
	} else {
		s.globalQueue.PushBack(task)
	}

	return nil
}

// worker is the main loop for each worker goroutine.
// It follows the work-stealing algorithm: local work -> global work -> steal -> backoff
func (s *workStealingStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	localQueue := s.workerQueues[workerID]
	missCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.quit:
			s.drain(ctx, localQueue, executor, pool)
			return
		default:
		}

		t := localQueue.PopBack()
		if t == nil {
			t = s.globalQueue.PopFront()
		}

		if t == nil {
			t = s.steal(int(workerID))
		}

		if t != nil {
			executeSubmitted(ctx, t, pool, executor)
			missCount = 0
			continue
		}

		missCount++
		s.backoff(missCount)
	}
}

// steal attempts to steal work from other workers' queues.
// Uses randomized victim selection to reduce contention.
// Steals from the front (FIFO) to minimize contention with the victim
// who is popping from the back (LIFO).
func (s *workStealingStrategy[T, R]) steal(thiefID int) *submittedTask[T, R] {
	n := s.workerCount
	if n <= 1 {
		return nil
	}

	startIndex := int(s.stealSeed.Add(1) % uint64(n))
	for i := range n {
		victimID := (startIndex + i) % n
		if victimID == thiefID {
			continue
		}

		if task, ok := s.workerQueues[victimID].TryPopFront(); ok {
			return task
		}

		if task := s.workerQueues[victimID].PopFront(); task != nil {
			return task
		}
	}

	return nil
}

// backoff implements an exponential backoff strategy for idle workers.
// This prevents busy-waiting and reduces CPU usage when there's no work.
//
// Backoff progression:
//   - missCount 1-10: No backoff (spin a bit first)
//   - missCount 11-20: Short yield (let other goroutines run)
//   - missCount 21+: Exponential sleep up to 10ms max
func (s *workStealingStrategy[T, R]) backoff(missCount int) {
	switch {
	case missCount <= 10:
		return

	case missCount <= 20:
		// Yield: give other goroutines a chance to run
		// runtime.Gosched() tells the scheduler to pause this goroutine
		// and run other goroutines before resuming
		runtime.Gosched()

	default:
		sleepTime := 100 * time.Microsecond
		for i := 20; i < missCount && sleepTime < 10*time.Millisecond; i++ {
			sleepTime *= 2
		}
		if sleepTime > 10*time.Millisecond {
			sleepTime = 10 * time.Millisecond
		}
		time.Sleep(sleepTime)
	}
}

// drain processes any remaining tasks in the local and global queues during shutdown.
// This ensures that all submitted tasks are completed before the worker exits.
// Both queues are drained in parallel for faster shutdown.
func (s *workStealingStrategy[T, R]) drain(ctx context.Context, localQueue *dequeue[T, R], executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			task := localQueue.PopBack()
			if task == nil {
				break
			}
			executeSubmitted(ctx, task, pool, executor)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			task := s.globalQueue.PopFront()
			if task == nil {
				break
			}
			executeSubmitted(ctx, task, pool, executor)
		}
	}()

	wg.Wait()
}

func (s *workStealingStrategy[T, R]) Shutdown() {
	close(s.quit)
}

// mpmcStrategy implements a lock-free multi-producer multi-consumer queue strategy.
//
// This strategy is optimized for high-throughput scenarios where many goroutines
// submit tasks concurrently to the pool.
type mpmcStrategy[T any, R any] struct {
	wg    sync.WaitGroup
	queue *MPMCQueue[*submittedTask[T, R]]
	quit  chan struct{}
}

// newMPMCStrategy creates a new MPMC queue strategy with the given configuration
func newMPMCStrategy[T any, R any](bounded bool, capacity int) *mpmcStrategy[T, R] {
	return &mpmcStrategy[T, R]{
		queue: NewMPMCQueue[*submittedTask[T, R]](capacity, bounded),
		quit:  make(chan struct{}),
	}
}

// Submit enqueues a task into the MPMC queue
func (s *mpmcStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	return s.queue.Enqueue(s.quit, task)
}

// worker is the main worker loop that dequeues and executes tasks
func (s *mpmcStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
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
func (s *mpmcStrategy[T, R]) drainQueue(ctx context.Context, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		task, ok := s.queue.TryDequeue()
		if !ok {
			return
		}
		executeSubmitted(ctx, task, pool, executor)
	}
}

// Shutdown gracefully stops the workers and closes the queue
func (s *mpmcStrategy[T, R]) Shutdown() {
	s.queue.Close()
	close(s.quit)
}
