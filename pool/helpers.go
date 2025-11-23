package pool

import (
	"fmt"
	"math"
	"time"
)

// calcBackoffDelay calculates the exponential backoff delay for retry attempts.
// attemptNumber is 0-indexed (0 = first retry, 1 = second retry, etc.)
// The delay doubles with each attempt: initialDelay * 2^attemptNumber
// For example, with initialDelay=1s:
//   - attempt 0 (first retry): 1s
//   - attempt 1 (second retry): 2s
//   - attempt 2 (third retry): 4s
func calcBackoffDelay(initialDelay time.Duration, attemptNumber int) time.Duration {
	if attemptNumber < 0 {
		return 0
	}

	backoffFactor := math.Pow(2, float64(attemptNumber))
	return time.Duration(float64(initialDelay) * backoffFactor)
}

// checkfuncs validates user-supplied hook functions against expected type signatures and returns
// safe wrapper functions for use within the worker pool. This ensures that user-provided hooks for
// task start, task end, and retry events match the types of tasks and results configured for the pool.
//
// Parameters:
//   - cfg: The worker pool configuration struct containing optional hook function fields and their type records.
//   - expectedTaskType: String representation of the expected task type (as computed by fmt.Sprintf("%T", ...)).
//   - expectedResultType: String representation of the expected result type.
//
// Returns:
//   - beforeTaskStart: Function to be called before each task starts (or nil if not configured).
//   - onTaskEnd: Function to be called after each task ends (or nil if not configured).
//   - onRetry: Function to be called on every retry attempt (or nil if not configured).
//
// Panics:
//
//	If any user-supplied hook function's type does not match the expected task/result types.
//	The panic message describes the type mismatch reason.
func checkfuncs[T any, R any](
	cfg *workerPoolConfig,
	expectedTaskType, expectedResultType string,
) (
	beforeTaskStart func(T),
	onTaskEnd func(T, R, error),
	onRetry func(T, int, error),
) {
	if cfg.beforeTaskStart != nil {
		if cfg.beforeTaskStartType != expectedTaskType {
			panic(fmt.Sprintf("WithBeforeTaskStart hook expects task type %s, but pool processes type %s",
				cfg.beforeTaskStartType, expectedTaskType))
		}
		beforeTaskStart = func(task T) {
			cfg.beforeTaskStart(task)
		}
	}

	if cfg.onTaskEnd != nil {
		if cfg.onTaskEndTaskType != expectedTaskType {
			panic(fmt.Sprintf("WithOnTaskEnd hook expects task type %s, but pool processes type %s",
				cfg.onTaskEndTaskType, expectedTaskType))
		}
		if cfg.onTaskEndResultType != expectedResultType {
			panic(fmt.Sprintf("WithOnTaskEnd hook expects result type %s, but pool produces type %s",
				cfg.onTaskEndResultType, expectedResultType))
		}
		onTaskEnd = func(task T, result R, err error) {
			cfg.onTaskEnd(task, result, err)
		}
	}

	if cfg.onRetry != nil {
		if cfg.onRetryType != expectedTaskType {
			panic(fmt.Sprintf("WithOnEachAttempt hook expects task type %s, but pool processes type %s",
				cfg.onRetryType, expectedTaskType))
		}
		onRetry = func(task T, attempt int, err error) {
			cfg.onRetry(task, attempt, err)
		}
	}

	return beforeTaskStart, onTaskEnd, onRetry
}
