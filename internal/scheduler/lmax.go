package scheduler

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	initialSequence          = ^uint64(0) // -1 in uint64 representation
	maxConsumerWaitSpins     = 100        // Max spins before yielding in waitForConsumer
	lmxConsumerBatchSize     = 16         // Number of tasks a consumer tries to process in one go
	defaultDisruptorCapacity = 1024       // defaultDisruptorCapacity is the default ring buffer capacity if not specified
)

// lmaxSeq is a sequence counter with cache-line padding to prevent false sharing.
// The padding ensures that each sequence counter occupies its own cache line, eliminating
// false sharing between concurrent goroutines accessing different sequences.
type lmaxSeq struct {
	_     [cacheLinePadding]byte     // Padding before value
	value uint64                     // The actual sequence value
	_     [cacheLinePadding - 8]byte // Padding after value (assuming uint64 is 8 bytes)
}

// get atomically loads and returns the current sequence value.
func (s *lmaxSeq) get() uint64 {
	return atomic.LoadUint64(&s.value)
}

// set atomically stores the given value to the sequence.
func (s *lmaxSeq) set(val uint64) {
	atomic.StoreUint64(&s.value, val)
}

// cas atomically compares the sequence value with old and sets it to new if they match.
// Returns true if the swap was successful, false otherwise.
func (s *lmaxSeq) cas(old, new uint64) bool {
	return atomic.CompareAndSwapUint64(&s.value, old, new)
}

// disruptorSlot represents a single slot in the Disruptor ring buffer.
// Each slot stores a task and its sequence number for coordination.
// Cache-line padding prevents false sharing between adjacent slots.
type lmaxSlot[T any, R any] struct {
	sequence uint64 // Sequence number for this slot (used for coordination)
	task     *types.SubmittedTask[T, R]
	_        [cacheLinePadding - 16]byte // Padding (assumes pointer + uint64 = 16 bytes)
}

// disruptor implements the LMAX Disruptor scheduling strategy.
//
// The Disruptor uses a pre-allocated ring buffer with lock-free sequence-based coordination.
// Key features:
// - Pre-allocated ring buffer (power-of-2 size for fast modulo)
// - Per-worker sequence tracking (unlike MPMC's single head counter)
// - Gating sequence to prevent ring wrap-around
// - Atomic CAS operations for lock-free coordination
// - Cache-line padding to prevent false sharing
type lmaxStrategy[T any, R any] struct {
	ring []lmaxSlot[T, R]
	mask uint64 // Capacity - 1, for fast modulo via bitwise AND

	// Sequence tracking (with cache-line padding between them)
	tail        lmaxSeq   // Next sequence to be published by producers
	consumerSeq lmaxSeq   // Shared consumer sequence for claiming (consumers compete via CAS)
	gatingSeq   lmaxSeq   // Slowest consumer's sequence (gates producers)
	workerSeqs  []lmaxSeq // Per-worker consumer sequences (for tracking progress)

	conf     *ProcessorConfig[T, R]
	capacity int

	quit   *workerSignal
	runner *workerRunner[T, R]
}

// newDisruptorStrategy creates a new Disruptor scheduling strategy.
//
// The capacity will be rounded up to the next power of 2 for efficient modulo operations.
// Worker sequences are initialized to -1, and the ring buffer slots are pre-allocated.
func newLmaxStrategy[T any, R any](conf *ProcessorConfig[T, R], capacity int) *lmaxStrategy[T, R] {
	if capacity <= 0 {
		capacity = defaultDisruptorCapacity
	}

	capacity = nextPowerOfTwo(capacity)
	ring := make([]lmaxSlot[T, R], capacity)

	// Initialize each slot's sequence to (index - capacity)
	// This ensures slots are not considered "published" until a producer explicitly publishes them
	// For example, with capacity 16: slot[0].sequence = -16, slot[1].sequence = -15, etc.
	for i := range ring {
		ring[i].sequence = uint64(i - capacity) // #nosec G115 -- intentional wraparound for initial state
	}

	// Create per-worker sequences (initialized to -1 means no slots claimed yet)
	workerSeqs := make([]lmaxSeq, conf.WorkerCount)
	for i := range workerSeqs {
		workerSeqs[i].set(initialSequence) // Set to max uint64 (equivalent to -1)
	}

	l := &lmaxStrategy[T, R]{
		ring:       ring,
		mask:       uint64(capacity - 1),
		workerSeqs: workerSeqs,
		conf:       conf,
		capacity:   capacity,
		quit:       newWorkerSignal(),
	}

	// Set to max uint64 (equivalent to -1)
	l.tail.set(initialSequence)
	l.consumerSeq.set(initialSequence)
	l.gatingSeq.set(initialSequence)
	l.runner = newWorkerRunner(conf, l)

	return l
}

