package pool

import (
	"container/heap"
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type channelStrategy[T any, R any] struct {
	config   *processorConfig[T, R]
	taskChan chan *submittedTask[T, R]
}

func newChannelStrategy[T any, R any](conf *processorConfig[T, R]) *channelStrategy[T, R] {
	return &channelStrategy[T, R]{
		config:   conf,
		taskChan: make(chan *submittedTask[T, R], conf.taskBuffer),
	}
}

func (s *channelStrategy[T, R]) Submit(task *submittedTask[T, R]) error {
	s.taskChan <- task
	return nil
}

func (s *channelStrategy[T, R]) Shutdown() {
	close(s.taskChan)
}

func (s *channelStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R]) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case t, ok := <-s.taskChan:
			if !ok {
				return nil
			}
			if err := executeSafely(ctx, t, s.config, executor); err != nil {
				return err
			}
		}
	}
}

// mpmc implements a lock-free multi-producer multi-consumer queue strategy.
//
// This strategy is optimized for high-throughput scenarios where many goroutines
// submit tasks concurrently to the pool.
type mpmc[T any, R any] struct {
	queue *mpmcQueue[*submittedTask[T, R]]
	conf  *processorConfig[T, R]
	quit  chan struct{}
}

// newMPMCStrategy creates a new MPMC queue strategy with the given configuration
func newMPMCStrategy[T any, R any](bounded bool, capacity int) *mpmc[T, R] {
	return &mpmc[T, R]{
		queue: newMPMCQueue[*submittedTask[T, R]](capacity, bounded),
		quit:  make(chan struct{}),
	}
}

// Submit enqueues a task into the MPMC queue
func (s *mpmc[T, R]) Submit(task *submittedTask[T, R]) error {
	return s.queue.Enqueue(s.quit, task)
}

// worker is the main worker loop that dequeues and executes tasks
func (s *mpmc[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R]) error {
	quitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-s.quit:
			cancel()
		case <-ctx.Done():
			cancel()
		}
	}()

	for {
		task, err := s.queue.Dequeue(quitCtx)
		if err != nil {
			if err == ErrQueueClosed || err == context.Canceled {
				s.drainQueue(ctx, executor)
				return nil
			}
			if ctx.Err() != nil {
				s.drainQueue(ctx, executor)
				return nil
			}
			continue
		}

		if err := executeSubmitted(ctx, task, s.conf, executor); err != nil && !s.conf.continueOnErr {
			return err
		}
	}
}

// drainQueue attempts to process any remaining tasks in the queue during shutdown
func (s *mpmc[T, R]) drainQueue(ctx context.Context, executor ProcessFunc[T, R]) {
	for {
		task, ok := s.queue.TryDequeue()
		if !ok {
			return
		}
		executeSubmitted(ctx, task, s.conf, executor)
	}
}

// Shutdown gracefully stops the workers and closes the queue
func (s *mpmc[T, R]) Shutdown() {
	s.queue.Close()
	close(s.quit)
}

// priorityQueueStrategy implements a priority-based task scheduling strategy for the worker pool.
// Tasks are executed in order of their priority rather than submission order.
type priorityQueueStrategy[T any, R any] struct {
	pq            *priorityQueue[T, R]
	conf          *processorConfig[T, R]
	mu            sync.Mutex
	availableChan chan struct{}
}

