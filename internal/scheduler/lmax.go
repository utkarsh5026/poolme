// Package scheduler provides various scheduling strategies for worker pool task distribution.
//
// This file implements the LMAX Disruptor pattern, a high-performance inter-thread
// messaging library designed by LMAX Exchange. The Disruptor achieves exceptional
// throughput by eliminating locks through careful memory layout and atomic operations.
//
// Key Design Principles:
//   - Lock-free coordination using atomic Compare-And-Swap (CAS) operations
//   - Cache-line padding to prevent false sharing between CPU cores
//   - Pre-allocated ring buffer for zero-allocation task submission
//   - Sequence-based coordination instead of traditional locks
//   - Batched consumption for improved throughput
//
// Architecture Overview:
//
//	                ┌─────────────────────────────────┐
//	                │         Ring Buffer             │
//	                │  [slot0][slot1][slot2]...[slotN]│
//	                └─────────────────────────────────┘
//	                       ▲                   │
//	                       │                   ▼
//	┌──────────┐     ┌─────┴─────┐     ┌──────────────┐
//	│ Producer │────▶│   Tail    │     │  Consumers   │
//	│  (CAS)   │     │ Sequence  │     │ (workerSeqs) │
//	└──────────┘     └───────────┘     └──────────────┘
//	                       ▲                   │
//	                       │                   ▼
//	                ┌──────┴──────────────────────┐
//	                │      Gating Sequence        │
//	                │  (prevents ring wrap-around)│
//	                └─────────────────────────────┘
//
// For more details on the LMAX Disruptor pattern, see:
// https://lmax-exchange.github.io/disruptor/
package scheduler

import (
	"context"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/gopool/internal/types"
)

const (
	// initialSequence represents the initial state of sequence counters.
	// Using max uint64 (equivalent to -1 in two's complement) allows the first
	// valid sequence to be 0 after incrementing.
	notStarted = ^uint64(0)

	// lmxConsumerBatchSize determines how many tasks a consumer attempts to claim
	// and process in a single batch. Larger batches improve throughput by reducing
	// CAS contention, but may increase latency for individual tasks.
	lmxConsumerBatchSize = 16

	// defaultDisruptorCapacity is the default ring buffer size when not specified.
	// Set to 16384 (2^14) to handle large workloads efficiently. Must be a power
	// of 2 to enable fast modulo operations via bitwise AND.
	defaultDisruptorCapacity = 16384
)

// lmaxSeq is a cache-line padded sequence counter for lock-free coordination.
//
// In concurrent systems, false sharing occurs when multiple CPU cores access
// different variables that happen to reside on the same cache line. When one
// core modifies its variable, the entire cache line is invalidated on all other
// cores, causing expensive cache coherency traffic.
//
// By padding the sequence value to occupy a full cache line (typically 64 bytes),
// we ensure that each sequence counter is isolated, allowing concurrent access
// from multiple goroutines without cache line bouncing.
//
// Memory Layout:
//
//	┌────────────────────────────────────────────────────────────────┐
//	│ [padding: 64 bytes] │ value: 8 bytes │ [padding: 56 bytes]     │
//	└────────────────────────────────────────────────────────────────┘
//	                      ◄─────── isolated cache line ───────►
type lmaxSeq struct {
	_     [cacheLinePadding]byte
	value uint64 // The actual sequence value (8 bytes)
	_     [cacheLinePadding - 8]byte
}

// get atomically loads and returns the current sequence value.
// Uses acquire semantics to ensure visibility of data published before this sequence.
func (s *lmaxSeq) get() uint64 {
	return atomic.LoadUint64(&s.value)
}

// set atomically stores the given value to the sequence.
// Uses release semantics to ensure all prior writes are visible to consumers.
func (s *lmaxSeq) set(val uint64) {
	atomic.StoreUint64(&s.value, val)
}

