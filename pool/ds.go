package pool

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
)

var (
	ErrQueueFull   = errors.New("queue is full")
	ErrQueueClosed = errors.New("queue is closed")
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
	pq.queue = append(pq.queue, x.(*submittedTask[T, R]))
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

// dequeue is a double-ended queue optimized for work-stealing algorithms.
// It supports efficient push/pop operations from both ends with proper synchronization.
type dequeue[T any, R any] struct {
	tasks []*submittedTask[T, R]
	mu    sync.Mutex
}

func newDequeue[T any, R any]() *dequeue[T, R] {
	return &dequeue[T, R]{
		tasks: make([]*submittedTask[T, R], 0, 16), // Pre-allocate some capacity
	}
}

// Len returns the current number of tasks in the dequeue.
// Note: This is not thread-safe and should only be used for debugging/monitoring.
func (d *dequeue[T, R]) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.tasks)
}

// PushFront adds a task to the front of the dequeue.
// This is less common but useful for prioritizing certain tasks.
func (d *dequeue[T, R]) PushFront(task *submittedTask[T, R]) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tasks = append([]*submittedTask[T, R]{task}, d.tasks...)
}

// PushBack adds a task to the back of the dequeue.
// This is the primary method for adding work to a worker's local queue.
// Uses LIFO order when combined with PopBack for better cache locality.
func (d *dequeue[T, R]) PushBack(task *submittedTask[T, R]) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tasks = append(d.tasks, task)
}

// PopFront removes and returns a task from the front of the dequeue.
// Returns nil if the dequeue is empty.
//
// This is used by:
//   - Other workers when stealing work (FIFO from victim's perspective)
//   - Global queue consumers
//
// The FIFO pattern for stealing reduces contention with the victim worker
// who is doing LIFO (PopBack) on the same queue.
func (d *dequeue[T, R]) PopFront() *submittedTask[T, R] {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.tasks) == 0 {
		return nil
	}
	task := d.tasks[0]
	d.tasks = d.tasks[1:]
	return task
}

// PopBack removes and returns a task from the back of the dequeue.
// Returns nil if the dequeue is empty.
//
// This is used by the local worker for its own queue (LIFO pattern).
// LIFO provides better cache locality as recently added tasks are likely
// to have related data still in CPU cache.
func (d *dequeue[T, R]) PopBack() *submittedTask[T, R] {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.tasks) == 0 {
		return nil
	}
	task := d.tasks[len(d.tasks)-1]
	d.tasks = d.tasks[:len(d.tasks)-1]
	return task
}

// TryPopFront attempts to pop from the front without blocking.
// Returns (task, true) if successful, (nil, false) if empty or locked.
// This is useful for non-blocking steal attempts.
func (d *dequeue[T, R]) TryPopFront() (*submittedTask[T, R], bool) {
	if !d.mu.TryLock() {
		return nil, false
	}
	defer d.mu.Unlock()

	if len(d.tasks) == 0 {
		return nil, false
	}

	task := d.tasks[0]
	d.tasks = d.tasks[1:]
	return task, true
}

const (
	// Cache line size for padding to prevent false sharing
	cacheLinePadding = 128
	// Default initial capacity for unbounded queue (large enough for most use cases)
	defaultInitialCapacity = 65536
	// Maximum spin attempts before yielding
	maxSpinAttempts = 100
)

// mpmcQueueSlot represents a single slot in the ring ring
type mpmcQueueSlot[T any] struct {
	// Sequence number for synchronization
	sequence uint64
	// The actual data
	value T
	// Padding to prevent false sharing between slots
	_ [cacheLinePadding - 16]byte
}

// MPMCQueue is a lock-free multi-producer multi-consumer queue
type MPMCQueue[T any] struct {
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
type MPMCQueueOption func(*MPMCQueue[any])

// NewMPMCQueue creates a new MPMC queue with the given capacity
// If capacity is 0, creates an unbounded queue with default initial capacity
func NewMPMCQueue[T any](capacity int, bounded bool) *MPMCQueue[T] {
	if capacity <= 0 {
		capacity = defaultInitialCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]mpmcQueueSlot[T], capacity)

	for i := range ring {
		ring[i].sequence = uint64(i)
	}

	return &MPMCQueue[T]{
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
func (q *MPMCQueue[T]) Enqueue(quit <-chan struct{}, value T) error {
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
func (q *MPMCQueue[T]) Dequeue(ctx context.Context) (T, error) {
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
func (q *MPMCQueue[T]) TryDequeue() (T, bool) {
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

func (q *MPMCQueue[T]) deque(head uint64, slot *mpmcQueueSlot[T]) (T, bool) {
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

func (q *MPMCQueue[T]) isClosed() bool {
	if q.closed.Load() {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)
		if head >= tail {
			return true
		}
	}
	return false
}

func (q *MPMCQueue[T]) load(ishead bool) (head uint64, tail uint64, slot *mpmcQueueSlot[T], diff int64) {
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
func (q *MPMCQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail > head {
		return int(tail - head)
	}
	return 0
}

// Cap returns the capacity of the queue
func (q *MPMCQueue[T]) Cap() int {
	return q.capacity
}

// IsBounded returns whether the queue is bounded
func (q *MPMCQueue[T]) IsBounded() bool {
	return q.bounded
}

// Close marks the queue as closed
// No new items can be enqueued after close
func (q *MPMCQueue[T]) Close() {
	q.closed.Store(true)
}

// IsClosed returns whether the queue is closed
func (q *MPMCQueue[T]) IsClosed() bool {
	return q.closed.Load()
}

// nextPowerOfTwo returns the next power of 2 >= n
func nextPowerOfTwo(n int) int {
	if n <= 0 {
		return 1
	}

	if n&(n-1) == 0 {
		return n
	}

	power := 1
	for power < n {
		power *= 2
	}
	return power
}
