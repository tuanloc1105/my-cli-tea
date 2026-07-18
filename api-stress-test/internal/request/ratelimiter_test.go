package request

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiterValidation(t *testing.T) {
	tests := []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1), 1e-20, 1e10}
	for _, rate := range tests {
		if limiter, err := NewRateLimiter(rate); err == nil {
			limiter.Stop()
			t.Errorf("NewRateLimiter(%v) succeeded, want error", rate)
		}
	}
}

func TestRateLimiterUnlimitedAndCancellation(t *testing.T) {
	limiter, err := NewRateLimiter(0)
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Stop()
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if !limiter.Wait(ctx) {
			t.Fatal("unlimited Wait returned false")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if limiter.Wait(cancelled) {
		t.Fatal("Wait succeeded with cancelled context")
	}
}

func TestRateLimiterUsesBurstOneWithoutBacklog(t *testing.T) {
	timer := newFakeRateTimer()
	limiter, err := newRateLimiter(10, func(time.Duration) rateTimer { return timer })
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Stop()
	ctx := context.Background()

	if !limiter.Wait(ctx) {
		t.Fatal("first Wait should be immediate")
	}
	timer.Tick()
	second := make(chan bool, 1)
	go func() { second <- limiter.Wait(ctx) }()
	select {
	case <-second:
		t.Fatal("idle tick was retained as backlog")
	case <-time.After(20 * time.Millisecond):
	}
	timer.Tick()
	if !<-second {
		t.Fatal("second Wait did not accept fresh tick")
	}

	third := make(chan bool, 1)
	go func() { third <- limiter.Wait(ctx) }()
	select {
	case <-third:
		t.Fatal("third Wait passed without a new tick")
	case <-time.After(20 * time.Millisecond):
	}
	timer.Tick()
	if !<-third {
		t.Fatal("third Wait did not accept fresh tick")
	}
	if resets := timer.ResetCount(); resets != 2 {
		t.Fatalf("timer resets = %d, want 2", resets)
	}
}

func TestRateLimiterCancellationAndStopUnblockWait(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "context", true: "stop"}[stop], func(t *testing.T) {
			timer := newFakeRateTimer()
			limiter, err := newRateLimiter(1, func(time.Duration) rateTimer { return timer })
			if err != nil {
				t.Fatal(err)
			}
			if !limiter.Wait(context.Background()) {
				t.Fatal("first Wait should be immediate")
			}
			ctx, cancel := context.WithCancel(context.Background())
			waited := make(chan bool, 1)
			go func() { waited <- limiter.Wait(ctx) }()
			timer.WaitForReset(t)
			if stop {
				limiter.Stop()
			} else {
				cancel()
			}
			if <-waited {
				t.Fatal("Wait succeeded after cancellation")
			}
			cancel()
			limiter.Stop()
		})
	}
}

type fakeRateTimer struct {
	c       chan time.Time
	resetCh chan struct{}
	mu      sync.Mutex
	resets  int
	armed   bool
}

func newFakeRateTimer() *fakeRateTimer {
	return &fakeRateTimer{c: make(chan time.Time, 1), resetCh: make(chan struct{}, 10)}
}

func (t *fakeRateTimer) Chan() <-chan time.Time { return t.c }
func (t *fakeRateTimer) Stop() bool             { return true }
func (t *fakeRateTimer) Reset(time.Duration) bool {
	t.mu.Lock()
	t.resets++
	t.armed = true
	t.mu.Unlock()
	t.resetCh <- struct{}{}
	return true
}
func (t *fakeRateTimer) Tick() {
	t.mu.Lock()
	if !t.armed {
		t.mu.Unlock()
		return
	}
	t.armed = false
	t.mu.Unlock()
	t.c <- time.Now()
}
func (t *fakeRateTimer) ResetCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.resets
}
func (t *fakeRateTimer) WaitForReset(tst *testing.T) {
	tst.Helper()
	select {
	case <-t.resetCh:
	case <-time.After(time.Second):
		tst.Fatal("timer was not reset")
	}
}
