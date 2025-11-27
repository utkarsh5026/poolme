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

type bitmaskStrategy[T, R any] struct {
	// The bitmask: 1 = Idle, 0 = Busy.
	// We use atomic CAS to reserve a worker (flipping 1 -> 0).
	idleMask atomic.Uint64

	// Per-worker channels to receive tasks
	workerChans []chan *types.SubmittedTask[T, R]

	// Fallback queue for when ALL workers are busy.
	// We use a simple channel for overflow to prevent blocking Submit() completely.
	globalQueue chan *types.SubmittedTask[T, R]

	config *ProcessorConfig[T, R]
	quit   chan struct{}
}

func newBitmaskStrategy[T, R any](conf *ProcessorConfig[T, R]) *bitmaskStrategy[T, R] {
	n := min(conf.WorkerCount, maxWorkersBitmask)

	queues := make([]chan *types.SubmittedTask[T, R], n)
	for i := range n {
		queues[i] = make(chan *types.SubmittedTask[T, R], 1)
	}

	return &bitmaskStrategy[T, R]{
		workerChans: queues,
		// Large buffer for overflow to handle bursts when everyone is busy
		globalQueue: make(chan *types.SubmittedTask[T, R], conf.TaskBuffer),
		config:      conf,
		quit:        make(chan struct{}),
	}
}

func (s *bitmaskStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	for range maxWorkerFindRetries {
		mask := s.idleMask.Load()

		if mask == 0 {
			// All workers busy, enqueue to global queue
			break
		}

		workerID := bits.TrailingZeros64(mask)
		newMask := mask &^ (1 << workerID) // Flip bit to 0 (busy)

		if s.idleMask.CompareAndSwap(mask, newMask) {
			// Successfully reserved worker
			s.workerChans[workerID] <- task
			return nil
		}
	}

	select {
	case s.globalQueue <- task:
		return nil
	case <-s.quit:
		return ErrSchedulerClosed
	}
}

func (s *bitmaskStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	count := 0
	for _, task := range tasks {
		err := s.Submit(task)
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (b *bitmaskStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	myChan := b.workerChans[workerID]
	myBit := uint64(1) << workerID

	for {
		b.announceIdle(myBit)

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-b.quit:
			return nil

		case task := <-myChan:
			if err := b.execute(ctx, task, myBit, executor, h); err != nil {
				return err
			}

		case task := <-b.globalQueue:
			b.markBusy(myBit)
			if err := b.execute(ctx, task, myBit, executor, h); err != nil {
				return err
			}
		}
	}
}

func (b *bitmaskStrategy[T, R]) execute(ctx context.Context, task *types.SubmittedTask[T, R], myBit uint64, executor types.ProcessFunc[T, R], h types.ResultHandler[T, R]) error {
	err := executeSubmitted(ctx, task, b.config, executor, h)
	if err := handleExecutionError(err, b.config.ContinueOnErr, func() {
		b.announceIdle(myBit)
	}); err != nil {
		return err
	}
	return nil
}

func (b *bitmaskStrategy[T, R]) announceIdle(myBit uint64) {
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

func (s *bitmaskStrategy[T, R]) Shutdown() {
	close(s.quit)
}
