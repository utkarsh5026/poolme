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

			if wp.rateLimiter != nil {
				if err := wp.rateLimiter.Wait(ctx); err != nil {
					return err
				}
			}

			actualTask := t.Task()
			if wp.beforeTaskStart != nil {
				wp.beforeTaskStart(actualTask)
			}

			result, err := processWithRecovery(ctx, wp, actualTask, processFn)
			if wp.onTaskEnd != nil {
				wp.onTaskEnd(actualTask, result, err)
			}

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

	maxAttempts := max(wp.maxAttempts, 1)

	for attempt := range maxAttempts {
		if attempt > 0 && wp.initialDelay > 0 {
			backoffDelay := calcBackoffDelay(wp.initialDelay, attempt-1)
			select {
			case <-time.After(backoffDelay):
			case <-ctx.Done():
				return result, ctx.Err()
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
