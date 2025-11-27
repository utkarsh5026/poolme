package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

const (
	defaultFusionWindow    = 100 * time.Microsecond
	defaultFusionBatchSize = 32
)

// fusionStrategy wraps an underlying scheduling strategy and batches tasks
// that arrive within a configurable time window.
type fusionStrategy[T any, R any] struct {
	underlying SchedulingStrategy[T, R] // The wrapped strategy

	mu    sync.Mutex
	batch []*types.SubmittedTask[T, R] // Accumulated tasks

	fusionWindow time.Duration // Time to wait for more tasks
	maxBatchSize int           // Max batch before forcing flush

	timer       *time.Timer // Flush timer
	timerActive bool        // Is timer running?

	quit chan struct{} // Shutdown signal
}

// newFusionStrategy creates a new fusionStrategy that wraps an underlying scheduling strategy.
// It batches incoming tasks that arrive within a certain time window (fusionWindow), or until
// the batch reaches maxBatchSize, before forwarding them to the underlying strategy.
// If conf.FusionWindow or conf.FusionBatchSize are not set, defaults are used.
func newFusionStrategy[T any, R any](conf *ProcessorConfig[T, R], underlying SchedulingStrategy[T, R]) *fusionStrategy[T, R] {
	window := conf.FusionWindow
	if window == 0 {
		window = defaultFusionWindow
	}

	batchSize := conf.FusionBatchSize
	if batchSize == 0 {
		batchSize = defaultFusionBatchSize // Default batch size
	}

	return &fusionStrategy[T, R]{
		underlying:   underlying,
		batch:        make([]*types.SubmittedTask[T, R], 0, batchSize),
		fusionWindow: window,
		maxBatchSize: batchSize,
		quit:         make(chan struct{}),
	}
}

// Submit enqueues a single task into the fusion batch. If it's the first
// task in the batch, starts a timer based on the fusion window.
// If the batch reaches the max size, it is immediately flushed.
func (f *fusionStrategy[T, R]) Submit(task *types.SubmittedTask[T, R]) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	select {
	case <-f.quit:
		return nil
	default:
	}

	f.batch = append(f.batch, task)
	if !f.timerActive && len(f.batch) == 1 {
		f.timerActive = true
		if f.timer == nil {
			f.timer = time.AfterFunc(f.fusionWindow, f.onTimerExpire)
		} else {
			f.timer.Reset(f.fusionWindow)
		}
	}

	if len(f.batch) >= f.maxBatchSize {
		f.flushAccumulator()
	}
	return nil
}

// onTimerExpire is called when the fusion window timer fires.
// It flushes the current batch if it is not empty.
func (f *fusionStrategy[T, R]) onTimerExpire() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.timerActive = false
	if len(f.batch) > 0 {
		f.flushAccumulator()
	}
}

// Shutdown flushes any remaining tasks and shuts down both the fusion strategy
// and the underlying scheduling strategy. It also stops the timer if it's active.
func (f *fusionStrategy[T, R]) Shutdown() {
	close(f.quit)
	f.mu.Lock()
	if f.timer != nil && f.timerActive {
		f.timer.Stop()
	}
	f.flushAccumulator()
	f.mu.Unlock()

	// Shutdown underlying strategy
	f.underlying.Shutdown()
}

// flushAccumulator sends the current batch of accumulated tasks to the underlying strategy
// and resets the accumulator. If the timer is running, it is stopped.
func (f *fusionStrategy[T, R]) flushAccumulator() {
	if len(f.batch) == 0 {
		return
	}

	if f.timerActive {
		f.timer.Stop()
		f.timerActive = false
	}

	batch := f.batch
	f.batch = make([]*types.SubmittedTask[T, R], 0, f.maxBatchSize)
	_, _ = f.underlying.SubmitBatch(batch)
}

// SubmitBatch immediately forwards a batch of tasks to the underlying strategy, bypassing fusion.
func (f *fusionStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	select {
	case <-f.quit:
		return 0, nil // Or return error
	default:
	}
	return f.underlying.SubmitBatch(tasks)
}

// Worker launches a worker using the underlying scheduling strategy.
func (f *fusionStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], resHandler types.ResultHandler[T, R]) error {
	return f.underlying.Worker(ctx, workerID, executor, resHandler)
}
