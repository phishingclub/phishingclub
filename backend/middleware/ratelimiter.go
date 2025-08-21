package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

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

//const cleanupInterval = 1 * time.Minute
//const entryExpiration = 10 * time.Minute

// KeyRateLimiter is a rate limiter for key such as username, email or IP
type KeyRateLimiter struct {
	// ips is a map of key to rate limit
	key sync.Map
	// limiter is the rate limit, e.g. 1 request per seconds
	limiter rate.Limit
	// burst is the maximum burst size, the maximum number of requests that can be made in a burst without being limited
	burst int
	// cleanupInterval is the interval at which the expired keys are cleaned up
	cleanupInterval time.Duration
}

// NewKeyRateLimiter creates a new key rate limiter
// limiter is the rate limit, e.g. 1 request per seconds
// burst is the maximum burst size, the maximum number of requests that can be made in a burst without being limited
func NewKeyRateLimiter(
	limiter rate.Limit,
	burst int,
	cleanupInterval time.Duration,
) *KeyRateLimiter {
	rl := &KeyRateLimiter{
		limiter: limiter,
		burst:   burst,
	}
	go rl.cleanup()
	return rl
}

// cleanup cleans up the expired keys, this is to avoid
// memory leaking through the sync.Map when the key is not used anymore
func (r *KeyRateLimiter) cleanup() {
	for range time.Tick(r.cleanupInterval) {
		now := time.Now()
		r.key.Range(func(key, value interface{}) bool {
			expirationTime := value.(time.Time)
			if now.After(expirationTime) {
				r.key.Delete(key)
			}
			return true
		})
	}
}

// GetLimiter gets the limiter for an key or creates one if it does not exist
func (r *KeyRateLimiter) GetLimiter(key string) *rate.Limiter {
	value, exists := r.key.Load(key)
	if exists {
		return value.(*rate.Limiter)
	}

	limiter := rate.NewLimiter(r.limiter, r.burst)
	r.key.Store(key, limiter)
	return limiter
}
