package pool

import "time"

// WorkerPoolOption is a functional option for configuring the worker pool.
type WorkerPoolOption func(*workerPoolConfig)

type workerPoolConfig struct {
	workerCount  int
	taskBuffer   int
	maxAttempts  int
	initialDelay time.Duration
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
