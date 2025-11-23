// Package pool provides a small, well-documented, generic worker pool
// for concurrent task processing.
//
// The primary type is WorkerPool[T, R], a configurable pool of workers
// which process tasks of type T and return results of type R. The pool
// supports context-aware processing, panic recovery, retry logic with
// exponential backoff, rate limiting, and configurable worker and buffer
// sizes via functional options.
//
// # Basic Usage
//
//	ctx := context.Background()
//	tasks := []int{1, 2, 3, 4}
//	pool := NewWorkerPool[int, int](WithWorkerCount(4))
//	results, err := pool.Process(ctx, tasks, func(ctx context.Context, t int) (int, error) {
//	    return t * 2, nil
//	})
//
// # Processing Options
//
// The pool supports three processing modes:
//
//   - Process: Processes a slice of tasks and returns results in the same order
//   - ProcessMap: Processes a map of tasks and returns a map of results with matching keys
//   - ProcessStream: Processes tasks from a channel and returns a results channel
//
// # Retry Logic
//
// Tasks can be automatically retried with exponential backoff on failure:
//
//	pool := NewWorkerPool[string, string](
//	    WithWorkerCount(4),
//	    WithRetryPolicy(3, 100*time.Millisecond), // 3 attempts, 100ms initial delay
//	)
//	results, err := pool.Process(ctx, tasks, processFn)
//
// Retry delays increase exponentially: 100ms, 200ms, 400ms, etc.
//
// # Rate Limiting
//
// Control throughput to prevent overwhelming external services:
//
//	pool := NewWorkerPool[string, APIResponse](
//	    WithWorkerCount(10),
//	    WithRateLimit(5.0, 10), // 5 tasks/sec, burst of 10
//	)
//	results, err := pool.Process(ctx, tasks, callAPI)
//
// # Map Processing
//
// For tasks naturally represented as maps:
//
//	tasks := map[string]int{"a": 1, "b": 2, "c": 3}
//	results, err := pool.ProcessMap(ctx, tasks, func(ctx context.Context, t int) (int, error) {
//	    return t * 2, nil
//	})
//	// results: map[string]int{"a": 2, "b": 4, "c": 6}
//
// # Streaming Processing
//
// For dynamic task streams:
//
//	taskChan := make(chan Task)
//	go func() {
//	    defer close(taskChan)
//	    for _, task := range tasks {
//	        taskChan <- task
//	    }
//	}()
//	resultChan, errChan := pool.ProcessStream(ctx, taskChan, processFn)
//	for result := range resultChan {
//	    // handle result
//	}
//	if err := <-errChan; err != nil {
//	    // handle error
//	}
//
// # Configuration Options
//
//   - WithWorkerCount(n): Set number of concurrent workers (default: GOMAXPROCS)
//   - WithTaskBuffer(n): Set task channel buffer size (default: worker count)
//   - WithRetryPolicy(maxAttempts, initialDelay): Enable retry with exponential backoff
//   - WithRateLimit(tasksPerSecond, burst): Enable rate limiting for controlled throughput
//
// # Error Handling
//
// The pool uses fail-fast semantics: when any worker encounters an error,
// processing stops and the error is returned. Panic recovery is built-in,
// converting panics to errors with stack traces to prevent worker crashes.
//
// The Result type provides detailed task-level information including the
// original task index, enabling partial result recovery and error analysis.
//
// The package is designed to be small and idiomatic for Go 1.18+ (generics).
package pool
