package service

import (
	"sync"
	"time"
)

// totpWindow is the lifetime of a used-token entry.
// pquerna/otp totp.Validate uses Skew:1 (current ±1 period × 30s = 90s total window).
const totpWindow = 90 * time.Second

type TOTPReplayCache struct {
	mu      sync.Mutex
	entries map[string]time.Time // "userID:token" -> usedAt
}

func NewTOTPReplayCache() *TOTPReplayCache {
	c := &TOTPReplayCache{entries: make(map[string]time.Time)}
	go c.runCleanup()
	return c
}

func (c *TOTPReplayCache) key(userID, token string) string {
	return userID + ":" + token
}

// isUsed reports whether token has already been consumed for userID within the window.
func (c *TOTPReplayCache) isUsed(userID, token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	usedAt, ok := c.entries[c.key(userID, token)]
	if !ok {
		return false
	}
	if time.Since(usedAt) > totpWindow {
		delete(c.entries, c.key(userID, token))
		return false
	}
	return true
}

// markUsed records token as consumed for userID.
func (c *TOTPReplayCache) markUsed(userID, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[c.key(userID, token)] = time.Now()
}

func (c *TOTPReplayCache) runCleanup() {
	ticker := time.NewTicker(totpWindow)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, usedAt := range c.entries {
			if now.Sub(usedAt) > totpWindow {
				delete(c.entries, k)
			}
		}
		c.mu.Unlock()
	}
}