// newPriorityQueueStrategy creates a new priority queue-based scheduling strategy.
func newPriorityQueueStrategy[T any, R any](conf *processorConfig[T, R], tasks []*submittedTask[T, R]) *priorityQueueStrategy[T, R] {
	if tasks == nil {
		tasks = make([]*submittedTask[T, R], 0, conf.taskBuffer)
	}
	return &priorityQueueStrategy[T, R]{
		pq:            newPriorityQueue(tasks, conf.pqFunc),
		conf:          conf,
		availableChan: make(chan struct{}, conf.taskBuffer),
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
func (s *priorityQueueStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R]) error {
	for {
		select {
		case <-ctx.Done():
			s.drain(ctx, executor)
			return nil

		case _, ok := <-s.availableChan:
			if !ok {
				return nil
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
				if err := executeSafely(ctx, task, s.conf, executor); err != nil {
					return err
				}
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
func (s *priorityQueueStrategy[T, R]) drain(ctx context.Context, executor ProcessFunc[T, R]) {
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
		executeSubmitted(ctx, task, s.conf, executor)
	}
}

// workSteal implements a work-stealing scheduler for efficient load balancing.
//
// Architecture:
//   - Each worker has its own local double-ended queue (wsDeque)
//   - A global queue serves as overflow and initial task distribution point
//   - Workers process their local queue in LIFO order (for cache locality)
//   - When idle, workers steal from other workers' queues in FIFO order
//
// This is based on the design used in Go's runtime scheduler and other
// modern work-stealing schedulers (Java ForkJoinPool, .NET TPL, etc.)
type workSteal[T any, R any] struct {
	globalQueue               *wsDeque[T, R]
	workerQueues              []*wsDeque[T, R]
	conf                      *processorConfig[T, R]
	nextWorker, stealSeed     atomic.Uint64 // Round-robin counter for task distribution
	workerCount, maxLocalSize int
	quit                      chan struct{}
}

func newWorkStealingStrategy[T any, R any](maxLocalSize int, conf *processorConfig[T, R]) *workSteal[T, R] {
	w := &workSteal[T, R]{
		globalQueue:  newWSDeque[T, R](maxLocalSize),
		maxLocalSize: maxLocalSize,
		quit:         make(chan struct{}),
		workerQueues: make([]*wsDeque[T, R], conf.workerCount),
		workerCount:  conf.workerCount,
	}

	for i := range conf.workerCount {
		w.workerQueues[i] = newWSDeque[T, R](maxLocalSize)
	}

	return w
}

// Submit uses a hybrid approach for optimal performance:
// 1. Try to add to a worker's local queue (round-robin for cache locality)
// 2. If local queue is above threshold, add to global queue (load balancing)
// 3. This gives both cache locality AND prevents queue buildup
func (s *workSteal[T, R]) Submit(task *submittedTask[T, R]) error {
	workerID := int(s.nextWorker.Add(1) % uint64(s.workerCount)) // #nosec G115 -- workerCount is always positive
	local := s.workerQueues[workerID]

	if local.Len() < localQueueThreshold {
		local.PushBack(task)
	} else {
		s.globalQueue.PushBack(task)
	}

	return nil
}

// worker is the main loop for each worker goroutine.
// It follows the work-stealing algorithm: local work -> global batch -> steal -> backoff
func (s *workSteal[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R]) error {
	localQueue := s.workerQueues[workerID]
	var missCount, globalCounter int

	for {
		if globalCounter%10 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.quit:
				s.drain(ctx, localQueue, executor)
				return nil
			default:
			}
		}
		globalCounter++

		for range fastCheckCounter {
			if t := localQueue.PopBack(); t != nil {
				if err := executeSafely(ctx, t, s.conf, executor); err != nil {
					return err
				}
				missCount = 0
			}
		}

		if s.globalQueue.Len() > 0 {
			if t := s.globalQueue.PopFront(); t != nil {
				if err := executeSafely(ctx, t, s.conf, executor); err != nil {
					return err
				}
				missCount = 0

				batchCount := min(s.globalQueue.Len(), batchStealSize-1)
				for range batchCount {
					if task := s.globalQueue.PopFront(); task != nil {
						localQueue.PushBack(task)
					}
				}
				continue
			}
		}

		if t := s.steal(int(workerID)); t != nil {
			if err := executeSafely(ctx, t, s.conf, executor); err != nil {
				return err
			}
			missCount = 0
			continue
		}

		missCount++
		s.backoff(missCount)
	}
}

// steal attempts to steal work from other workers' queues using batch stealing.
// Batch stealing reduces overhead by taking multiple tasks at once.
// Uses randomized victim selection to reduce contention.
// Steals from the front (FIFO) to minimize contention with the victim
// who is popping from the back (LIFO).
func (s *workSteal[T, R]) steal(thiefID int) *submittedTask[T, R] {
	n := s.workerCount
	if n <= 1 {
		return nil
	}

	maxAttempts := min(n-1, maxStealAttempts)
	startIndex := int(s.stealSeed.Add(1) % uint64(n)) // #nosec G115 -- n is workerCount which is always positive
	thiefQueue := s.workerQueues[thiefID]

	for i := range maxAttempts {
		victimID := (startIndex + i) % n
		if victimID == thiefID {
			continue
		}

		victimQueue := s.workerQueues[victimID]

		// Batch steal: take multiple tasks at once to reduce overhead
		// Only do this if victim has enough tasks (don't steal from small queues)
		victimLen := victimQueue.Len()
		if victimLen > batchStealSize*2 {
			stealCount := min(victimLen/2, batchStealSize)

			// Steal first task to return, rest go to local queue
			firstTask := victimQueue.PopFront()
			if firstTask == nil {
				continue
			}

			// Steal additional tasks to local queue
			for j := 1; j < stealCount; j++ {
				if task := victimQueue.PopFront(); task != nil {
					thiefQueue.PushBack(task)
				}
			}

			return firstTask
		} else if victimLen > 0 {
			if task := victimQueue.PopFront(); task != nil {
				return task
			}
		}
	}

	return nil
}

// backoff implements an adaptive exponential backoff strategy for idle workers.
// This prevents busy-waiting while maintaining responsiveness.
//
// Backoff progression (optimized for responsiveness):
//   - missCount 1-20: Active spinning (check frequently for new work)
//   - missCount 21-30: Yield to scheduler (let other goroutines run)
//   - missCount 31+: Exponential sleep (conserve CPU when truly idle)
func (s *workSteal[T, R]) backoff(missCount int) {
	switch {
	case missCount <= 20:
		// Active spinning - keep checking aggressively
		// This helps with bursty workloads where new tasks arrive quickly
		return

	case missCount <= 30:
		// Yield: give other goroutines a chance to run
		// runtime.Gosched() tells the scheduler to pause this goroutine
		// and run other goroutines before resuming
		runtime.Gosched()

	default:
		// Exponential backoff for truly idle workers
		// Start with 50µs and double up to 5ms max (more responsive than before)
		sleepTime := 50 * time.Microsecond
		iterations := missCount - 30
		for i := 0; i < iterations && sleepTime < 5*time.Millisecond; i++ {
			sleepTime *= 2
		}
		if sleepTime > 5*time.Millisecond {
			sleepTime = 5 * time.Millisecond
		}
		time.Sleep(sleepTime)
	}
}

// drain processes any remaining tasks in the local and global queues during shutdown.
// This ensures that all submitted tasks are completed before the worker exits.
// Both queues are drained in parallel for faster shutdown.
func (s *workSteal[T, R]) drain(ctx context.Context, localQueue *wsDeque[T, R], executor ProcessFunc[T, R]) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			task := localQueue.PopBack()
			if task == nil {
				break
			}
			executeSubmitted(ctx, task, s.conf, executor)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			task := s.globalQueue.PopFront()
			if task == nil {
				break
			}
			executeSubmitted(ctx, task, s.conf, executor)
		}
	}()

	wg.Wait()
}

func (s *workSteal[T, R]) Shutdown() {
	close(s.quit)
}
