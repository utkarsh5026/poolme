package pool

import (
	"context"
	"sync"
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"golang.org/x/time/rate"
)

// ProcessFunc is a function type that defines how individual tasks are processed in the worker pool.
// It takes a context for cancellation/timeout control and a task of type T, returning a result of type R.
// If processing fails, it should return an error which will be collected by the pool and halt further processing.
//
// Type parameters:
//   - T: The type of input task to be processed
//   - R: The type of result produced after processing
type ProcessFunc[T any, R any] func(ctx context.Context, task T) (R, error)

// ResultHandler is a function type for handling the result of a processed task.
type resultHandler[T, R any] func(task *submittedTask[T, R], result *Result[R, int64])

// Result represents the outcome of processing a single task in the worker pool.
// It encapsulates both successful results and errors, along with the task's key/identifier.
//
// Type parameters:
//   - R: The type of the result value
//   - K: The type of the key/identifier (e.g., int for index, string for map key)
//
// Fields:
//   - Value: The result produced by processing the task (only valid if Error is nil)
//   - Error: Any error that occurred during task processing (nil if successful)
//   - Key: The key/identifier of the task (e.g., original index or map key)
type Result[R any, K comparable] struct {
	Value R     // The result value produced by processing the task
	Error error // Error encountered during processing, nil if successful
	Key   K     // Key/identifier of the task
}

func newResult[R any, K comparable](val R, key K, err error) *Result[R, K] {
	return &Result[R, K]{
		Value: val,
		Key:   key,
		Error: err,
	}
}

// Future represents a value that will be available in the future after asynchronous task processing.
// It provides methods to retrieve results with or without blocking, and supports context-based cancellation.
//
// Type parameters:
//   - R: The type of the result value
//   - K: The type of the key/identifier
type Future[R any, K comparable] struct {
	result chan Result[R, K]
	value  R
	key    K
	err    error
	once   sync.Once
	done   chan struct{}
}

// newFuture creates a new Future instance with initialized channels.
func newFuture[R any, K comparable]() *Future[R, K] {
	return &Future[R, K]{
		result: make(chan Result[R, K], 1),
		done:   make(chan struct{}),
	}
}

// Get blocks until the result is available and returns it.
// This method can be called multiple times and will return the same result.
// It does not support context cancellation - use GetWithContext for that.
func (f *Future[R, K]) Get() (R, K, error) {
	f.once.Do(func() {
		r := <-f.result
		f.value = r.Value
		f.key = r.Key
		f.err = r.Error
		close(f.done)
	})
	return f.value, f.key, f.err
}

// GetWithContext blocks until the result is available or the context is cancelled.
// This method can be called multiple times and will return the same result.
// If the context is cancelled before the result is available, returns context error.
func (f *Future[R, K]) GetWithContext(ctx context.Context) (R, K, error) {
	select {
	case <-f.done:
		return f.value, f.key, f.err
	case <-ctx.Done():
		var zero R
		var zeroK K
		return zero, zeroK, ctx.Err()
	default:
	}

	f.once.Do(func() {
		select {
		case r := <-f.result:
			f.value = r.Value
			f.key = r.Key
			f.err = r.Error
			close(f.done)
		case <-ctx.Done():
			// If context cancelled, we still need to drain the result channel
			// to prevent goroutine leak, but we'll return context error
			go func() {
				r := <-f.result
				f.value = r.Value
				f.key = r.Key
				f.err = r.Error
				close(f.done)
			}()
		}
	})

	select {
	case <-f.done:
		return f.value, f.key, f.err
	case <-ctx.Done():
		var zero R
		var zeroK K
		return zero, zeroK, ctx.Err()
	}
}

// TryGet attempts to retrieve the result without blocking.
// Returns the result and true if available, or zero values and false if not ready yet.
func (f *Future[R, K]) TryGet() (R, K, error, bool) {
	select {
	case <-f.done:
		return f.value, f.key, f.err, true
	default:
	}

	select {
	case r := <-f.result:
		f.once.Do(func() {
			f.value = r.Value
			f.key = r.Key
			f.err = r.Error
			close(f.done)
		})
		return f.value, f.key, f.err, true
	default:
		var zero R
		var zeroK K
		return zero, zeroK, nil, false
	}
}

// Done returns a channel that's closed when the result becomes available.
// Useful for select statements and composing multiple futures.
func (f *Future[R, K]) Done() <-chan struct{} {
	return f.done
}

// IsReady returns true if the result is available without blocking.
func (f *Future[R, K]) IsReady() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

// submittedTask wraps a task with its unique ID and result future.
type submittedTask[T any, R any] struct {
	task   T
	id     int64
	future *Future[R, int64]
}

// schedulingStrategy defines the behavior for distributing tasks to workers.
// Any new algorithm (Work Stealing, Priority Queue, Ring Buffer) must implement this.
type schedulingStrategy[T any, R any] interface {
	// Submit accepts a task into the scheduling system.
	// It handles the logic of where the task goes (Global Queue vs Local Queue).
	Submit(task *submittedTask[T, R]) error

	// SubmitBatch accepts multiple tasks at once for optimized batch submission.
	// Strategies can pre-distribute tasks to worker queues, avoiding per-task submission overhead.
	// Returns the number of tasks successfully submitted and an error if any occurred.
	SubmitBatch(tasks []*submittedTask[T, R]) (int, error)

	// Shutdown gracefully stops the workers and waits for them to finish.
	Shutdown()

	// Worker executes tasks assigned to a specific worker, using the provided executor and pool.
	Worker(ctx context.Context, workerID int64, executor ProcessFunc[T, R], resHandler resultHandler[T, R]) error
}

// processorConfig holds all configuration for a pool of workers and task scheduling.
// This enables flexible tuning of core pool behavior, retry, backoff, queueing, and hooks.
type processorConfig[T, R any] struct {
	// Number of worker goroutines in the pool.
	workerCount int

	// Size of the internal task/result buffer (impacts batching & throughput).
	taskBuffer int

	// Maximum number of processing attempts per task (for retry logic).
	maxAttempts int

	// Optional token bucket rate limiter applied per task (may be nil).
	rateLimiter *rate.Limiter

	// Initial delay before first retry attempt.
	initialDelay time.Duration

	// If true, worker should continue on task error (don't abort processing on first failure).
	continueOnErr bool

	// The scheduling strategy used for distributing tasks (Channel, WorkStealing, PriorityQueue, etc).
	schedulingStrategy SchedulingStrategyType

	// Hook called before a task starts (can be used for logging/tracing).
	beforeTaskStart func(T)

	// Hook called after a task ends (receives the input, result, and error if any).
	onTaskEnd func(T, R, error)

	// Hook called on retry with the task, current attempt, and last error.
	onRetry func(T, int, error)

	// Backoff calculation strategy used between retries (exponential, fixed, etc.).
	backoffStrategy algorithms.BackoffStrategy

	// If true, enable priority queue for scheduling.
	usePq bool

	// Function that computes priority for a task (lower value = higher priority).
	pqFunc func(a T) int

	// MPMC queue configuration:

	// Use a bounded queue (if true) or unbounded.
	mpmcBounded bool

	// Explicit queue capacity if using bounded queue.
	mpmcCapacity int
}