func (l *lmaxStrategy[T, R]) Submit(t *types.SubmittedTask[T, R]) error {
	if l.quit.IsClosed() {
		return ErrSchedulerClosed
	}

	var nextSeq uint64
	for {
		current := l.tail.get()
		nextSeq = current + 1

		if err := l.waitForConsumer(nextSeq); err != nil {
			return err
		}

		if l.tail.cas(current, nextSeq) {
			break
		}
	}
	idx := nextSeq & l.mask
	l.ring[idx].task = t
	atomic.StoreUint64(&l.ring[idx].sequence, nextSeq)
	return nil
}

func (l *lmaxStrategy[T, R]) waitForConsumer(nextSeq uint64) error {
	wrapSlot := nextSeq - uint64(l.capacity) // x - capacity
	spinns := 0
	for {
		if l.quit.IsClosed() {
			return context.Canceled
		}

		gating := l.gatingSeq.get()
		// Special case for initial state: if no consumer has started (gatingSeq == initialSequence),
		// only allow submitting the first `capacity` tasks to prevent wrapping before consumers start
		if gating == initialSequence {
			if nextSeq < uint64(l.capacity) {
				return nil
			}
			// Block if trying to wrap around before any consumer has processed tasks
			spinns++
			if spinns > maxConsumerWaitSpins {
				spinns = 0
				runtime.Gosched()
			}
			continue
		}

		// If we're still in the first lap (nextSeq < capacity), always allow
		// since wrapSlot would wrap to a huge number in unsigned arithmetic
		if nextSeq < uint64(l.capacity) {
			return nil
		}

		if wrapSlot <= gating {
			return nil
		}
		spinns++
		if spinns > maxConsumerWaitSpins {
			spinns = 0
			runtime.Gosched()
		}
	}
}

