package scheduler

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	fastCheckCounter          = 3
	defaultLocalQueueCapacity = 256 // Increased for better local throughput
	maxStealAttempts          = 8   // Increased to check more victims
	localQueueThreshold       = 128 // Push to global when local exceeds this
	batchStealSize            = 4   // Steal multiple tasks at once
)

// wsDeque implements a lock-free work-stealing deque (double-ended queue) optimized for
// concurrent access patterns in work-stealing schedulers.
//
// Design principles:
//   - Lock-free: Uses atomic operations instead of mutexes for better performance
//   - Cache-line padding: Prevents false sharing between head and tail indices
//   - Dynamic growth: Automatically doubles capacity when full
//   - Ring buffer: Uses bitwise AND for fast modulo operations (capacity must be power of 2)
//
// Concurrency model:
//   - The tail is modified only by the owner worker (single writer)
//   - The head is modified by thieves attempting to steal work (multiple readers/writers)
//   - Memory ordering is carefully managed using atomic operations
//
// This implementation is based on the Chase-Lev work-stealing deque algorithm,
// widely used in Go's runtime scheduler, Java's ForkJoinPool, and other modern schedulers.
//
// References:
//   - "Dynamic Circular Work-Stealing Deque" by Chase and Lev (2005)
//   - Go runtime scheduler: runtime/proc.go
type wsDeque[T, R any] struct {
	// Ring buffer of tasks
	ring atomic.Pointer[[]*types.SubmittedTask[T, R]]

	// Mask for fast modulo (capacity - 1)
	mask uint64

	// Head index - modified by stealers (other workers)
	// Padded to prevent false sharing
	_    [cacheLinePadding]byte
	head atomic.Int64
	_    [cacheLinePadding - 8]byte

	// Tail index - modified only by owner worker
	// Uses atomic operations to ensure visibility to stealers reading in PopFront
	tail atomic.Int64

	// Capacity (power of 2)
	capacity uint64
}

func newWSDeque[T, R any](capacity int) *wsDeque[T, R] {
	if capacity <= 0 {
		capacity = defaultLocalQueueCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]*types.SubmittedTask[T, R], capacity)

	// Safe conversion: capacity is guaranteed to be positive after validation and nextPowerOfTwo
	dq := &wsDeque[T, R]{
		mask:     uint64(capacity - 1), // #nosec G115 -- capacity is validated positive, no overflow possible
		capacity: uint64(capacity),     // #nosec G115 -- capacity is validated positive, no overflow possible
	}

	dq.ring.Store(&ring)
	return dq
}

// PushBack adds a task to the back of the deque. This operation is only safe when
// called by the owner worker and should never be called concurrently by multiple goroutines.
//
// The deque automatically grows if it reaches capacity. Growth is amortized O(1) since
// it doubles in size each time.
//
// Parameters:
//   - t: The task to add to the back of the deque
//
// Memory ordering:
//   - Uses atomic store for tail to ensure visibility to stealing workers
//   - Load of head uses acquire semantics to synchronize with stealing operations
func (w *wsDeque[T, R]) PushBack(t *types.SubmittedTask[T, R]) {
	tail := w.tail.Load()
	head := w.head.Load()
	ring := *w.ring.Load()

	// #nosec G115 -- intentional conversion for capacity check, tail-head is safe to convert
	if uint64(tail-head) >= w.capacity {
		ring = w.grow(head, tail)
	}

	ring[tail&int64(w.mask)] = t // #nosec G115 -- intentional conversion for ring indexing with wraparound
	w.tail.Store(tail + 1)
}

// grow doubles the capacity of the deque and copies existing tasks to the new buffer.
// This is called automatically by PushBack when the deque is full.
//
// Parameters:
//   - head: Current head index
//   - tail: Current tail index
//
// Returns:
//   - The newly allocated ring buffer
//
// Note: This operation is not thread-safe and should only be called by the owner worker.
func (w *wsDeque[T, R]) grow(head, tail int64) []*types.SubmittedTask[T, R] {
	old := *w.ring.Load()
	oldCap := w.capacity

	newCap := oldCap << 1
	newRing := make([]*types.SubmittedTask[T, R], newCap)

	for i := head; i < tail; i++ {
		newRing[i&int64(newCap-1)] = old[i&int64(oldCap-1)] // #nosec G115 -- intentional conversion for ring indexing
	}

	w.ring.Store(&newRing)
	w.capacity = newCap
	w.mask = newCap - 1
	return newRing
}

