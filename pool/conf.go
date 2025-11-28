package pool

import (
	"fmt"
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"github.com/utkarsh5026/poolme/internal/scheduler"
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

// SchedulingStrategyType defines the task scheduling algorithm to use.
type SchedulingStrategyType = scheduler.SchedulingStrategyType

const (
	// SchedulingChannel uses a simple channel-based strategy (default).
	// All workers pull from a single shared channel. Simple and efficient for most use cases.
	// Best for: General purpose workloads with uniform task complexity.
	SchedulingChannel SchedulingStrategyType = iota

	// SchedulingWorkStealing uses a work-stealing algorithm with per-worker queues.
	// Each worker has its own local queue and steals from others when idle.
	// Provides better cache locality and automatic load balancing.
	// Best for: CPU-intensive tasks, recursive workloads, and scenarios with variable task complexity.
	// Based on algorithms used in Go's runtime, Java's ForkJoinPool, and .NET's TPL.
	SchedulingWorkStealing

	// SchedulingPriorityQueue uses a priority queue with task prioritization.
	// Tasks are processed based on their priority value.
	// Best for: Workloads where certain tasks must be processed before others.
	// Requires WithPriorityQueue to be set with a priority function.
	SchedulingPriorityQueue

	// SchedulingMPMC uses a lock-free multi-producer multi-consumer queue.
	// Multiple producers can submit tasks concurrently with minimal contention.
	// Multiple consumers (workers) can dequeue tasks concurrently.
	// Best for: High-throughput scenarios with many concurrent submitters.
	// Can be configured as bounded (fixed capacity) or unbounded (dynamic growth).
	SchedulingMPMC

	// SchedulingBitmask uses a bitmask-based scheduling with per-worker channels.
	// Uses a 64-bit atomic bitmask to track worker idle/busy state (1 = idle, 0 = busy).
	// Each worker has its own dedicated channel, with a fallback global queue for overflow.
	// Best for: Low-latency task dispatch with up to 64 workers, minimal lock contention.
	// Limited to maximum 64 workers due to bitmask size.
	SchedulingBitmask
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

	schedulingStrategy scheduler.SchedulingStrategyType

	usePq    bool
	lessFunc func(a, b any) bool

	// MPMC queue configuration
	mpmcBounded  bool
	mpmcCapacity int

	// Task Fusion configuration
	useFusion       bool
	fusionWindow    time.Duration
	fusionBatchSize int

	// Task affinity configuration
	affinityFunc     func(any) string
	affinityFuncType string

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

// WithPriorityQueue configures the pool to use a priority queue with a custom comparison function.
// The lessFunc should return true if task 'a' has higher priority than task 'b'.
// This allows flexible priority ordering based on any task properties.
//
// Example (lower values have higher priority):
//
//	type Task struct { Priority int }
//	pool := NewWorkerPool[Task, string](
//	    WithPriorityQueue(func(a, b Task) bool {
//	        return a.Priority < b.Priority
//	    }),
//	)
//
// Example (earlier deadlines have higher priority):
//
//	type Task struct { Deadline time.Time }
//	pool := NewWorkerPool[Task, string](
//	    WithPriorityQueue(func(a, b Task) bool {
//	        return a.Deadline.Before(b.Deadline)
//	    }),
//	)
func WithPriorityQueue[T any](lessFunc func(a, b T) bool) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.usePq = true
		cfg.schedulingStrategy = scheduler.SchedulingPriorityQueue
		cfg.lessFunc = func(a, b any) bool {
			ta, okA := a.(T)
			tb, okB := b.(T)
			if okA && okB {
				return lessFunc(ta, tb)
			}
			return false
		}
	}
}

