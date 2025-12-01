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

// dequeBuffer holds the slice and the mask together.
// This ensures that when a stealer loads the buffer, the mask matches the slice capacity.
type dequeBuffer[T, R any] struct {
	ring []*types.SubmittedTask[T, R]
	mask uint64
}

// wsDeque implements a lock-free work-stealing deque (double-ended queue) optimized for
// concurrent access patterns in work-stealing schedulers.
//
// Concurrency model:
//   - The tail is modified only by the owner worker (single writer)
//   - The head is modified by thieves attempting to steal work (multiple readers/writers)
//   - Memory ordering is carefully managed using atomic operations
//
// This implementation is based on the Chase-Lev work-stealing deque algorithm,
// widely used in Go's runtime scheduler, Java's ForkJoinPool, and other modern schedulers.
type wsDeque[T, R any] struct {
	// state holds the atomic pointer to the current consistent snapshot.
	buffer atomic.Pointer[dequeBuffer[T, R]]

	// Head index - modified by stealers (other workers)
	// Padded to prevent false sharing
	_    [cacheLinePadding]byte
	head atomic.Int64
	_    [cacheLinePadding - 8]byte

	// Tail index - modified only by owner worker
	// Uses atomic operations to ensure visibility to stealers reading in PopFront
	tail atomic.Int64
}

func newWSDeque[T, R any](capacity int) *wsDeque[T, R] {
	if capacity <= 0 {
		capacity = defaultLocalQueueCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]*types.SubmittedTask[T, R], capacity)

	dq := &wsDeque[T, R]{}

	dq.buffer.Store(&dequeBuffer[T, R]{
		ring: ring,
		mask: uint64(capacity - 1), // #nosec G115 -- capacity is validated positive, no overflow possible
	})
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
	ringBuf := w.buffer.Load()
	ring := ringBuf.ring

	// #nosec G115 -- intentional conversion for capacity check, tail-head is safe to convert
	if uint64(tail-head) >= ringBuf.mask {
		ring = w.grow(head, tail)
	}

	ring[tail&int64(ringBuf.mask)] = t // #nosec G115 -- intentional conversion for ring indexing with wraparound
	w.tail.Store(tail + 1)
}

