package types

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFuture_Get(t *testing.T) {
	t.Run("successful result", func(t *testing.T) {
		future := NewFuture[string, int]()

		// Send result in background
		go func() {
			time.Sleep(50 * time.Millisecond)
			future.result <- Result[string, int]{
				Value: "success",
				Key:   42,
				Error: nil,
			}
		}()

		value, key, err := future.Get()

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if value != "success" {
			t.Errorf("expected value 'success', got %v", value)
		}
		if key != 42 {
			t.Errorf("expected key 42, got %v", key)
		}
	})

	t.Run("error result", func(t *testing.T) {
		future := NewFuture[string, int]()
		expectedErr := errors.New("task failed")

		go func() {
			future.result <- Result[string, int]{
				Value: "",
				Key:   10,
				Error: expectedErr,
			}
		}()

		value, key, err := future.Get()

		if err != expectedErr {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
		if value != "" {
			t.Errorf("expected empty value, got %v", value)
		}
		if key != 10 {
			t.Errorf("expected key 10, got %v", key)
		}
	})

	t.Run("multiple Get calls return same result", func(t *testing.T) {
		future := NewFuture[int, string]()

		go func() {
			future.result <- Result[int, string]{
				Value: 123,
				Key:   "test",
				Error: nil,
			}
		}()

		// First call
		value1, key1, err1 := future.Get()

		// Second call should return same result
		value2, key2, err2 := future.Get()

		if value1 != value2 || key1 != key2 || err1 != err2 {
			t.Errorf("Get calls returned different results")
		}
		if value1 != 123 {
			t.Errorf("expected value 123, got %v", value1)
		}
	})
}

func TestFuture_GetWithContext(t *testing.T) {
	t.Run("successful result before timeout", func(t *testing.T) {
		future := NewFuture[string, int]()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		go func() {
			time.Sleep(50 * time.Millisecond)
			future.result <- Result[string, int]{
				Value: "success",
				Key:   42,
				Error: nil,
			}
		}()

		value, key, err := future.GetWithContext(ctx)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if value != "success" {
			t.Errorf("expected value 'success', got %v", value)
		}
		if key != 42 {
			t.Errorf("expected key 42, got %v", key)
		}
	})

	t.Run("context timeout before result", func(t *testing.T) {
		future := NewFuture[string, int]()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		go func() {
			time.Sleep(200 * time.Millisecond)
			future.result <- Result[string, int]{
				Value: "too late",
				Key:   99,
				Error: nil,
			}
		}()

		value, key, err := future.GetWithContext(ctx)

		if err != context.DeadlineExceeded {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
		if value != "" {
			t.Errorf("expected empty value, got %v", value)
		}
		if key != 0 {
			t.Errorf("expected zero key, got %v", key)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		future := NewFuture[string, int]()
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		go func() {
			time.Sleep(200 * time.Millisecond)
			future.result <- Result[string, int]{
				Value: "too late",
				Key:   99,
				Error: nil,
			}
		}()

		value, key, err := future.GetWithContext(ctx)

		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if value != "" {
			t.Errorf("expected empty value, got %v", value)
		}
		if key != 0 {
			t.Errorf("expected zero key, got %v", key)
		}
	})

	t.Run("multiple GetWithContext calls return same result", func(t *testing.T) {
		future := NewFuture[int, string]()
		ctx := context.Background()

		go func() {
			future.result <- Result[int, string]{
				Value: 456,
				Key:   "key",
				Error: nil,
			}
		}()

		// First call
		value1, key1, err1 := future.GetWithContext(ctx)

		// Second call should return same result
		value2, key2, err2 := future.GetWithContext(ctx)

		if value1 != value2 || key1 != key2 || err1 != err2 {
			t.Errorf("GetWithContext calls returned different results")
		}
	})
}

func TestFuture_TryGet(t *testing.T) {
	t.Run("result not ready", func(t *testing.T) {
		future := NewFuture[string, int]()

		value, key, err, ready := future.TryGet()

		if ready {
			t.Error("expected ready to be false")
		}
		if value != "" {
			t.Errorf("expected empty value, got %v", value)
		}
		if key != 0 {
			t.Errorf("expected zero key, got %v", key)
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("result ready", func(t *testing.T) {
		future := NewFuture[string, int]()

		// Send result
		future.result <- Result[string, int]{
			Value: "ready",
			Key:   100,
			Error: nil,
		}

		// Small delay to ensure channel is ready
		time.Sleep(10 * time.Millisecond)

		value, key, err, ready := future.TryGet()

		if !ready {
			t.Error("expected ready to be true")
		}
		if value != "ready" {
			t.Errorf("expected value 'ready', got %v", value)
		}
		if key != 100 {
			t.Errorf("expected key 100, got %v", key)
		}
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("multiple TryGet calls after ready", func(t *testing.T) {
		future := NewFuture[int, string]()

		future.result <- Result[int, string]{
			Value: 789,
			Key:   "abc",
			Error: nil,
		}

		time.Sleep(10 * time.Millisecond)

		// First call
		value1, key1, err1, ready1 := future.TryGet()

		// Second call
		value2, key2, err2, ready2 := future.TryGet()

		if !ready1 || !ready2 {
			t.Error("expected both calls to be ready")
		}
		if value1 != value2 || key1 != key2 || err1 != err2 {
			t.Errorf("TryGet calls returned different results")
		}
	})
}

func TestFuture_Done(t *testing.T) {
	t.Run("channel closed when result ready", func(t *testing.T) {
		future := NewFuture[string, int]()

		// Result not ready yet
		select {
		case <-future.Done():
			t.Error("Done channel should not be closed yet")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}

		// Send result
		future.result <- Result[string, int]{
			Value: "done",
			Key:   1,
			Error: nil,
		}

		// Trigger result processing
		go future.Get()

		// Wait for Done channel
		select {
		case <-future.Done():
			// Expected
		case <-time.After(200 * time.Millisecond):
			t.Error("Done channel should be closed after result is ready")
		}
	})

	t.Run("use Done in select", func(t *testing.T) {
		future := NewFuture[string, int]()

		go func() {
			time.Sleep(50 * time.Millisecond)
			future.result <- Result[string, int]{
				Value: "selected",
				Key:   2,
				Error: nil,
			}
		}()

		// Use Done in select with Get
		go future.Get()

		select {
		case <-future.Done():
			value, key, err := future.Get()
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if value != "selected" {
				t.Errorf("expected value 'selected', got %v", value)
			}
			if key != 2 {
				t.Errorf("expected key 2, got %v", key)
			}
		case <-time.After(200 * time.Millisecond):
			t.Error("timeout waiting for Done")
		}
	})
}

func TestFuture_IsReady(t *testing.T) {
	t.Run("not ready initially", func(t *testing.T) {
		future := NewFuture[string, int]()

		if future.IsReady() {
			t.Error("expected IsReady to be false")
		}
	})

	t.Run("ready after result sent", func(t *testing.T) {
		future := NewFuture[string, int]()

		future.result <- Result[string, int]{
			Value: "ready",
			Key:   5,
			Error: nil,
		}

		// Trigger processing
		go future.Get()

		// Wait a bit for processing
		time.Sleep(50 * time.Millisecond)

		if !future.IsReady() {
			t.Error("expected IsReady to be true")
		}
	})
}

func TestFuture_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent Get calls", func(t *testing.T) {
		future := NewFuture[int, string]()

		go func() {
			time.Sleep(50 * time.Millisecond)
			future.result <- Result[int, string]{
				Value: 999,
				Key:   "concurrent",
				Error: nil,
			}
		}()

		done := make(chan bool, 10)

		// Launch multiple concurrent Get calls
		for range 10 {
			go func() {
				value, key, err := future.Get()
				if err != nil || value != 999 || key != "concurrent" {
					t.Errorf("unexpected result: value=%v, key=%v, err=%v", value, key, err)
				}
				done <- true
			}()
		}

		// Wait for all goroutines
		for range 10 {
			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("timeout waiting for concurrent Get calls")
			}
		}
	})
}
