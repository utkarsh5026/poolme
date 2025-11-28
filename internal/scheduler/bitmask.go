package scheduler

import (
	"context"
	"math/bits"
	"sync/atomic"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	maxWorkersBitmask    = 64
	maxWorkerFindRetries = 4
)

// bitmaskStrategy is a scheduler strategy that uses a lock-free bitmask for tracking
// worker idleness/busyness. Each bit represents a worker: 1 means idle, 0 means busy.
// Tasks are assigned to idle workers by atomically claiming a bit, otherwise overflowing
// to a global queue if all are busy.
//
// T: task type, R: result type.
type bitmaskStrategy[T, R any] struct {
	// idleMask marks worker status: 1 = idle, 0 = busy.
	// Uses atomic ops to reserve/release worker slots.
	idleMask atomic.Uint64

	// workerChans is a set of per-worker channels to push assigned tasks to.
	workerChans []chan *types.SubmittedTask[T, R]

	// globalQueue holds tasks that couldn't be assigned because all workers were busy.
	// Used for overflow/backpressure, so Submit never blocks unless this is full.
	globalQueue chan *types.SubmittedTask[T, R]

	// config holds user configuration.
	config *ProcessorConfig[T, R]
	// quit is closed to signal shutdown to ongoing task submissions.
	quit chan struct{}
}

// newBitmaskStrategy constructs a bitmaskStrategy using the given config.
// Caps worker count by maxWorkersBitmask, allocates required task channels,
// and initializes all workers to idle (bit = 1) in the mask.
func newBitmaskStrategy[T, R any](conf *ProcessorConfig[T, R]) *bitmaskStrategy[T, R] {
	n := min(conf.WorkerCount, maxWorkersBitmask)

	queues := make([]chan *types.SubmittedTask[T, R], n)
	for i := range n {
		queues[i] = make(chan *types.SubmittedTask[T, R], 1)
	}

	// Set all bits as idle
	// This allows tasks to be submitted directly to worker channels before workers start
	var initialMask uint64
	if n == 64 {
		initialMask = ^uint64(0) // All bits set
	} else {
		initialMask = (1 << n) - 1 // Set n bits for n workers
	}

	globalQueueSize := max(conf.TaskBuffer, conf.WorkerCount*10)
	strategy := &bitmaskStrategy[T, R]{
		workerChans: queues,
		globalQueue: make(chan *types.SubmittedTask[T, R], globalQueueSize),
		config:      conf,
		quit:        make(chan struct{}),
	}
	strategy.idleMask.Store(initialMask)

	return strategy
}

// Submit attempts to find and claim an idle worker for the task,
// atomically flipping the worker's bit to busy. If all workers are busy,
// enqueues the task to the global queue (which may block if full).
// Returns ErrSchedulerClosed if shutting down.
func (s *bitmaskStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	// Check if shutting down first to prioritize cancellation
	select {
	case <-s.quit:
		return ErrSchedulerClosed
	default:
	}

	for range maxWorkerFindRetries {
		mask := s.idleMask.Load()

		if mask == 0 {
			// All workers busy, enqueue to global queue
			break
		}

		workerID := bits.TrailingZeros64(mask)
		newMask := mask &^ (1 << workerID) // Flip bit to 0 (busy)

		if s.idleMask.CompareAndSwap(mask, newMask) {
			select {
			case s.workerChans[workerID] <- task:
				return nil
			case <-s.quit:
				// Shutdown signal received, restore the bit and exit
				s.announceIdle(workerID)
				return ErrSchedulerClosed
			}
		}
	}

	select {
	case s.globalQueue <- task:
		return nil
	case <-s.quit:
		return ErrSchedulerClosed
	}
}

// SubmitBatch submits a batch of tasks for execution synchronously.
// Returns the number of tasks successfully submitted and an error if shutdown was signaled during submission.
// If shutdown occurs mid-batch, returns the count of tasks submitted before shutdown and the error.
func (s *bitmaskStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	submitted := 0
	for _, task := range tasks {
		err := s.Submit(task)
		if err != nil {
			if submitted == 0 {
				return 0, err
			}
			return submitted, err
		}
		submitted++
	}
	return submitted, nil
}

