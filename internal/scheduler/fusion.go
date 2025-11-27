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

func (f *fusionStrategy[T, R]) onTimerExpire() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.timerActive = false
	if len(f.batch) > 0 {
		f.flushAccumulator()
	}
}

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

func (f *fusionStrategy[T, R]) SubmitBatch(tasks []*types.SubmittedTask[T, R]) (int, error) {
	select {
	case <-f.quit:
		return 0, nil // Or return error
	default:
	}
	return f.underlying.SubmitBatch(tasks)
}

func (f *fusionStrategy[T, R]) Worker(ctx context.Context, workerID int64, executor types.ProcessFunc[T, R], resHandler types.ResultHandler[T, R]) error {
	return f.underlying.Worker(ctx, workerID, executor, resHandler)
}
