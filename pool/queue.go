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
	maxSpinAttempts = 10
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

	// Notification channel for data (BUFFERED, NEVER CLOSED)
	notifyC chan struct{}

	// Notification channel for shutdown (UNBUFFERED, CLOSED ON SHUTDOWN)
	closeC chan struct{}

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
		ring[i].sequence = uint64(i) // #nosec G115 -- i is loop index within valid ring bounds
	}

	return &mpmcQueue[T]{
		ring:     ring,
		mask:     uint64(capacity - 1), // #nosec G115 -- capacity is validated positive, no overflow possible
		bounded:  bounded,
		capacity: capacity,
		notifyC:  make(chan struct{}, 1),
		closeC:   make(chan struct{}),
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
				select {
				case q.notifyC <- struct{}{}:
				default:
				}
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

		head, _, slot, diff := q.load(true)
		if diff == 0 {
			if val, ok := q.deque(head, slot); ok {
				return val, nil
			}
			continue
		}

		spinCount++
		if spinCount < maxSpinAttempts {
			runtime.Gosched()
			continue
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-q.closeC:
			return zero, ErrQueueClosed
		case <-q.notifyC:
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
		diff = int64(seq) - int64(head+1) // #nosec G115 -- intentional conversion for sequence comparison
	} else {
		diff = int64(seq) - int64(tail) // #nosec G115 -- intentional conversion for sequence comparison
	}

	return
}

// Len returns the approximate number of items in the queue
// This is an approximation due to concurrent operations
func (q *mpmcQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail > head {
		return int(tail - head) // #nosec G115 -- safe conversion, tail > head guarantees result fits in int
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
	if q.closed.CompareAndSwap(false, true) {
		close(q.closeC)
	}
}

// IsClosed returns whether the queue is closed
func (q *mpmcQueue[T]) IsClosed() bool {
	return q.closed.Load()
}

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
	ring atomic.Pointer[[]*submittedTask[T, R]]

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
	ring := make([]*submittedTask[T, R], capacity)

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
func (w *wsDeque[T, R]) PushBack(t *submittedTask[T, R]) {
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
func (w *wsDeque[T, R]) grow(head, tail int64) []*submittedTask[T, R] {
	old := *w.ring.Load()
	oldCap := w.capacity

	newCap := oldCap << 1
	newRing := make([]*submittedTask[T, R], newCap)

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
func (w *wsDeque[T, R]) PopBack() *submittedTask[T, R] {
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
func (w *wsDeque[T, R]) PopFront() *submittedTask[T, R] {
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
