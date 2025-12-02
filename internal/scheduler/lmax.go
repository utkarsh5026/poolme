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

	quitter *quitter
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
		quitter:    newQuitter(),
	}

	// Set to max uint64 (equivalent to -1)
	l.tail.set(initialSequence)
	l.consumerSeq.set(initialSequence)
	l.gatingSeq.set(initialSequence)

	return l
}

func (l *lmaxStrategy[T, R]) Submit(t *types.SubmittedTask[T, R]) error {
	if l.quitter.IsClosed() {
		return context.Canceled
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
		if l.quitter.IsClosed() {
			return context.Canceled
		}

		if wrapSlot <= l.gatingSeq.get() {
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
	l.quitter.Close()
}

func (l *lmaxStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	l.workerSeqs[workerID].set(initialSequence)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-l.quitter.Closed():
			return nil

		default:
			if err := l.consume(ctx, workerID, executor, h); err != nil {
				return err
			}
		}
	}
}

func (l *lmaxStrategy[T, R]) consume(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	producer := l.tail.get()
	consmer := l.consumerSeq.get()

	if consmer >= producer {
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
		if err := l.waitForProducer(seq); err != nil {
			return err
		}

		task := l.ring[seq&l.mask].task
		if err := handleWithCare(ctx, task, l.conf, executor, h, nil); err != nil {
			return err
		}

		l.workerSeqs[workerID].set(seq)
	}

	l.updateGatingSequence()
	return nil
}

func (l *lmaxStrategy[T, R]) waitForProducer(seq uint64) error {
	slot := l.getSlot(seq)
	spins := 0

	for {
		if l.quitter.IsClosed() {
			return ErrSchedulerClosed
		}

		if atomic.LoadUint64(&slot.sequence) == seq {
			return nil
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

	minSeq := l.workerSeqs[0].get()
	for i := 1; i < len(l.workerSeqs); i++ {
		minSeq = min(minSeq, l.workerSeqs[i].get())
	}

	for {
		old := l.gatingSeq.get()
		if minSeq <= old || l.gatingSeq.cas(old, minSeq) {
			break
		}
	}
}

func (l *lmaxStrategy[T, R]) getSlot(seq uint64) *lmaxSlot[T, R] {
	idx := seq & l.mask
	return &l.ring[idx]
}
