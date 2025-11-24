package algorithms

import (
	"sync"
	"testing"
	"time"
)

func TestDecorrelatedJitterBackoff_NextDelay(t *testing.T) {
	tests := []struct {
		name          string
		initialDelay  time.Duration
		maxDelay      time.Duration
		attemptNumber int
		wantMin       time.Duration
		wantMax       time.Duration
	}{
		{
			name:          "first attempt returns initial delay",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 0,
			wantMin:       100 * time.Millisecond,
			wantMax:       100 * time.Millisecond,
		},
		{
			name:          "second attempt between initial and 3x initial",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 1,
			wantMin:       100 * time.Millisecond,
			wantMax:       300 * time.Millisecond,
		},
		{
			name:          "respects max delay",
			initialDelay:  1 * time.Second,
			maxDelay:      2 * time.Second,
			attemptNumber: 10,
			wantMin:       1 * time.Second,
			wantMax:       2 * time.Second,
		},
		{
			name:          "small max delay returns initial delay",
			initialDelay:  1 * time.Second,
			maxDelay:      500 * time.Millisecond,
			attemptNumber: 1,
			wantMin:       1 * time.Second,
			wantMax:       1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			djb := newDecorrelatedJitterBackoff(tt.initialDelay, tt.maxDelay)

			var delay time.Duration
			for i := 0; i <= tt.attemptNumber; i++ {
				delay = djb.NextDelay(i, nil)
			}

			if delay < tt.wantMin || delay > tt.wantMax {
				t.Errorf("NextDelay() = %v, want between %v and %v", delay, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestDecorrelatedJitterBackoff_Distribution(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second

	// Test that multiple instances with the same parameters produce different sequences
	// This verifies the randomness of the decorrelated jitter
	delays := make([]time.Duration, 50)
	for i := 0; i < 50; i++ {
		djb := newDecorrelatedJitterBackoff(initialDelay, maxDelay)
		djb.NextDelay(0, nil) // First attempt
		delays[i] = djb.NextDelay(1, nil) // Second attempt
	}

	// Check that we got some variation (not all the same)
	allSame := true
	for i := 1; i < len(delays); i++ {
		if delays[i] != delays[0] {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("Expected variation in decorrelated jitter delays, but all delays were the same")
	}

	// Check all delays are within bounds for second attempt
	// (should be between initialDelay and 3*initialDelay)
	for i, delay := range delays {
		if delay < initialDelay || delay > 3*initialDelay {
			t.Errorf("Delay[%d] = %v, want between %v and %v", i, delay, initialDelay, 3*initialDelay)
		}
	}
}

func TestDecorrelatedJitterBackoff_GrowthBehavior(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	djb := newDecorrelatedJitterBackoff(initialDelay, maxDelay)

	// Test that delays grow over time but stay within bounds
	firstDelay := djb.NextDelay(0, nil)
	if firstDelay != initialDelay {
		t.Errorf("First delay = %v, want %v", firstDelay, initialDelay)
	}

	// Run several iterations and verify bounds
	for i := 1; i < 20; i++ {
		delay := djb.NextDelay(i, nil)

		// Delay should be between initialDelay and maxDelay
		if delay < initialDelay {
			t.Errorf("Iteration %d: delay = %v, should not be less than initialDelay %v", i, delay, initialDelay)
		}
		if delay > maxDelay {
			t.Errorf("Iteration %d: delay = %v, should not exceed maxDelay %v", i, delay, maxDelay)
		}
	}
}

func TestDecorrelatedJitterBackoff_Reset(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	djb := newDecorrelatedJitterBackoff(initialDelay, maxDelay)

	// Progress through several attempts
	djb.NextDelay(0, nil)
	djb.NextDelay(1, nil)
	djb.NextDelay(2, nil)

	// Reset
	djb.Reset()

	// Next delay should be initial delay
	delay := djb.NextDelay(0, nil)
	if delay != initialDelay {
		t.Errorf("After Reset(), NextDelay(0) = %v, want %v", delay, initialDelay)
	}
}

func TestDecorrelatedJitterBackoff_ThreadSafety(t *testing.T) {
	initialDelay := 10 * time.Millisecond
	maxDelay := 1 * time.Second
	djb := newDecorrelatedJitterBackoff(initialDelay, maxDelay)

	var wg sync.WaitGroup
	concurrentCalls := 100

	// Run concurrent NextDelay calls
	for i := 0; i < concurrentCalls; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			djb.NextDelay(attempt%10, nil)
		}(i)
	}

	wg.Wait()
	// If there's a race condition, this test will fail with -race flag
}

func TestJitteredBackoff_NextDelay(t *testing.T) {
	tests := []struct {
		name          string
		initialDelay  time.Duration
		maxDelay      time.Duration
		jitterFactor  float64
		attemptNumber int
		wantMin       time.Duration
		wantMax       time.Duration
	}{
		{
			name:          "first attempt with jitter",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			jitterFactor:  0.1,
			attemptNumber: 0,
			wantMin:       90 * time.Millisecond,
			wantMax:       110 * time.Millisecond,
		},
		{
			name:          "second attempt with jitter",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			jitterFactor:  0.1,
			attemptNumber: 1,
			wantMin:       180 * time.Millisecond,
			wantMax:       220 * time.Millisecond,
		},
		{
			name:          "negative attempt returns zero",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			jitterFactor:  0.1,
			attemptNumber: -1,
			wantMin:       0,
			wantMax:       0,
		},
		{
			name:          "respects max delay",
			initialDelay:  1 * time.Second,
			maxDelay:      2 * time.Second,
			jitterFactor:  0.1,
			attemptNumber: 10,
			wantMin:       0,
			wantMax:       2 * time.Second,
		},
		{
			name:          "zero jitter factor",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			jitterFactor:  0.0,
			attemptNumber: 0,
			wantMin:       100 * time.Millisecond,
			wantMax:       100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jb := newJitteredBackoff(tt.initialDelay, tt.maxDelay, tt.jitterFactor)
			delay := jb.NextDelay(tt.attemptNumber, nil)

			if delay < tt.wantMin || delay > tt.wantMax {
				t.Errorf("NextDelay() = %v, want between %v and %v", delay, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestJitteredBackoff_JitterFactorClamping(t *testing.T) {
	tests := []struct {
		name         string
		jitterFactor float64
	}{
		{"negative jitter factor", -0.5},
		{"jitter factor > 1", 1.5},
		{"valid jitter factor", 0.3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialDelay := 100 * time.Millisecond
			maxDelay := 10 * time.Second

			// Should not panic
			jb := newJitteredBackoff(initialDelay, maxDelay, tt.jitterFactor)
			delay := jb.NextDelay(0, nil)

			if delay < 0 || delay > maxDelay {
				t.Errorf("NextDelay() = %v, want between 0 and %v", delay, maxDelay)
			}
		})
	}
}

func TestJitteredBackoff_Reset(t *testing.T) {
	jb := newJitteredBackoff(100*time.Millisecond, 10*time.Second, 0.1)

	// Progress through several attempts
	jb.NextDelay(0, nil)
	jb.NextDelay(1, nil)

	// Reset (should be no-op for jittered backoff)
	jb.Reset()

	// Should still work normally
	delay := jb.NextDelay(0, nil)
	if delay < 0 || delay > 10*time.Second {
		t.Errorf("After Reset(), NextDelay() = %v, expected valid delay", delay)
	}
}

func TestJitteredBackoff_ThreadSafety(t *testing.T) {
	jb := newJitteredBackoff(10*time.Millisecond, 1*time.Second, 0.2)

	var wg sync.WaitGroup
	concurrentCalls := 100

	for i := 0; i < concurrentCalls; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			jb.NextDelay(attempt%10, nil)
		}(i)
	}

	wg.Wait()
	// If there's a race condition, this test will fail with -race flag
}

func TestExponentialBackoff_NextDelay(t *testing.T) {
	tests := []struct {
		name          string
		initialDelay  time.Duration
		maxDelay      time.Duration
		attemptNumber int
		want          time.Duration
	}{
		{
			name:          "first attempt",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 0,
			want:          100 * time.Millisecond,
		},
		{
			name:          "second attempt",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 1,
			want:          200 * time.Millisecond,
		},
		{
			name:          "third attempt",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 2,
			want:          400 * time.Millisecond,
		},
		{
			name:          "fourth attempt",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: 3,
			want:          800 * time.Millisecond,
		},
		{
			name:          "negative attempt returns zero",
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			attemptNumber: -1,
			want:          0,
		},
		{
			name:          "respects max delay",
			initialDelay:  1 * time.Second,
			maxDelay:      5 * time.Second,
			attemptNumber: 10,
			want:          5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eb := newExponentialBackoff(tt.initialDelay, tt.maxDelay)
			delay := eb.NextDelay(tt.attemptNumber, nil)

			if delay != tt.want {
				t.Errorf("NextDelay() = %v, want %v", delay, tt.want)
			}
		})
	}
}

func TestExponentialBackoff_Reset(t *testing.T) {
	eb := newExponentialBackoff(100*time.Millisecond, 10*time.Second)

	// Progress through several attempts
	eb.NextDelay(0, nil)
	eb.NextDelay(1, nil)

	// Reset (should be no-op for exponential backoff)
	eb.Reset()

	// Should still work normally
	delay := eb.NextDelay(0, nil)
	if delay != 100*time.Millisecond {
		t.Errorf("After Reset(), NextDelay(0) = %v, want %v", delay, 100*time.Millisecond)
	}
}

func TestCalcExponentialDelay(t *testing.T) {
	tests := []struct {
		name          string
		attemptNumber int
		initialDelay  time.Duration
		maxDelay      time.Duration
		want          time.Duration
	}{
		{
			name:          "attempt 0 returns initial delay",
			attemptNumber: 0,
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			want:          100 * time.Millisecond,
		},
		{
			name:          "attempt 1 doubles initial delay",
			attemptNumber: 1,
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			want:          200 * time.Millisecond,
		},
		{
			name:          "attempt 5",
			attemptNumber: 5,
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			want:          3200 * time.Millisecond,
		},
		{
			name:          "negative attempt returns zero",
			attemptNumber: -1,
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			want:          0,
		},
		{
			name:          "very large attempt returns max delay",
			attemptNumber: 100,
			initialDelay:  100 * time.Millisecond,
			maxDelay:      10 * time.Second,
			want:          10 * time.Second,
		},
		{
			name:          "maxAttempts boundary",
			attemptNumber: maxAttempts,
			initialDelay:  1 * time.Millisecond,
			maxDelay:      1 * time.Hour,
			want:          1 * time.Hour,
		},
		{
			name:          "overflow protection",
			attemptNumber: 50,
			initialDelay:  1 * time.Hour,
			maxDelay:      24 * time.Hour,
			want:          24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcExponentialDelay(tt.attemptNumber, tt.initialDelay, tt.maxDelay)
			if got != tt.want {
				t.Errorf("calcExponentialDelay() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcExponentialDelay_ExponentialGrowth(t *testing.T) {
	initialDelay := 100 * time.Millisecond
	maxDelay := 1 * time.Hour

	// Verify exponential growth pattern
	expectedDelays := []time.Duration{
		100 * time.Millisecond,  // 2^0 * 100ms
		200 * time.Millisecond,  // 2^1 * 100ms
		400 * time.Millisecond,  // 2^2 * 100ms
		800 * time.Millisecond,  // 2^3 * 100ms
		1600 * time.Millisecond, // 2^4 * 100ms
		3200 * time.Millisecond, // 2^5 * 100ms
	}

	for i, expected := range expectedDelays {
		got := calcExponentialDelay(i, initialDelay, maxDelay)
		if got != expected {
			t.Errorf("calcExponentialDelay(%d) = %v, want %v", i, got, expected)
		}
	}
}

func BenchmarkDecorrelatedJitterBackoff(b *testing.B) {
	djb := newDecorrelatedJitterBackoff(100*time.Millisecond, 10*time.Second)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		djb.NextDelay(i%10, nil)
	}
}

func BenchmarkJitteredBackoff(b *testing.B) {
	jb := newJitteredBackoff(100*time.Millisecond, 10*time.Second, 0.1)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		jb.NextDelay(i%10, nil)
	}
}

func BenchmarkExponentialBackoff(b *testing.B) {
	eb := newExponentialBackoff(100*time.Millisecond, 10*time.Second)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eb.NextDelay(i%10, nil)
	}
}

func BenchmarkCalcExponentialDelay(b *testing.B) {
	initialDelay := 100 * time.Millisecond
	maxDelay := 10 * time.Second
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		calcExponentialDelay(i%10, initialDelay, maxDelay)
	}
}
