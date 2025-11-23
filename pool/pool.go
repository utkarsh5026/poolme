package pool

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

// WorkerPool is a generic, production-ready worker pool implementation.
// It provides concurrent task processing with configurable worker count,
// context support, and proper error handling.
//
// Type parameters:
//   - T: The input task type
//   - R: The result type
type WorkerPool[T any, R any] struct {
	workerCount     int
	taskBuffer      int
	maxAttempts     int
	initialDelay    time.Duration
	rateLimiter     *rate.Limiter
	continueOnError bool

	beforeTaskStart func(T)
	onTaskEnd       func(T, R, error)
	onRetry         func(T, int, error)
}

// NewWorkerPool creates a new worker pool with the given options.
// Default configuration: workers = GOMAXPROCS, buffer = worker count.
func NewWorkerPool[T any, R any](opts ...WorkerPoolOption) *WorkerPool[T, R] {
	cfg := &workerPoolConfig{
		workerCount:  runtime.GOMAXPROCS(0),
		taskBuffer:   0, // Will be set to workerCount if not specified
		maxAttempts:  1,
		initialDelay: 0,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.taskBuffer == 0 {
		cfg.taskBuffer = cfg.workerCount
	}

	var zeroT T
	var zeroR R
	expectedTaskType := fmt.Sprintf("%T", zeroT)
	expectedResultType := fmt.Sprintf("%T", zeroR)

	beforeTaskStart, onTaskEnd, onRetry := checkfuncs[T, R](cfg, expectedTaskType, expectedResultType)

	return &WorkerPool[T, R]{
		workerCount:     cfg.workerCount,
		taskBuffer:      cfg.taskBuffer,
		maxAttempts:     cfg.maxAttempts,
		initialDelay:    cfg.initialDelay,
		rateLimiter:     cfg.rateLimiter,
		beforeTaskStart: beforeTaskStart,
		onTaskEnd:       onTaskEnd,
		onRetry:         onRetry,
		continueOnError: cfg.continueOnError,
	}
}

// Process executes tasks concurrently using a pool of workers.
// It returns all results and any errors encountered during processing.
// If any worker returns an error, all workers are cancelled and the error is returned.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - tasks: Slice of tasks to process
//   - processFn: Function to process each task
//
// Returns:
//   - results: Slice of all results (may be partial if errors occurred)
//   - error: First error encountered, if any
func (wp *WorkerPool[T, R]) Process(
	ctx context.Context,
	tasks []T,
	processFn ProcessFunc[T, R],
) ([]R, error) {
	if len(tasks) == 0 {
		return []R{}, nil
	}

	g, ctx := errgroup.WithContext(ctx)

	taskChan := make(chan task[T, int], wp.taskBuffer)
	resultChan := make(chan Result[R, int], len(tasks))

	numWorkers := min(wp.workerCount, len(tasks))
	for range numWorkers {
		g.Go(func() error {
			return worker(wp, ctx, taskChan, resultChan, processFn)
		})
	}

	g.Go(func() error {
		return produceFromSlice(ctx, taskChan, tasks)
	})

	results := make([]R, len(tasks))
	var collectionErr error
	var collectionWg sync.WaitGroup

	collectionWg.Add(1)
	go func() {
		defer collectionWg.Done()
		for result := range resultChan {
			if result.Error != nil {
				collectionErr = result.Error
				continue
			}
			if result.Key >= 0 && result.Key < len(results) {
				results[result.Key] = result.Value
			}
		}
	}()

	if err := g.Wait(); err != nil {
		close(resultChan)
		collectionWg.Wait()
		return results, err
	}

	close(resultChan)
	collectionWg.Wait()

	if collectionErr != nil {
		return results, collectionErr
	}

	return results, nil
}

// ProcessMap is similar to Process but works with map inputs instead of slices.
// Useful when tasks are naturally represented as a map.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - tasks: Map of tasks to process
//   - processFn: Function to process each task
//
// Returns:
//   - results: Map of results with the same keys as input tasks
//   - error: First error encountered, if any
func (wp *WorkerPool[T, R]) ProcessMap(
	ctx context.Context,
	tasks map[string]T,
	processFn ProcessFunc[T, R],
) (map[string]R, error) {
	if len(tasks) == 0 {
		return map[string]R{}, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	taskChan := make(chan task[T, string], wp.taskBuffer)
	resultChan := make(chan Result[R, string], len(tasks))

	numWorkers := min(wp.workerCount, len(tasks))
	for range numWorkers {
		g.Go(func() error {
			return worker(wp, ctx, taskChan, resultChan, processFn)
		})
	}

	g.Go(func() error {
		return produceFromMap(ctx, taskChan, tasks)
	})

	results := make(map[string]R, len(tasks))
	var collectionErr error
	var collectionWg sync.WaitGroup
	collectionWg.Add(1)

	go func() {
		defer collectionWg.Done()
		for result := range resultChan {
			if result.Error != nil {
				collectionErr = result.Error
				continue
			}
			results[result.Key] = result.Value
		}
	}()

	if err := g.Wait(); err != nil {
		close(resultChan)
		collectionWg.Wait()
		return results, err
	}

	close(resultChan)
	collectionWg.Wait()

	if collectionErr != nil {
		return results, collectionErr
	}

	return results, nil
}

// ProcessStream processes tasks from a channel instead of a slice.
// This is useful for streaming scenarios where tasks arrive dynamically.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - taskChan: Input channel of tasks (caller must close it)
//   - processFn: Function to process each task
//
// Returns:
//   - resultChan: Output channel of results (closed when all tasks are processed)
//   - error: Channel for error propagation
func (wp *WorkerPool[T, R]) ProcessStream(
	ctx context.Context,
	taskChan <-chan T,
	processFn ProcessFunc[T, R],
) (resultChan <-chan R, errChan <-chan error) {
	resChan := make(chan R, wp.taskBuffer)
	errCh := make(chan error, 1)
	internalTaskChan := make(chan task[T, int], wp.taskBuffer)
	internalResultChan := make(chan Result[R, int], wp.taskBuffer)

	go func() {
		defer close(resChan)
		defer close(errCh)

		g, gctx := errgroup.WithContext(ctx)

		for i := 0; i < wp.workerCount; i++ {
			g.Go(func() error {
				return worker(wp, gctx, internalTaskChan, internalResultChan, processFn)
			})
		}

		var collectorWg sync.WaitGroup
		collectorWg.Add(1)
		var firstErr error
		go func() {
			defer collectorWg.Done()
			for result := range internalResultChan {
				if result.Error != nil && firstErr == nil {
					firstErr = result.Error
				}
				resChan <- result.Value
			}
		}()

		workerErr := g.Wait()
		close(internalResultChan)

		collectorWg.Wait()

		if workerErr != nil {
			errCh <- workerErr
		} else if firstErr != nil {
			errCh <- firstErr
		}
	}()

	go produceFromChannel(ctx, internalTaskChan, taskChan)

	return resChan, errCh
}
