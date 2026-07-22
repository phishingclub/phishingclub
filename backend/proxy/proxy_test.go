package proxy

import (
	"net/http"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/service"
)

const (
	testOriginalHost = "login.microsoftonline.com"
	testPhishHost    = "login.phish.example.com"
)

func testHostConfig() map[string]service.ProxyServiceDomainConfig {
	return map[string]service.ProxyServiceDomainConfig{
		testOriginalHost: {To: testPhishHost},
	}
}

// patchUrls must reverse a phishing host back to the upstream host when a
// request travels toward the upstream server.
func TestPatchUrls_ReverseMapsPhishHostToOriginal(t *testing.T) {
	m := &ProxyHandler{}
	in := []byte("https://" + testPhishHost + "/oauth2/authorize?scope=openid")
	want := "https://" + testOriginalHost + "/oauth2/authorize?scope=openid"

	got := string(m.patchUrls(testHostConfig(), in, CONVERT_TO_ORIGINAL_URLS))
	if got != want {
		t.Fatalf("reverse map failed\n got: %s\nwant: %s", got, want)
	}
}

// A request without a session must still reverse the phishing host embedded in
// a query parameter such as redirect_uri, so the phishing domain is not sent to
// the upstream server. This is the concrete leak the fix closes.
func TestPatchQueryParameters_ReverseMapsRedirectURI(t *testing.T) {
	m := &ProxyHandler{}
	reqCtx := &RequestContext{ConfigMap: testHostConfig()}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://"+testPhishHost+"/authorize?client_id=abc&redirect_uri=https%3A%2F%2F"+testPhishHost+"%2Fcb",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	m.patchQueryParametersWithContext(req, reqCtx)

	got := req.URL.Query().Get("redirect_uri")
	want := "https://" + testOriginalHost + "/cb"
	if got != want {
		t.Fatalf("redirect_uri not reverse mapped\n got: %s\nwant: %s", got, want)
	}
	if cid := req.URL.Query().Get("client_id"); cid != "abc" {
		t.Fatalf("unrelated parameter altered: client_id=%s", cid)
	}
}

// With no mapping the query patch is a safe no-op and must not panic. This
// documents that populating the mapping is what enables the reverse map.
func TestPatchQueryParameters_EmptyConfigIsNoOp(t *testing.T) {
	m := &ProxyHandler{}
	reqCtx := &RequestContext{}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://"+testPhishHost+"/authorize?redirect_uri=https%3A%2F%2F"+testPhishHost+"%2Fcb",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	m.patchQueryParametersWithContext(req, reqCtx)

	if got := req.URL.Query().Get("redirect_uri"); got != "https://"+testPhishHost+"/cb" {
		t.Fatalf("unexpected rewrite with empty config: %s", got)
	}
}

