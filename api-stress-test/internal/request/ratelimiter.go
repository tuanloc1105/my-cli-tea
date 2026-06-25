package request

import (
	"context"
	"sync/atomic"
	"time"
)

// RateLimiter provides a simple token-bucket rate limiter using time.Ticker.
type RateLimiter struct {
	ticker    *time.Ticker
	firstDone atomic.Bool
}

// NewRateLimiter creates a rate limiter that allows rps requests per second.
// If rps is <= 0, returns a no-op limiter (unlimited).
func NewRateLimiter(rps float64) *RateLimiter {
	if rps <= 0 {
		return &RateLimiter{}
	}
	interval := time.Duration(float64(time.Second) / rps)
	return &RateLimiter{ticker: time.NewTicker(interval)}
}

// Wait blocks until the next request is allowed or context is cancelled.
// Returns true if allowed, false if context was cancelled.
// The first request is allowed immediately without waiting for the ticker.
func (r *RateLimiter) Wait(ctx context.Context) bool {
	if r.ticker == nil {
		return ctx.Err() == nil
	}
	// Allow the first request to proceed immediately
	if r.firstDone.CompareAndSwap(false, true) {
		return ctx.Err() == nil
	}
	select {
	case <-r.ticker.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Stop releases the rate limiter resources.
func (r *RateLimiter) Stop() {
	if r.ticker != nil {
		r.ticker.Stop()
	}
}
