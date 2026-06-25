package request

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterUnlimited(t *testing.T) {
	limiter := NewRateLimiter(0)
	defer limiter.Stop()
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 100; i++ {
		if !limiter.Wait(ctx) {
			t.Fatal("Wait returned false unexpectedly")
		}
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("unlimited limiter took %v, expected near-instant", elapsed)
	}
}

func TestRateLimiterNegative(t *testing.T) {
	limiter := NewRateLimiter(-1)
	defer limiter.Stop()
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 100; i++ {
		if !limiter.Wait(ctx) {
			t.Fatal("Wait returned false unexpectedly")
		}
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("negative rate limiter took %v, expected near-instant", elapsed)
	}
}

func TestRateLimiterThrottles(t *testing.T) {
	// 10 req/s = 100ms interval
	limiter := NewRateLimiter(10)
	defer limiter.Stop()
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 5; i++ {
		if !limiter.Wait(ctx) {
			t.Fatal("Wait returned false unexpectedly")
		}
	}
	elapsed := time.Since(start)

	// First request is immediate, then 4 ticks at 100ms each ≈ 400ms
	if elapsed < 350*time.Millisecond {
		t.Errorf("rate limiter elapsed %v, expected at least ~350ms for 5 requests at 10 req/s", elapsed)
	}
}

func TestRateLimiterContextCancelled(t *testing.T) {
	// Very slow rate: 1 req per 10 seconds
	limiter := NewRateLimiter(0.1)
	defer limiter.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// First wait might succeed (ticker fires immediately), but eventually should return false
	start := time.Now()
	for limiter.Wait(ctx) {
		// keep waiting
	}
	elapsed := time.Since(start)

	// Should return within ~100ms (50ms cancel + scheduling), NOT 10 seconds
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancelled limiter took %v, expected quick return after cancel", elapsed)
	}
}

func TestRateLimiterUnlimitedContextCancelled(t *testing.T) {
	limiter := NewRateLimiter(0)
	defer limiter.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if limiter.Wait(ctx) {
		t.Error("Wait should return false when context is already cancelled")
	}
}

func TestRateLimiterFirstRequestImmediate(t *testing.T) {
	// Very slow rate: 1 req per 10 seconds
	limiter := NewRateLimiter(0.1)
	defer limiter.Stop()
	ctx := context.Background()

	start := time.Now()
	if !limiter.Wait(ctx) {
		t.Fatal("first Wait should return true")
	}
	elapsed := time.Since(start)

	// First request should be near-instant, not wait 10 seconds
	if elapsed > 100*time.Millisecond {
		t.Errorf("first request took %v, expected near-instant", elapsed)
	}
}
