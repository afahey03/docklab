package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter is an in-memory token bucket limiter keyed by an arbitrary string
// (typically client IP plus route group). Buckets refill continuously at
// ratePerMinute and hold at most burst tokens.
type RateLimiter struct {
	mu            sync.Mutex
	buckets       map[string]*tokenBucket
	ratePerMinute float64
	burst         float64
	now           func() time.Time
	lastSweep     time.Time
}

type tokenBucket struct {
	tokens   float64
	lastSeen time.Time
}

func NewRateLimiter(ratePerMinute int) *RateLimiter {
	if ratePerMinute <= 0 {
		ratePerMinute = 60
	}
	return &RateLimiter{
		buckets:       make(map[string]*tokenBucket),
		ratePerMinute: float64(ratePerMinute),
		burst:         float64(ratePerMinute),
		now:           time.Now,
		lastSweep:     time.Now(),
	}
}

// Allow consumes one token for key and reports whether the request may proceed.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = bucket
	}

	elapsed := now.Sub(bucket.lastSeen).Minutes()
	bucket.tokens += elapsed * l.ratePerMinute
	if bucket.tokens > l.burst {
		bucket.tokens = l.burst
	}
	bucket.lastSeen = now

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// sweepLocked drops buckets idle for over 10 minutes so memory stays bounded.
func (l *RateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeen) > 10*time.Minute {
			delete(l.buckets, key)
		}
	}
}

// RateLimit returns gin middleware enforcing the limiter per client IP.
func RateLimit(limiter *RateLimiter, scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := scope + "|" + c.ClientIP()
		if !limiter.Allow(key) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":  "rate_limited",
				"error": "too many requests; slow down and retry shortly",
			})
			return
		}
		c.Next()
	}
}
