package pool

import (
	"time"

	"golang.org/x/time/rate"
)

// WorkerPoolOption is a functional option for configuring the worker pool.
type WorkerPoolOption func(*workerPoolConfig)

type workerPoolConfig struct {
	workerCount  int
	taskBuffer   int
	maxAttempts  int
	initialDelay time.Duration
	rateLimiter  *rate.Limiter
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
// initialDelay specifies the delay before the first retry, subsequent retries
// will use exponential backoff. If not specified, no retries are performed.
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