// PopBack removes and returns a task from the back of the deque (LIFO order).
// / This operation is only safe when called by the owner worker.
//
// LIFO ordering provides better cache locality since recently pushed tasks are more
// likely to still be in the CPU cache.
//
// Returns:
//   - The task from the back of the deque, or nil if the deque is empty
//
// Race handling:
//   - If the deque has only one element and a concurrent steal occurs, the CAS
//     operation ensures only one party successfully claims the task
func (w *wsDeque[T, R]) PopBack() *types.SubmittedTask[T, R] {
	tail := w.tail.Load() - 1
	w.tail.Store(tail)

	head := w.head.Load()
	if head > tail {
		w.tail.Store(head)
		return nil
	}

	ring := *w.ring.Load()
	t := ring[tail&int64(w.mask)] // #nosec G115 -- intentional conversion for ring indexing with wraparound

	if head == tail {
		if !w.head.CompareAndSwap(head, head+1) {
			t = nil
		}
		w.tail.Store(head + 1)
	}

	return t
}

// PopFront removes and returns a task from the front of the deque (FIFO order).
// This operation is safe to call concurrently from multiple stealing workers.
//
// FIFO ordering for steals minimizes contention with the owner who pops from the back,
// as they access opposite ends of the queue.
//
// Returns:
//   - The task from the front of the deque, or nil if the deque is empty or the steal failed
//
// Concurrency:
//   - Uses CAS to atomically claim a task, ensuring only one stealer succeeds
//   - Multiple failed CAS attempts indicate high contention
func (w *wsDeque[T, R]) PopFront() *types.SubmittedTask[T, R] {
	head := w.head.Load()
	tail := w.tail.Load()

	if head >= tail {
		return nil
	}

	ring := *w.ring.Load()
	t := ring[head&int64(w.mask)] // #nosec G115 -- intentional conversion for ring indexing with wraparound

	if !w.head.CompareAndSwap(head, head+1) {
		return nil
	}

	return t
}

// Len returns the approximate number of tasks in the deque.
// The result may be stale immediately after the call returns due to concurrent operations.
//
// Returns:
//   - The number of tasks currently in the deque (may be approximate due to races)
//
// Note: This is primarily useful for monitoring and debugging, not for making
// critical synchronization decisions.
func (w *wsDeque[T, R]) Len() int {
	head := w.head.Load()
	tail := w.tail.Load()
	return int(tail - head)
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
	conf                      *ProcessorConfig[T, R]
	nextWorker, stealSeed     atomic.Uint64 // Round-robin counter for task distribution
	workerCount, maxLocalSize int
	quit                      chan struct{}
}

func newWorkStealingStrategy[T any, R any](maxLocalSize int, conf *ProcessorConfig[T, R]) *workSteal[T, R] {
	n := conf.WorkerCount
	w := &workSteal[T, R]{
		globalQueue:  newWSDeque[T, R](maxLocalSize),
		maxLocalSize: maxLocalSize,
		quit:         make(chan struct{}),
		workerQueues: make([]*wsDeque[T, R], n),
		workerCount:  n,
		conf:         conf,
	}

	for i := range n {
		w.workerQueues[i] = newWSDeque[T, R](maxLocalSize)
	}

	return w
}

