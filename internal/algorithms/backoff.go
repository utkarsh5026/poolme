package algorithms

import (
	"math/rand"
	"sync"
	"time"
)

const (
	maxAttempts = 63 // Prevent overflow in backoff calculation
)

// jitteredBackoff adds randomization to exponential backoff to prevent thundering herd.
// Delay formula: exponentialDelay * (1 ± jitterFactor)
//
// The "thundering herd" problem occurs when multiple tasks fail simultaneously and all
// retry at the same time, creating a spike in load. Jitter spreads out these retry
// attempts by adding randomness.
//
// Example with jitterFactor=0.1:
// Base delay of 1s becomes random value between 900ms and 1100ms
type jitteredBackoff struct {
	initialDelay, maxDelay time.Duration
	jitterFactor           float64 // 0.0 to 1.0 (e.g., 0.1 = ±10% jitter)
	rng                    *rand.Rand
	mu                     sync.Mutex // Protect RNG access for thread-safety
}

// newJitteredBackoff creates a new jittered backoff strategy.
// jitterFactor should be between 0.0 and 1.0 (typical values: 0.1 to 0.3).
func newJitteredBackoff(initialDelay, maxDelay time.Duration, jitterFactor float64) *jitteredBackoff {
	return &jitteredBackoff{
		initialDelay: initialDelay,
		maxDelay:     maxDelay,
		jitterFactor: clamp(jitterFactor, 0, 1),
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NextDelay calculates the jittered exponential backoff delay.
func (jb *jitteredBackoff) NextDelay(attemptNumber int, lastError error) time.Duration {
	if attemptNumber < 0 {
		return 0
	}

	baseDelay := calcExponentialDelay(attemptNumber, jb.initialDelay, jb.maxDelay)

	jb.mu.Lock()
	jitterMultiplier := 1.0 + (jb.rng.Float64()*2-1)*jb.jitterFactor
	jb.mu.Unlock()

	actualDelay := time.Duration(float64(baseDelay) * jitterMultiplier)
	return clamp(actualDelay, 0, jb.maxDelay)
}

// Reset does nothing for jittered backoff (RNG state doesn't need reset).
func (jb *jitteredBackoff) Reset() {
	// No state to reset (RNG state doesn't need reset)
}

// exponentialBackoff implements simple exponential backoff.
// Delay formula: initialDelay * 2^attemptNumber
//
// This is the default and simplest backoff strategy. Delays grow exponentially:
// Attempt 0: 1x initialDelay
// Attempt 1: 2x initialDelay
// Attempt 2: 4x initialDelay
// Attempt 3: 8x initialDelay
// ...until maxDelay is reached
type exponentialBackoff struct {
	initialDelay time.Duration
	maxDelay     time.Duration
}

// newExponentialBackoff creates a new exponential backoff strategy.
func newExponentialBackoff(initialDelay, maxDelay time.Duration) *exponentialBackoff {
	return &exponentialBackoff{
		initialDelay: initialDelay,
		maxDelay:     maxDelay,
	}
}

// NextDelay calculates the exponential backoff delay for the given attempt number.
// Uses bit shifting (2^n) for performance instead of math.Pow.
func (eb *exponentialBackoff) NextDelay(attemptNumber int, lastError error) time.Duration {
	return calcExponentialDelay(attemptNumber, eb.initialDelay, eb.maxDelay)
}

// Reset does nothing for exponential backoff as it has no internal state.
func (eb *exponentialBackoff) Reset() {
	// No state to reset
}

func calcExponentialDelay(attemptNumber int, initialDelay, maxDelay time.Duration) time.Duration {
	if attemptNumber < 0 {
		return 0
	}

	if attemptNumber >= maxAttempts {
		return maxDelay
	}

	backoffFactor := int64(1) << uint(attemptNumber)
	delay := time.Duration(backoffFactor) * initialDelay

	if delay > maxDelay || delay < 0 {
		return maxDelay
	}

	return delay
}
