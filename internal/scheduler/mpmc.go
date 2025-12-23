// Package scheduler provides various scheduling strategies for worker pool task distribution.
//
// This file implements a lock-free Multi-Producer Multi-Consumer (MPMC) queue,
// a fundamental concurrent data structure that allows multiple goroutines to
// safely enqueue and dequeue items without traditional mutex locks.
//
// # MPMC Queue Design
//
// The queue uses a ring buffer with sequence-based coordination:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                      Ring Buffer                            │
//	│  ┌──────┬──────┬──────┬──────┬──────┬──────┬──────┬──────┐  │
//	│  │ S:0  │ S:1  │ S:2  │ S:3  │ S:4  │ S:5  │ S:6  │ S:7  │  │
//	│  │ V:__ │ V:__ │ V:A  │ V:B  │ V:C  │ V:__ │ V:__ │ V:__ │  │
//	│  └──────┴──────┴──────┴──────┴──────┴──────┴──────┴──────┘  │
//	│              ▲                       ▲                      │
//	│              │                       │                      │
//	│            head=2                  tail=5                   │
//	│         (consumers)             (producers)                 │
//	└─────────────────────────────────────────────────────────────┘
//
// Each slot contains:
//   - sequence: Coordination number that advances as slots are used
//   - value: The actual data being stored
//
// # Lock-Free Coordination
//
// The queue achieves lock-freedom through atomic Compare-And-Swap (CAS) operations:
//
//	Producer:                          Consumer:
//	1. Load tail                       1. Load head
//	2. Check slot.sequence == tail     2. Check slot.sequence == head+1
//	3. CAS tail → tail+1               3. CAS head → head+1
//	4. Write value                     4. Read value
//	5. Store slot.sequence = tail+1    5. Store slot.sequence = head+mask+1
//
// # Bounded vs Unbounded Mode
//
// The queue supports two modes:
//   - Bounded: Returns ErrQueueFull when capacity is reached (back-pressure)
//   - Unbounded: Spins/yields when full, waiting for space (higher latency)
package scheduler

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

// Sentinel errors for MPMC queue operations.
// These errors indicate terminal states that callers should handle.
var (
	ErrQueueFull   = errors.New("queue is full")
	ErrQueueClosed = errors.New("queue is closed")
)

const (
	// cacheLinePadding is the size in bytes used to pad struct fields to prevent
	// false sharing between CPU cores. Modern x86-64 CPUs typically have 64-byte
	// cache lines, but we use 128 bytes to account for prefetching and ensure
	// isolation on various architectures (including Apple Silicon with 128-byte lines).
	cacheLinePadding = 128

	// defaultInitialCapacity is the default ring buffer size when capacity is not
	// specified or is zero. Set to 65536 (2^16) to handle high-throughput scenarios
	// while maintaining reasonable memory usage (~1MB for pointer-sized elements).
	defaultInitialCapacity = 65536

	// maxSpinAttempts defines how many times to spin-check before yielding the
	// processor. Spinning is faster for short waits (avoids context switch overhead)
	// but wastes CPU for longer waits. This value balances latency vs CPU efficiency.
	maxSpinAttempts = 10
)

// mpmcQueueSlot represents a single entry in the MPMC ring buffer.
//
// Each slot contains a sequence number used for lock-free coordination between
// producers and consumers. The sequence serves as a "version" that indicates
// the slot's current state:
type mpmcQueueSlot[T any] struct {
	sequence uint64
	value    T
	_        [cacheLinePadding - 16]byte
}

