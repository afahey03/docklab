package middleware

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsWithinBurst(t *testing.T) {
	limiter := NewRateLimiter(5)

	for i := 0; i < 5; i++ {
		if !limiter.Allow("client-a") {
			t.Fatalf("expected request %d within burst to be allowed", i+1)
		}
	}
	if limiter.Allow("client-a") {
		t.Fatal("expected request over burst to be rejected")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	limiter := NewRateLimiter(1)

	if !limiter.Allow("client-a") {
		t.Fatal("expected first request for client-a to be allowed")
	}
	if !limiter.Allow("client-b") {
		t.Fatal("expected first request for client-b to be allowed despite client-a exhaustion")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	limiter := NewRateLimiter(60) // one token per second
	current := time.Now()
	limiter.now = func() time.Time { return current }

	for i := 0; i < 60; i++ {
		limiter.Allow("client-a")
	}
	if limiter.Allow("client-a") {
		t.Fatal("expected bucket to be empty")
	}

	current = current.Add(2 * time.Second)
	if !limiter.Allow("client-a") {
		t.Fatal("expected bucket to refill after time passes")
	}
}
