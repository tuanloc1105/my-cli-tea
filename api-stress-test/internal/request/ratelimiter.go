package request

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type rateTimer interface {
	Chan() <-chan time.Time
	Reset(time.Duration) bool
	Stop() bool
}

type timerAdapter struct {
	*time.Timer
}

func (t timerAdapter) Chan() <-chan time.Time {
	return t.C
}

type timerFactory func(time.Duration) rateTimer

// RateLimiter enforces a global burst size of one without retaining idle ticks.
type RateLimiter struct {
	interval  time.Duration
	timer     rateTimer
	waitMu    sync.Mutex
	firstDone bool
	stopCh    chan struct{}
	stopped   atomic.Bool
	stopOnce  sync.Once
}

// NewRateLimiter creates a limiter for rps requests per second. A zero rate is
// unlimited; invalid rates return an error.
func NewRateLimiter(rps float64) (*RateLimiter, error) {
	return newRateLimiter(rps, func(interval time.Duration) rateTimer {
		timer := time.NewTimer(interval)
		if !timer.Stop() {
			<-timer.C
		}
		return timerAdapter{Timer: timer}
	})
}

func newRateLimiter(rps float64, newTimer timerFactory) (*RateLimiter, error) {
	if math.IsNaN(rps) || math.IsInf(rps, 0) || rps < 0 {
		return nil, fmt.Errorf("rate must be finite and non-negative (got %v)", rps)
	}
	limiter := &RateLimiter{stopCh: make(chan struct{})}
	if rps == 0 {
		return limiter, nil
	}
	intervalNanoseconds := float64(time.Second) / rps
	if intervalNanoseconds < 1 || intervalNanoseconds > float64(math.MaxInt64) {
		return nil, fmt.Errorf("rate produces an unrepresentable interval (got %v)", rps)
	}
	if newTimer == nil {
		return nil, fmt.Errorf("rate timer factory is nil")
	}
	limiter.interval = time.Duration(intervalNanoseconds)
	limiter.timer = newTimer(limiter.interval)
	if limiter.timer == nil {
		return nil, fmt.Errorf("rate timer factory returned nil")
	}
	return limiter, nil
}

// Wait blocks until the next request may start or cancellation occurs. The
// first request starts immediately; later intervals begin after the prior
// caller has returned from Wait, so an idle limiter cannot build a backlog.
func (r *RateLimiter) Wait(ctx context.Context) bool {
	if r == nil || ctx == nil {
		return false
	}
	r.waitMu.Lock()
	defer r.waitMu.Unlock()

	if r.stopped.Load() || ctx.Err() != nil {
		return false
	}
	if r.interval == 0 {
		return true
	}
	if !r.firstDone {
		r.firstDone = true
		return true
	}

	r.timer.Reset(r.interval)
	select {
	case <-r.timer.Chan():
		return ctx.Err() == nil && !r.stopped.Load()
	case <-ctx.Done():
		r.stopTimer()
		return false
	case <-r.stopCh:
		r.stopTimer()
		return false
	}
}

func (r *RateLimiter) stopTimer() {
	if r.timer == nil || r.timer.Stop() {
		return
	}
	select {
	case <-r.timer.Chan():
	default:
	}
}

// Stop releases the rate limiter resources and unblocks a pending Wait.
func (r *RateLimiter) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		r.stopped.Store(true)
		close(r.stopCh)
	})
}