// mpmcQueue is a lock-free bounded Multi-Producer Multi-Consumer queue.
//
// This queue provides thread-safe concurrent access for multiple producers and
// consumers without using traditional mutex locks. Instead, it relies on atomic
// Compare-And-Swap (CAS) operations and careful memory ordering.
//
// # Performance Characteristics
//
//	┌────────────────────┬─────────────────────────────────────────────┐
//	│ Operation          │ Complexity                                  │
//	├────────────────────┼─────────────────────────────────────────────┤
//	│ Enqueue            │ O(1) amortized, O(n) worst case under       │
//	│                    │ contention (n = concurrent producers)       │
//	├────────────────────┼─────────────────────────────────────────────┤
//	│ Dequeue            │ O(1) amortized, O(n) worst case under       │
//	│                    │ contention (n = concurrent consumers)       │
//	├────────────────────┼─────────────────────────────────────────────┤
//	│ Len (approximate)  │ O(1)                                        │
//	├────────────────────┼─────────────────────────────────────────────┤
//	│ Memory             │ O(capacity) - pre-allocated ring buffer     │
//	└────────────────────┴─────────────────────────────────────────────┘
type mpmcQueue[T any] struct {
	// ring is the pre-allocated circular buffer holding queue slots.
	// Size is always a power of 2 for efficient modulo via bitwise AND.
	ring []mpmcQueueSlot[T]

	// mask equals (capacity - 1) for fast index calculation: index = pos & mask
	// Example: capacity=16, mask=15, pos=17 → index = 17 & 15 = 1
	mask uint64

	// Head and tail positions are padded to prevent false sharing.
	// Each occupies its own cache line to avoid contention between
	// producers (writing tail) and consumers (writing head).

	_    [cacheLinePadding]byte     // Padding before head
	head uint64                     // Next position to dequeue from (consumer side)
	_    [cacheLinePadding - 8]byte // Padding between head and tail
	tail uint64                     // Next position to enqueue to (producer side)
	_    [cacheLinePadding - 8]byte // Padding after tail

	// notifyC signals consumers when new data is available.
	// IMPORTANT: This channel is BUFFERED (size 1) and NEVER CLOSED.
	// Producers send non-blocking; consumers block when queue appears empty.
	notifyC chan signal

	// close signals shutdown to all waiters.
	// IMPORTANT: This uses workerSignal which can be safely closed once.
	close *workerSignal

	bounded  bool // If true, Enqueue returns ErrQueueFull when capacity reached
	capacity int  // Ring buffer size (always power of 2)
}

// newMPMCQueue creates and initializes a new MPMC queue.
func newMPMCQueue[T any](capacity int, bounded bool) *mpmcQueue[T] {
	if capacity <= 0 {
		capacity = defaultInitialCapacity
	}

	// Round to power of 2 for fast modulo via bitwise AND
	capacity = nextPowerOfTwo(capacity)
	ring := make([]mpmcQueueSlot[T], capacity)

	// Initialize sequences: slot[i].sequence = i means "empty, ready for producer"
	for i := range ring {
		ring[i].sequence = uint64(i) // #nosec G115 -- i is loop index within valid ring bounds
	}

	return &mpmcQueue[T]{
		ring:     ring,
		mask:     uint64(capacity - 1), // #nosec G115 -- capacity is validated positive, no overflow possible
		bounded:  bounded,
		capacity: capacity,
		notifyC:  make(chan signal, 1),
		close:    newWorkerSignal(),
	}
}

