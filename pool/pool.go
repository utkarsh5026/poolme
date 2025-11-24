package pool

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"golang.org/x/time/rate"
)

// poolState holds the runtime state for a long-running worker pool.
// It manages worker goroutines, task/result channels, and lifecycle.
type poolState[T any, R any] struct {
	// taskChan      chan *submittedTask[T, R]
	ctx context.Context
	// cancel        context.CancelFunc
	// wg            sync.WaitGroup
	started       atomic.Bool
	shutdown      atomic.Bool
	taskIDCounter atomic.Int64
	strategy      SchedulingStrategy[T, R]
}

// WorkerPool is a generic, production-ready worker pool implementation.
// It provides concurrent task processing with configurable worker count,
// context support, and proper error handling.
//
// The pool supports two modes of operation:
// 1. Batch mode: Use Process/ProcessMap/ProcessStream for processing collections
// 2. Long-running mode: Use Start/Submit/Shutdown for persistent worker pool
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

	backoffStrategy algorithms.BackoffStrategy

	mu    sync.RWMutex
	state *poolState[T, R]
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
	cfg := &workerPoolConfig{
		workerCount:         runtime.GOMAXPROCS(0),
		taskBuffer:          0, // Will be set to workerCount if not specified
		maxAttempts:         1,
		initialDelay:        0,
		backoffType:         BackoffExponential, // Default backoff
		backoffInitialDelay: 100 * time.Millisecond,
		backoffMaxDelay:     5 * time.Second,
		backoffJitterFactor: 0.1, // Default 10% jitter for jittered backoff
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.taskBuffer == 0 {
		cfg.taskBuffer = cfg.workerCount
	}

	if cfg.retryPolicySet {
		cfg.backoffInitialDelay = cfg.initialDelay
	}

	backoffStrategy := algorithms.NewBackoffStrategy(
		cfg.backoffType,
		cfg.backoffInitialDelay,
		cfg.backoffMaxDelay,
		cfg.backoffJitterFactor,
	)

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
		backoffStrategy: backoffStrategy,
	}
}

// Process executes a batch of tasks concurrently using a pool of workers.
// It processes all tasks in the slice and returns when all tasks are complete or an error occurs.
//
// This method uses batch mode processing and is suitable for processing a known set of tasks.
// The pool will use min(workerCount, len(tasks)) workers to process the tasks concurrently.
//
// Behavior:
//   - Tasks are distributed across available workers
//   - Results maintain the same order as input tasks
//   - If continueOnError is false (default), processing stops on first error
//   - If continueOnError is true, all tasks are processed despite errors
//   - Context cancellation will interrupt processing
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
	sp := newSliceProcessor(tasks, processFn, wp)
	return sp.Process(ctx, min(wp.workerCount, len(tasks)))
}

// ProcessMap executes a batch of tasks from a map concurrently using a pool of workers.
// It processes all map entries and returns a map of results with the same keys.
//
// This method is similar to Process but works with map inputs, making it suitable
// for scenarios where tasks have string identifiers or natural key-value relationships.
// The pool will use min(workerCount, len(tasks)) workers to process the tasks concurrently.
//
// Behavior:
//   - Tasks are distributed across available workers
//   - Results map contains the same keys as the input tasks map
//   - Processing order is non-deterministic (map iteration order)
//   - If continueOnError is false (default), processing stops on first error
//   - If continueOnError is true, all tasks are processed despite errors
//   - Context cancellation will interrupt processing
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
	return mp.Process(ctx, min(wp.workerCount, len(tasks)))
}

