package algorithms

import "time"

// BackoffStrategy defines how retry delays are calculated (internal only).
//
// Note: This interface is exported so the pool package can use type assertions,
// but implementations remain internal.
type BackoffStrategy interface {
	// NextDelay calculates the delay before the next retry attempt.
	// attemptNumber is 0-indexed (0 = first retry after initial failure).
	// lastError can be used by adaptive strategies to adjust delays based on error type.
	// Returns the duration to wait before the next retry attempt.
	NextDelay(attemptNumber int, lastError error) time.Duration

	// Reset resets any internal state (for stateful strategies like decorrelated jitter).
	// This should be called when starting a new task or after successful completion.
	Reset()
}
