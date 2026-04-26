package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
)

// rateBucket tracks token-bucket state per key.
type rateBucket struct {
	tokens    float64
	lastCheck time.Time
}

// RateLimiter provides in-memory token-bucket rate limiting per IP.
// Suitable for single-instance deployments. For multi-instance, replace
// the in-memory map with Redis (GCRA algorithm).
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     float64       // tokens per second
	burst    int           // max tokens (bucket capacity)
	cleanupT *time.Ticker  // periodic eviction of stale entries
	stopCh   chan struct{} // signal to stop cleanup goroutine
}

// NewRateLimiter creates a rate limiter with the given requests/second and burst capacity.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*rateBucket),
		rate:    rps,
		burst:   burst,
		stopCh:  make(chan struct{}),
	}

	// Cleanup stale entries every 5 minutes to prevent memory leak.
	rl.cleanupT = time.NewTicker(5 * time.Minute)
	go rl.cleanupLoop()

	return rl
}

// Stop terminates the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
	rl.cleanupT.Stop()
}

func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.stopCh:
			return
		case <-rl.cleanupT.C:
			rl.evictStale()
		}
	}
}

func (rl *RateLimiter) evictStale() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-10 * time.Minute)
	for key, bucket := range rl.buckets {
		if bucket.lastCheck.Before(threshold) {
			delete(rl.buckets, key)
		}
	}
}

// allow checks if a request from the given key should be allowed.
func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &rateBucket{
			tokens:    float64(rl.burst) - 1,
			lastCheck: now,
		}
		return true
	}

	// Add tokens based on elapsed time
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimit returns a Gin middleware that rate-limits by client IP.
// Uses token-bucket algorithm: `rps` tokens per second, `burst` max capacity.
//
// Recommended values for auth endpoints: rps=1, burst=5
// (5 rapid requests allowed, then 1/sec sustained).
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	limiter := NewRateLimiter(rps, burst)

	return func(c *gin.Context) {
		key := c.ClientIP()
		if !limiter.allow(key) {
			appErr := apperror.NewValidation("too many requests, please try again later")
			appErr.HTTPStatus = http.StatusTooManyRequests
			_ = c.Error(appErr)
			c.Abort()
			return
		}
		c.Next()
	}
}
