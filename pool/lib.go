package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// Scheduler represents a long-running, reusable worker pool that can be started and then
// accept asynchronous task submissions. It maintains worker goroutines, schedules work, and tracks state
// for lifecycle management and safe concurrent usage.
//
// Type parameters:
//   - T: The input task type processed by workers
//   - R: The output/result type produced by processing tasks
type Scheduler[T, R any] struct {
	config *processorConfig[T, R]
	mu     sync.RWMutex
	state  *poolState[T, R]
}

// NewScheduler creates a new Scheduler instance with the specified configuration options.
// This does NOT start any workers immediately; use Start to begin processing tasks.
//
// Parameters:
//   - opts: Variadic set of WorkerPoolOption for customizing worker count, task buffer, etc.
//
// Returns:
//   - *Scheduler: An unstarted persistent pool (call Start to launch).
//
// Example:
//
//	sched := NewScheduler[int, string](WithWorkerCount(8), WithTaskBuffer(32))
//	_ = sched.Start(ctx, processFunc)
//	future := sched.Submit(5)
func NewScheduler[T, R any](opts ...WorkerPoolOption) *Scheduler[T, R] {
	cfg := createConfig[T, R](opts...)
	return &Scheduler[T, R]{
		config: cfg,
	}
}

// Start initializes the worker pool in long-running mode and starts persistent workers.
// This enables the pool to accept tasks via the Submit method for asynchronous processing.
//
// Parameters:
//   - ctx: Context for the pool's lifetime and task processing
//   - processFn: Function to process each submitted task (func(context.Context, T) (R, error))
//
// Returns:
//   - error: Non-nil if the pool is already started
//
// Example:
//
//	pool := NewWorkerPool[int, string](WithWorkerCount(5))
//	err := pool.Start(ctx, func(ctx context.Context, n int) (string, error) {
//	    return fmt.Sprintf("result: %d", n*2), nil
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer pool.Shutdown(5 * time.Second)
//
//	// Now you can submit tasks
//	future, _ := pool.Submit(42)
func (wp *Scheduler[T, R]) Start(ctx context.Context, processFn ProcessFunc[T, R]) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.state != nil && wp.state.started.Load() {
		return errors.New("pool already started")
	}

	strategy, err := createSchedulingStrategy(wp.config, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	state := &poolState[T, R]{
		strategy: strategy,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	wp.state = state
	state.started.Store(true)

	n := wp.config.workerCount
	var g errgroup.Group

	var resHandler resultHandler[T, R] = func(t *submittedTask[T, R], r *Result[R, int64]) {
		t.future.result <- *r
	}

	for i := range n {
		g.Go(func() error {
			return strategy.Worker(ctx, int64(i), processFn, resHandler)
		})
	}

	go func() {
		_ = g.Wait()
		close(state.done)
	}()

	return nil
}

// Submit submits a single task to the pool for asynchronous processing.
// It returns a Future that can be used to retrieve the result when the task completes.
//
// Parameters:
//   - task: The task to process
//
// Returns:
//   - future: A Future[R, int64] for retrieving the result asynchronously
//   - error: Non-nil if pool is not started, shut down, or context is cancelled
//
// Example:
//
//	future, err := pool.Submit(42)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Option 1: Block until result is ready
//	result, err := future.Get()
//
//	// Option 2: Wait with timeout
//	result, err := future.GetWithTimeout(5 * time.Second)
//
//	// Option 3: Check if ready without blocking
//	if future.IsReady() {
//	    result, _ := future.Get()
//	}
func (wp *Scheduler[T, R]) Submit(task T) (*Future[R, int64], error) {
	wp.mu.RLock()
	state := wp.state
	wp.mu.RUnlock()

	if state == nil || !state.started.Load() {
		return nil, errors.New("pool not started")
	}

	if state.shutdown.Load() {
		return nil, errors.New("pool shut down")
	}

	taskID := state.taskIDCounter.Add(1)
	future := newFuture[R, int64]()

	st := &submittedTask[T, R]{
		task:   task,
		id:     taskID,
		future: future,
	}

	err := state.strategy.Submit(st)
	return future, err
}

// Shutdown gracefully shuts down the worker pool started with Start.
// It waits for all in-flight tasks to complete before returning, ensuring no work is lost.
//
// Parameters:
//   - timeout: Maximum duration to wait for graceful shutdown (0 = wait forever)
//
// Returns:
//   - error: Non-nil if pool not started, already shut down, or timeout exceeded
//
// Example:
//
//	// Graceful shutdown with timeout
//	if err := pool.Shutdown(5 * time.Second); err != nil {
//	    log.Printf("shutdown error: %v", err)
//	}
//
//	// Wait indefinitely for all tasks to complete
//	pool.Shutdown(0)
//
//	// Common pattern with defer
//	pool.Start(ctx, processFn)
//	defer pool.Shutdown(10 * time.Second)
func (wp *Scheduler[T, R]) Shutdown(timeout time.Duration) error {
	wp.mu.Lock()
	state := wp.state
	if state == nil || !state.started.Load() {
		wp.mu.Unlock()
		return errors.New("pool not started")
	}

	if !state.shutdown.CompareAndSwap(false, true) {
		wp.mu.Unlock()
		return errors.New("pool already shut down")
	}
	wp.mu.Unlock()

	// Signal shutdown to the strategy (closes task channels/queues)
	// Workers will drain their queues and exit gracefully
	state.strategy.Shutdown()

	// Wait for all workers to finish draining and exit
	return waitUntil(state.done, timeout)
}

// poolState holds the runtime state for a long-running worker pool.
// It manages worker goroutines, task/result channels, and lifecycle.
type poolState[T any, R any] struct {
	cancel        context.CancelFunc
	started       atomic.Bool
	shutdown      atomic.Bool
	taskIDCounter atomic.Int64
	strategy      schedulingStrategy[T, R]
	done          chan struct{} // Closed when all workers have finished
}

// WorkerPool is a generic, production-ready worker pool implementation.
// It provides concurrent task processing with configurable worker count,
// context support, and proper error handling.
//
// Type parameters:
//   - T: The input task type
//   - R: The result type
type WorkerPool[T any, R any] struct {
	conf *processorConfig[T, R]
}

// NewWorkerPool creates a new worker pool with the given options.
// It returns a WorkerPool instance configured according to the provided options.
//
// Default configuration:
//   - workerCount: runtime.GOMAXPROCS(0) (number of logical CPUs)
//   - taskBuffer: equal to workerCount
//   - maxAttempts: 1 (no retries)
//   - initialDelay: 0 (no delay)
//   - continueOnError: false
//
// Type parameters:
//   - T: The input task type
//   - R: The result type produced by processing tasks
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(10),
//	    WithTaskBuffer(20),
//	    WithMaxAttempts(3),
//	)
func NewWorkerPool[T any, R any](opts ...WorkerPoolOption) *WorkerPool[T, R] {
	cfg := createConfig[T, R](opts...)
	return &WorkerPool[T, R]{
		conf: cfg,
	}
}

// Process executes a batch of tasks concurrently using a pool of workers.
// It processes all tasks in the slice and returns when all tasks are complete or an error occurs.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - tasks: Slice of tasks to process
//   - processFn: Function to process each task (func(context.Context, T) (R, error))
//
// Returns:
//   - results: Slice of all results in the same order as input tasks
//   - error: First error encountered, or nil if all tasks succeeded
//
// Example:
//
//	tasks := []int{1, 2, 3, 4, 5}
//	results, err := pool.Process(ctx, tasks, func(ctx context.Context, n int) (string, error) {
//	    return fmt.Sprintf("processed %d", n), nil
//	})
func (wp *WorkerPool[T, R]) Process(
	ctx context.Context,
	tasks []T,
	processFn ProcessFunc[T, R],
) ([]R, error) {
	sp := newSliceProcessor(tasks, processFn, wp.conf)
	return sp.Process(ctx, min(wp.conf.workerCount, len(tasks)))
}

// ProcessMap executes a batch of tasks from a map concurrently using a pool of workers.
// It processes all map entries and returns a map of results with the same keys.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - tasks: Map of tasks to process, where keys are string identifiers
//   - processFn: Function to process each task (func(context.Context, T) (R, error))
//
// Returns:
//   - results: Map of results with the same keys as input tasks
//   - error: First error encountered, or nil if all tasks succeeded
//
// Example:
//
//	tasks := map[string]int{"a": 1, "b": 2, "c": 3}
//	results, err := pool.ProcessMap(ctx, tasks, func(ctx context.Context, n int) (string, error) {
//	    return fmt.Sprintf("processed %d", n), nil
//	})
//	// results["a"] = "processed 1", results["b"] = "processed 2", ...
func (wp *WorkerPool[T, R]) ProcessMap(
	ctx context.Context,
	tasks map[string]T,
	processFn ProcessFunc[T, R],
) (map[string]R, error) {
	mp := newMapProcessor(tasks, processFn, wp)
	return mp.Process(ctx, min(wp.conf.workerCount, len(tasks)))
}
