package pool

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_Start(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			defer pool.Shutdown(time.Second)

			// Verify pool is started
			if pool.state == nil {
				t.Error("pool state should not be nil after start")
			}
			if !pool.state.started.Load() {
				t.Error("pool should be marked as started")
			}
		}, 4)
	})

	t.Run("double start fails", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("first start failed: %v", err)
			}
			defer pool.Shutdown(time.Second)

			err = pool.Start(context.Background(), processFn)
			if err == nil {
				t.Error("expected error on second start")
			}
			if err.Error() != "pool already started" {
				t.Errorf("expected 'pool already started', got %v", err)
			}
		}, 2)
	})

	t.Run("start with cancelled context", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			err := pool.Start(ctx, processFn)
			if err != nil {
				t.Fatalf("start should succeed even with cancelled context: %v", err)
			}
			defer pool.Shutdown(time.Second)

			// Try to submit - should fail or return context cancelled
			future, err := pool.Submit(1)
			if err == nil {
				// Use GetWithContext with a short timeout instead of blocking Get
				getCtx, getCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer getCancel()
				_, _, err = future.GetWithContext(getCtx)
				if err == nil {
					t.Error("expected error with cancelled context")
				}
			}
		}, 2)
	})
}

func TestWorkerPool_Shutdown(t *testing.T) {
	t.Run("successful shutdown", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			err = pool.Shutdown(time.Second)
			if err != nil {
				t.Errorf("shutdown failed: %v", err)
			}

			if !pool.state.shutdown.Load() {
				t.Error("pool should be marked as shutdown")
			}
		}, 2)
	})

	t.Run("shutdown without start fails", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			err := pool.Shutdown(time.Second)
			if err == nil {
				t.Error("expected error when shutting down non-started pool")
			}
			if err.Error() != "pool not started" {
				t.Errorf("expected 'pool not started', got %v", err)
			}
		}, 2)
	})

	t.Run("double shutdown fails", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			err = pool.Shutdown(time.Second)
			if err != nil {
				t.Fatalf("first shutdown failed: %v", err)
			}

			err = pool.Shutdown(time.Second)
			if err == nil {
				t.Error("expected error on second shutdown")
			}
			if err.Error() != "pool already shut down" {
				t.Errorf("expected 'pool already shut down', got %v", err)
			}
		}, 2)
	})

	t.Run("shutdown with zero timeout", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				time.Sleep(10 * time.Millisecond)
				return "result", nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			// Submit some tasks
			for i := 0; i < 5; i++ {
				_, _ = pool.Submit(i)
			}

			// Shutdown with zero timeout (wait indefinitely)
			err = pool.Shutdown(0)
			if err != nil {
				t.Errorf("shutdown with zero timeout should succeed: %v", err)
			}
		}, 2)
	})
}

func TestWorkerPool_Shutdown_GracefulWait(t *testing.T) {
	t.Run("waits for in-flight tasks", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, int](s.opts...)

			var completedCount atomic.Int32

			processFn := func(ctx context.Context, task int) (int, error) {
				time.Sleep(100 * time.Millisecond)
				completedCount.Add(1)
				return task * 2, nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			// Submit tasks
			numTasks := 10
			futures := make([]*Future[int, int64], numTasks)
			for i := 0; i < numTasks; i++ {
				future, err := pool.Submit(i)
				if err != nil {
					t.Fatalf("failed to submit task %d: %v", i, err)
				}
				futures[i] = future
			}

			// Shutdown with generous timeout
			err = pool.Shutdown(5 * time.Second)
			if err != nil {
				t.Errorf("shutdown failed: %v", err)
			}

			// All tasks should be completed
			completed := completedCount.Load()
			if completed != int32(numTasks) {
				t.Errorf("expected %d completed tasks, got %d", numTasks, completed)
			}

			// All futures should have results - verify by getting them
			for i, future := range futures {
				value, _, err := future.Get()
				if err != nil {
					t.Errorf("future %d failed: %v", i, err)
				}
				expected := i * 2
				if value != expected {
					t.Errorf("future %d: expected %d, got %d", i, expected, value)
				}
			}
		}, 2)
	})
}

func TestWorkerPool_Shutdown_Timeout(t *testing.T) {
	t.Run("timeout exceeded", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, int](s.opts...)

			processFn := func(ctx context.Context, task int) (int, error) {
				// Long-running task that ignores context
				time.Sleep(2 * time.Second)
				return task, nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			// Submit task
			_, err = pool.Submit(1)
			if err != nil {
				t.Fatalf("failed to submit task: %v", err)
			}

			// Shutdown with short timeout
			start := time.Now()
			err = pool.Shutdown(100 * time.Millisecond)
			elapsed := time.Since(start)

			if err == nil {
				t.Error("expected timeout error")
			}
			if !errors.Is(err, ErrShutdownTimeout) {
				t.Errorf("expected ErrShutdownTimeout, got %v", err)
			}

			// Should timeout around 100ms, not wait for full 2s
			if elapsed > 500*time.Millisecond {
				t.Errorf("shutdown took too long: %v", elapsed)
			}
		}, 1)
	})

	t.Run("completes before timeout", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, int](s.opts...)

			processFn := func(ctx context.Context, task int) (int, error) {
				time.Sleep(50 * time.Millisecond)
				return task, nil
			}

			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("failed to start: %v", err)
			}

			// Submit tasks
			for i := 0; i < 5; i++ {
				_, _ = pool.Submit(i)
			}

			// Shutdown with generous timeout
			err = pool.Shutdown(2 * time.Second)
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		}, 2)
	})
}