// Submit uses a hybrid approach for optimal performance:
// 1. Try to add to a worker's local queue (round-robin for cache locality)
// 2. If local queue is above threshold, add to global queue (load balancing)
// 3. This gives both cache locality AND prevents queue buildup
func (s *workSteal[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	workerID := int(s.nextWorker.Add(1) % uint64(s.workerCount)) // #nosec G115 -- workerCount is always positive
	local := s.workerQueues[workerID]

	if local.Len() < localQueueThreshold {
		local.PushBack(task)
	} else {
		s.globalQueue.PushBack(task)
	}

	return nil
}

// SubmitBatch pre-distributes tasks across worker queues for optimal load balancing.
// This eliminates per-task submission overhead and gives workers balanced starting queues.
func (s *workSteal[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}

	tasksPerWorker := len(tasks) / s.workerCount
	remainder := len(tasks) % s.workerCount

	taskIndex := 0

	// Distribute tasks evenly across worker queues
	for workerID := 0; workerID < s.workerCount && taskIndex < len(tasks); workerID++ {
		localQueue := s.workerQueues[workerID]

		// Give this worker its share of tasks (plus one extra if there's remainder)
		tasksForThisWorker := tasksPerWorker
		if workerID < remainder {
			tasksForThisWorker++
		}

		// Add tasks to this worker's local queue
		for i := 0; i < tasksForThisWorker && taskIndex < len(tasks); i++ {
			localQueue.PushBack(tasks[taskIndex])
			taskIndex++
		}
	}

	return taskIndex, nil
}

// worker is the main loop for each worker goroutine.
// It follows the work-stealing algorithm: local work -> global batch -> steal -> backoff
func (s *workSteal[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	localQueue := s.workerQueues[workerID]
	var missCount, globalCounter int

	drain := func() {
		s.drain(ctx, localQueue, executor, h)
	}

	for {
		if globalCounter%10 == 0 {
			if err := s.checkQuit(ctx, drain); err != nil {
				return err
			}
		}
		globalCounter++

		for range fastCheckCounter {
			if t := localQueue.PopBack(); t != nil {
				if err := handleWithCare(ctx, t, s.conf, executor, h, drain); err != nil {
					return err
				}
				missCount = 0
			}
		}

		if s.globalQueue.Len() > 0 {
			if t := s.globalQueue.PopFront(); t != nil {
				if err := handleWithCare(ctx, t, s.conf, executor, h, drain); err != nil {
					return err
				}
				missCount = 0
				s.addToLocalBatch(localQueue)
				continue
			}
		}

		if t := s.steal(int(workerID)); t != nil {
			if err := handleWithCare(ctx, t, s.conf, executor, h, drain); err != nil {
				return err
			}
			missCount = 0
			continue
		}

		missCount++
		s.backoff(missCount)
	}
}

// addToLocalBatch moves a batch of tasks from the global shared queue to the worker's local queue.
// This is a form of local caching that improves locality and reduces contention on the global queue.
// Up to (batchStealSize-1) tasks are transferred (not including the initial task already consumed).
// The actual batch count is the smaller of the global queue length or (batchStealSize-1).
func (s *workSteal[T, R]) addToLocalBatch(localQueue *wsDeque[T, R]) {
	batchCount := min(s.globalQueue.Len(), batchStealSize-1)
	for range batchCount {
		if task := s.globalQueue.PopFront(); task != nil {
			localQueue.PushBack(task)
		}
	}
}

// checkQuit checks for cancellation signals via context or explicit quit channel.
// If a cancellation is detected, it drains the worker's local queue to ensure
// no tasks are abandoned, then returns the appropriate error (ctx.Err() or nil).
// If no quit condition occurs, it returns nil and work may continue.
func (s *workSteal[T, R]) checkQuit(ctx context.Context, drainFunc func()) error {
	select {
	case <-ctx.Done():
		drainFunc()
		return ctx.Err()
	case <-s.quit:
		drainFunc()
		return nil
	default:
		return nil
	}
}

// steal attempts to steal work from other workers' queues using batch stealing.
// Batch stealing reduces overhead by taking multiple tasks at once.
// Uses randomized victim selection to reduce contention.
// Steals from the front (FIFO) to minimize contention with the victim
// who is popping from the back (LIFO).
func (s *workSteal[T, R]) steal(thiefID int) *types.SubmittedTask[T, R] {
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
func (s *workSteal[T, R]) drain(ctx context.Context, localQueue *wsDeque[T, R], executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			task := localQueue.PopBack()
			if task == nil {
				break
			}
			_ = executeSubmitted(ctx, task, s.conf, executor, h)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			task := s.globalQueue.PopFront()
			if task == nil {
				break
			}
			_ = executeSubmitted(ctx, task, s.conf, executor, h)
		}
	}()

	wg.Wait()
}

func (s *workSteal[T, R]) Shutdown() {
	close(s.quit)
}
