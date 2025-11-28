package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_ContinueOnError_Process_StopsOnError(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			var processedCount atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				processedCount.Add(1)
				if task == 5 {
					return 0, errors.New("error on task 5")
				}
				return task * 2, nil
			}

			_, err := pool.Process(context.Background(), tasks, processFn)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if err.Error() != "error on task 5" {
				t.Errorf("expected 'error on task 5', got %v", err)
			}

			// Some tasks should have been processed before the error
			if processedCount.Load() == 0 {
				t.Error("expected some tasks to be processed")
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_Process_ContinuesOnError(t *testing.T) {
	strategies := getAllStrategiesWithOpts(4, WithContinueOnError(true))

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			var processedCount atomic.Int32
			var errorCount atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				processedCount.Add(1)
				if task%3 == 0 {
					errorCount.Add(1)
					return 0, fmt.Errorf("error on task %d", task)
				}
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)

			// Should process all tasks despite errors
			if processedCount.Load() != int32(len(tasks)) {
				t.Errorf("expected all %d tasks to be processed, got %d", len(tasks), processedCount.Load())
			}

			// Should still return an error (from collection)
			if err == nil {
				t.Error("expected error to be returned even with continueOnError=true")
			}

			// Results array should have the correct length
			if len(results) != len(tasks) {
				t.Errorf("expected results length %d, got %d", len(tasks), len(results))
			}

			// Verify successful results are present
			for i, task := range tasks {
				if task%3 != 0 {
					expected := task * 2
					if results[i] != expected {
						t.Errorf("task %d: expected %d, got %d", task, expected, results[i])
					}
				}
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_Process_AllTasksFail(t *testing.T) {
	strategies := getAllStrategiesWithOpts(2, WithContinueOnError(true))

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5}
			var processedCount atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				processedCount.Add(1)
				return 0, fmt.Errorf("error on task %d", task)
			}

			_, err := pool.Process(context.Background(), tasks, processFn)

			// Should process all tasks
			if processedCount.Load() != int32(len(tasks)) {
				t.Errorf("expected all %d tasks to be processed, got %d", len(tasks), processedCount.Load())
			}

			// Should return error
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_ProcessMap_StopsOnError(t *testing.T) {
	// Default behavior: stop on first error
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := map[string]int{
				"a": 1,
				"b": 2,
				"c": 3,
				"d": 4,
				"e": 5,
			}

			processFn := func(ctx context.Context, task int) (int, error) {
				if task == 3 {
					return 0, errors.New("error on task 3")
				}
				return task * 2, nil
			}

			_, err := pool.ProcessMap(context.Background(), tasks, processFn)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if err.Error() != "error on task 3" {
				t.Errorf("expected 'error on task 3', got %v", err)
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_ProcessMap_ContinuesOnError(t *testing.T) {
	strategies := getAllStrategiesWithOpts(4, WithContinueOnError(true))

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := map[string]int{
				"a": 1,
				"b": 2,
				"c": 3,
				"d": 4,
				"e": 5,
				"f": 6,
			}

			var processedCount atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				processedCount.Add(1)
				if task%2 == 0 {
					return 0, fmt.Errorf("error on task %d", task)
				}
				return task * 2, nil
			}

			results, err := pool.ProcessMap(context.Background(), tasks, processFn)

			// Should process all tasks despite errors
			if processedCount.Load() != int32(len(tasks)) {
				t.Errorf("expected all %d tasks to be processed, got %d", len(tasks), processedCount.Load())
			}

			// Should still return an error (from collection)
			if err == nil {
				t.Error("expected error to be returned even with continueOnError=true")
			}

			// Verify successful results are present
			for key, task := range tasks {
				if task%2 != 0 {
					expected := task * 2
					if result, ok := results[key]; !ok {
						t.Errorf("missing result for key %s", key)
					} else if result != expected {
						t.Errorf("key %s: expected %d, got %d", key, expected, result)
					}
				}
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_WithRetry(t *testing.T) {
	strategies := getAllStrategiesWithOpts(2, WithContinueOnError(true), WithRetryPolicy(3, 10*time.Millisecond))

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5}
			var attemptCounts sync.Map

			processFn := func(ctx context.Context, task int) (int, error) {
				val, _ := attemptCounts.LoadOrStore(task, new(atomic.Int32))
				count := val.(*atomic.Int32).Add(1)

				// Task 2 and 4 always fail
				if task == 2 || task == 4 {
					return 0, fmt.Errorf("error on task %d", task)
				}

				// Task 3 succeeds on second attempt
				if task == 3 && count < 2 {
					return 0, errors.New("temporary failure")
				}

				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)

			// Should continue processing all tasks despite some failures
			if err == nil {
				t.Error("expected error due to failed tasks")
			}

			// Verify successful tasks have results
			if results[0] != 2 { // task 1
				t.Errorf("task 1: expected 2, got %d", results[0])
			}
			if results[2] != 6 { // task 3 (succeeds on retry)
				t.Errorf("task 3: expected 6, got %d", results[2])
			}
			if results[4] != 10 { // task 5
				t.Errorf("task 5: expected 10, got %d", results[4])
			}

			// Verify retry counts
			val2, _ := attemptCounts.Load(2)
			if count2 := val2.(*atomic.Int32).Load(); count2 != 3 {
				t.Errorf("task 2: expected 3 attempts, got %d", count2)
			}

			val3, _ := attemptCounts.Load(3)
			if count3 := val3.(*atomic.Int32).Load(); count3 != 2 {
				t.Errorf("task 3: expected 2 attempts, got %d", count3)
			}

			val4, _ := attemptCounts.Load(4)
			if count4 := val4.(*atomic.Int32).Load(); count4 != 3 {
				t.Errorf("task 4: expected 3 attempts, got %d", count4)
			}
		})
	}
}

func TestWorkerPool_ContinueOnError_WithHooks(t *testing.T) {
	strategies := getAllStrategies(2)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			var taskEndCalls atomic.Int32
			var taskEndErrors atomic.Int32

			opts := append(strategy.opts,
				WithContinueOnError(true),
				WithOnTaskEnd(func(task int, result int, err error) {
					taskEndCalls.Add(1)
					if err != nil {
						taskEndErrors.Add(1)
					}
				}),
			)

			pool := NewWorkerPool[int, int](opts...)

			tasks := []int{1, 2, 3, 4, 5}

			processFn := func(ctx context.Context, task int) (int, error) {
				if task%2 == 0 {
					return 0, fmt.Errorf("error on task %d", task)
				}
				return task * 2, nil
			}

			pool.Process(context.Background(), tasks, processFn)

			// Verify all tasks called the hook
			if taskEndCalls.Load() != int32(len(tasks)) {
				t.Errorf("expected %d task end calls, got %d", len(tasks), taskEndCalls.Load())
			}

			// Verify correct number of errors
			expectedErrors := int32(2) // tasks 2 and 4
			if taskEndErrors.Load() != expectedErrors {
				t.Errorf("expected %d task end errors, got %d", expectedErrors, taskEndErrors.Load())
			}
		})
	}
}