func TestWorkerPool_Shutdown_InFlightTasks(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		pool := NewScheduler[int, string](s.opts...)

		var startedCount atomic.Int32
		var completedCount atomic.Int32

		processFn := func(ctx context.Context, task int) (string, error) {
			startedCount.Add(1)
			time.Sleep(100 * time.Millisecond)
			completedCount.Add(1)
			return fmt.Sprintf("result-%d", task), nil
		}

		err := pool.Start(context.Background(), processFn)
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Submit tasks
		numTasks := 20
		futures := make([]*Future[string, int64], numTasks)
		for i := 0; i < numTasks; i++ {
			future, err := pool.Submit(i)
			if err != nil {
				t.Fatalf("failed to submit task %d: %v", i, err)
			}
			futures[i] = future
			time.Sleep(5 * time.Millisecond) // Stagger submissions
		}

		// Wait a bit for some tasks to start
		time.Sleep(50 * time.Millisecond)

		// Shutdown
		err = pool.Shutdown(5 * time.Second)
		if err != nil {
			t.Errorf("shutdown failed: %v", err)
		}

		started := startedCount.Load()
		completed := completedCount.Load()

		t.Logf("Started: %d, Completed: %d out of %d tasks", started, completed, numTasks)

		// All started tasks should complete
		if started != completed {
			t.Errorf("all started tasks should complete: started=%d, completed=%d", started, completed)
		}

		// Count how many futures have results by trying to get them with a timeout
		readyCount := 0
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		for _, future := range futures {
			_, _, err := future.GetWithContext(ctx)
			if err == nil {
				readyCount++
			}
		}

		if readyCount != int(completed) {
			t.Errorf("ready futures (%d) should match completed tasks (%d)", readyCount, completed)
		}
	}, 3)
}

func TestWorkerPool_StartShutdown_Cycle(t *testing.T) {
	t.Run("cannot restart after shutdown", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			pool := NewScheduler[int, string](s.opts...)

			processFn := func(ctx context.Context, task int) (string, error) {
				return "result", nil
			}

			// First cycle
			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("first start failed: %v", err)
			}

			err = pool.Shutdown(time.Second)
			if err != nil {
				t.Fatalf("shutdown failed: %v", err)
			}

			// Try to restart - this should fail because we don't reset state
			err = pool.Start(context.Background(), processFn)
			if err == nil {
				t.Error("expected error when restarting after shutdown")
			}
		}, 2)
	})
}

func TestWorkerPool_Lifecycle_Integration(t *testing.T) {
	t.Run("full lifecycle with submit and shutdown", func(t *testing.T) {
		runStrategyTest(t, func(t *testing.T, s strategyConfig) {
			opts := append(s.opts, WithTaskBuffer(10))
			pool := NewScheduler[int, int](opts...)

			processFn := func(ctx context.Context, task int) (int, error) {
				time.Sleep(20 * time.Millisecond)
				return task * task, nil
			}

			// Start
			err := pool.Start(context.Background(), processFn)
			if err != nil {
				t.Fatalf("start failed: %v", err)
			}

			// Submit tasks
			numTasks := 50
			futures := make([]*Future[int, int64], numTasks)

			for i := 0; i < numTasks; i++ {
				future, err := pool.Submit(i)
				if err != nil {
					t.Fatalf("submit task %d failed: %v", i, err)
				}
				futures[i] = future
			}

			// Collect half the results
			for i := 0; i < numTasks/2; i++ {
				value, _, err := futures[i].Get()
				if err != nil {
					t.Errorf("task %d failed: %v", i, err)
				}
				expected := i * i
				if value != expected {
					t.Errorf("task %d: expected %d, got %d", i, expected, value)
				}
			}

			// Shutdown
			err = pool.Shutdown(5 * time.Second)
			if err != nil {
				t.Errorf("shutdown failed: %v", err)
			}

			// Collect remaining results
			for i := numTasks / 2; i < numTasks; i++ {
				value, _, err := futures[i].Get()
				if err != nil {
					t.Errorf("task %d failed: %v", i, err)
				}
				expected := i * i
				if value != expected {
					t.Errorf("task %d: expected %d, got %d", i, expected, value)
				}
			}
		}, 4)
	})
}

func TestWorkerPool_Lifecycle_ContextCancellation(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		ctx, cancel := context.WithCancel(context.Background())

		pool := NewScheduler[int, int](s.opts...)

		processFn := func(ctx context.Context, task int) (int, error) {
			select {
			case <-time.After(200 * time.Millisecond):
				return task, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}

		err := pool.Start(ctx, processFn)
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}

		// Submit tasks
		numTasks := 10
		futures := make([]*Future[int, int64], numTasks)
		for i := 0; i < numTasks; i++ {
			future, err := pool.Submit(i)
			if err != nil {
				t.Fatalf("submit task %d failed: %v", i, err)
			}
			futures[i] = future
		}

		// Cancel context
		time.Sleep(50 * time.Millisecond)
		cancel()

		// Wait a bit for cancellation to propagate
		time.Sleep(100 * time.Millisecond)

		// Shutdown
		err = pool.Shutdown(time.Second)
		if err != nil {
			t.Errorf("shutdown failed: %v", err)
		}

		// Check that most futures have errors due to cancellation
		// Use GetWithContext to avoid blocking indefinitely if context was cancelled
		errorCount := 0
		getCtx, getCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer getCancel()

		for _, future := range futures {
			_, _, err := future.GetWithContext(getCtx)
			if err != nil {
				errorCount++
			}
		}

		// At least some tasks should have been cancelled
		if errorCount == 0 {
			t.Error("expected some tasks to be cancelled")
		}

		t.Logf("Cancelled tasks: %d out of %d", errorCount, numTasks)
	}, 3)
}
