package pool

import (
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

// WorkerPoolOption is a functional option for configuring the worker pool.
type WorkerPoolOption func(*workerPoolConfig)

type workerPoolConfig struct {
	workerCount     int
	taskBuffer      int
	maxAttempts     int
	initialDelay    time.Duration
	rateLimiter     *rate.Limiter
	continueOnError bool

	// Hook functions stored as any for flexibility
	beforeTaskStart     func(any)
	beforeTaskStartType string
	onTaskEnd           func(any, any, error)
	onTaskEndTaskType   string
	onTaskEndResultType string
	onRetry             func(any, int, error)
	onRetryType         string
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

// WithRetryPolicy sets a retry policy for task processing.
// maxAttempts specifies the maximum number of attempts for each task.
// initialDelay specifies the delay before the first retry, with subsequent retries
// using exponential backoff. Set initialDelay to 0 for immediate retries without delay.
// If not specified, no retries are performed.
func WithRetryPolicy(maxAttempts int, initialDelay time.Duration) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if maxAttempts > 0 {
			cfg.maxAttempts = maxAttempts
		}

		if initialDelay > 0 {
			cfg.initialDelay = initialDelay
		}
	}
}

// WithRateLimit sets a rate limiter for controlling task throughput.
// tasksPerSecond specifies the maximum number of tasks to process per second.
// burst specifies the maximum number of tasks that can be processed in a burst.
// This is useful for preventing overwhelming external services or APIs.
// If not specified, no rate limiting is applied.
//
// Example:
//
//	WithRateLimit(10, 5) // Allow 10 tasks/sec with burst of 5
func WithRateLimit(tasksPerSecond float64, burst int) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if tasksPerSecond > 0 && burst > 0 {
			cfg.rateLimiter = rate.NewLimiter(rate.Limit(tasksPerSecond), burst)
		}
	}
}

// WithBeforeTaskStart sets a hook that is called before each task starts processing.
// The hook receives the task being processed.
// Hook must be thread-safe as it may be called concurrently by multiple workers.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithBeforeTaskStart(func(task int) {
//	        log.Printf("Starting task: %d", task)
//	    }),
//	)
func WithBeforeTaskStart[T any](startFunc func(task T)) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if startFunc == nil {
			return
		}
		cfg.beforeTaskStart = func(task any) {
			if t, ok := task.(T); ok {
				startFunc(t)
			}
		}
		var zero T
		cfg.beforeTaskStartType = getTypeName(zero)
	}
}

// WithOnTaskEnd sets a hook that is called after each task completes processing.
// The hook receives the task, result, and any error that occurred.
// Hook must be thread-safe as it may be called concurrently by multiple workers.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithOnTaskEnd(func(task int, result string, err error) {
//	        if err != nil {
//	            log.Printf("Task %d failed: %v", task, err)
//	        } else {
//	            log.Printf("Task %d completed: %s", task, result)
//	        }
//	    }),
//	)
func WithOnTaskEnd[T any, R any](endFunc func(task T, result R, err error)) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if endFunc == nil {
			return
		}
		cfg.onTaskEnd = func(task any, result any, err error) {
			var r R
			if result != nil {
				if converted, ok := result.(R); ok {
					r = converted
				}
			}
			if t, ok := task.(T); ok {
				endFunc(t, r, err)
			}
		}
		var zeroT T
		var zeroR R
		cfg.onTaskEndTaskType = getTypeName(zeroT)
		cfg.onTaskEndResultType = getTypeName(zeroR)
	}
}

// WithOnEachAttempt sets a hook that is called after each retry attempt.
// The hook receives the task, attempt number (1-based), and the error that caused the retry.
// This is only called when retry policy is configured and a task fails.
// Hook must be thread-safe as it may be called concurrently by multiple workers.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithRetryPolicy(3, 100*time.Millisecond),
//	    WithOnEachAttempt(func(task int, attempt int, err error) {
//	        log.Printf("Task %d attempt %d failed: %v", task, attempt, err)
//	    }),
//	)
func WithOnEachAttempt[T any](attemptFunc func(task T, attempt int, err error)) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if attemptFunc == nil {
			return
		}
		cfg.onRetry = func(task any, attempt int, err error) {
			if t, ok := task.(T); ok {
				attemptFunc(t, attempt, err)
			}
		}
		var zero T
		cfg.onRetryType = getTypeName(zero)
	}
}

// WithContinueOnError configures whether the worker pool should continue processing
// remaining tasks when a task fails. If set to true, task failures will not stop
// the pool from processing other tasks in the queue. If set to false (default),
// the pool may stop processing based on error handling behavior.
// This is useful when you want to ensure all tasks are attempted regardless of failures.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithContinueOnError(true), // Continue processing even if some tasks fail
//	)
func WithContinueOnError(continueOnError bool) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.continueOnError = continueOnError
	}
}

// getTypeName returns a string representation of the type for validation.
// This uses fmt.Sprintf with %T which is sufficient for type checking.
func getTypeName(v any) string {
	return fmt.Sprintf("%T", v)
}