// Enqueue adds an item to the queue using lock-free CAS operations.
func (q *mpmcQueue[T]) Enqueue(task T) error {
	spinCount := 0

	if q.isClosed() {
		return ErrQueueClosed
	}

	for {
		_, tail, slot, diff := q.load(false)

		if diff == 0 {
			// Slot is ready - attempt to claim it atomically
			if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
				slot.value = task
				atomic.StoreUint64(&slot.sequence, tail+1)

				select {
				case q.notifyC <- struct{}{}:
				default:
				}
				return nil
			}
			continue
		}

		// diff < 0 means slot hasn't been consumed yet (queue full)
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

// EnqueueBatch adds multiple items to the queue using a single atomic reservation.
// This significantly reduces CAS contention for high-throughput batch submissions.
func (q *mpmcQueue[T]) EnqueueBatch(tasks []T) (int, error) {
	if q.isClosed() {
		return 0, ErrQueueClosed
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	count := uint64(len(tasks))
	spinCount := 0

	for {
		tail := atomic.LoadUint64(&q.tail)

		if q.bounded {
			head := atomic.LoadUint64(&q.head)
			if (tail + count - head) > uint64(q.capacity) {
				return 0, ErrQueueFull
			}
		}

		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+count) {

			for i := range count {
				currentSeq := tail + i
				slot := &q.ring[currentSeq&q.mask]

				// Wait for the slot to be ready (sequence == currentSeq)
				// This handles the case where we reserved the slot but the consumer
				// hasn't finished reading the previous value yet.
				// Since we already reserved the tail, we MUST wait.
				waitSpins := 0
				for atomic.LoadUint64(&slot.sequence) != currentSeq {
					waitSpins++
					if waitSpins > maxSpinAttempts {
						runtime.Gosched()
						waitSpins = 0
					}
				}

				slot.value = tasks[i]
				atomic.StoreUint64(&slot.sequence, currentSeq+1)
			}

			// Send multiple notifications to wake up waiting workers.
			// We send notifications equal to the number of tasks (up to a reasonable limit)
			// to ensure multiple workers can wake up and process tasks concurrently.
			notifyCount := min(int(count), 10)
		loop:
			for range notifyCount {
				select {
				case q.notifyC <- signal{}:
				default:
					// Channel full, at least one worker will wake up
					break loop
				}
			}

			return int(count), nil
		}

		spinCount++
		if spinCount > maxSpinAttempts {
			runtime.Gosched()
			spinCount = 0
		}
	}
}

// Dequeue removes and returns an item from the queue.
func (q *mpmcQueue[T]) Dequeue() (T, error) {
	var zero T
	spinCount := 0

	for {
		if q.isClosed() {
			return zero, ErrQueueClosed
		}

		head, _, slot, diff := q.load(true)

		if diff == 0 {
			// Slot has data - attempt to claim it atomically
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
		case <-q.close.Wait():
			return zero, ErrQueueClosed
		case <-q.notifyC:
			spinCount = 0
		}
	}
}

// TryDequeue attempts to dequeue an item without blocking.
//
// This is the non-blocking variant of Dequeue, useful for:
//   - Draining remaining items during shutdown
//   - Polling in select statements
//   - Implementing custom wait strategies
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

// deque performs the actual dequeue operation after slot validation.
func (q *mpmcQueue[T]) deque(head uint64, slot *mpmcQueueSlot[T]) (T, bool) {
	var zero T
	if atomic.CompareAndSwapUint64(&q.head, head, head+1) {
		value := slot.value
		slot.value = zero // Clear to help GC
		atomic.StoreUint64(&slot.sequence, head+q.mask+1)
		return value, true
	}
	return zero, false
}

// isClosed checks if the queue is in a terminal closed state.
//
// The queue is considered "closed" for consumers when BOTH conditions are true:
//  1. The close signal has been triggered (Close() was called)
//  2. The queue is empty (head >= tail)
func (q *mpmcQueue[T]) isClosed() bool {
	if q.close.IsClosed() {
		head := atomic.LoadUint64(&q.head)
		tail := atomic.LoadUint64(&q.tail)
		if head >= tail {
			return true
		}
	}
	return false
}

// load atomically reads queue state and computes slot readiness.
//
// This helper centralizes the common pattern of:
//  1. Loading head and tail positions
//  2. Calculating the target slot index
//  3. Reading the slot's sequence number
//  4. Computing the "diff" that indicates readiness
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
		// Consumer: slot ready when sequence == head + 1 (data published)
		diff = int64(seq) - int64(head+1) // #nosec G115 -- intentional conversion for sequence comparison
	} else {
		// Producer: slot ready when sequence == tail (slot empty)
		diff = int64(seq) - int64(tail) // #nosec G115 -- intentional conversion for sequence comparison
	}

	return
}

// Len returns the approximate number of items currently in the queue.
//
// This is an approximation because head and tail are read non-atomically
// with respect to each other. In a concurrent environment, the actual
// count may differ by the time this method returns.
func (q *mpmcQueue[T]) Len() int {
	head := atomic.LoadUint64(&q.head)
	tail := atomic.LoadUint64(&q.tail)

	if tail > head {
		return int(tail - head) // #nosec G115 -- safe conversion, tail > head guarantees result fits in int
	}
	return 0
}