// cas performs an atomic Compare-And-Swap operation on the sequence.
func (s *lmaxSeq) cas(old, new uint64) bool {
	return atomic.CompareAndSwapUint64(&s.value, old, new)
}

// lmaxSlot represents a single entry in the Disruptor ring buffer.
//
// Each slot contains:
//   - A sequence number that indicates the "version" of data in this slot
//   - A pointer to the submitted task
type lmaxSlot[T any, R any] struct {
	sequence uint64
	task     *types.SubmittedTask[T, R]
	_        [cacheLinePadding - 16]byte // Padding to fill cache line (8 bytes seq + 8 bytes pointer = 16 bytes)
}

// lmaxStrategy implements the LMAX Disruptor scheduling pattern.
//
// The Disruptor is a high-performance, lock-free data structure for inter-thread
// messaging. Originally developed by LMAX Exchange for their financial trading platform,
// it achieves throughput of millions of operations per second with predictable latency.
type lmaxStrategy[T any, R any] struct {
	// ring is the pre-allocated circular buffer holding task slots.
	// Size is always a power of 2 for efficient index calculation.
	ring []lmaxSlot[T, R]

	// mask is (capacity - 1), used for fast modulo: index = seq & mask
	// Example: capacity=16, mask=15 (0b1111), seq=17 → index = 17 & 15 = 1
	mask uint64

	// Sequence Tracking
	// -----------------
	// All sequences are cache-line padded to prevent false sharing.

	// tail tracks the next sequence to be claimed by producers.
	// Producers CAS this value to reserve their slot.
	tail lmaxSeq

	// consumerSeq is the shared claiming sequence for consumers.
	// Workers CAS this to claim batches of work, enabling parallel consumption.
	consumerSeq lmaxSeq

	// gatingSeq tracks the slowest consumer's progress.
	// Producers wait when (tail - gatingSeq) >= capacity to prevent overwrites.
	gatingSeq lmaxSeq

	// workerSeqs tracks each worker's individual progress.
	// Used to calculate gatingSeq (minimum across all workers).
	workerSeqs []lmaxSeq

	// Configuration and lifecycle
	conf     *ProcessorConfig[T, R] // Processor configuration
	capacity int                    // Ring buffer capacity (power of 2)

	quit   *workerSignal       // Shutdown signal
	runner *workerRunner[T, R] // Task execution helper
}

// newLmaxStrategy creates and initializes a new LMAX Disruptor scheduling strategy.
//
// Parameters:
//   - conf: Processor configuration including worker count and execution settings
//   - capacity: Desired ring buffer size (will be rounded up to next power of 2)
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
		// Use intentional wraparound: uint64(i) will be less than uint64(capacity) for valid indices,
		// so the subtraction wraps around correctly in unsigned arithmetic
		ring[i].sequence = uint64(i) - uint64(capacity) // #nosec G115 -- intentional wraparound for ring buffer sequence initialization
	}

	workerSeqs := make([]lmaxSeq, conf.WorkerCount)
	for i := range workerSeqs {
		workerSeqs[i].set(notStarted) // Set to max uint64 (equivalent to -1)
	}

	l := &lmaxStrategy[T, R]{
		ring:       ring,
		mask:       uint64(capacity - 1), // #nosec G115 -- capacity is positive power of 2, capacity-1 is always positive
		workerSeqs: workerSeqs,
		conf:       conf,
		capacity:   capacity,
		quit:       newWorkerSignal(),
	}

	l.tail.set(notStarted)
	l.consumerSeq.set(notStarted)
	l.gatingSeq.set(notStarted)
	l.runner = newWorkerRunner(conf, l)

	return l
}

// Submit adds a single task to the ring buffer for processing.
//
// The submission process follows a two-phase protocol:
// Submit reserves the next slot by CAS on tail,
// blocks if ring is full, then publishes task and sequence.
// Returns ErrSchedulerClosed if shutting down or blocks if full.
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

