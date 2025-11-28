package pool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// strategyConfig defines a test configuration for a scheduling strategy
type strategyConfig struct {
	name string
	opts []WorkerPoolOption
}

// getAllStrategies returns all scheduling strategies to test
// Each strategy is configured with appropriate options
func getAllStrategies(workerCount int) []strategyConfig {
	return []strategyConfig{
		{
			name: "Channel",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingWorkStealing),
			},
		},
		{
			name: "MPMC",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithMPMCQueue(WithBoundedQueue(1000)), // bounded with reasonable capacity
			},
		},
		{
			name: "PriorityQueue",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithPriorityQueue(func(a, b int) bool {
					return a < b
				}),
			},
		},
		{
			name: "Bitmask",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingBitmask),
			},
		},
	}
}

// getAllStrategiesWithOpts returns all scheduling strategies with additional options
func getAllStrategiesWithOpts(workerCount int, additionalOpts ...WorkerPoolOption) []strategyConfig {
	baseStrategies := getAllStrategies(workerCount)
	for i := range baseStrategies {
		baseStrategies[i].opts = append(baseStrategies[i].opts, additionalOpts...)
	}
	return baseStrategies
}

func TestWorkerPool_Process_BasicFunctionality(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
			processFn := func(ctx context.Context, task int) (int, error) {
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != len(tasks) {
				t.Fatalf("expected %d results, got %d", len(tasks), len(results))
			}

			for i, task := range tasks {
				expected := task * 2
				if results[i] != expected {
					t.Errorf("task %d: expected %d, got %d", i, expected, results[i])
				}
			}
		})
	}
}

func TestWorkerPool_Process_EmptyTasks(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{}
			processFn := func(ctx context.Context, task int) (int, error) {
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != 0 {
				t.Fatalf("expected 0 results, got %d", len(results))
			}
		})
	}
}

func TestWorkerPool_Process_SingleTask(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{42}
			processFn := func(ctx context.Context, task int) (int, error) {
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}

			if results[0] != 84 {
				t.Errorf("expected 84, got %d", results[0])
			}
		})
	}
}

func TestWorkerPool_Process_ErrorHandling(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5}
			expectedErr := errors.New("processing error")

			processFn := func(ctx context.Context, task int) (int, error) {
				if task == 3 {
					return 0, expectedErr
				}
				return task * 2, nil
			}

			_, err := pool.Process(context.Background(), tasks, processFn)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !errors.Is(err, expectedErr) {
				t.Errorf("expected error %v, got %v", expectedErr, err)
			}
		})
	}
}

func TestWorkerPool_Process_ContextCancellation(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			ctx, cancel := context.WithCancel(context.Background())
			tasks := make([]int, 100)
			for i := range tasks {
				tasks[i] = i
			}

			var processedCount atomic.Int32
			processFn := func(ctx context.Context, task int) (int, error) {
				// Cancel after processing a few tasks
				if processedCount.Add(1) == 5 {
					cancel()
				}
				time.Sleep(10 * time.Millisecond) // Simulate work
				return task * 2, nil
			}

			_, err := pool.Process(ctx, tasks, processFn)
			if err == nil {
				t.Fatal("expected context cancellation error, got nil")
			}

			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		})
	}
}

func TestWorkerPool_Process_ContextTimeout(t *testing.T) {
	strategies := getAllStrategies(2)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			tasks := []int{1, 2, 3, 4, 5}
			processFn := func(ctx context.Context, task int) (int, error) {
				time.Sleep(100 * time.Millisecond) // Exceed timeout
				return task * 2, nil
			}

			_, err := pool.Process(ctx, tasks, processFn)
			if err == nil {
				t.Fatal("expected timeout error, got nil")
			}

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("expected context.DeadlineExceeded, got %v", err)
			}
		})
	}
}

