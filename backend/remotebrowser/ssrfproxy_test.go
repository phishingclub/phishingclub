package remotebrowser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

func TestIsInternalIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1": true, "127.5.5.5": true, "::1": true,
		"10.0.0.5": true, "172.16.9.9": true, "172.31.1.1": true, "192.168.1.1": true,
		"169.254.169.254": true, "169.254.1.1": true,
		"100.100.100.200": true, // carrier grade NAT (some metadata services)
		"0.0.0.0": true, "0.1.2.3": true, // this network
		"192.0.0.170": true, // NAT64 discovery
		"198.18.0.1":  true, // benchmarking
		"240.0.0.1":   true, // reserved
		"255.255.255.255": true, // broadcast
		"::": true,
		"fc00::1": true, "fd12:3456::1": true, "fe80::1": true,
		"::ffff:10.0.0.1": true, "::ffff:127.0.0.1": true, // v4 mapped
		"64:ff9b::7f00:1": true, // NAT64 wrapping 127.0.0.1
		"2002:7f00:1::":   true, // 6to4 wrapping 127.0.0.1
		"::a00:1":         true, // v4 compatible wrapping 10.0.0.1
		"2001::1":         true, // Teredo range
		"224.0.0.1":       true, // multicast
		"ff02::1":         true, // v6 multicast
		"8.8.8.8": false, "1.1.1.1": false, "203.0.113.7": false, "2606:4700::1111": false,
	}
	for ipStr, want := range cases {
		if got := isInternalIP(net.ParseIP(ipStr)); got != want {
			t.Errorf("isInternalIP(%s)=%v want %v", ipStr, got, want)
		}
	}
	if !isInternalIP(nil) {
		t.Errorf("nil IP must be treated as internal (fail closed)")
	}
}

// The proxy must refuse a request whose target is internal (here loopback).
func TestGuardedProxyBlocksInternal(t *testing.T) {
	var hits int
	var mu sync.Mutex
	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Write([]byte("REACHED"))
	}))
	defer victim.Close()

	proxy, err := startGuardedProxy()
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.close()

	proxyURL, _ := url.Parse("http://" + proxy.addr())
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(victim.URL + "/ssrf")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("expected the proxy to block loopback, got status %d", resp.StatusCode)
		}
	}
	if proxy.blockedRequests() == 0 {
		t.Errorf("proxy did not record a blocked request")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("internal victim was reached %d times; the guard failed", hits)
	}
}

// TestGuardFlagsConfigured locks the critical guard launcher flags so a change
// that removes the proxy or adds back a loopback bypass fails in CI without a
// browser. The renderer uses this same applyGuardFlags, so the two cannot drift.
func TestGuardFlagsConfigured(t *testing.T) {
	l := applyGuardFlags(launcher.New(), "127.0.0.1:12345")
	if got := l.Get("proxy-server"); got != "127.0.0.1:12345" {
		t.Errorf("proxy-server = %q, want the guard proxy address", got)
	}
	if got := l.Get("proxy-bypass-list"); got != "<-loopback>" {
		t.Errorf("proxy-bypass-list = %q, want <-loopback> so loopback routes through the guard", got)
	}
	if !l.Has("disable-quic") {
		t.Errorf("disable-quic flag missing (UDP egress path could bypass the guard)")
	}
	if got := l.Get("force-webrtc-ip-handling-policy"); got != "disable_non_proxied_udp" {
		t.Errorf("force-webrtc-ip-handling-policy = %q, want disable_non_proxied_udp", got)
	}
	if !l.Has("disable-background-networking") {
		t.Errorf("disable-background-networking flag missing")
	}
}

// proxyClient returns an http client that sends all requests through the proxy.
func proxyClient(p *guardedProxy) *http.Client {
	proxyURL, _ := url.Parse("http://" + p.addr())
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
}