// waitForConsumer implements producer back-pressure by blocking until it's safe to write.
//
// The ring buffer has fixed capacity. If producers write faster than consumers read,
// eventually a producer would need to write to a slot that still contains unconsumed
// data. This function prevents that by blocking until consumers have advanced.
func (l *lmaxStrategy[T, R]) waitForConsumer(nextSeq uint64) error {
	// #nosec G115 -- capacity is bounded by configuration, safe conversion
	if nextSeq < uint64(l.capacity) {
		return nil
	}

	wrapSlot := nextSeq - uint64(l.capacity) // #nosec G115 -- capacity is bounded by configuration
	spinCount := 0
	const maxSpinBeforeSleep = 1000

	for {
		if l.quit.IsClosed() {
			return context.Canceled
		}

		gating := l.gatingSeq.get()

		// Only proceed when gating is initialized AND we're safe to write.
		// gating represents the minimum slot currently in use by workers.
		if gating != notStarted && wrapSlot < gating {
			return nil
		}

		spinCount++
		if spinCount < maxSpinBeforeSleep {
			runtime.Gosched()
		} else {
			time.Sleep(time.Microsecond * 4)
			spinCount = 0
		}
	}
}

// SubmitBatch submits multiple tasks to the ring buffer sequentially.
//
// Each task is submitted individually via Submit(). This approach:
//   - Maintains FIFO ordering of tasks
//   - Applies back-pressure per-task (may block between submissions)
//   - Returns partial success count if an error occurs mid-batch
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

// Shutdown initiates graceful shutdown of the strategy.
func (l *lmaxStrategy[T, R]) Shutdown() {
	l.quit.Close()
}

// Worker runs the consumer loop for a single worker goroutine.
func (l *lmaxStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	l.workerSeqs[workerID].set(notStarted)
	drainFunc := func(all bool) {
		l.drain(ctx, workerID, executor, h, all)
	}

	for {
		select {
		case <-ctx.Done():
			drainFunc(true)
			return ctx.Err()

		case <-l.quit.Wait():
			drainFunc(false)
			return nil

		default:
			if err := l.consume(ctx, workerID, executor, h); err != nil {
				l.quit.Close()
				return err
			}
		}
	}
}

// drain processes remaining tasks during shutdown or context cancellation.
//
// This method ensures graceful handling of in-flight tasks:
//
// The drain process uses the same batching logic as normal consumption,
// claiming sequences via CAS to coordinate with other draining workers.
func (l *lmaxStrategy[T, R]) drain(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R], processAll bool) {
	for {
		nextSeq, end, ok := l.resolveSeq()
		if !ok {
			if nextSeq == notStarted {
				return
			}
			continue
		}

		l.workerSeqs[workerID].set(nextSeq)
		for seq := nextSeq; seq <= end; seq++ {
			slot := &l.ring[seq&l.mask]
			if atomic.LoadUint64(&slot.sequence) != seq {
				if processAll {
					l.workerSeqs[workerID].set(seq + 1)
					continue
				} else {
					l.workerSeqs[workerID].set(seq)
					l.updateGatingSequence()
					return
				}
			}

			if slot.task != nil {
				_ = l.runner.Execute(ctx, slot.task, executor, h, nil)
			}
			l.workerSeqs[workerID].set(seq + 1)
		}

		l.updateGatingSequence()
		if !processAll {
			return
		}
	}
}

