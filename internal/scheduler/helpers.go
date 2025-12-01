package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/utkarsh5026/poolme/internal/types"
)

var (
	ErrSchedulerClosed error = errors.New("scheduler is closed")
)

// nextPowerOfTwo returns the next power of 2 >= n
func nextPowerOfTwo(n int) int {
	if n <= 0 {
		return 1
	}

	if n&(n-1) == 0 {
		return n
	}

	power := 1
	for power < n {
		power *= 2
	}
	return power
}

// executeSubmitted executes a submitted task and sends the result to its associated future.
// This function is called by workers to process tasks that were submitted via SubmitWithFuture.
// It wraps the task execution logic and ensures the result is properly delivered to the waiting future.
func executeSubmitted[T, R any](ctx context.Context, s *types.SubmittedTask[T, R], conf *ProcessorConfig[T, R], executor types.ProcessFunc[T, R], handler types.ResultHandler[T, R]) error {
	result, err := executeTask(ctx, conf, s.Task, executor)
	handler(s, types.NewResult(result, s.Id, err))
	return err
}

// executeTask encapsulates the common logic for executing a task with hooks, rate limiting, and processing.
// This function is used by both worker and runSubmitWorker to avoid code duplication.
// It handles rate limiting, hook execution (BeforeTaskStart and onTaskEnd), and task processing with retry.
func executeTask[T, R any](
	ctx context.Context,
	conf *ProcessorConfig[T, R],
	task T,
	processFn types.ProcessFunc[T, R],
) (R, error) {
	if conf.RateLimiter != nil {
		if err := conf.RateLimiter.Wait(ctx); err != nil {
			var zero R
			return zero, err
		}
	}

	if conf.BeforeTaskStart != nil {
		conf.BeforeTaskStart(task)
	}

	result, err := processWithRecovery(ctx, conf, task, processFn)

	if conf.OnTaskEnd != nil {
		conf.OnTaskEnd(task, result, err)
	}

	return result, err
}

// processWithRecovery executes a task with panic recovery and retry logic.
// If a panic occurs, it's converted to an error to prevent crashing the worker.
// Retries use exponential backoff if initialDelay is configured.
func processWithRecovery[T, R any](
	ctx context.Context,
	conf *ProcessorConfig[T, R],
	task T,
	processFn types.ProcessFunc[T, R],
) (result R, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			err = fmt.Errorf("worker panic: %v\nstack trace:\n%s", r, buf[:n])
		}
	}()

	return processWithRetry(ctx, task, conf, processFn)
}

// processWithRetry executes the given processFn for the task, retrying up to wp.maxAttempts times on error.
// It uses the configured backoff strategy to calculate delays between retries.
// If wp.onRetry is set, it is called before each retry (i.e., on every failure except the last).
// The function will respect context cancellation and abort early if the context is done.
// On success, it returns the result and nil error; otherwise, the final error is returned (after retries).
func processWithRetry[T, R any](
	ctx context.Context,
	task T,
	conf *ProcessorConfig[T, R],
	processFn types.ProcessFunc[T, R],
) (R, error) {
	var result R
	var err error
	maxAttempts := max(conf.MaxAttempts, 1)

	for attempt := range maxAttempts {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if attempt > 0 && conf.BackoffStrategy != nil {
			delay := conf.BackoffStrategy.NextDelay(attempt-1, err)
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return result, ctx.Err()
				}
			}
		}

		result, err = processFn(ctx, task)
		if err == nil {
			return result, nil
		}

		if conf.OnRetry != nil && attempt < maxAttempts-1 {
			conf.OnRetry(task, attempt+1, err)
		}
	}

	return result, err
}

// handleExecutionError handles errors from executeSubmitted with standardized logic.
// Returns the error if the worker should stop, or nil if it should continue.
// Calls drainFunc before returning for context errors.
func handleExecutionError(err error, continueOnErr bool, drainFunc func()) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		drainFunc()
		return err
	}

	if !continueOnErr {
		return err
	}

	return nil
}

// handleWithCare wraps task execution and error handling for a submitted task.
// It executes the given task using the provided processor configuration and process function.
// If an error occurs during execution, it delegates error handling to handleExecutionError,
// which determines whether to continue processing or stop based on the configuration.
func handleWithCare[T, R any](
	ctx context.Context,
	s *types.SubmittedTask[T, R],
	conf *ProcessorConfig[T, R],
	f types.ProcessFunc[T, R],
	handler types.ResultHandler[T, R],
	drainFunc func(),
) error {
	result, err := executeTask(ctx, conf, s.Task, f)
	handler(s, types.NewResult(result, s.Id, err))

	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		drainFunc()
		return err
	}

	if !conf.ContinueOnErr {
		return err
	}

	return nil
}