// The proxy must block the cloud metadata endpoint (plain HTTP path).
func TestGuardedProxyBlocksMetadataLiteral(t *testing.T) {
	proxy, err := startGuardedProxy()
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.close()
	resp, err := proxyClient(proxy).Get("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("metadata endpoint not blocked, status %d", resp.StatusCode)
		}
	}
	if proxy.blockedRequests() == 0 {
		t.Errorf("metadata request was not recorded as blocked")
	}
}

// A hostname that resolves to an internal address must be blocked (the DNS
// rebinding chokepoint: resolution happens inside the guard).
func TestGuardedProxyBlocksHostnameResolvingInternal(t *testing.T) {
	// only meaningful if localhost actually resolves to an internal address; skip
	// otherwise so a resolution quirk cannot make this pass for the wrong reason.
	ips, lerr := net.LookupIP("localhost")
	if lerr != nil || len(ips) == 0 || !isInternalIP(ips[0]) {
		t.Skip("localhost does not resolve to an internal address here")
	}
	proxy, err := startGuardedProxy()
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.close()
	// localhost resolves to 127.0.0.1 / ::1, both internal.
	resp, err := proxyClient(proxy).Get("http://localhost/")
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("hostname resolving to loopback not blocked, status %d", resp.StatusCode)
		}
	}
	if proxy.blockedRequests() == 0 {
		t.Errorf("localhost request was not recorded as blocked")
	}
}

// The CONNECT (https) path must also block internal targets.
func TestGuardedProxyBlocksConnectInternal(t *testing.T) {
	proxy, err := startGuardedProxy()
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.close()
	// https forces the client to issue CONNECT to the proxy.
	_, err = proxyClient(proxy).Get("https://127.0.0.1:1/")
	if err == nil {
		t.Errorf("expected the CONNECT to an internal target to fail")
	}
	if proxy.blockedRequests() == 0 {
		t.Errorf("CONNECT to internal target was not recorded as blocked")
	}
}

// dialGuarded must NOT classify a public destination as blocked. It may fail to
// connect (no network), but the failure must not be the policy block.
func TestDialGuardedAllowsPublicDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialGuarded(ctx, "tcp", "8.8.8.8:80")
	if conn != nil {
		conn.Close()
	}
	if err != nil && errors.Is(err, errBlocked) {
		t.Errorf("public destination 8.8.8.8 was wrongly blocked by policy")
	}
}

// End to end: proves the production Chrome flags route egress through the guard so
// the browser cannot reach loopback (verified directly by the victim counter). A
// positive control (the proxy's blocked count reaching the two internal targets
// given) ensures the requests actually fired, so the test cannot pass vacuously.
// Metadata endpoint blocking specifically is covered without a browser by
// TestGuardedProxyBlocksMetadataLiteral. Skips when no browser binary is present.
func TestReportBrowserEgressGuarded(t *testing.T) {
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
	_, victimPort, _ := net.SplitHostPort(victim.Listener.Addr().String())

	proxy, err := startGuardedProxy()
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.close()

	l := applyGuardFlags(
		launcher.New().Bin(bin).Headless(true).
			Set("no-sandbox").Set("disable-gpu").Set("disable-dev-shm-usage"),
		proxy.addr(),
	).
		// turn off Chrome's own network access checks so a 0 result is attributable
		// to this guard, not to a masking browser feature.
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
	_ = pg.SetDocumentContent(fmt.Sprintf(`<html><body>
	  <img src="http://127.0.0.1:%s/ssrf">
	  <img src="http://169.254.169.254/latest/meta-data/">
	</body></html>`, victimPort))

	// positive control: wait until the guard has refused both internal targets. If
	// the requests never fire, this stays below 2 and the test fails loudly rather
	// than passing without exercising the guard.
	deadline := time.Now().Add(10 * time.Second)
	for proxy.blockedRequests() < 2 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	pg.Close()

	if got := proxy.blockedRequests(); got < 2 {
		t.Fatalf("guard refused %d requests; expected it to refuse both loopback and metadata (the requests may not have fired)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("loopback victim reached %d times through the report browser; SSRF guard failed", hits)
	}
}
