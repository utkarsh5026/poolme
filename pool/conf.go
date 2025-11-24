package pool

import (
	"fmt"
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"golang.org/x/time/rate"
)

// WorkerPoolOption is a functional option for configuring the worker pool.
type WorkerPoolOption func(*workerPoolConfig)

// BackoffType defines the retry backoff algorithm to use.
// This is re-exported from the internal algorithms package for user convenience.
type BackoffType = algorithms.BackoffType

const (
	// BackoffExponential uses simple exponential backoff (default).
	// Delay = initialDelay * 2^attemptNumber
	// This is the simplest strategy with predictable, exponentially growing delays.
	BackoffExponential = algorithms.BackoffExponential

	// BackoffJittered adds random jitter to prevent thundering herd.
	// Delay = exponentialDelay * (1 ± jitterFactor)
	// Useful when multiple tasks might fail and retry simultaneously.
	BackoffJittered = algorithms.BackoffJittered

	// BackoffDecorrelated uses AWS-style decorrelated jitter (recommended for high-retry scenarios).
	// Delay = random(initialDelay, prevDelay * 3)
	// Proven to reduce retry traffic by 95% in AWS production systems.
	// Best for scenarios with many concurrent failures and retries.
	BackoffDecorrelated = algorithms.BackoffDecorrelated
)

type workerPoolConfig struct {
	workerCount     int
	taskBuffer      int
	maxAttempts     int
	initialDelay    time.Duration
	rateLimiter     *rate.Limiter
	continueOnError bool

	backoffType         BackoffType
	backoffInitialDelay time.Duration
	backoffMaxDelay     time.Duration
	backoffJitterFactor float64
	retryPolicySet      bool // Track if WithRetryPolicy was explicitly called

	usePq  bool
	pqFunc func(a any) int

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

		cfg.initialDelay = initialDelay
		cfg.retryPolicySet = true
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

// WithBackoff sets the retry backoff algorithm and its parameters.
// This allows fine-grained control over retry behavior.
//
// Parameters:
//   - backoffType: The backoff algorithm to use (Exponential, Jittered, Decorrelated, Adaptive)
//   - initialDelay: The initial delay before the first retry
//   - maxDelay: The maximum delay between retries (prevents unbounded growth)
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithRetryPolicy(5, 100*time.Millisecond),
//	    WithBackoff(BackoffDecorrelated, 100*time.Millisecond, 5*time.Second),
//	)
func WithBackoff(backoffType BackoffType, initialDelay, maxDelay time.Duration) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.backoffType = backoffType
		cfg.backoffInitialDelay = initialDelay
		cfg.backoffMaxDelay = maxDelay
	}
}

// WithJitteredBackoff is a convenience option for jittered backoff with custom jitter factor.
// jitterFactor should be between 0.0 and 1.0 (e.g., 0.1 = ±10% jitter, 0.2 = ±20% jitter).
// This adds randomization to exponential backoff to prevent thundering herd problems.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithRetryPolicy(5, 100*time.Millisecond),
//	    WithJitteredBackoff(100*time.Millisecond, 5*time.Second, 0.2), // ±20% jitter
//	)
func WithJitteredBackoff(initialDelay, maxDelay time.Duration, jitterFactor float64) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.backoffType = BackoffJittered
		cfg.backoffInitialDelay = initialDelay
		cfg.backoffMaxDelay = maxDelay
		cfg.backoffJitterFactor = jitterFactor
	}
}

// WithDecorrelatedJitter is a convenience option for AWS-style decorrelated jitter.
// This is the recommended backoff strategy for high-retry scenarios as it has been
// proven to reduce retry traffic by 95% in AWS production systems.
//
// Best for scenarios with:
//   - Many concurrent failures and retries
//   - External service dependencies that may throttle
//   - Retry storms that need to be dampened
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithRetryPolicy(5, 50*time.Millisecond),
//	    WithDecorrelatedJitter(50*time.Millisecond, 3*time.Second),
//	)
func WithDecorrelatedJitter(initialDelay, maxDelay time.Duration) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.backoffType = BackoffDecorrelated
		cfg.backoffInitialDelay = initialDelay
		cfg.backoffMaxDelay = maxDelay
	}
}

func WithPriorityQueue[T any](checkPrior func(a T) int) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.usePq = true
		cfg.pqFunc = func(a any) int {
			if t, ok := a.(T); ok {
				return checkPrior(t)
			}
			return 0
		}
	}
}

// getTypeName returns a string representation of the type for validation.
// This uses fmt.Sprintf with %T which is sufficient for type checking.
func getTypeName(v any) string {
	return fmt.Sprintf("%T", v)
}