func TestWorkerPool_Process_PanicRecovery(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := []int{1, 2, 3, 4, 5}
			processFn := func(ctx context.Context, task int) (int, error) {
				if task == 3 {
					panic("intentional panic")
				}
				return task * 2, nil
			}

			_, err := pool.Process(context.Background(), tasks, processFn)
			if err == nil {
				t.Fatal("expected panic recovery error, got nil")
			}

			errStr := err.Error()
			if !contains(errStr, "worker panic") || !contains(errStr, "intentional panic") {
				t.Errorf("expected panic recovery error message, got: %v", err)
			}
		})
	}
}

func TestWorkerPool_Process_Concurrency(t *testing.T) {
	workerCount := 4
	strategies := getAllStrategies(workerCount)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := make([]int, 100)
			for i := range tasks {
				tasks[i] = i
			}

			var activeWorkers atomic.Int32
			var maxConcurrent atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				current := activeWorkers.Add(1)
				defer activeWorkers.Add(-1)

				// Track max concurrent workers
				for {
					max := maxConcurrent.Load()
					if current <= max || maxConcurrent.CompareAndSwap(max, current) {
						break
					}
				}

				time.Sleep(10 * time.Millisecond) // Simulate work
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != len(tasks) {
				t.Fatalf("expected %d results, got %d", len(tasks), len(results))
			}

			// Verify we actually used concurrent workers
			if maxConcurrent.Load() < int32(workerCount) {
				t.Errorf("expected at least %d concurrent workers, got %d", workerCount, maxConcurrent.Load())
			}
		})
	}
}

func TestWorkerPool_ProcessMap_BasicFunctionality(t *testing.T) {
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
				return task * 2, nil
			}

			results, err := pool.ProcessMap(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != len(tasks) {
				t.Fatalf("expected %d results, got %d", len(tasks), len(results))
			}

			for key, task := range tasks {
				expected := task * 2
				if result, ok := results[key]; !ok {
					t.Errorf("missing result for key %s", key)
				} else if result != expected {
					t.Errorf("key %s: expected %d, got %d", key, expected, result)
				}
			}
		})
	}
}

func TestWorkerPool_ProcessMap_EmptyMap(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := map[string]int{}
			processFn := func(ctx context.Context, task int) (int, error) {
				return task * 2, nil
			}

			results, err := pool.ProcessMap(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != 0 {
				t.Fatalf("expected 0 results, got %d", len(results))
			}
		})
	}
}

func TestWorkerPool_ProcessMap_ErrorHandling(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := map[string]int{
				"a": 1,
				"b": 2,
				"c": 3,
			}

			expectedErr := errors.New("processing error")
			processFn := func(ctx context.Context, task int) (int, error) {
				if task == 2 {
					return 0, expectedErr
				}
				return task * 2, nil
			}

			_, err := pool.ProcessMap(context.Background(), tasks, processFn)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !errors.Is(err, expectedErr) {
				t.Errorf("expected error %v, got %v", expectedErr, err)
			}
		})
	}
}

func TestWorkerPool_WithOptions(t *testing.T) {
	tests := []struct {
		name        string
		opts        []WorkerPoolOption
		wantWorkers int
		wantBuffer  int
	}{
		{
			name:        "default options",
			opts:        nil,
			wantWorkers: -1, // Will use runtime.GOMAXPROCS(0)
			wantBuffer:  -1, // Will equal worker count
		},
		{
			name:        "custom worker count",
			opts:        []WorkerPoolOption{WithWorkerCount(8)},
			wantWorkers: 8,
			wantBuffer:  8,
		},
		{
			name:        "custom buffer size",
			opts:        []WorkerPoolOption{WithTaskBuffer(16)},
			wantWorkers: -1,
			wantBuffer:  16,
		},
		{
			name:        "custom worker and buffer",
			opts:        []WorkerPoolOption{WithWorkerCount(4), WithTaskBuffer(32)},
			wantWorkers: 4,
			wantBuffer:  32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](tt.opts...)

			if tt.wantWorkers > 0 && pool.conf.WorkerCount != tt.wantWorkers {
				t.Errorf("expected %d workers, got %d", tt.wantWorkers, pool.conf.WorkerCount)
			}

			if tt.wantBuffer > 0 && pool.conf.TaskBuffer != tt.wantBuffer {
				t.Errorf("expected buffer size %d, got %d", tt.wantBuffer, pool.conf.TaskBuffer)
			}
		})
	}
}

