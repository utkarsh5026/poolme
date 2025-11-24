package algorithms

import "time"

// BackoffType defines the retry backoff algorithm to use.
type BackoffType int

const (
	// BackoffExponential uses simple exponential backoff (default).
	BackoffExponential BackoffType = iota
	// BackoffJittered adds random jitter to prevent thundering herd.
	BackoffJittered
	// BackoffDecorrelated uses AWS-style decorrelated jitter.
	BackoffDecorrelated
)

// NewBackoffStrategy creates a backoff strategy based on the configuration.
// This is the internal factory function used by the pool package.
func NewBackoffStrategy(
	backoffType BackoffType,
	initialDelay, maxDelay time.Duration,
	jitterFactor float64,
) BackoffStrategy {
	switch backoffType {
	case BackoffJittered:
		return newJitteredBackoff(initialDelay, maxDelay, jitterFactor)

	case BackoffDecorrelated:
		return newDecorrelatedJitterBackoff(initialDelay, maxDelay)

	default:
		return newExponentialBackoff(initialDelay, maxDelay)
	}
}