// normalizeRequestHeaders must reverse the phishing host in the Origin and
// Referer headers. This guards against regressing to an unpopulated header
// config, which would leak the phishing domain to the upstream server.
func TestNormalizeRequestHeaders_ReverseMapsOriginAndReferer(t *testing.T) {
	m := &ProxyHandler{}

	session := &service.ProxySession{}
	for host, cfg := range testHostConfig() {
		session.Config.Store(host, cfg)
	}

	req, err := http.NewRequest(http.MethodGet, "https://"+testPhishHost+"/authorize", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://"+testPhishHost)
	req.Header.Set("Referer", "https://"+testPhishHost+"/foo")

	m.normalizeRequestHeaders(req, session)

	if got, want := req.Header.Get("Origin"), "https://"+testOriginalHost; got != want {
		t.Fatalf("Origin not reverse mapped\n got: %s\nwant: %s", got, want)
	}
	if got, want := req.Header.Get("Referer"), "https://"+testOriginalHost+"/foo"; got != want {
		t.Fatalf("Referer not reverse mapped\n got: %s\nwant: %s", got, want)
	}
}

// --- multi host upstream cookie rewriting ---

// multiHostConfig maps two upstream hosts to two phishing hosts, mirroring an
// AiTM flow that spans more than one upstream login server.
func multiHostConfig() map[string]service.ProxyServiceDomainConfig {
	return map[string]service.ProxyServiceDomainConfig{
		"login.microsoftonline.com": {To: "login.phish.example.com"},
		"login.live.com":            {To: "live.phish.example.com"},
	}
}

func newCookieResponse(t *testing.T, setCookies ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://login.phish.example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{Header: make(http.Header), Request: req}
	for _, sc := range setCookies {
		resp.Header.Add("Set-Cookie", sc)
	}
	return resp
}

// cookieDomain returns the Domain of the named cookie without a leading dot.
func cookieDomain(t *testing.T, resp *http.Response, name string) string {
	t.Helper()
	for _, ck := range resp.Cookies() {
		if ck.Name == name {
			return strings.TrimPrefix(ck.Domain, ".")
		}
	}
	t.Fatalf("cookie %q not found in response", name)
	return ""
}

// A Set-Cookie from a secondary upstream host must be rewritten to that host's
// phishing counterpart using the full host mapping. This fails while the cookie
// path only knows the primary target to phish pair, so the secondary cookie
// keeps its upstream domain and the browser on the phishing domain drops it.
func TestProcessCookies_SecondaryHostRewrittenToPhish(t *testing.T) {
	m := &ProxyHandler{}
	reqCtx := &RequestContext{
		TargetDomain: "login.microsoftonline.com",
		PhishDomain:  "login.phish.example.com",
		ConfigMap:    multiHostConfig(),
	}
	resp := newCookieResponse(t, "ESTSAUTH=abc; Domain=login.live.com; Path=/; Secure")

	m.processCookiesForPhishingDomainWithContext(resp, reqCtx)

	if got, want := cookieDomain(t, resp, "ESTSAUTH"), "live.phish.example.com"; got != want {
		t.Fatalf("secondary host cookie domain\n got: %s\nwant: %s", got, want)
	}
}

// The primary host cookie must keep being rewritten when the full mapping is
// present. Regression guard, expected to pass before and after the fix.
func TestProcessCookies_PrimaryHostRewritten(t *testing.T) {
	m := &ProxyHandler{}
	reqCtx := &RequestContext{
		TargetDomain: "login.microsoftonline.com",
		PhishDomain:  "login.phish.example.com",
		ConfigMap:    multiHostConfig(),
	}
	resp := newCookieResponse(t, "ESTSAUTHPERSISTENT=xyz; Domain=login.microsoftonline.com; Path=/; Secure")

	m.processCookiesForPhishingDomainWithContext(resp, reqCtx)

	if got, want := cookieDomain(t, resp, "ESTSAUTHPERSISTENT"), "login.phish.example.com"; got != want {
		t.Fatalf("primary host cookie domain\n got: %s\nwant: %s", got, want)
	}
}

// With no full mapping available the path must fall back to the primary target
// to phish pair, so the primary cookie is still rewritten. Regression guard,
// expected to pass before and after the fix.
func TestProcessCookies_FallbackWhenConfigMapEmpty(t *testing.T) {
	m := &ProxyHandler{}
	reqCtx := &RequestContext{
		TargetDomain: "login.microsoftonline.com",
		PhishDomain:  "login.phish.example.com",
	}
	resp := newCookieResponse(t, "ESTSAUTHPERSISTENT=xyz; Domain=login.microsoftonline.com; Path=/; Secure")

	m.processCookiesForPhishingDomainWithContext(resp, reqCtx)

	if got, want := cookieDomain(t, resp, "ESTSAUTHPERSISTENT"), "login.phish.example.com"; got != want {
		t.Fatalf("fallback primary cookie domain\n got: %s\nwant: %s", got, want)
	}
}

// --- registrable domain for the session cookie ---

// extractTopLevelDomain must return the registrable domain so the session
// cookie is scoped correctly. The naive last two labels approach breaks for
// multi label public suffixes such as co.uk, for IP hosts and for hosts that
// carry a port.
func TestExtractTopLevelDomain(t *testing.T) {
	m := &ProxyHandler{}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple com", "login.evilcorp.com", "evilcorp.com"},
		{"two label", "evilcorp.com", "evilcorp.com"},
		{"deep subdomain com", "a.b.c.evilcorp.com", "evilcorp.com"},
		{"dev test tld", "login.proxysaurous.test", "proxysaurous.test"},
		{"single label", "localhost", "localhost"},
		{"multi label suffix", "login.evilcorp.co.uk", "evilcorp.co.uk"},
		{"deep multi label suffix", "a.b.evilcorp.co.uk", "evilcorp.co.uk"},
		{"ip address", "127.0.0.1", "127.0.0.1"},
		{"host with port", "login.evilcorp.com:8443", "evilcorp.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.extractTopLevelDomain(tc.in); got != tc.want {
				t.Fatalf("extractTopLevelDomain(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}