// WithSchedulingStrategy sets the task scheduling algorithm to use.
// This determines how tasks are distributed to and processed by workers.
//
// Available strategies:
//   - SchedulingChannel: Simple shared channel (default) - best for general use
//   - SchedulingWorkStealing: Per-worker queues with work stealing - best for CPU-intensive tasks
//   - SchedulingPriorityQueue: Priority-based processing - requires WithPriorityQueue
//   - SchedulingMPMC: Lock-free multi-producer multi-consumer queue - best for high-throughput
//   - SchedulingBitmask: Bitmask-based with per-worker channels - best for low-latency (max 64 workers)
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithSchedulingStrategy(SchedulingWorkStealing),
//	)
func WithSchedulingStrategy(strategy SchedulingStrategyType) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.schedulingStrategy = strategy
	}
}

// WithWorkStealing is a convenience option to enable work-stealing scheduling.
// This is equivalent to WithSchedulingStrategy(SchedulingWorkStealing).
//
// Work-stealing provides:
//   - Better cache locality (LIFO local queue processing)
//   - Automatic load balancing (idle workers steal from busy workers)
//   - Reduced lock contention (workers primarily use their own queues)
//   - Better scalability for CPU-intensive workloads
//
// Best for:
//   - CPU-bound tasks with variable complexity
//   - Recursive or divide-and-conquer algorithms
//   - Workloads where some tasks spawn additional tasks
//   - High-throughput scenarios with many concurrent tasks
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(8),
//	    WithWorkStealing(),
//	)
func WithWorkStealing() WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.schedulingStrategy = SchedulingWorkStealing
	}
}

// WithMPMCQueue is a convenience option to enable MPMC queue scheduling.
// This is equivalent to WithSchedulingStrategy(SchedulingMPMC).
// By default, creates an unbounded queue that grows dynamically.
//
// MPMC (Multi-Producer Multi-Consumer) queue provides:
//   - Lock-free concurrent enqueue/dequeue operations
//   - Minimal contention with many concurrent submitters
//   - Better scalability for high-throughput scenarios
//   - Efficient ring buffer implementation
//
// Best for:
//   - High-throughput scenarios with many concurrent task submitters
//   - Workloads where multiple goroutines submit tasks simultaneously
//   - Scenarios requiring predictable low-latency task submission
//   - Applications with bursty task submission patterns
//
// Example (unbounded):
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(8),
//	    WithMPMCQueue(),
//	)
//
// Example (bounded with capacity):
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(8),
//	    WithMPMCQueue(WithBoundedQueue(1000)),
//	)
func WithMPMCQueue(opts ...MPMCOption) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.schedulingStrategy = SchedulingMPMC

		// Apply MPMC-specific options
		for _, opt := range opts {
			opt(cfg)
		}
	}
}

// MPMCOption is a function that configures MPMC-specific settings
type MPMCOption func(*workerPoolConfig)

// WithBoundedQueue configures the MPMC queue as bounded with a fixed capacity.
// When the queue is full, Submit operations will return an error (ErrQueueFull).
// This is useful for applying backpressure and preventing unbounded memory growth.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithMPMCQueue(WithBoundedQueue(1000)),
//	)
func WithBoundedQueue(capacity int) MPMCOption {
	return func(cfg *workerPoolConfig) {
		cfg.mpmcBounded = true
		cfg.mpmcCapacity = capacity
	}
}

// WithUnboundedQueue explicitly configures the MPMC queue as unbounded.
// This is the default behavior, but can be used for clarity.
// The queue will grow dynamically as needed.
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithMPMCQueue(WithUnboundedQueue()),
//	)
func WithUnboundedQueue() MPMCOption {
	return func(cfg *workerPoolConfig) {
		cfg.mpmcBounded = false
		cfg.mpmcCapacity = 0 // Use default initial capacity
	}
}

