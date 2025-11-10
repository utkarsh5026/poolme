// Package pool provides a small, well-documented, generic worker pool
// for concurrent task processing.
//
// The primary type is WorkerPool[T, R], a configurable pool of workers
// which process tasks of type T and return results of type R. The pool
// supports context-aware processing, panic recovery, and configurable
// worker and buffer sizes via functional options.
//
// Basic usage:
//
//	ctx := context.Background()
//	tasks := []int{1,2,3,4}
//	pool := NewWorkerPool[int, int](WithWorkerCount(4))
//	results, err := pool.Process(ctx, tasks, func(ctx context.Context, t int) (int, error) {
//	    return t * 2, nil
//	})
//
// For map inputs use `ProcessMap` which returns a map of results keyed by
// the input map's keys. For streaming tasks use `ProcessStream` which takes
// an input channel and returns a results channel and an error channel.
//
// The package is designed to be small and idiomatic for Go 1.18+ (generics).
package pool
