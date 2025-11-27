package scheduler

import (
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"golang.org/x/time/rate"
)

type SchedulingStrategyType int

const (
	SchedulingChannel SchedulingStrategyType = iota
	SchedulingWorkStealing
	SchedulingPriorityQueue
	SchedulingMPMC
	SchedulingBitmask
)

// processorConfig holds all configuration for a pool of workers and task scheduling.
// This enables flexible tuning of core pool behavior, retry, backoff, queueing, and hooks.
type ProcessorConfig[T, R any] struct {
	// Number of worker goroutines in the pool.
	WorkerCount int

	// Size of the internal task/result buffer (impacts batching & throughput).
	TaskBuffer int

	// Maximum number of processing attempts per task (for retry logic).
	MaxAttempts int

	// Optional token bucket rate limiter applied per task (may be nil).
	RateLimiter *rate.Limiter

	// Initial delay before first retry attempt.
	InitialDelay time.Duration

	// If true, worker should continue on task error (don't abort processing on first failure).
	ContinueOnErr bool

	// The scheduling strategy used for distributing tasks (Channel, WorkStealing, PriorityQueue, etc).
	SchedulingStrategy SchedulingStrategyType

	// Hook called before a task starts (can be used for logging/tracing).
	BeforeTaskStart func(T)

	// Hook called after a task ends (receives the input, result, and error if any).
	OnTaskEnd func(T, R, error)

	// Hook called on retry with the task, current attempt, and last error.
	OnRetry func(T, int, error)

	// Backoff calculation strategy used between retries (exponential, fixed, etc.).
	BackoffStrategy algorithms.BackoffStrategy

	// If true, enable priority queue for scheduling.
	UsePq bool

	// Function that compares two tasks, returns true if 'a' has higher priority than 'b'.
	LessFunc func(a, b T) bool

	// MPMC queue configuration:

	// Use a bounded queue (if true) or unbounded.
	MpmcBounded bool

	// Explicit queue capacity if using bounded queue.
	MpmcCapacity int

	// Task Fusion configuration:

	// If true, wrap the scheduling strategy with fusion (batching) strategy.
	UseFusion bool

	// Time window to accumulate tasks before flushing (used when UseFusion is true).
	FusionWindow time.Duration

	// Maximum batch size before forcing a flush (used when UseFusion is true).
	FusionBatchSize int
}
