package subroutines

import "time"

// staticLimiter provides a deterministic workqueue.TypedRateLimiter implementation for tests.
type staticLimiter struct {
	delay time.Duration
}

func (s staticLimiter) When(ClusteredName) time.Duration { return s.delay }
func (s staticLimiter) Forget(ClusteredName)             {}
func (s staticLimiter) NumRequeues(ClusteredName) int    { return 0 }