// Cap returns the capacity of the ring buffer.
//
// This is the maximum number of items that can be stored before:
//   - Bounded mode: Enqueue returns ErrQueueFull
//   - Unbounded mode: Enqueue blocks/spins waiting for space
//
// Note: This is always a power of 2 (may be larger than requested capacity).
func (q *mpmcQueue[T]) Cap() int {
	return q.capacity
}

// IsBounded returns whether the queue operates in bounded mode.
func (q *mpmcQueue[T]) IsBounded() bool {
	return q.bounded
}

// Close marks the queue as closed and signals all waiting consumers.
func (q *mpmcQueue[T]) Close() {
	q.close.Close()
}

// mpmc implements a lock-free Multi-Producer Multi-Consumer scheduling strategy.
//
// This strategy wraps the mpmcQueue to provide the Strategy interface for
// worker pool task distribution. It excels in scenarios with:
//   - High task submission rates from multiple goroutines
//   - Relatively uniform task processing times
//   - Need for predictable latency without lock contention
type mpmc[T any, R any] struct {
	queue   *mpmcQueue[*types.SubmittedTask[T, R]]
	conf    *ProcessorConfig[T, R]
	runner  *workerRunner[T, R]
	quitter *workerSignal // Shutdown coordination
}

// newMPMCStrategy creates a new MPMC-based scheduling strategy.
func newMPMCStrategy[T any, R any](conf *ProcessorConfig[T, R], bounded bool, capacity int) *mpmc[T, R] {
	m := &mpmc[T, R]{
		queue:   newMPMCQueue[*types.SubmittedTask[T, R]](capacity, bounded),
		conf:    conf,
		quitter: newWorkerSignal(),
	}
	m.runner = newWorkerRunner(conf, m)
	return m
}

// Submit enqueues a single task for processing by workers.
//
// The task is added to the MPMC queue where workers will pick it up
// for execution. In bounded mode, returns immediately with ErrQueueFull
// if the queue is at capacity.
func (s *mpmc[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	if s.quitter.IsClosed() {
		return ErrQueueClosed
	}

	return s.queue.Enqueue(task)
}

// SubmitBatch enqueues multiple tasks, returning the count of successful submissions.
//
// Uses the optimized EnqueueBatch for lock-free bulk submission.
func (s *mpmc[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	if s.quitter.IsClosed() {
		return 0, ErrQueueClosed
	}

	n, err := s.queue.EnqueueBatch(tasks)
	if err == nil {
		return n, nil
	}

	if err == ErrQueueFull {
		for i, task := range tasks {
			select {
			case <-s.quitter.Wait():
				return i, ErrQueueClosed
			default:
				if err := s.queue.Enqueue(task); err != nil {
					return i, err
				}
			}
		}
		return len(tasks), nil
	}

	return n, err
}

// Worker runs the main processing loop for a single worker goroutine.
func (s *mpmc[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	drainFunc := func() {
		s.drainQueue(ctx, executor, h)
	}

	for {
		task, err := s.queue.Dequeue()
		if err != nil {
			if err == ErrQueueClosed || err == context.Canceled {
				drainFunc()
				return nil
			}
			if ctx.Err() != nil {
				drainFunc()
				return ctx.Err()
			}
			continue
		}

		if err := s.runner.Execute(ctx, task, executor, h, drainFunc); err != nil {
			return err
		}
	}
}

// drainQueue processes all remaining tasks in the queue during shutdown.
//
// This method uses TryDequeue (non-blocking) to avoid deadlock scenarios
// where multiple workers are draining simultaneously. Tasks are executed
// using ExecuteWithoutCare which ignores errors to ensure all tasks get
// a chance to complete.
func (s *mpmc[T, R]) drainQueue(ctx context.Context, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) {
	for {
		task, ok := s.queue.TryDequeue()
		if !ok {
			return
		}
		s.runner.ExecuteWithoutCare(ctx, task, executor, h)
	}
}

// Shutdown initiates graceful shutdown of the MPMC strategy.
//
// The shutdown sequence:
//  1. Signal workers to stop accepting new work (quitter.Close)
//  2. Close the queue (prevents new enqueues, unblocks waiting dequeues)
//  3. Workers drain remaining tasks and exit
func (s *mpmc[T, R]) Shutdown() {
	s.quitter.Close()
	s.queue.Close()
}
