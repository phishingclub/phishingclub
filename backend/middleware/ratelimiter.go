package middleware

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// limiterEntry holds a rate limiter and the last time it was accessed as
// unix nanoseconds. lastAccess is accessed atomically to avoid data races
// between GetLimiter (writer) and cleanup (reader) under concurrent requests.
type limiterEntry struct {
	limiter    *rate.Limiter
	lastAccess atomic.Int64
}

// NewIPRateLimiterMiddleware creates a middleware that limits the number of requests per IP
// limit is the number of requests per second
// burst is the maximum burst size, the maximum number of requests that can be made in a burst without being limited
func NewIPRateLimiterMiddleware(limit float64, burst int) gin.HandlerFunc {
	ipLimiter := NewKeyRateLimiter(rate.Limit(limit), burst, 10*time.Minute)
	return func(c *gin.Context) {
		limiter := ipLimiter.GetLimiter(c.ClientIP())
		if !limiter.Allow() {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

// KeyRateLimiter is a rate limiter for key such as username, email or IP
type KeyRateLimiter struct {
	// key is a map of key to limiterEntry
	key sync.Map
	// limiter is the rate limit, e.g. 1 request per second
	limiter rate.Limit
	// burst is the maximum burst size, the maximum number of requests that can be made in a burst without being limited
	burst int
	// cleanupInterval is the interval at which idle entries are evicted
	cleanupInterval time.Duration
}

// NewKeyRateLimiter creates a new key rate limiter
// limiter is the rate limit, e.g. 1 request per second
// burst is the maximum burst size, the maximum number of requests that can be made in a burst without being limited
func NewKeyRateLimiter(
	limiter rate.Limit,
	burst int,
	cleanupInterval time.Duration,
) *KeyRateLimiter {
	rl := &KeyRateLimiter{
		limiter:         limiter,
		burst:           burst,
		cleanupInterval: cleanupInterval,
	}
	go rl.cleanup()
	return rl
}

// cleanup evicts entries that have not been accessed within the cleanup interval,
// preventing unbounded memory growth from the sync.Map
func (r *KeyRateLimiter) cleanup() {
	for range time.Tick(r.cleanupInterval) {
		threshold := time.Now().Add(-r.cleanupInterval).UnixNano()
		r.key.Range(func(key, value interface{}) bool {
			entry, ok := value.(*limiterEntry)
			if !ok {
				// remove any entry with an unexpected type
				r.key.Delete(key)
				return true
			}
			if entry.lastAccess.Load() < threshold {
				r.key.Delete(key)
			}
			return true
		})
	}
}

// GetLimiter gets the limiter for a key or creates one if it does not exist
func (r *KeyRateLimiter) GetLimiter(key string) *rate.Limiter {
	entry := &limiterEntry{
		limiter: rate.NewLimiter(r.limiter, r.burst),
	}
	entry.lastAccess.Store(time.Now().UnixNano())

	// LoadOrStore atomically either stores our new entry or returns the
	// existing one — correctly handles both the common case and concurrent
	// goroutines racing to create an entry for the same key
	actual, loaded := r.key.LoadOrStore(key, entry)
	if loaded {
		existing := actual.(*limiterEntry)
		existing.lastAccess.Store(time.Now().UnixNano())
		return existing.limiter
	}

	return entry.limiter
}
