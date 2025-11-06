package ja4plus

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
)

// JA4Middleware is a helper to plug the JA4 fingerprinting into your HTTP server.
// It only exists because there is no direct way to pass information from the TLS handshake to the HTTP handler.
// Usage:
//
//	ja4middleware := ja4plus.JA4Middleware{}
//	srv := http.Server{
//		Handler: ja4middleware.Wrap(...),
//		TLSConfig: &tls.Config{
//			GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
//				ja4middleware.StoreFingerprintFromClientHello(chi)
//				return nil, nil
//			},
//		},
//		ConnState: ja4middleware.ConnStateCallback,
//	}
//	srv.ListenAndServeTLS("cert.pem", "key.pem")
//
// Afterwards the fingerprint can be accessed via [JA4FromContext]
type JA4Middleware struct {
	connectionFingerprints sync.Map
}

// StoreFingerprintFromClientHello stores the JA4 fingerprint of the provided [tls.ClientHelloInfo] in the middleware.
func (m *JA4Middleware) StoreFingerprintFromClientHello(hello *tls.ClientHelloInfo) {
	m.connectionFingerprints.Store(hello.Conn.RemoteAddr().String(), JA4(hello))
}

// ConnStateCallback is a callback that should be set as the [http.Server]'s ConnState to clean up the fingerprint cache.
func (m *JA4Middleware) ConnStateCallback(conn net.Conn, state http.ConnState) {
	switch state {
	case http.StateClosed, http.StateHijacked:
		m.connectionFingerprints.Delete(conn.RemoteAddr().String())
	}
}

type ja4FingerprintCtxKey struct{}

// Wrap wraps the provided [http.Handler] and injects the JA4 fingerprint into the [http.Request.Context].
// It requires a server set up with [JA4Middleware] to work.
func (m *JA4Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if cacheEntry, _ := m.connectionFingerprints.Load(r.RemoteAddr); cacheEntry != nil {
			ctx = context.WithValue(ctx, ja4FingerprintCtxKey{}, cacheEntry.(string))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// JA4FromContext extracts the JA4 fingerprint from the provided [http.Request.Context].
// It requires a server set up with [JA4Middleware] to work.
func JA4FromContext(ctx context.Context) string {
	if fingerprint, ok := ctx.Value(ja4FingerprintCtxKey{}).(string); ok {
		return fingerprint
	}
	return ""
}
