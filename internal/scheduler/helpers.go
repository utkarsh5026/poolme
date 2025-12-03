package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
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
			// Rate limiter's error doesn't wrap context errors, so check context explicitly
			if ctxErr := ctx.Err(); ctxErr != nil {
				return zero, ctxErr
			}
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

// workerSignal provides a thread-safe signaling mechanism for coordinating worker availability.
// Unlike quitter which signals shutdown by closing the channel, workerSignal sends discrete
// signals to indicate worker state changes (e.g., worker becoming available).
// It combines signaling with safe closure to prevent panics when sending on closed channels.
type workerSignal struct {
	sig    chan signal // Channel for sending and receiving availability signals
	closed atomic.Bool // Atomic flag indicating whether the channel is closed
	once   sync.Once   // Ensures the channel is closed exactly once
}

// newWorkerSignal creates a new workerSignal instance with a buffered signal channel.
// The channel has a buffer of 100 to prevent signal loss when workers haven't started yet.
func newWorkerSignal() *workerSignal {
	return &workerSignal{
		sig: make(chan signal, 1),
	}
}

// Close safely closes the signal channel.
// This method is safe to call multiple times and from multiple goroutines.
// Only the first call will close the channel; subsequent calls are no-ops.
// After closing, Signal() calls will be no-ops and Wait() will return a closed channel.
func (ws *workerSignal) Close() {
	ws.once.Do(func() {
		ws.closed.Store(true)
		close(ws.sig)
	})
}

// IsClosed returns true if Close has been called, false otherwise.
// This provides a non-blocking way to check if the signal channel has been closed.
func (ws *workerSignal) IsClosed() bool {
	return ws.closed.Load()
}

// Signal attempts to send a signal on the channel without blocking.
// If the channel is full or closed, the signal is dropped (non-blocking send).
// This is useful for notifying listeners that a worker has become available
// without blocking the signaling goroutine.
func (ws *workerSignal) Signal() {
	if ws.IsClosed() {
		return
	}

	select {
	case ws.sig <- signal{}:
	default:
	}
}

// Wait returns a receive-only channel that can be used to wait for signals.
// Callers can select on this channel to be notified when Signal() is called.
// If the signal has been closed, this will return a closed channel.
func (ws *workerSignal) Wait() <-chan signal {
	return ws.sig
}

type workerRunner[T, R any] struct {
	config    *ProcessorConfig[T, R]
	scheduler SchedulingStrategy[T, R]
}

func newWorkerRunner[T, R any](conf *ProcessorConfig[T, R], s SchedulingStrategy[T, R]) *workerRunner[T, R] {
	return &workerRunner[T, R]{
		config:    conf,
		scheduler: s,
	}
}

func (w *workerRunner[T, R]) Execute(ctx context.Context, s *types.SubmittedTask[T, R], f types.ProcessFunc[T, R], h types.ResultHandler[T, R], d drainFunc) error {
	result, err := w.executeTask(ctx, s.Task, f)
	h(s, types.NewResult(result, s.Id, err))

	if err == nil {
		return nil
	}

	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !w.config.ContinueOnErr {
		w.scheduler.Shutdown()
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if d != nil {
			d()
		}
		return err
	}

	if !w.config.ContinueOnErr {
		return err
	}

	return nil
}

// executeTask encapsulates the common logic for executing a task with hooks, rate limiting, and processing.
// This function is used by both worker and runSubmitWorker to avoid code duplication.
// It handles rate limiting, hook execution (BeforeTaskStart and onTaskEnd), and task processing with retry.
func (w *workerRunner[T, R]) executeTask(
	ctx context.Context,
	task T,
	f types.ProcessFunc[T, R],
) (R, error) {
	if w.config.RateLimiter != nil {
		if err := w.config.RateLimiter.Wait(ctx); err != nil {
			var zero R
			if ctxErr := ctx.Err(); ctxErr != nil {
				return zero, ctxErr
			}
			return zero, err
		}
	}

	if w.config.BeforeTaskStart != nil {
		w.config.BeforeTaskStart(task)
	}

	result, err := w.processWithRecovery(ctx, task, f)

	if w.config.OnTaskEnd != nil {
		w.config.OnTaskEnd(task, result, err)
	}

	return result, err
}

// processWithRecovery executes a task with panic recovery and retry logic.
// If a panic occurs, it's converted to an error to prevent crashing the worker.
// Retries use exponential backoff if initialDelay is configured.
func (w *workerRunner[T, R]) processWithRecovery(
	ctx context.Context,
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

	return w.processWithRetry(ctx, task, processFn)
}

// processWithRetry executes the given processFn for the task, retrying up to wp.maxAttempts times on error.
// It uses the configured backoff strategy to calculate delays between retries.
// If wp.onRetry is set, it is called before each retry (i.e., on every failure except the last).
// The function will respect context cancellation and abort early if the context is done.
// On success, it returns the result and nil error; otherwise, the final error is returned (after retries).
func (w *workerRunner[T, R]) processWithRetry(
	ctx context.Context,
	task T,
	processFn types.ProcessFunc[T, R],
) (R, error) {
	var result R
	var err error
	maxAttempts := max(w.config.MaxAttempts, 1)

	for attempt := range maxAttempts {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if attempt > 0 && w.config.BackoffStrategy != nil {
			delay := w.config.BackoffStrategy.NextDelay(attempt-1, err)
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

		if w.config.OnRetry != nil && attempt < maxAttempts-1 {
			w.config.OnRetry(task, attempt+1, err)
		}
	}

	return result, err
}
