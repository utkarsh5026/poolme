package pool

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// WorkerPool is a generic, production-ready worker pool implementation.
// It provides concurrent task processing with configurable worker count,
// context support, and proper error handling.
//
// Type parameters:
//   - T: The input task type
//   - R: The result type
type WorkerPool[T any, R any] struct {
	workerCount int
	taskBuffer  int
}

// WorkerPoolOption is a functional option for configuring the worker pool.
type WorkerPoolOption func(*workerPoolConfig)

type workerPoolConfig struct {
	workerCount int
	taskBuffer  int
}

// WithWorkerCount sets the number of concurrent workers.
// If not specified, defaults to runtime.GOMAXPROCS(0).
func WithWorkerCount(count int) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if count > 0 {
			cfg.workerCount = count
		}
	}
}

// WithTaskBuffer sets the buffer size for the task channel.
// A larger buffer can improve throughput but uses more memory.
// If not specified, defaults to the number of workers.
func WithTaskBuffer(size int) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if size >= 0 {
			cfg.taskBuffer = size
		}
	}
}

// NewWorkerPool creates a new worker pool with the given options.
// Default configuration: workers = GOMAXPROCS, buffer = worker count.
func NewWorkerPool[T any, R any](opts ...WorkerPoolOption) *WorkerPool[T, R] {
	cfg := &workerPoolConfig{
		workerCount: runtime.GOMAXPROCS(0),
		taskBuffer:  0, // Will be set to workerCount if not specified
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.taskBuffer == 0 {
		cfg.taskBuffer = cfg.workerCount
	}

	return &WorkerPool[T, R]{
		workerCount: cfg.workerCount,
		taskBuffer:  cfg.taskBuffer,
	}
}

// ProcessFunc is a function that processes a single task and returns a result.
// If an error is returned, the worker pool will collect it and stop processing.
type ProcessFunc[T any, R any] func(ctx context.Context, task T) (R, error)

// Result represents the outcome of processing a task.
type Result[R any] struct {
	Value R
	Error error
	Index int // Original index of the task (if tasks were provided as a slice)
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

	// Use errgroup for automatic error propagation and context cancellation
	g, ctx := errgroup.WithContext(ctx)

	// Create buffered channels for tasks and results
	taskChan := make(chan indexedTask[T], wp.taskBuffer)
	resultChan := make(chan Result[R], len(tasks))

	// Start workers
	numWorkers := min(wp.workerCount, len(tasks))
	for range numWorkers {
		g.Go(func() error {
			return wp.worker(ctx, taskChan, resultChan, processFn)
		})
	}

	// Send tasks asynchronously
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
	collectionWg.Add(1)

	go func() {
		defer collectionWg.Done()
		for result := range resultChan {
			if result.Error != nil {
				collectionErr = result.Error
				continue
			}
			if result.Index >= 0 && result.Index < len(results) {
				results[result.Index] = result.Value
			}
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
		key  string
		task T
	}

	type keyedResult struct {
		key   string
		value R
		err   error
	}

	// Use errgroup for automatic error propagation
	g, ctx := errgroup.WithContext(ctx)

	// Create buffered channels
	taskChan := make(chan keyedTask, wp.taskBuffer)
	resultChan := make(chan keyedResult, len(tasks))

	// Start workers
	numWorkers := min(wp.workerCount, len(tasks))
	for i := 0; i < numWorkers; i++ {
		g.Go(func() error {
			for {
				select {
				case task, ok := <-taskChan:
					if !ok {
						return nil
					}
					result, err := wp.processWithRecovery(ctx, task.task, processFn)
					select {
					case resultChan <- keyedResult{key: task.key, value: result, err: err}:
					case <-ctx.Done():
						return ctx.Err()
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
) (<-chan R, <-chan error) {
	resultChan := make(chan R, wp.taskBuffer)
	errChan := make(chan error, 1)

	go func() {
		defer close(resultChan)
		defer close(errChan)

		g, ctx := errgroup.WithContext(ctx)

		for i := 0; i < wp.workerCount; i++ {
			g.Go(func() error {
				for {
					select {
					case task, ok := <-taskChan:
						if !ok {
							return nil
						}
						result, err := wp.processWithRecovery(ctx, task, processFn)
						if err != nil {
							return err
						}
						select {
						case resultChan <- result:
						case <-ctx.Done():
							return ctx.Err()
						}
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			})
		}

		if err := g.Wait(); err != nil {
			errChan <- err
		}
	}()

	return resultChan, errChan
}

// indexedTask wraps a task with its original index
type indexedTask[T any] struct {
	index int
	task  T
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
			result, err := wp.processWithRecovery(ctx, task.task, processFn)
			select {
			case resultChan <- Result[R]{Value: result, Error: err, Index: task.index}:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err != nil {
				return err // Stop on first error
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// processWithRecovery executes a task with panic recovery.
// If a panic occurs, it's converted to an error to prevent crashing the worker.
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

	return processFn(ctx, task)
}
