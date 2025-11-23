package pool

import (
	"math"
	"time"
)

// calcBackoffDelay calculates the exponential backoff delay for retry attempts.
// attemptNumber is 0-indexed (0 = first retry, 1 = second retry, etc.)
// The delay doubles with each attempt: initialDelay * 2^attemptNumber
// For example, with initialDelay=1s:
//   - attempt 0 (first retry): 1s
//   - attempt 1 (second retry): 2s
//   - attempt 2 (third retry): 4s
func calcBackoffDelay(initialDelay time.Duration, attemptNumber int) time.Duration {
	if attemptNumber < 0 {
		return 0
	}

	backoffFactor := math.Pow(2, float64(attemptNumber))
	return time.Duration(float64(initialDelay) * backoffFactor)
}
