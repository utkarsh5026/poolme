package pool

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
)

var (
	ErrQueueFull   = errors.New("queue is full")
	ErrQueueClosed = errors.New("queue is closed")
)

const (
	// Cache line size for padding to prevent false sharing
	cacheLinePadding = 128
	// Default initial capacity for unbounded queue (large enough for most use cases)
	defaultInitialCapacity = 65536
	// Maximum spin attempts before yielding
	maxSpinAttempts = 100
)

// mpmcQueueSlot represents a single slot in the ring buffer
type mpmcQueueSlot[T any] struct {
	// Sequence number for synchronization
	sequence uint64
	// The actual data
	value T
	// Padding to prevent false sharing between slots
	_ [cacheLinePadding - 16]byte
}

// mpmcQueue is a lock-free multi-producer multi-consumer queue
type mpmcQueue[T any] struct {
	ring []mpmcQueueSlot[T]
	// Capacity mask (capacity - 1) for fast modulo
	mask uint64

	// Head and tail positions with padding to prevent false sharing
	_    [cacheLinePadding]byte
	head uint64
	_    [cacheLinePadding - 8]byte
	tail uint64
	_    [cacheLinePadding - 8]byte

	// Closed flag
	closed atomic.Bool

	// Configuration
	bounded  bool
	capacity int
}

// MPMCQueueOption is a function that configures the MPMC queue
type MPMCQueueOption func(*mpmcQueue[any])

// newMPMCQueue creates a new MPMC queue with the given capacity
// If capacity is 0, creates an unbounded queue with default initial capacity
func newMPMCQueue[T any](capacity int, bounded bool) *mpmcQueue[T] {
	if capacity <= 0 {
		capacity = defaultInitialCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]mpmcQueueSlot[T], capacity)

	for i := range ring {
		ring[i].sequence = uint64(i)
	}

	return &mpmcQueue[T]{
		ring:     ring,
		mask:     uint64(capacity - 1),
		bounded:  bounded,
		capacity: capacity,
	}
}

// Enqueue adds an item to the queue
// Returns ErrQueueClosed if queue is closed
// Returns ErrQueueFull if queue is bounded and full (only in bounded mode)
// Blocks if context allows until space is available
func (q *mpmcQueue[T]) Enqueue(quit <-chan struct{}, value T) error {
	if q.closed.Load() {
		return ErrQueueClosed
	}
	spinCount := 0

	for {
		select {
		case <-quit:
			return nil
		default:
		}

		_, tail, slot, diff := q.load(false)
		if diff == 0 {
			if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
				slot.value = value
				atomic.StoreUint64(&slot.sequence, tail+1)
				return nil
			}
			continue
		}

		if diff < 0 && q.bounded {
			return ErrQueueFull
		}

		spinCount++
		if spinCount > maxSpinAttempts {
			runtime.Gosched()
			spinCount = 0
		}
	}
}

// Dequeue removes and returns an item from the queue
// Returns ErrQueueClosed if queue is closed and empty
// Blocks if context allows until item is available
func (q *mpmcQueue[T]) Dequeue(ctx context.Context) (T, error) {
	var zero T
	spinCount := 0

	for {
		if q.isClosed() {
			return zero, ErrQueueClosed
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		head, _, slot, diff := q.load(true)
		if diff == 0 {
			if val, ok := q.deque(head, slot); ok {
				return val, nil
			}
			continue
		}

		spinCount++
		if spinCount > maxSpinAttempts {
			runtime.Gosched()
			spinCount = 0
		}
	}
}

// TryDequeue attempts to dequeue an item without blocking
// Returns (value, true) if successful, (zero, false) if queue is empty
func (q *mpmcQueue[T]) TryDequeue() (T, bool) {
	var zero T

	if q.isClosed() {
		return zero, false
	}

	head, _, slot, diff := q.load(true)
	if diff == 0 {
		return q.deque(head, slot)
	}

	return zero, false
}

func (q *mpmcQueue[T]) deque(head uint64, slot *mpmcQueueSlot[T]) (T, bool) {
	var zero T
	if atomic.CompareAndSwapUint64(&q.head, head, head+1) {
		value := slot.value
		slot.value = zero
		// Release the slot to producers
		// if head is N, next sequence should be N + capacity
		atomic.StoreUint64(&slot.sequence, head+q.mask+1)
		return value, true
	}
	return zero, false
}

// isClosed checks if the queue is closed and empty
func (q *mpmcQueue[T]) isClosed() bool {
	if q.closed.Load() {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)
		if head >= tail {
			return true
		}
	}
	return false
}

// load atomically loads head and tail positions and the corresponding slot
// Also computes the difference between slot sequence and expected sequence
func (q *mpmcQueue[T]) load(ishead bool) (head uint64, tail uint64, slot *mpmcQueueSlot[T], diff int64) {
	head = atomic.LoadUint64(&q.head)
	tail = atomic.LoadUint64(&q.tail)

	pos := tail
	if ishead {
		pos = head
	}

	index := pos & q.mask
	slot = &q.ring[index]
	seq := atomic.LoadUint64(&slot.sequence)

	if ishead {
		diff = int64(seq) - int64(head+1)
	} else {
		diff = int64(seq) - int64(tail)
	}

	return
}

// Len returns the approximate number of items in the queue
// This is an approximation due to concurrent operations
func (q *mpmcQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail > head {
		return int(tail - head)
	}
	return 0
}

// Cap returns the capacity of the queue
func (q *mpmcQueue[T]) Cap() int {
	return q.capacity
}

// IsBounded returns whether the queue is bounded
func (q *mpmcQueue[T]) IsBounded() bool {
	return q.bounded
}

// Close marks the queue as closed
// No new items can be enqueued after close
func (q *mpmcQueue[T]) Close() {
	q.closed.Store(true)
}

// IsClosed returns whether the queue is closed
func (q *mpmcQueue[T]) IsClosed() bool {
	return q.closed.Load()
}

// mpmc implements a lock-free multi-producer multi-consumer queue strategy.
//
// This strategy is optimized for high-throughput scenarios where many goroutines
// submit tasks concurrently to the pool.
type mpmc[T any, R any] struct {
	queue *mpmcQueue[*submittedTask[T, R]]
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
func (s *mpmc[T, R]) Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
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
func (s *mpmc[T, R]) drainQueue(ctx context.Context, executor ProcessFunc[T, R], pool *WorkerPool[T, R]) {
	for {
		task, ok := s.queue.TryDequeue()
		if !ok {
			return
		}
		executeSubmitted(ctx, task, pool, executor)
	}
}

// Shutdown gracefully stops the workers and closes the queue
func (s *mpmc[T, R]) Shutdown() {
	s.queue.Close()
	close(s.quit)
}
