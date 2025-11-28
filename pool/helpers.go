package pool

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/utkarsh5026/poolme/internal/algorithms"
	"github.com/utkarsh5026/poolme/internal/scheduler"
)

var (
	ErrShutdownTimeout = errors.New("error in shutting down: timeout reached")
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
//   - affinityFunc: Function to map tasks to affinity keys for worker routing (or nil if not configured).
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
	affinityFunc func(T) string,
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

	if cfg.affinityFunc != nil {
		if cfg.affinityFuncType != expectedTaskType {
			panic(fmt.Sprintf("WithAffinity expects task type %s, but pool processes type %s",
				cfg.affinityFuncType, expectedTaskType))
		}
		affinityFunc = func(task T) string {
			return cfg.affinityFunc(task)
		}
	}

	return beforeTaskStart, onTaskEnd, onRetry, affinityFunc
}

// waitUntil blocks until either the done channel is closed or the timeout is reached.
// It is used during graceful shutdown to wait for workers to complete their tasks.
func waitUntil(d <-chan struct{}, timeout time.Duration) error {
	if timeout <= 0 {
		<-d
		return nil
	}

	select {
	case <-d:
		return nil
	case <-time.After(timeout):
		return ErrShutdownTimeout
	}
}

// createConfig constructs a scheduler.ProcessorConfig from the provided WorkerPoolOptions.
// It applies default values for any options not explicitly set by the user.
// It also validates and wraps user-supplied hook functions to ensure type safety.
func createConfig[T, R any](opts ...WorkerPoolOption) *scheduler.ProcessorConfig[T, R] {
	cfg := &workerPoolConfig{
		workerCount:         runtime.GOMAXPROCS(0),
		taskBuffer:          0, // Will be set to workerCount if not specified
		maxAttempts:         1,
		initialDelay:        0,
		backoffType:         BackoffExponential, // Default backoff
		backoffInitialDelay: 100 * time.Millisecond,
		backoffMaxDelay:     5 * time.Second,
		backoffJitterFactor: 0.1, // Default 10% jitter for jittered backoff
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.taskBuffer == 0 {
		cfg.taskBuffer = cfg.workerCount
	}

	if cfg.retryPolicySet {
		cfg.backoffInitialDelay = cfg.initialDelay
	}

	backoffStrategy := algorithms.NewBackoffStrategy(
		cfg.backoffType,
		cfg.backoffInitialDelay,
		cfg.backoffMaxDelay,
		cfg.backoffJitterFactor,
	)

	var zeroT T
	var zeroR R
	expectedTaskType := fmt.Sprintf("%T", zeroT)
	expectedResultType := fmt.Sprintf("%T", zeroR)

	beforeTaskStart, onTaskEnd, onRetry, affinityFunc := checkfuncs[T, R](cfg, expectedTaskType, expectedResultType)

	return &scheduler.ProcessorConfig[T, R]{
		WorkerCount:        cfg.workerCount,
		TaskBuffer:         cfg.taskBuffer,
		MaxAttempts:        cfg.maxAttempts,
		InitialDelay:       cfg.initialDelay,
		RateLimiter:        cfg.rateLimiter,
		BeforeTaskStart:    beforeTaskStart,
		OnTaskEnd:          onTaskEnd,
		OnRetry:            onRetry,
		ContinueOnErr:      cfg.continueOnError,
		BackoffStrategy:    backoffStrategy,
		SchedulingStrategy: cfg.schedulingStrategy,
		UsePq:              cfg.usePq,
		// The type of cfg.lessFunc is func(a, b any) bool, but processorConfig expects func(a, b T) bool
		// Provide an adapter if lessFunc is non-nil
		LessFunc: func() func(a, b T) bool {
			if cfg.lessFunc == nil {
				return nil
			}
			return func(a, b T) bool {
				return cfg.lessFunc(any(a), any(b))
			}
		}(),
		MpmcBounded:  cfg.mpmcBounded,
		MpmcCapacity: cfg.mpmcCapacity,
		AffinityFunc: affinityFunc,
	}
}
