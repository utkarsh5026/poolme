package pool

import (
	"context"
	"fmt"
	"math"
	"time"
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

func checkfuncs[T any, R any](cfg *workerPoolConfig, expectedTaskType, expectedResultType string) (beforeTaskStart func(T), onTaskEnd func(T, R, error), onRetry func(T, int, error)) {
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

	return beforeTaskStart, onTaskEnd, onRetry
}

// produceFromChannel produces tasks from an input channel and sends them to taskChan.
// It wraps each task with a dummy index (-1) and handles context cancellation.
// The channel is closed when the input channel is closed or context is cancelled.
func produceFromChannel[T any](ctx context.Context, taskChan chan<- task[T, int], inputChan <-chan T) {
	defer close(taskChan)
	for t := range inputChan {
		select {
		case <-ctx.Done():
			return
		default:
			taskChan <- &indexedTask[T]{task: t, index: -1}
		}
	}
}