func TestWorkerPool_Process_OrderPreservation(t *testing.T) {
	strategies := getAllStrategies(4)

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := make([]int, 100)
			for i := range tasks {
				tasks[i] = i
			}

			processFn := func(ctx context.Context, task int) (int, error) {
				// Add variable delay to test order preservation
				time.Sleep(time.Duration(100-task) * time.Microsecond)
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify order is preserved
			for i, task := range tasks {
				expected := task * 2
				if results[i] != expected {
					t.Errorf("index %d: expected %d, got %d (order not preserved)", i, expected, results[i])
				}
			}
		})
	}
}

func TestWorkerPool_Process_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high concurrency test in short mode")
	}

	workerCount := 16
	taskCount := 10000

	strategies := []strategyConfig{
		{
			name: "Channel",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingChannel),
			},
		},
		{
			name: "WorkStealing",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingWorkStealing),
			},
		},
		{
			name: "MPMC",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithMPMCQueue(WithUnboundedQueue()), // Use unbounded queue for high concurrency
			},
		},
		{
			name: "PriorityQueue",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithPriorityQueue(func(a, b int) bool {
					return a < b
				}),
			},
		},
		{
			name: "Bitmask",
			opts: []WorkerPoolOption{
				WithWorkerCount(workerCount),
				WithSchedulingStrategy(SchedulingBitmask),
			},
		},
	}

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			pool := NewWorkerPool[int, int](strategy.opts...)

			tasks := make([]int, taskCount)
			for i := range tasks {
				tasks[i] = i
			}

			var counter atomic.Int64
			processFn := func(ctx context.Context, task int) (int, error) {
				counter.Add(1)
				return task * 2, nil
			}

			results, err := pool.Process(context.Background(), tasks, processFn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != taskCount {
				t.Fatalf("expected %d results, got %d", taskCount, len(results))
			}

			if counter.Load() != int64(taskCount) {
				t.Errorf("expected %d tasks processed, got %d", taskCount, counter.Load())
			}
		})
	}
}

// Benchmark tests
func BenchmarkWorkerPool_Process(b *testing.B) {
	pool := NewWorkerPool[int, int](WithWorkerCount(8))

	tasks := make([]int, 1000)
	for i := range tasks {
		tasks[i] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		// Simulate some work
		sum := 0
		for i := 0; i < 100; i++ {
			sum += i
		}
		return task * 2, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.Process(context.Background(), tasks, processFn)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkWorkerPool_ProcessMap(b *testing.B) {
	pool := NewWorkerPool[int, int](WithWorkerCount(8))

	tasks := make(map[string]int, 1000)
	for i := 0; i < 1000; i++ {
		tasks[fmt.Sprintf("key_%d", i)] = i
	}

	processFn := func(ctx context.Context, task int) (int, error) {
		sum := 0
		for i := 0; i < 100; i++ {
			sum += i
		}
		return task * 2, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.ProcessMap(context.Background(), tasks, processFn)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// Retry logic tests

func TestCalcBackoffDelay(t *testing.T) {
	tests := []struct {
		name          string
		initialDelay  time.Duration
		attemptNumber int
		expected      time.Duration
	}{
		{
			name:          "first retry (attempt 0)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 0,
			expected:      100 * time.Millisecond, // 2^0 = 1
		},
		{
			name:          "second retry (attempt 1)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 1,
			expected:      200 * time.Millisecond, // 2^1 = 2
		},
		{
			name:          "third retry (attempt 2)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 2,
			expected:      400 * time.Millisecond, // 2^2 = 4
		},
		{
			name:          "fourth retry (attempt 3)",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: 3,
			expected:      800 * time.Millisecond, // 2^3 = 8
		},
		{
			name:          "negative attempt number",
			initialDelay:  100 * time.Millisecond,
			attemptNumber: -1,
			expected:      0,
		},
		{
			name:          "zero initial delay",
			initialDelay:  0,
			attemptNumber: 2,
			expected:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calcBackoffDelay(tt.initialDelay, tt.attemptNumber)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
