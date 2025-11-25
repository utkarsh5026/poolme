package pool

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type wsDeque[T, R any] struct {
	// Ring buffer of tasks
	ring atomic.Pointer[[]*submittedTask[T, R]]

	// Mask for fast modulo (capacity - 1)
	mask uint64

	// Head index - modified by stealers (other workers)
	// Padded to prevent false sharing
	_    [cacheLinePadding]byte
	head atomic.Int64
	_    [cacheLinePadding - 8]byte

	// Tail index - modified only by owner worker
	// No padding needed as only owner touches it
	tail int64

	// Capacity (power of 2)
	capacity uint64
}

func newWSDeque[T, R any](capacity int) *wsDeque[T, R] {
	if capacity <= 0 {
		capacity = defaultInitialCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]*submittedTask[T, R], capacity)

	dq := &wsDeque[T, R]{
		mask:     uint64(capacity - 1),
		capacity: uint64(capacity),
	}

	dq.ring.Store(&ring)
	return dq
}

func (w *wsDeque[T, R]) PushBack(t *submittedTask[T, R]) {
	tail := w.tail
	head := w.head.Load()
	ring := *w.ring.Load()

	if uint64(tail-head) >= w.capacity {
		ring = w.grow(head, tail)
	}

	ring[tail&int64(w.mask)] = t
	atomic.StoreInt64(&w.tail, tail+1)
}

func (w *wsDeque[T, R]) grow(head, tail int64) []*submittedTask[T, R] {
	old := *w.ring.Load()
	oldCap := w.capacity

	newCap := oldCap << 1
	newRing := make([]*submittedTask[T, R], newCap)

	for i := head; i < tail; i++ {
		newRing[i&int64(newCap-1)] = old[i&int64(oldCap-1)]
	}

	w.ring.Store(&newRing)
	w.capacity = newCap
	w.mask = newCap - 1
	return newRing
}

func (w *wsDeque[T, R]) PopBack() *submittedTask[T, R] {
	tail := w.tail - 1
	w.tail = tail

	head := w.head.Load()
	if head > tail {
		w.tail = head
		return nil
	}

	ring := *w.ring.Load()
	t := ring[tail&int64(w.mask)]

	if head == tail {
		if !w.head.CompareAndSwap(head, head+1) {
			t = nil
		}
		w.tail = head + 1
	}

	return t
}

func (w *wsDeque[T, R]) PopFront() *submittedTask[T, R] {
	head := w.head.Load()
	tail := atomic.LoadInt64(&w.tail)

	if head >= tail {
		return nil
	}

	ring := *w.ring.Load()
	t := ring[head&int64(w.mask)]

	if !w.head.CompareAndSwap(head, head+1) {
		return nil
	}

	return t
}

func (w *wsDeque[T, R]) Len() int {
	head := w.head.Load()
	tail := atomic.LoadInt64(&w.tail)
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
	nextWorker, stealSeed     atomic.Uint64 // Round-robin counter for task distribution
	workerCount, maxLocalSize int
	quit                      chan struct{}
}

func newWorkStealingStrategy[T any, R any](maxLocalSize int, workers int) *workSteal[T, R] {
	w := &workSteal[T, R]{
		globalQueue:  newWSDeque[T, R](maxLocalSize),
		maxLocalSize: maxLocalSize,
		quit:         make(chan struct{}),
		workerQueues: make([]*wsDeque[T, R], workers),
		workerCount:  workers,
	}

	for i := range workers {
		w.workerQueues[i] = newWSDeque[T, R](maxLocalSize)
	}

	return w
}

// Submit distributes tasks to worker queues using round-robin for load balancing.
// If a worker's local queue is full, the task goes to the global queue instead.
func (s *workSteal[T, R]) Submit(task *submittedTask[T, R]) error {
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
func (s *workSteal[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
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
func (s *workSteal[T, R]) steal(thiefID int) *submittedTask[T, R] {
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
func (s *workSteal[T, R]) backoff(missCount int) {
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
func (s *workSteal[T, R]) drain(ctx context.Context, localQueue *wsDeque[T, R], executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
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

func (s *workSteal[T, R]) Shutdown() {
	close(s.quit)
}
