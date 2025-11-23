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

	taskChan := make(chan indexedTask[T], wp.taskBuffer)
	resultChan := make(chan Result[R], len(tasks))

	numWorkers := min(wp.workerCount, len(tasks))
	for range numWorkers {
		g.Go(func() error {
			return wp.worker(ctx, taskChan, resultChan, processFn)
		})
	}

	g.Go(func() error {
		defer close(taskChan)
		for idx, task := range tasks {
			select {
			case taskChan <- indexedTask[T]{index: idx, task: task}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Collect results asynchronously
	results := make([]R, len(tasks))
	var collectionErr error
	var collectionWg sync.WaitGroup

	collectionWg.Go(func() {
		for result := range resultChan {
			if result.Error != nil {
				collectionErr = result.Error
				continue
			}
			if result.Index >= 0 && result.Index < len(results) {
				results[result.Index] = result.Value
			}
		}
	})

	// Wait for all workers to complete
	if err := g.Wait(); err != nil {
		close(resultChan)
		collectionWg.Wait()
		return results, err
	}

	// Close result channel and wait for collection to complete
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

	type keyedTask struct {
		task T
		key  string
	}

	type keyedResult struct {
		value R
		err   error
		key   string
	}

	// Use errgroup for automatic error propagation
	g, ctx := errgroup.WithContext(ctx)

	// Create buffered channels
	taskChan := make(chan keyedTask, wp.taskBuffer)
	resultChan := make(chan keyedResult, len(tasks))

	// Start workers
	numWorkers := min(wp.workerCount, len(tasks))
	for range numWorkers {
		g.Go(func() error {
			for {
				select {
				case task, ok := <-taskChan:
					if !ok {
						return nil
					}
					if wp.rateLimiter != nil {
						if err := wp.rateLimiter.Wait(ctx); err != nil {
							return err
						}
					}
					if wp.beforeTaskStart != nil {
						wp.beforeTaskStart(task.task)
					}
					result, err := wp.processWithRecovery(ctx, task.task, processFn)
					if wp.onTaskEnd != nil {
						wp.onTaskEnd(task.task, result, err)
					}
					select {
					case resultChan <- keyedResult{key: task.key, value: result, err: err}:
					case <-ctx.Done():
						return ctx.Err()
					}
					if err != nil && !wp.continueOnError {
						return err // Stop on first error
					}
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		})
	}

	// Send tasks asynchronously
	g.Go(func() error {
		defer close(taskChan)
		for key, task := range tasks {
			select {
			case taskChan <- keyedTask{key: key, task: task}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	// Collect results asynchronously
	results := make(map[string]R, len(tasks))
	var collectionErr error
	var collectionWg sync.WaitGroup
	collectionWg.Add(1)

	go func() {
		defer collectionWg.Done()
		for result := range resultChan {
			if result.err != nil {
				collectionErr = result.err
				continue
			}
			results[result.key] = result.value
		}
	}()

	// Wait for all workers to complete
	if err := g.Wait(); err != nil {
		close(resultChan)
		collectionWg.Wait()
		return results, err
	}

	// Close result channel and wait for collection to complete
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
	retryableTaskChan := make(chan indexedTask[T], wp.taskBuffer)

	go func() {
		defer close(resChan)
		defer close(errCh)

		g, gctx := errgroup.WithContext(ctx)

		for i := 0; i < wp.workerCount; i++ {
			g.Go(func() error {
				for {
					select {
					case task, ok := <-retryableTaskChan:
						if !ok {
							return nil
						}
						if wp.rateLimiter != nil {
							if err := wp.rateLimiter.Wait(gctx); err != nil {
								return err
							}
						}
						if wp.beforeTaskStart != nil {
							wp.beforeTaskStart(task.task)
						}
						result, err := wp.processWithRecovery(gctx, task.task, processFn)
						if wp.onTaskEnd != nil {
							wp.onTaskEnd(task.task, result, err)
						}
						if err != nil && !wp.continueOnError {
							return err
						}
						select {
						case resChan <- result:
						case <-gctx.Done():
							return gctx.Err()
						}
					case <-gctx.Done():
						return gctx.Err()
					}
				}
			})
		}

		if err := g.Wait(); err != nil {
			errCh <- err
		}
	}()

	go func() {
		defer close(retryableTaskChan)
		for t := range taskChan {
			select {
			case <-ctx.Done():
				return

			default:
				retryableTaskChan <- indexedTask[T]{task: t, index: -1}
			}
		}
	}()

	return resChan, errCh
}

// worker is the core worker function that processes tasks from the task channel.
// It includes panic recovery to prevent a single task from crashing the entire pool.
func (wp *WorkerPool[T, R]) worker(
	ctx context.Context,
	taskChan <-chan indexedTask[T],
	resultChan chan<- Result[R],
	processFn ProcessFunc[T, R],
) error {
	for {
		select {
		case task, ok := <-taskChan:
			if !ok {
				return nil
			}
			if wp.rateLimiter != nil {
				if err := wp.rateLimiter.Wait(ctx); err != nil {
					return err
				}
			}
			if wp.beforeTaskStart != nil {
				wp.beforeTaskStart(task.task)
			}
			result, err := wp.processWithRecovery(ctx, task.task, processFn)
			if wp.onTaskEnd != nil {
				wp.onTaskEnd(task.task, result, err)
			}
			select {
			case resultChan <- Result[R]{Value: result, Error: err, Index: task.index}:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err != nil && !wp.continueOnError {
				return err // Stop on first error
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// processWithRecovery executes a task with panic recovery and retry logic.
// If a panic occurs, it's converted to an error to prevent crashing the worker.
// Retries use exponential backoff if initialDelay is configured.
func (wp *WorkerPool[T, R]) processWithRecovery(
	ctx context.Context,
	task T,
	processFn ProcessFunc[T, R],
) (result R, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			err = fmt.Errorf("worker panic: %v\nstack trace:\n%s", r, buf[:n])
		}
	}()

	maxAttempts := max(wp.maxAttempts, 1)

	for attempt := range maxAttempts {
		if attempt > 0 && wp.initialDelay > 0 {
			backoffDelay := calcBackoffDelay(wp.initialDelay, attempt-1)
			select {
			case <-time.After(backoffDelay):
			case <-ctx.Done():
				return result, ctx.Err()
			}
		}

		result, err = processFn(ctx, task)
		if err == nil {
			return result, nil
		}

		if wp.onRetry != nil && attempt < maxAttempts-1 {
			wp.onRetry(task, attempt+1, err)
		}
	}

	return result, err
}