// grow doubles the capacity of the deque and copies existing tasks to the new buffer.
func (w *wsDeque[T, R]) grow(head, tail int64) []*types.SubmittedTask[T, R] {
	old := *w.buffer.Load()
	oldCap := len(old.ring)

	newCap := oldCap << 1
	newRing := make([]*types.SubmittedTask[T, R], newCap)

	for i := head; i < tail; i++ {
		newRing[i&int64(newCap-1)] = old.ring[i&int64(oldCap-1)] // #nosec G115 -- intentional conversion for ring indexing
	}

	newBuf := &dequeBuffer[T, R]{
		ring: newRing,
		mask: uint64(newCap - 1),
	}
	w.buffer.Store(newBuf)
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
func (w *wsDeque[T, R]) PopBack() *types.SubmittedTask[T, R] {
	tail := w.tail.Load() - 1
	w.tail.Store(tail)

	head := w.head.Load()
	if head > tail {
		w.tail.Store(head)
		return nil
	}

	r := w.buffer.Load()
	t := r.ring[tail&int64(r.mask)] // #nosec G115 -- intentional conversion for ring indexing with wraparound

	// compete the pop
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
func (w *wsDeque[T, R]) PopFront() *types.SubmittedTask[T, R] {
	head := w.head.Load()
	tail := w.tail.Load()

	if head >= tail {
		return nil
	}

	ringBuf := w.buffer.Load()
	t := ringBuf.ring[head&int64(ringBuf.mask)] // #nosec G115 -- intentional conversion for ring indexing with wraparound

	if !w.head.CompareAndSwap(head, head+1) {
		return nil
	}

	return t
}

// Len returns the approximate number of tasks in the deque.
// The result may be stale immediately after the call returns due to concurrent operations.
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
	glock                     sync.Mutex
	workerQueues              []*wsDeque[T, R]
	conf                      *ProcessorConfig[T, R]
	stealSeed                 atomic.Uint64 // Round-robin counter for task distribution
	workerCount, maxLocalSize int
	quit                      chan struct{}
	wakeup                    chan struct{} // Buffered channel to wake sleeping workers
}

func newWorkStealingStrategy[T any, R any](maxLocalSize int, conf *ProcessorConfig[T, R]) *workSteal[T, R] {
	n := conf.WorkerCount
	w := &workSteal[T, R]{
		globalQueue:  newWSDeque[T, R](maxLocalSize),
		maxLocalSize: maxLocalSize,
		quit:         make(chan struct{}),
		wakeup:       make(chan struct{}, n), // Buffered to wake multiple workers without blocking
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
	s.glock.Lock()
	s.globalQueue.PushBack(task)
	s.glock.Unlock()

	select {
	case s.wakeup <- struct{}{}:
	default:
	}
	return nil
}

// SubmitBatch pre-distributes tasks across worker queues for optimal load balancing.
// This eliminates per-task submission overhead and gives workers balanced starting queues.
func (s *workSteal[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}

	s.glock.Lock()
	for _, t := range tasks {
		s.globalQueue.PushBack(t)
	}
	s.glock.Unlock()

	workersToWake := min(s.workerCount, (len(tasks)+1)/2)
	for range workersToWake {
		select {
		case s.wakeup <- struct{}{}:
		default:
		}
	}

	return len(tasks), nil
}

// worker is the main loop for each worker goroutine.
// It follows the work-stealing algorithm: local work -> global batch -> steal -> backoff
func (s *workSteal[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	localQueue := s.workerQueues[workerID]
	var missCount, globalCounter int

	drain := func() {
		s.drain(ctx, localQueue, executor, h)
	}

	executeTask := func(t *types.SubmittedTask[T, R]) error {
		if err := handleWithCare(ctx, t, s.conf, executor, h, drain); err != nil {
			return err
		}
		return nil
	}

	for {
		if globalCounter%64 == 0 {
			if err := s.checkQuit(ctx, drain); err != nil {
				return err
			}
		}
		globalCounter++

		for range fastCheckCounter {
			if t := localQueue.PopBack(); t != nil {
				if err := executeTask(t); err != nil {
					return err
				}
				missCount = 0
				continue
			}
		}

		if s.globalQueue.Len() > 0 {
			if t := s.globalQueue.PopFront(); t != nil {
				if err := executeTask(t); err != nil {
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
			if err := executeTask(t); err != nil {
				return err
			}
			missCount = 0
			continue
		}

		missCount++

		// Check for wakeup signal before sleeping
		select {
		case <-s.wakeup:
			// New work available, reset miss count and continue
			missCount = 0
			continue
		default:
			if quit := s.backoff(missCount); quit {
				return nil
			}
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
		return ErrSchedulerClosed
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
		n := victimQueue.Len()
		if n > batchStealSize*2 {
			stealCount := min(n/2, batchStealSize)

			firstTask := victimQueue.PopFront()
			if firstTask == nil {
				continue
			}

			for range stealCount - 1 {
				if t := victimQueue.PopFront(); t != nil {
					thiefQueue.PushBack(t)
				}
			}

			return firstTask
		}

		if n > 0 {
			if t := victimQueue.PopFront(); t != nil {
				return t
			}
		}
	}

	return nil
}

// backoff implements an adaptive exponential backoff strategy for idle workers.
// This prevents busy-waiting while maintaining responsiveness.
func (s *workSteal[T, R]) backoff(missCount int) (quit bool) {
	switch {
	case missCount <= 20:
		// Active spinning - keep checking aggressively
		// This helps with bursty workloads where new tasks arrive quickly
		return false

	case missCount <= 30:
		// Yield: give other goroutines a chance to run
		// runtime.Gosched() tells the scheduler to pause this goroutine
		// and run other goroutines before resuming
		runtime.Gosched()

	default:
		sleepTime := 50 * time.Microsecond
		iterations := missCount - 30
		for i := 0; i < iterations && sleepTime < 5*time.Millisecond; i++ {
			sleepTime *= 2
		}
		if sleepTime > 5*time.Millisecond {
			sleepTime = 5 * time.Millisecond
		}

		select {
		case <-s.wakeup:
			// Woken up by new work arriving
		case <-s.quit:
			// Shutting down
			return true
		case <-time.After(sleepTime):
			// Normal timeout
		}
	}

	return false
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
			t := localQueue.PopBack()
			if t == nil {
				break
			}
			_ = executeSubmitted(ctx, t, s.conf, executor, h)
		}
	}()

	go func() {
		defer wg.Done()
		for {
			t := s.globalQueue.PopFront()
			if t == nil {
				break
			}
			_ = executeSubmitted(ctx, t, s.conf, executor, h)
		}
	}()

	wg.Wait()
}

func (s *workSteal[T, R]) Shutdown() {
	close(s.quit)
	close(s.wakeup)
}
