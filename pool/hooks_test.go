package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestHooksBasic demonstrates basic hook usage
func TestHooksBasic(t *testing.T) {
	testFunc := func(t *testing.T, strategy strategyConfig) {
		var mu sync.Mutex
		events := []string{}

		opts := append(strategy.opts,
			WithBeforeTaskStart(func(task int) {
				mu.Lock()
				events = append(events, fmt.Sprintf("start:%d", task))
				mu.Unlock()
			}),
			WithOnTaskEnd(func(task int, result string, err error) {
				mu.Lock()
				if err != nil {
					events = append(events, fmt.Sprintf("end:%d:error", task))
				} else {
					events = append(events, fmt.Sprintf("end:%d:%s", task, result))
				}
				mu.Unlock()
			}),
		)

		wp := NewWorkerPool[int, string](opts...)

		tasks := []int{1, 2, 3}
		results, err := wp.Process(context.Background(), tasks, func(ctx context.Context, task int) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return fmt.Sprintf("result-%d", task), nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}

		// Verify all events were recorded
		mu.Lock()
		defer mu.Unlock()

		if len(events) != 6 { // 3 starts + 3 ends
			t.Errorf("expected 6 events, got %d: %v", len(events), events)
		}

		// Check that we have start and end for each task
		for _, task := range tasks {
			startFound := false
			endFound := false
			for _, event := range events {
				if event == fmt.Sprintf("start:%d", task) {
					startFound = true
				}
				if event == fmt.Sprintf("end:%d:result-%d", task, task) {
					endFound = true
				}
			}
			if !startFound {
				t.Errorf("start event not found for task %d", task)
			}
			if !endFound {
				t.Errorf("end event not found for task %d", task)
			}
		}
	}
	runStrategyTest(t, testFunc, 2)
}

// TestHooksWithRetry demonstrates retry hook usage
func TestHooksWithRetry(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		var mu sync.Mutex
		retries := make(map[int][]int) // task -> attempts

		opts := append(s.opts,
			WithRetryPolicy(3, 10*time.Millisecond),
			WithOnEachAttempt(func(task int, attempt int, err error) {
				mu.Lock()
				retries[task] = append(retries[task], attempt)
				mu.Unlock()
			}),
		)

		wp := NewWorkerPool[int, string](opts...)

		attemptCount := 0
		tasks := []int{1}

		_, err := wp.Process(context.Background(), tasks, func(ctx context.Context, task int) (string, error) {
			attemptCount++
			if attemptCount < 3 {
				return "", errors.New("temporary error")
			}
			return "success", nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		// Should have 2 retry attempts (attempts 1 and 2 failed, 3 succeeded)
		if len(retries[1]) != 2 {
			t.Errorf("expected 2 retry callbacks, got %d: %v", len(retries[1]), retries[1])
		}

		// Verify attempt numbers
		expectedAttempts := []int{1, 2}
		for i, attempt := range retries[1] {
			if attempt != expectedAttempts[i] {
				t.Errorf("expected attempt %d, got %d", expectedAttempts[i], attempt)
			}
		}
	}, 1)
}

// TestHooksWithError demonstrates hook behavior with errors
func TestHooksWithError(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		var mu sync.Mutex
		var lastError error

		opts := append(s.opts,
			WithOnTaskEnd(func(task int, result string, err error) {
				mu.Lock()
				if err != nil {
					lastError = err
				}
				mu.Unlock()
			}),
		)

		wp := NewWorkerPool[int, string](opts...)

		tasks := []int{1, 2, 3}
		_, err := wp.Process(context.Background(), tasks, func(ctx context.Context, task int) (string, error) {
			if task == 2 {
				return "", errors.New("task 2 failed")
			}
			return fmt.Sprintf("result-%d", task), nil
		})

		if err == nil {
			t.Fatal("expected error but got nil")
		}

		mu.Lock()
		defer mu.Unlock()

		if lastError == nil {
			t.Error("expected lastError to be set by hook")
		}
	}, 2)
}

// TestHooksTypeSafety verifies that type mismatches panic at initialization
func TestHooksTypeSafety(t *testing.T) {
	t.Run("beforeTaskStart type mismatch", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for type mismatch")
			}
			msg := fmt.Sprintf("%v", r)
			if !contains(msg, "WithBeforeTaskStart") && !contains(msg, "type") {
				t.Errorf("panic message doesn't mention type issue: %s", msg)
			}
		}()

		// This should panic because we're using string hook with int pool
		stringHook := WithBeforeTaskStart(func(task string) {})
		_ = NewWorkerPool[int, string](stringHook)
	})

	t.Run("onTaskEnd type mismatch", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for type mismatch")
			}
			msg := fmt.Sprintf("%v", r)
			if !contains(msg, "WithOnTaskEnd") && !contains(msg, "type") {
				t.Errorf("panic message doesn't mention type issue: %s", msg)
			}
		}()

		// This should panic because result type is wrong
		wrongHook := WithOnTaskEnd(func(task int, result int, err error) {})
		_ = NewWorkerPool[int, string](wrongHook)
	})

	t.Run("onRetry type mismatch", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for type mismatch")
			}
			msg := fmt.Sprintf("%v", r)
			if !contains(msg, "WithOnEachAttempt") && !contains(msg, "type") {
				t.Errorf("panic message doesn't mention type issue: %s", msg)
			}
		}()

		// This should panic because task type is wrong
		wrongHook := WithOnEachAttempt(func(task string, attempt int, err error) {})
		_ = NewWorkerPool[int, string](wrongHook)
	})
}

// TestHooksWithProcessMap demonstrates hooks work with ProcessMap
func TestHooksWithProcessMap(t *testing.T) {
	runStrategyTest(t, func(t *testing.T, s strategyConfig) {
		var mu sync.Mutex
		processedKeys := []string{}

		opts := append(s.opts,
			WithBeforeTaskStart(func(task string) {
				mu.Lock()
				processedKeys = append(processedKeys, task)
				mu.Unlock()
			}),
		)

		wp := NewWorkerPool[string, int](opts...)

		tasks := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		results, err := wp.ProcessMap(context.Background(), tasks, func(ctx context.Context, task string) (int, error) {
			return len(task), nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 results, got %d", len(results))
		}

		mu.Lock()
		defer mu.Unlock()

		if len(processedKeys) != 3 {
			t.Errorf("expected 3 processed keys, got %d", len(processedKeys))
		}
	}, 2)
}
