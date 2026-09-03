package remotebrowser

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// TestRenderHTMLToPDFRoutesThroughGuard drives the real production entrypoint and
// asserts that a report embedding a loopback resource cannot reach it. This is the
// only test that exercises RenderHTMLToPDF itself, so it catches removal of the
// guard wiring from the real render path (not just flag drift). Skips without a
// browser. The produced PDF is the positive control that the render actually ran.
func TestRenderHTMLToPDFRoutesThroughGuard(t *testing.T) {
	bin := ""
	for _, cand := range []string{"/usr/bin/google-chrome", "/usr/bin/chromium-browser", "/usr/bin/chromium"} {
		if _, err := os.Stat(cand); err == nil {
			bin = cand
			break
		}
	}
	if bin == "" {
		t.Skip("no chrome/chromium binary available")
	}

	var hits int
	var mu sync.Mutex
	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer victim.Close()

	html := `<html><body>Report<img src="` + victim.URL + `/ssrf"></body></html>`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pdf, err := RenderHTMLToPDF(ctx, html, bin)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(pdf) == 0 {
		t.Fatalf("no PDF produced; render did not run so the guard was not exercised")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("loopback resource reached %d times through RenderHTMLToPDF; guard wiring missing", hits)
	}
}

// startAllowAllProxy is a permissive forward proxy used only to isolate the flag
// behavior from the guard policy: it forwards everything (including loopback).
func startAllowAllProxy(t *testing.T) (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tr := &http.Transport{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			upstream, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
			if err != nil {
				http.Error(w, "", http.StatusBadGateway)
				return
			}
			client, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				_ = upstream.Close()
				return
			}
			_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
			_, _ = io.Copy(client, upstream)
			_ = client.Close()
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		resp, err := tr.RoundTrip(out)
		if err != nil {
			http.Error(w, "", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), func() { _ = srv.Close() }
}

// TestNormalResourcesLoadWithProductionFlags is a smoke test: it confirms Chrome
// launches with the full production flag set (including --disable-quic and the
// WebRTC policy) and can still fetch a normal proxied resource, and that
// proxy-bypass-list=<-loopback> correctly routes even loopback through the proxy.
// It exercises the plain HTTP forward path, not the https/QUIC negotiation path,
// so it does not by itself prove QUIC fallback; that rests on the reasoning that
// QUIC is only ever reached after a TCP connection and always has a TCP fallback.
// Skips when no browser is present.
func TestNormalResourcesLoadWithProductionFlags(t *testing.T) {
	bin := ""
	for _, cand := range []string{"/usr/bin/google-chrome", "/usr/bin/chromium-browser", "/usr/bin/chromium"} {
		if _, err := os.Stat(cand); err == nil {
			bin = cand
			break
		}
	}
	if bin == "" {
		t.Skip("no chrome/chromium binary available")
	}

	var hits int
	var mu sync.Mutex
	res := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "image/gif")
		// 1x1 gif so the browser treats it as a real successful image load
		w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b})
	}))
	defer res.Close()

	proxyAddr, closeProxy := startAllowAllProxy(t)
	defer closeProxy()

	// the real production flag set, via the shared helper, plus the PNA check
	// disables so the fetch is attributable to the flags rather than a masking
	// browser feature.
	l := applyGuardFlags(
		launcher.New().Bin(bin).Headless(true).
			Set("no-sandbox").Set("disable-gpu").Set("disable-dev-shm-usage"),
		proxyAddr,
	).
		Set("disable-features", "PrivateNetworkAccessChecks,LocalNetworkAccessChecks,BlockInsecurePrivateNetworkRequests")
	u, err := l.Launch()
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer l.Cleanup()
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer browser.Close()

	pg := browser.MustPage()
	_ = pg.SetDocumentContent(`<html><body><img src="` + res.URL + `/normal.gif"></body></html>`)

	deadline := time.Now().Add(8 * time.Second)
	for func() int { mu.Lock(); defer mu.Unlock(); return hits }() == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	pg.Close()

	mu.Lock()
	defer mu.Unlock()
	if hits == 0 {
		t.Fatalf("normal http resource did not load with the production flags (disable-quic may have broken fetching)")
	}
}
