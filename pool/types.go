package pool

import "context"

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
