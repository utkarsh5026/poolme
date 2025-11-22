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
	Value R
	Error error
	Index int // Original index of the task (if tasks were provided as a slice)
}
