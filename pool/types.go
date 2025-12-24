package pool

import (
	"github.com/utkarsh5026/gopool/internal/scheduler"
	"github.com/utkarsh5026/gopool/internal/types"
)

// ProcessFunc is a function type that defines how individual tasks are processed in the worker pool.
// It takes a context for cancellation/timeout control and a task of type T, returning a result of type R.
// If processing fails, it should return an error which will be collected by the pool and halt further processing.
//
// Type parameters:
//   - T: The type of input task to be processed
//   - R: The type of result produced after processing
type ProcessFunc[T any, R any] = types.ProcessFunc[T, R]

// ResultHandler is a function type for handling the result of a processed task.
type resultHandler[T, R any] = types.ResultHandler[T, R]

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
type Result[R any, K comparable] = types.Result[R, K]

// Future represents a value that will be available in the future after asynchronous task processing.
// It provides methods to retrieve results with or without blocking, and supports context-based cancellation.
//
// Type parameters:
//   - R: The type of the result value
//   - K: The type of the key/identifier
type Future[R any, K comparable] = types.Future[R, K]

// submittedTask wraps a task with its unique ID and result future.
type submittedTask[T any, R any] = types.SubmittedTask[T, R]

// schedulingStrategy defines the behavior for distributing tasks to workers.
// Any new algorithm (Work Stealing, Priority Queue, Ring Buffer) must implement this.
type schedulingStrategy[T any, R any] = scheduler.SchedulingStrategy[T, R]

// processorConfig holds all configuration for a pool of workers and task scheduling.
// This enables flexible tuning of core pool behavior, retry, backoff, queueing, and hooks.
type processorConfig[T, R any] = scheduler.ProcessorConfig[T, R]
