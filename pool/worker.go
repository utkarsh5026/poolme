package pool

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// worker is the core worker function that processes tasks from the task channel.
// It includes panic recovery to prevent a single task from crashing the entire pool.
// This function is generic over the key type K to support different task types.
func worker[T any, R any, K comparable](
	wp *WorkerPool[T, R],
	ctx context.Context,
	taskChan <-chan task[T, K],
	resultChan chan<- Result[R, K],
	processFn ProcessFunc[T, R],
) error {
	for {
		select {
		case t, ok := <-taskChan:
			if !ok {
				return nil
			}

			actualTask := t.Task()
			result, err := executeTask(ctx, wp, actualTask, processFn)

			select {
			case resultChan <- Result[R, K]{Value: result, Error: err, Key: t.Key()}:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err != nil && !wp.continueOnError {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// executeTask encapsulates the common logic for executing a task with hooks, rate limiting, and processing.
// This function is used by both worker and runSubmitWorker to avoid code duplication.
// It handles rate limiting, hook execution (beforeTaskStart and onTaskEnd), and task processing with retry.
func executeTask[T, R any](
	ctx context.Context,
	wp *WorkerPool[T, R],
	task T,
	processFn ProcessFunc[T, R],
) (R, error) {
	if wp.rateLimiter != nil {
		if err := wp.rateLimiter.Wait(ctx); err != nil {
			var zero R
			return zero, err
		}
	}

	if wp.beforeTaskStart != nil {
		wp.beforeTaskStart(task)
	}

	result, err := processWithRecovery(ctx, wp, task, processFn)

	if wp.onTaskEnd != nil {
		wp.onTaskEnd(task, result, err)
	}

	return result, err
}

// processWithRecovery executes a task with panic recovery and retry logic.
// If a panic occurs, it's converted to an error to prevent crashing the worker.
// Retries use exponential backoff if initialDelay is configured.
func processWithRecovery[T, R any](
	ctx context.Context,
	wp *WorkerPool[T, R],
	task T,
	processFn ProcessFunc[T, R],
) (result R, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			err = fmt.Errorf("worker panic: %v\nstack trace:\n%s", r, buf[:n])
		}
	}()

	return processWithRetry(ctx, task, wp, processFn)
}

// processWithRetry executes the given processFn for the task, retrying up to wp.maxAttempts times on error.
// It uses the configured backoff strategy to calculate delays between retries.
// If wp.onRetry is set, it is called before each retry (i.e., on every failure except the last).
// The function will respect context cancellation and abort early if the context is done.
// On success, it returns the result and nil error; otherwise, the final error is returned (after retries).
func processWithRetry[T, R any](
	ctx context.Context,
	task T,
	wp *WorkerPool[T, R],
	processFn ProcessFunc[T, R],
) (R, error) {
	var result R
	var err error
	maxAttempts := max(wp.maxAttempts, 1)

	for attempt := range maxAttempts {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if attempt > 0 && wp.backoffStrategy != nil {
			delay := wp.backoffStrategy.NextDelay(attempt-1, err)
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

		if wp.onRetry != nil && attempt < maxAttempts-1 {
			wp.onRetry(task, attempt+1, err)
		}
	}

	return result, err
}