func (l *lmaxStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	n := 0
	for _, t := range tasks {
		if err := l.Submit(t); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (l *lmaxStrategy[T, R]) Shutdown() {
	l.quit.Close()
}

func (l *lmaxStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	l.workerSeqs[workerID].set(initialSequence)
	for {
		select {
		case <-ctx.Done():
			l.drain(ctx, workerID, executor, h, true)
			return ctx.Err()

		case <-l.quit.Wait():
			l.drain(ctx, workerID, executor, h, false)
			return nil

		default:
			if err := l.consume(ctx, workerID, executor, h); err != nil {
				l.quit.Close()
				return err
			}
		}
	}
}

func (l *lmaxStrategy[T, R]) drain(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R], processAll bool) {
	// Keep processing tasks until we've caught up with the producer
	// If processAll is false (shutdown scenario), only process immediately available tasks
	for {
		producer := l.tail.get()
		consmer := l.consumerSeq.get()

		// No more tasks to drain
		if (consmer == initialSequence && producer == initialSequence) ||
			(consmer != initialSequence && consmer >= producer) {
			debugLog("drain complete: consmer=%d, producer=%d", consmer, producer)
			return
		}

		// Try to claim a batch
		nextSeq := consmer + 1
		potentialEnd := min(nextSeq+uint64(lmxConsumerBatchSize)-1, producer)

		if !l.consumerSeq.cas(consmer, potentialEnd) {
			runtime.Gosched()
			continue
		}

		// Process claimed tasks
		for seq := nextSeq; seq <= potentialEnd; seq++ {
			slot := l.getSlot(seq)
			// Check if task is already published
			if atomic.LoadUint64(&slot.sequence) != seq {
				// Task not published yet
				if processAll {
					// Context cancelled - skip unpublished tasks
					debugLog("drain skipping unpublished task: seq=%d", seq)
					l.workerSeqs[workerID].set(seq)
					continue
				} else {
					// Shutdown - don't wait for unpublished tasks, exit early
					debugLog("drain exiting early due to unpublished task: seq=%d", seq)
					l.workerSeqs[workerID].set(seq - 1) // Set to last completed
					l.updateGatingSequence()
					return
				}
			}

			task := slot.task
			if task != nil {
				_ = l.runner.Execute(ctx, task, executor, h, nil)
				debugLog("drain processed task: seq=%d", seq)
			}
			l.workerSeqs[workerID].set(seq)
		}

		l.updateGatingSequence()

		// If not processing all tasks (shutdown), exit after one batch
		if !processAll {
			return
		}
	}
}

func (l *lmaxStrategy[T, R]) consume(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	producer := l.tail.get()
	consmer := l.consumerSeq.get()

	if (consmer == initialSequence && producer == initialSequence) ||
		(consmer != initialSequence && consmer >= producer) {
		debugLog("consumer is greedy: consmer=%d, producer=%d", consmer, producer)
		runtime.Gosched()
		return nil
	}

	nextSeq := consmer + 1
	potentialEnd := min(nextSeq+uint64(lmxConsumerBatchSize)-1, producer)

	if !l.consumerSeq.cas(consmer, potentialEnd) {
		runtime.Gosched()
		return nil
	}

	for seq := nextSeq; seq <= potentialEnd; seq++ {
		if err := l.waitForProducer(ctx, seq); err != nil {
			return err
		}

		task := l.ring[seq&l.mask].task
		if err := l.runner.Execute(ctx, task, executor, h, nil); err != nil {
			return err
		}

		l.workerSeqs[workerID].set(seq)
	}

	l.updateGatingSequence()
	return nil
}

func (l *lmaxStrategy[T, R]) waitForProducer(ctx context.Context, seq uint64) error {
	slot := l.getSlot(seq)
	spins := 0

	for {
		// Check if task is already available first
		if atomic.LoadUint64(&slot.sequence) == seq {
			return nil
		}

		// If shutting down and task is not ready yet, don't wait for it
		// This prevents workers from waiting indefinitely for tasks that may never arrive
		if l.quit.IsClosed() {
			return ErrSchedulerClosed
		}

		// Only check context cancellation if we need to wait
		// This allows processing already-published tasks even if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		spins++
		if spins > maxConsumerWaitSpins {
			spins = 0
			runtime.Gosched()
		}
	}
}

func (l *lmaxStrategy[T, R]) updateGatingSequence() {
	if len(l.workerSeqs) == 0 {
		return
	}

	// Find minimum sequence among workers that have claimed sequences
	// Skip workers still at initialSequence to avoid blocking progress
	minSeq := uint64(0)
	initialized := false

	for i := 0; i < len(l.workerSeqs); i++ {
		seq := l.workerSeqs[i].get()
		if seq == initialSequence {
			continue // Skip workers that haven't claimed any sequences yet
		}
		if !initialized {
			minSeq = seq
			initialized = true
		} else {
			minSeq = min(minSeq, seq)
		}
	}

	// If no worker has claimed anything yet, keep gating at initialSequence
	if !initialized {
		return
	}

	for {
		old := l.gatingSeq.get()
		// Special case: if gatingSeq is at initial state, always try to update it.
		// Otherwise, only update if minSeq has advanced beyond old.
		if old == initialSequence || minSeq > old {
			if l.gatingSeq.cas(old, minSeq) {
				break
			}
		} else {
			// minSeq <= old and old != initialSequence, gating is already ahead
			break
		}
	}
}

func (l *lmaxStrategy[T, R]) getSlot(seq uint64) *lmaxSlot[T, R] {
	idx := seq & l.mask
	return &l.ring[idx]
}