// WithBitmask is a convenience option to enable bitmask-based scheduling.
// This is equivalent to WithSchedulingStrategy(SchedulingBitmask).
//
// Bitmask scheduling provides:
//   - Ultra-low latency task dispatch using atomic operations
//   - Per-worker dedicated channels for direct task assignment
//   - Lock-free worker idle/busy tracking via 64-bit bitmask
//   - Automatic fallback to global queue when all workers are busy
//   - Minimal contention and cache-line bouncing
//
// Limitations:
//   - Maximum 64 workers (enforced by 64-bit bitmask)
//   - If more workers are configured, only first 64 will be used
//
// Best for:
//   - Latency-sensitive applications requiring fast task dispatch
//   - Workloads with moderate worker counts (≤64)
//   - Scenarios where direct worker assignment is beneficial
//   - Systems requiring predictable, low-overhead scheduling
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(32),
//	    WithBitmask(),
//	)
func WithBitmask() WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		cfg.schedulingStrategy = SchedulingBitmask
	}
}

// WithTaskFusion enables task batching/fusion to improve throughput.
// Tasks are accumulated in batches before being dispatched to workers.
//
// Task fusion provides:
//   - Reduced scheduling overhead by processing tasks in batches
//   - Better throughput for high-volume, small task workloads
//   - Automatic flushing based on time window or batch size
//   - Lower lock contention through batch operations
//
// Parameters:
//   - window: Maximum time to wait before flushing accumulated tasks
//   - batchSize: Maximum number of tasks to accumulate before forcing a flush
//
// Best for:
//   - High-throughput scenarios with many small tasks
//   - I/O-bound workloads where batching reduces overhead
//   - Scenarios where small delays are acceptable for better throughput
//   - Systems with high task submission rates
//
// Example:
//
//	pool := NewWorkerPool[int, string](
//	    WithWorkerCount(8),
//	    WithTaskFusion(100*time.Millisecond, 50), // Flush every 100ms or 50 tasks
//	)
func WithTaskFusion(window time.Duration, batchSize int) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if window > 0 && batchSize > 0 {
			cfg.useFusion = true
			cfg.fusionWindow = window
			cfg.fusionBatchSize = batchSize
		}
	}
}

// WithAffinity configures task affinity to ensure tasks with the same affinity key
// are routed to the same worker. This is useful for workloads where task locality matters,
// such as caching, stateful processing, or reducing lock contention.
//
// The affinityFunc receives a task and returns a string key. Tasks with the same key
// will always be processed by the same worker using a fast FNV-1a hash function.
//
// Task affinity provides:
//   - Consistent routing of related tasks to the same worker
//   - Better cache locality for stateful processing
//   - Reduced lock contention when tasks share resources
//   - Predictable worker assignment for debugging and monitoring
//
// Best for:
//   - Workloads with per-key state (e.g., session processing, user-specific tasks)
//   - Tasks that benefit from warm caches (e.g., database connections per tenant)
//   - Scenarios where task ordering matters for specific keys
//   - Reducing coordination overhead between workers
//
// Example (route by user ID):
//
//	type UserTask struct {
//	    UserID int
//	    Action string
//	}
//	pool := NewWorkerPool[UserTask, string](
//	    WithWorkerCount(8),
//	    WithAffinity(func(task UserTask) string {
//	        return fmt.Sprintf("user-%d", task.UserID)
//	    }),
//	)
//
// Example (route by tenant):
//
//	type Request struct {
//	    TenantID string
//	    Data     []byte
//	}
//	pool := NewWorkerPool[Request, Response](
//	    WithAffinity(func(req Request) string {
//	        return req.TenantID
//	    }),
//	)
func WithAffinity[T any](affinityFunc func(task T) string) WorkerPoolOption {
	return func(cfg *workerPoolConfig) {
		if affinityFunc == nil {
			return
		}
		cfg.affinityFunc = func(task any) string {
			if t, ok := task.(T); ok {
				return affinityFunc(t)
			}
			return ""
		}
		var zero T
		cfg.affinityFuncType = getTypeName(zero)
	}
}

// getTypeName returns a string representation of the type for validation.
// This uses fmt.Sprintf with %T which is sufficient for type checking.
func getTypeName(v any) string {
	return fmt.Sprintf("%T", v)
}