// ProcessStream processes tasks from an input channel concurrently using a pool of workers.
// It returns channels for receiving results and errors, enabling streaming/pipeline processing.
//
// This method is ideal for scenarios where:
//   - Tasks arrive dynamically or are generated on-the-fly
//   - You want to process tasks as they arrive without collecting them first
//   - Memory constraints prevent loading all tasks at once
//   - You need to build processing pipelines
//
// The pool will use workerCount workers to process tasks concurrently.
//
// Behavior:
//   - Workers consume tasks from taskChan as they arrive
//   - Results are sent to the result channel as tasks complete (order not preserved)
//   - The caller MUST close the taskChan when no more tasks will be sent
//   - The result channel is automatically closed when all tasks are processed
//   - If an error occurs, it's sent to the error channel
//   - Context cancellation will interrupt processing and close channels
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - taskChan: Input channel of tasks (caller is responsible for closing it)
//   - processFn: Function to process each task (func(context.Context, T) (R, error))
//
// Returns:
//   - resultChan: Output channel for receiving results (read-only, auto-closed when done)
//   - errChan: Channel for receiving errors (read-only)
//
// Example:
//
//	taskChan := make(chan int, 10)
//	go func() {
//	    for i := 0; i < 100; i++ {
//	        taskChan <- i
//	    }
//	    close(taskChan)
//	}()
//	resultChan, errChan := pool.ProcessStream(ctx, taskChan, processFunc)
//	for result := range resultChan {
//	    fmt.Println(result)
//	}
func (wp *WorkerPool[T, R]) ProcessStream(
	ctx context.Context,
	taskChan <-chan T,
	processFn ProcessFunc[T, R],
) (resultChan <-chan R, errChan <-chan error) {
	sp := newStreamProcessor(wp, taskChan)
	return sp.Process(ctx, processFn, wp.workerCount)
}

// Start initializes the worker pool in long-running mode and starts persistent workers.
// This enables the pool to accept tasks via the Submit method for asynchronous processing.
//
// Long-running mode is suitable for:
//   - Server applications that need to process tasks continuously
//   - Scenarios where tasks arrive asynchronously over time
//   - Applications requiring fine-grained control over individual task submission
//   - Cases where you need futures/promises for tracking individual task results
//
// The pool will spawn workerCount goroutines that will continuously process tasks
// until Shutdown is called. The provided processFn will be used to process all
// tasks submitted via the Submit method.
//
// Behavior:
//   - Creates workerCount persistent worker goroutines
//   - Workers remain active until Shutdown is called
//   - All submitted tasks use the same processFn
//   - The context is used for the lifetime of the pool and task processing
//   - Context cancellation will interrupt all workers and in-flight tasks
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
func (wp *WorkerPool[T, R]) Start(ctx context.Context, processFn ProcessFunc[T, R]) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.state != nil && wp.state.started.Load() {
		return errors.New("pool already started")
	}

	state := &poolState[T, R]{
		ctx:      ctx,
		strategy: NewChannelStrategy(wp, processFn),
	}

	wp.state = state
	state.started.Store(true)

	return state.strategy.Start(ctx, wp.workerCount)
}

// Submit submits a single task to the pool for asynchronous processing.
// It returns a Future that can be used to retrieve the result when the task completes.
//
// This method is only available after calling Start to initialize the pool in long-running mode.
// Tasks are queued in a buffered channel (size configured via WithTaskBuffer) and processed
// by available workers using the processFn provided to Start.
//
// The returned Future provides methods to:
//   - Get(): Block and wait for the result
//   - GetWithTimeout(): Wait for the result with a timeout
//   - IsReady(): Check if the result is available without blocking
//
// Behavior:
//   - Tasks are assigned a unique monotonically increasing ID
//   - Tasks are processed in the order they're received by workers
//   - If the task buffer is full, Submit blocks until space is available
//   - Returns error immediately if pool is not started or has been shut down
//   - Context cancellation (from Start) will interrupt task submission
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
func (wp *WorkerPool[T, R]) Submit(task T) (*Future[R, int64], error) {
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

	err := state.strategy.Submit(state.ctx, st)
	return future, err
}

// Shutdown gracefully shuts down the worker pool started with Start.
// It waits for all in-flight tasks to complete before returning, ensuring no work is lost.
//
// The shutdown process:
//  1. Marks the pool as shutdown (prevents new task submissions)
//  2. Closes the task channel (signals workers no more tasks will arrive)
//  3. Waits for all workers to finish processing current tasks
//  4. Cancels the pool context
//
// Behavior:
//   - All tasks already submitted will be processed to completion
//   - New Submit calls after Shutdown will return an error
//   - If timeout is 0, waits indefinitely for all tasks to complete
//   - If timeout > 0, waits up to that duration before forcefully cancelling
//   - Forceful cancellation (via timeout) will interrupt in-flight tasks
//   - Safe to call multiple times (subsequent calls return error)
//   - Returns error if pool was never started
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
func (wp *WorkerPool[T, R]) Shutdown(timeout time.Duration) error {
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
	return waitUntil(state.strategy.Shutdown(), timeout)
}
