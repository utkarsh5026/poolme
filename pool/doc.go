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
// The pool supports two operational modes:
//
// 1. Batch Mode (stateless): Process collections of tasks
//   - Process: Processes a slice of tasks and returns results in the same order
//   - ProcessMap: Processes a map of tasks and returns a map of results with matching keys
//   - ProcessStream: Processes tasks from a channel and returns a results channel
//
// 2. Long-Running Mode (stateful): Submit individual tasks with futures
//   - Start/Submit/Shutdown: Initialize pool once, submit tasks as needed, get futures for async results
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
// # Long-Running Pool with Submit
//
// For persistent worker pools with individual task submission and future-based results:
//
//	pool := NewWorkerPool[int, string](WithWorkerCount(4))
//	err := pool.Start(ctx, func(ctx context.Context, task int) (string, error) {
//	    return fmt.Sprintf("processed-%d", task), nil
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer pool.Shutdown(5 * time.Second)
//
//	// Submit individual tasks
//	future, err := pool.Submit(42)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Get result (blocks until ready)
//	result, key, err := future.Get()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Or use context-aware get
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//	result, key, err = future.GetWithContext(ctx)
//
//	// Or try non-blocking get
//	result, key, err, ready := future.TryGet()
//	if ready {
//	    fmt.Printf("Result: %v\n", result)
//	}
//
// # Future Methods
//
// Future objects provide multiple ways to retrieve results:
//
//   - Get(): Blocks until result is available, can be called multiple times
//   - GetWithContext(ctx): Blocks with context cancellation support
//   - TryGet(): Non-blocking, returns immediately with ready status
//   - Done(): Returns channel that closes when result is ready
//   - IsReady(): Quick check if result is available
//
// # Choosing Between Batch and Long-Running Mode
//
// Use Batch Mode (Process/ProcessMap/ProcessStream) when:
//   - You have a known collection of tasks to process
//   - Workers can be ephemeral (created per batch)
//   - You want results collected in a single operation
//
// Use Long-Running Mode (Start/Submit/Shutdown) when:
//   - Tasks arrive dynamically over time
//   - You want persistent workers to avoid startup overhead
//   - You need individual futures for each task
//   - You want fine-grained control over result retrieval timing
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
