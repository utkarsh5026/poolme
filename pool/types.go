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
// It encapsulates both successful results and errors, along with the task's original position.
//
// Type parameters:
//   - R: The type of the result value
//
// Fields:
//   - Value: The result produced by processing the task (only valid if Error is nil)
//   - Error: Any error that occurred during task processing (nil if successful)
//   - Index: The original position of the task in the input slice, useful for maintaining order
type Result[R any] struct {
	Value R     // The result value produced by processing the task
	Error error // Error encountered during processing, nil if successful
	Index int   // Original index of the task in the input slice
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
