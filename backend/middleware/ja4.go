package middleware

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/exaring/ja4plus"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	// HeaderJA4 is the internal header key for ja4 fingerprint
	HeaderJA4 = "X-JA4"

	// ContextKeyJA4 is the gin context key for ja4 fingerprint
	ContextKeyJA4 = "ja4_fingerprint"
)

// FingerprintEntry stores a ja4 fingerprint with timestamp
type FingerprintEntry struct {
	Fingerprint string
	LastAccess  time.Time
}

// JA4Middleware handles ja4+ fingerprinting for tls connections
type JA4Middleware struct {
	ConnectionFingerprints sync.Map
	logger                 *zap.SugaredLogger
}

// NewJA4Middleware creates a new ja4 middleware instance
func NewJA4Middleware(logger *zap.SugaredLogger) *JA4Middleware {
	m := &JA4Middleware{
		logger: logger,
	}

	// start periodic cleanup routine to prevent memory leaks
	// in case ConnState callback doesn't fire reliably
	go m.periodicCleanup()

	return m
}

// periodicCleanup removes stale fingerprint entries
func (m *JA4Middleware) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		staleThreshold := 10 * time.Minute
		count := 0

		m.ConnectionFingerprints.Range(func(key, value interface{}) bool {
			if entry, ok := value.(*FingerprintEntry); ok {
				if now.Sub(entry.LastAccess) > staleThreshold {
					m.ConnectionFingerprints.Delete(key)
					count++
				}
			}
			return true
		})

	}
}

// StoreFingerprintFromClientHello stores the ja4 fingerprint from tls clienthello
func (m *JA4Middleware) StoreFingerprintFromClientHello(hello *tls.ClientHelloInfo) {
	fingerprint := ja4plus.JA4(hello)
	entry := &FingerprintEntry{
		Fingerprint: fingerprint,
		LastAccess:  time.Now(),
	}
	m.ConnectionFingerprints.Store(hello.Conn.RemoteAddr().String(), entry)
}

// ConnStateCallback cleans up fingerprint cache when connection closes
func (m *JA4Middleware) ConnStateCallback(conn net.Conn, state http.ConnState) {
	switch state {
	case http.StateClosed, http.StateHijacked:
		addr := conn.RemoteAddr().String()
		m.ConnectionFingerprints.Delete(addr)
	}
}

// GinHandler returns a gin handler that injects ja4 fingerprint into context and headers
func (m *JA4Middleware) GinHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// try to get fingerprint from cache
		if cacheEntry, ok := m.ConnectionFingerprints.Load(c.Request.RemoteAddr); ok {
			if entry, ok := cacheEntry.(*FingerprintEntry); ok {
				fingerprint := entry.Fingerprint

				// update last access time
				entry.LastAccess = time.Now()

				// set as internal header for downstream use
				c.Request.Header.Set(HeaderJA4, fingerprint)

				// set in gin context
				c.Set(ContextKeyJA4, fingerprint)
			}
		}

		c.Next()
	}
}

// GetConfigForClient returns a tls.Config callback for capturing clienthello
func (m *JA4Middleware) GetConfigForClient(hello *tls.ClientHelloInfo) (*tls.Config, error) {
	m.StoreFingerprintFromClientHello(hello)
	return nil, nil
}

// GetJA4FromContext extracts the ja4 fingerprint from gin context
func GetJA4FromContext(c *gin.Context) string {
	if fingerprint, exists := c.Get(ContextKeyJA4); exists {
		if fp, ok := fingerprint.(string); ok {
			return fp
		}
	}
	return ""
}

// GetJA4FromHeader extracts the ja4 fingerprint from request headers
func GetJA4FromHeader(c *gin.Context) string {
	return c.Request.Header.Get(HeaderJA4)
}