// Worker is the main loop for a worker goroutine. The worker
// repeatedly marks itself idle in the mask, then waits for:
//  1. ctx.Done: Worker should terminate and return context error
//  2. <-b.quit: System-wide shutdown signal; worker exits cleanly
//  3. Task from its channel (high-priority, reserved tasks): executes and releases its bit
//  4. Task from globalQueue (overflow pool): marks busy, executes, and releases bit
//
// Returns when context or quit is signaled, or if process function yields error (depending on config).
func (b *bitmaskStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	myChan := b.workerChans[workerID]
	myBit := uint64(1) << workerID

	drain := func() {
		b.drain(ctx, executor, h, workerID)
	}

	for {
		b.announceIdle(int(workerID))

		select {
		case <-ctx.Done():
			drain()
			return ctx.Err()

		case <-b.quit:
			drain()
			return nil

		case task := <-myChan:
			if err := b.execute(ctx, task, workerID, executor, h); err != nil {
				drain()
				return err
			}

		case task := <-b.globalQueue:
			b.markBusy(myBit)
			if err := b.execute(ctx, task, workerID, executor, h); err != nil {
				drain()
				return err
			}
		}
	}
}

// drain processes any remaining tasks during shutdown.
//
// It drains both the worker's dedicated channel and helps drain the shared global queue.
// Tasks executed with cancelled context will fail fast (via processWithRetry's context check)
// and generate error results, ensuring all submitted tasks produce results for collection.
func (b *bitmaskStrategy[T, R]) drain(
	ctx context.Context,
	executor types.ProcessFunc[T, R],
	h types.ResultHandler[T, R],
	workerID int64,
) {
	myChan := b.workerChans[workerID]
	for {
		select {
		case task, ok := <-myChan:
			if !ok {
				return
			}
			_ = executeSubmitted(ctx, task, b.config, executor, h)
		default:
			// Worker channel empty, now help drain global queue
			// All workers cooperatively drain the global queue during shutdown
			for {
				select {
				case task, ok := <-b.globalQueue:
					if !ok {
						return
					}
					_ = executeSubmitted(ctx, task, b.config, executor, h)
				default:
					return
				}
			}
		}
	}
}

// execute runs the executor on the given task, invokes result handler (h),
// and manages error handling per config. On failure with ContinueOnErr, re-announces idle.
func (b *bitmaskStrategy[T, R]) execute(ctx context.Context, task *types.SubmittedTask[T, R], workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	err := executeSubmitted(ctx, task, b.config, executor, h)
	if err := handleExecutionError(err, b.config.ContinueOnErr, func() {
		b.announceIdle(int(workerID))
	}); err != nil {
		return err
	}
	return nil
}

// announceIdle sets this worker's bit to 1 (idle) in the mask if not already set.
func (b *bitmaskStrategy[T, R]) announceIdle(workerID int) {
	myBit := uint64(1) << workerID
	for {
		oldMask := b.idleMask.Load()
		if oldMask&myBit != 0 {
			break // Already set (shouldn't happen in correct flow, but safe)
		}

		if b.idleMask.CompareAndSwap(oldMask, oldMask|myBit) {
			break
		}
	}
}

// markBusy clears this worker's bit (sets to 0), marking self busy. Idempotent.
func (s *bitmaskStrategy[T, R]) markBusy(myBit uint64) {
	for {
		oldMask := s.idleMask.Load()
		if oldMask&myBit == 0 {
			return // Already marked busy
		}
		if s.idleMask.CompareAndSwap(oldMask, oldMask&^myBit) {
			return
		}
	}
}

// Shutdown gracefully shuts down the bitmask strategy by closing the quit channel.
// This signals ongoing task submissions to stop.
func (s *bitmaskStrategy[T, R]) Shutdown() {
	close(s.quit)
}
