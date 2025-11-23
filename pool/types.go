package pool

import (
	"context"
	"sync"
)

// ProcessFunc is a function type that defines how individual tasks are processed in the worker pool.
// It takes a context for cancellation/timeout control and a task of type T, returning a result of type R.
// If processing fails, it should return an error which will be collected by the pool and halt further processing.
//
// Type parameters:
//   - T: The type of input task to be processed
//   - R: The type of result produced after processing
type ProcessFunc[T any, R any] func(ctx context.Context, task T) (R, error)

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

// indexedTask wraps a task with its original index for result ordering.
// It maintains task order in results while supporting retry logic.
//
// Type parameters:
//   - T: The type of the task being wrapped
//
// Fields:
//   - task: The original task to be processed
//   - index: The original position of the task in the input slice
type indexedTask[T any] struct {
	task  T   // The original task to be processed
	index int // Original index in the input slice
}

// task is a generic interface for all task types in the worker pool.
// It provides a unified way to access the underlying task and its key/identifier.
//
// Type parameters:
//   - T: The type of the task
//   - K: The type of the key/identifier (must be comparable)
type task[T any, K comparable] interface {
	Task() T
	Key() K
}

type keyedTask[T any, K comparable] struct {
	task T
	key  K
}

func (kt *keyedTask[T, K]) Task() T {
	return kt.task
}

func (kt *keyedTask[T, K]) Key() K {
	return kt.key
}

func (it *indexedTask[T]) Task() T {
	return it.task
}

func (it *indexedTask[T]) Key() int {
	return it.index
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