// resolveSeq attempts to claim a batch of sequences for processing.
//
// This method coordinates multiple workers competing to claim work from the ring buffer.
// It uses Compare-And-Swap (CAS) to atomically advance the consumer sequence, ensuring
// each batch is claimed by exactly one worker.
//
// Returns:
//   - start: The first sequence number in the claimed batch
//   - end: The last sequence number in the claimed batch (inclusive)
//   - correct: true if the batch was successfully claimed, false otherwise
//
// Behavior:
//   - Returns (notStarted, 0, false) if no work is available (nothing published yet or all caught up)
//   - Returns (start, end, false) if the CAS failed (another worker claimed this batch)
//   - Returns (start, end, true) on successful claim
//
// The batch size is limited by lmxConsumerBatchSize to balance throughput and latency.
// Larger batches reduce CAS contention but may increase per-task latency.
func (l *lmaxStrategy[T, R]) resolveSeq() (start, end uint64, correct bool) {
	producer := l.tail.get()
	consumer := l.consumerSeq.get()

	if (consumer == notStarted && producer == notStarted) ||
		(consumer != notStarted && consumer >= producer) {
		return notStarted, 0, false
	}

	start = consumer + 1
	end = min(start+uint64(lmxConsumerBatchSize)-1, producer)

	if !l.consumerSeq.cas(consumer, end) {
		return start, end, false
	}

	return start, end, true
}

// consume claims and processes a batch of tasks from the ring buffer.
// This is the core consumption loop implementing batched processing:
func (l *lmaxStrategy[T, R]) consume(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	start, end, ok := l.resolveSeq()
	if !ok {
		runtime.Gosched()
		return nil
	}

	// Set workerSeq BEFORE processing to prevent gating from advancing
	// past slots we're about to read. This tells the producer "I'm using slot 'start'".
	l.workerSeqs[workerID].set(start)

	// Update gating immediately so producer can make progress.
	// Without this, producer would wait forever if it wraps before any task completes.
	l.updateGatingSequence()

	for i := start; i <= end; i++ {
		if err := l.waitForProducer(ctx, i); err != nil {
			return err
		}

		slot := l.ring[i&l.mask]
		if err := l.runner.Execute(ctx, slot.task, executor, h, nil); err != nil {
			return err
		}

		// Set to i+1 to indicate "I've finished slot i, now at slot i+1"
		// This allows producer to overwrite slot i immediately after we're done
		l.workerSeqs[workerID].set(i + 1)
	}

	l.updateGatingSequence()
	return nil
}

// waitForProducer spins until the producer publishes data to the claimed slot.
//
// In the Disruptor pattern, consumers may claim a sequence before the producer
// has finished writing to it. This method bridges that gap by waiting for the
// producer to signal completion via the slot's sequence number.
func (l *lmaxStrategy[T, R]) waitForProducer(ctx context.Context, seq uint64) error {
	slot := &l.ring[seq&l.mask]

	for {
		if atomic.LoadUint64(&slot.sequence) == seq {
			return nil
		}

		if l.quit.IsClosed() {
			return ErrSchedulerClosed
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		runtime.Gosched()
	}
}

// updateGatingSequence recalculates and updates the gating sequence.
//
// The gating sequence controls back-pressure: producers cannot write to slots where
// sequence > gatingSeq + capacity (would overwrite unconsumed data).
//
// Algorithm:
// 1. Find the minimum workerSeq among workers that have actually processed tasks
// 2. If no workers have processed anything yet, do NOT update gating
//
// Workers at notStarted are SKIPPED because they haven't consumed any slots yet.
// We do NOT use consumerSeq as a fallback because it only tracks what's CLAIMED,
// not what's actually PROCESSED. Using claimed sequences would be unsafe.
func (l *lmaxStrategy[T, R]) updateGatingSequence() {
	if len(l.workerSeqs) == 0 {
		return
	}

	var minSeq = notStarted
	foundActiveWorker := false

	for i := 0; i < len(l.workerSeqs); i++ {
		seq := l.workerSeqs[i].get()

		if seq == notStarted {
			continue
		}

		if !foundActiveWorker || seq < minSeq {
			minSeq = seq
			foundActiveWorker = true
		}
	}

	// If no workers have processed anything, don't update gating.
	// The producer will wait until at least one worker makes progress.
	if !foundActiveWorker {
		return
	}

	for {
		old := l.gatingSeq.get()
		if old == notStarted || minSeq > old {
			if l.gatingSeq.cas(old, minSeq) {
				return
			}
		} else {
			return
		}
	}
}
