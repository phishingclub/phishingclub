package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
)

// newTestHandler returns a handler with a no-op logger so functions that log on
// their error paths can be exercised without a nil logger panic.
func newTestHandler() *ProxyHandler {
	return &ProxyHandler{logger: zap.NewNop().Sugar()}
}

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
// the upstream server.
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
// Referer headers, so the phishing domain is not leaked to the upstream server.
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
// phishing counterpart using the full host mapping. Otherwise the cookie keeps
// its upstream domain and the browser on the phishing domain drops it.
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

// The primary host cookie must be rewritten to its phishing counterpart when
// the full mapping is present.
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

// With no full mapping available the path falls back to the primary target to
// phish pair, so the primary cookie is still rewritten.
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

// --- host mapping ---

func TestBuildHostMapping_Directions(t *testing.T) {
	m := &ProxyHandler{}
	cfg := map[string]service.ProxyServiceDomainConfig{
		"login.microsoftonline.com": {To: "login.phish.example.com"},
		"empty.example.com":         {To: ""}, // no phish target, must be skipped
	}

	rev, revHosts := m.buildHostMapping(cfg, CONVERT_TO_ORIGINAL_URLS)
	if rev["login.phish.example.com"] != "login.microsoftonline.com" {
		t.Fatalf("reverse mapping wrong: %v", rev)
	}

	fwd, _ := m.buildHostMapping(cfg, CONVERT_TO_PHISHING_URLS)
	if fwd["login.microsoftonline.com"] != "login.phish.example.com" {
		t.Fatalf("forward mapping wrong: %v", fwd)
	}

	if len(revHosts) != 1 {
		t.Fatalf("entry with empty To must be skipped, got hosts: %v", revHosts)
	}
}

func TestPatchUrls_ForwardMapsOriginalToPhish(t *testing.T) {
	m := &ProxyHandler{}
	in := []byte("https://" + testOriginalHost + "/authorize")
	want := "https://" + testPhishHost + "/authorize"
	if got := string(m.patchUrls(testHostConfig(), in, CONVERT_TO_PHISHING_URLS)); got != want {
		t.Fatalf("forward patch\n got: %s\nwant: %s", got, want)
	}
}

func TestReplaceHostWithPhished(t *testing.T) {
	m := &ProxyHandler{}
	cfg := testHostConfig()

	if got := m.replaceHostWithPhished(testOriginalHost, cfg); got != testPhishHost {
		t.Fatalf("exact match\n got: %s\nwant: %s", got, testPhishHost)
	}
	if got := m.replaceHostWithPhished("foo."+testOriginalHost, cfg); got != "foo."+testPhishHost {
		t.Fatalf("subdomain match\n got: %s\nwant: foo.%s", got, testPhishHost)
	}
	if got := m.replaceHostWithPhished("unrelated.example.org", cfg); got != "" {
		t.Fatalf("no match must return empty, got: %s", got)
	}
}

func TestReplaceHostWithOriginal(t *testing.T) {
	m := &ProxyHandler{}
	cfg := testHostConfig()

	if got := m.replaceHostWithOriginal(testPhishHost, cfg); got != testOriginalHost {
		t.Fatalf("reverse host\n got: %s\nwant: %s", got, testOriginalHost)
	}
	if got := m.replaceHostWithOriginal("nomatch.example.org", cfg); got != "" {
		t.Fatalf("no match must return empty, got: %s", got)
	}
}

// --- content gates ---

func TestShouldProcessContent(t *testing.T) {
	m := &ProxyHandler{}
	process := []string{
		"text/html", "application/javascript", "application/x-javascript",
		"text/javascript", "text/css", "application/json; charset=utf-8",
	}
	for _, ct := range process {
		if !m.shouldProcessContent(ct) {
			t.Errorf("expected %q to be processed", ct)
		}
	}
	skip := []string{"image/png", "application/octet-stream", "font/woff2", ""}
	for _, ct := range skip {
		if m.shouldProcessContent(ct) {
			t.Errorf("expected %q to be skipped", ct)
		}
	}
}

func TestShouldCacheControlContent(t *testing.T) {
	m := &ProxyHandler{}
	yes := []string{"text/html", "application/javascript", "application/json"}
	for _, ct := range yes {
		if !m.shouldCacheControlContent(ct) {
			t.Errorf("expected cache control for %q", ct)
		}
	}
	// css is processed for rewriting but is not cache control managed
	no := []string{"text/css", "image/png", ""}
	for _, ct := range no {
		if m.shouldCacheControlContent(ct) {
			t.Errorf("expected no cache control for %q", ct)
		}
	}
}

// --- cookie helpers ---

func TestSameSiteToString(t *testing.T) {
	m := &ProxyHandler{}
	cases := map[http.SameSite]string{
		http.SameSiteDefaultMode: "Default",
		http.SameSiteLaxMode:     "Lax",
		http.SameSiteStrictMode:  "Strict",
		http.SameSiteNoneMode:    "None",
	}
	for in, want := range cases {
		if got := m.sameSiteToString(in); got != want {
			t.Errorf("sameSiteToString(%d)\n got: %s\nwant: %s", in, got, want)
		}
	}
	if got := m.sameSiteToString(http.SameSite(99)); got != "Unknown(99)" {
		t.Errorf("unknown same site\n got: %s\nwant: Unknown(99)", got)
	}
}

func TestAdjustCookieSettings_SecureBecomesSameSiteNone(t *testing.T) {
	m := &ProxyHandler{}
	ck := &http.Cookie{Name: "a", Secure: true}
	m.adjustCookieSettings(ck, nil, nil)
	if ck.SameSite != http.SameSiteNoneMode {
		t.Fatalf("secure cookie must become SameSite=None, got %v", ck.SameSite)
	}
}

func TestAdjustCookieSettings_DefaultBecomesLax(t *testing.T) {
	m := &ProxyHandler{}
	ck := &http.Cookie{Name: "a", Secure: false, SameSite: http.SameSiteDefaultMode}
	m.adjustCookieSettings(ck, nil, nil)
	if ck.SameSite != http.SameSiteLaxMode {
		t.Fatalf("non secure default cookie must become SameSite=Lax, got %v", ck.SameSite)
	}
}

func TestStripCookieDomainPort(t *testing.T) {
	if got := stripCookieDomainPort("login.example.com:8443"); got != "login.example.com" {
		t.Fatalf("port must be stripped, got %s", got)
	}
	if got := stripCookieDomainPort("login.example.com"); got != "login.example.com" {
		t.Fatalf("host without port unchanged, got %s", got)
	}
}

// --- config helpers ---

func TestConfigToMap(t *testing.T) {
	m := &ProxyHandler{}
	var sm sync.Map
	sm.Store(testOriginalHost, service.ProxyServiceDomainConfig{To: testPhishHost})

	out := m.configToMap(&sm)
	if len(out) != 1 || out[testOriginalHost].To != testPhishHost {
		t.Fatalf("configToMap wrong: %v", out)
	}
}

func TestBuildSessionConfig(t *testing.T) {
	m := &ProxyHandler{}
	pc := &service.ProxyServiceConfigYAML{
		Hosts: map[string]*service.ProxyServiceDomainConfig{
			"login.live.com": {To: "live.phish.example.com"},
		},
		Global: &service.ProxyServiceRules{
			Capture: []service.ProxyServiceCaptureRule{{Name: "creds"}},
		},
	}

	cfg := m.buildSessionConfig(testOriginalHost, testPhishHost, pc)

	if cfg[testOriginalHost].To != testPhishHost {
		t.Fatalf("primary target to phish mapping missing: %v", cfg[testOriginalHost])
	}
	if cfg["login.live.com"].To != "live.phish.example.com" {
		t.Fatalf("secondary host not merged: %v", cfg["login.live.com"])
	}
	if len(cfg[testOriginalHost].Capture) != 1 {
		t.Fatalf("global capture must be appended to the primary target, got %d", len(cfg[testOriginalHost].Capture))
	}
}

func TestSetProxyConfigDefaults(t *testing.T) {
	m := &ProxyHandler{}
	cfg := &service.ProxyServiceConfigYAML{
		Hosts: map[string]*service.ProxyServiceDomainConfig{
			testOriginalHost: {
				To:       testPhishHost,
				Capture:  []service.ProxyServiceCaptureRule{{Name: "creds"}},
				Response: []service.ProxyServiceResponseRule{{}},
				Access:   &service.ProxyServiceAccessControl{},
			},
		},
	}

	m.setProxyConfigDefaults(cfg)

	if cfg.Version != "0.0" {
		t.Errorf("version default\n got: %s\nwant: 0.0", cfg.Version)
	}
	hc := cfg.Hosts[testOriginalHost]
	if hc.Capture[0].Required == nil || !*hc.Capture[0].Required {
		t.Error("capture Required must default to true")
	}
	if hc.Response[0].Status != 200 {
		t.Errorf("response status default\n got: %d\nwant: 200", hc.Response[0].Status)
	}
	if hc.Access.Mode != "private" {
		t.Errorf("access mode default\n got: %s\nwant: private", hc.Access.Mode)
	}
	if hc.Access.OnDeny != "404" {
		t.Errorf("private on_deny default\n got: %s\nwant: 404", hc.Access.OnDeny)
	}
}

func TestParseProxyConfig_ValidAndInvalid(t *testing.T) {
	m := &ProxyHandler{}

	valid := testOriginalHost + ":\n  to: " + testPhishHost + "\n"
	cfg, err := m.parseProxyConfig(valid)
	if err != nil {
		t.Fatalf("valid config must parse: %v", err)
	}
	if cfg.Hosts[testOriginalHost] == nil || cfg.Hosts[testOriginalHost].To != testPhishHost {
		t.Fatalf("inline host not parsed: %v", cfg.Hosts)
	}

	if _, err := m.parseProxyConfig("- a\n- b\n"); err == nil {
		t.Fatal("a yaml sequence must fail to parse into the config struct")
	}
}

// --- capture: json path ---

func TestParseJSONPath(t *testing.T) {
	m := &ProxyHandler{}

	got := m.parseJSONPath("user.tokens[1].id")
	want := []jsonPathPart{
		{isArray: false, key: "user"},
		{isArray: false, key: "tokens"},
		{isArray: true, index: 1},
		{isArray: false, key: "id"},
	}
	if len(got) != len(want) {
		t.Fatalf("parts length\n got: %v\nwant: %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("part %d\n got: %+v\nwant: %+v", i, got[i], want[i])
		}
	}
}

func TestExtractJSONPath(t *testing.T) {
	m := &ProxyHandler{}
	data := map[string]interface{}{
		"user":   map[string]interface{}{"name": "alice"},
		"tokens": []interface{}{"t0", "t1"},
	}

	cases := []struct {
		path string
		want string
	}{
		{"user.name", "alice"},
		{"tokens[1]", "t1"},
		{"tokens[5]", ""}, // out of range
		{"user.age", ""},  // missing key
		{"missing", ""},   // missing top level
		{"", ""},          // empty path
	}
	for _, tc := range cases {
		if got := m.extractJSONPath(data, tc.path); got != tc.want {
			t.Errorf("extractJSONPath(%q)\n got: %s\nwant: %s", tc.path, got, tc.want)
		}
	}
}

func TestJSONValueToString(t *testing.T) {
	m := &ProxyHandler{}
	if got := m.jsonValueToString("x"); got != "x" {
		t.Errorf("string: got %s", got)
	}
	if got := m.jsonValueToString(float64(42)); got != "42" {
		t.Errorf("float whole: got %s", got)
	}
	if got := m.jsonValueToString(float64(3.5)); got != "3.5" {
		t.Errorf("float frac: got %s", got)
	}
	if got := m.jsonValueToString(true); got != "true" {
		t.Errorf("bool: got %s", got)
	}
	if got := m.jsonValueToString(nil); got != "" {
		t.Errorf("nil: got %s", got)
	}
	if got := m.jsonValueToString(map[string]interface{}{"a": "b"}); got != `{"a":"b"}` {
		t.Errorf("complex: got %s", got)
	}
}

// --- capture: rule matching ---

func TestMatchesPath(t *testing.T) {
	m := &ProxyHandler{}

	// nil PathRe matches anything
	req, _ := http.NewRequest(http.MethodGet, "https://x/anything", nil)
	if !m.matchesPath(service.ProxyServiceCaptureRule{}, req) {
		t.Error("nil PathRe must match")
	}

	rule := service.ProxyServiceCaptureRule{PathRe: regexp.MustCompile("^/login")}
	loginReq, _ := http.NewRequest(http.MethodGet, "https://x/login", nil)
	if !m.matchesPath(rule, loginReq) {
		t.Error("expected /login to match ^/login")
	}
	otherReq, _ := http.NewRequest(http.MethodGet, "https://x/other", nil)
	if m.matchesPath(rule, otherReq) {
		t.Error("expected /other not to match ^/login")
	}

	// empty path is normalized to /
	rootRule := service.ProxyServiceCaptureRule{PathRe: regexp.MustCompile("^/$")}
	emptyReq := &http.Request{URL: &url.URL{Path: ""}}
	if !m.matchesPath(rootRule, emptyReq) {
		t.Error("empty path must normalize to / and match ^/$")
	}
}

func TestShouldApplyCaptureRule(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodPost, "https://x/login", nil)

	// engine cookie is owned by the cookie path and never applies here
	if m.shouldApplyCaptureRule(service.ProxyServiceCaptureRule{Engine: "cookie"}, "request_body", req) {
		t.Error("engine cookie must not apply in the text pipeline")
	}
	// From mismatch
	if m.shouldApplyCaptureRule(service.ProxyServiceCaptureRule{From: "response_body"}, "request_body", req) {
		t.Error("From mismatch must not apply")
	}
	// From any applies
	if !m.shouldApplyCaptureRule(service.ProxyServiceCaptureRule{From: "any"}, "request_body", req) {
		t.Error("From any must apply")
	}
	// method mismatch
	if m.shouldApplyCaptureRule(service.ProxyServiceCaptureRule{Method: http.MethodGet}, "request_body", req) {
		t.Error("method mismatch must not apply")
	}
	// no constraints applies
	if !m.shouldApplyCaptureRule(service.ProxyServiceCaptureRule{}, "request_body", req) {
		t.Error("unconstrained rule must apply")
	}
}

func TestShouldProcessResponseBodyCapture(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodGet, "https://x/callback", nil)

	if m.shouldProcessResponseBodyCapture(service.ProxyServiceCaptureRule{Engine: "cookie"}, req) {
		t.Error("engine cookie must not process in response body pipeline")
	}
	// path based rule matching the request path
	pathRule := service.ProxyServiceCaptureRule{Path: "^/callback", PathRe: regexp.MustCompile("^/callback")}
	if !m.shouldProcessResponseBodyCapture(pathRule, req) {
		t.Error("path based rule matching the path must process")
	}
	// plain response body rule
	if !m.shouldProcessResponseBodyCapture(service.ProxyServiceCaptureRule{From: "response_body"}, req) {
		t.Error("response_body rule must process")
	}
}

// --- capture: extractors ---

func TestCaptureFromURLEncoded(t *testing.T) {
	m := &ProxyHandler{}
	rule := service.ProxyServiceCaptureRule{Name: "creds", Find: []string{"username", "password"}}

	got := m.captureFromURLEncoded("username=alice&password=secret&other=x", rule, nil, nil, "request_body")
	if got == nil {
		t.Fatal("expected a capture result")
	}
	if got["username"] != "alice" || got["password"] != "secret" {
		t.Fatalf("wrong fields captured: %v", got)
	}
	if got["capture_name"] != "creds" {
		t.Errorf("capture_name missing: %v", got)
	}
	if _, ok := got["other"]; ok {
		t.Error("unlisted field must not be captured")
	}

	// nothing matches
	if got := m.captureFromURLEncoded("foo=bar", rule, nil, nil, "request_body"); got != nil {
		t.Errorf("no matching field must return nil, got: %v", got)
	}
}

func TestCaptureFromJSON(t *testing.T) {
	m := newTestHandler()
	rule := service.ProxyServiceCaptureRule{Name: "creds", Find: []string{"user.name", "token"}}

	got := m.captureFromJSON(`{"user":{"name":"alice"},"token":"abc"}`, rule, nil, nil, "request_body")
	if got == nil || got["user.name"] != "alice" || got["token"] != "abc" {
		t.Fatalf("wrong json capture: %v", got)
	}

	// invalid json takes the logged error path and returns nil
	if got := m.captureFromJSON("{not json", rule, nil, nil, "request_body"); got != nil {
		t.Errorf("invalid json must return nil, got: %v", got)
	}
	// valid json without the fields
	if got := m.captureFromJSON(`{"unrelated":1}`, rule, nil, nil, "request_body"); got != nil {
		t.Errorf("no matching field must return nil, got: %v", got)
	}
}

func TestCaptureFromRegex(t *testing.T) {
	m := &ProxyHandler{}
	rule := service.ProxyServiceCaptureRule{Name: "tok", Find: "secret[0-9]+"}

	got, err := m.captureFromRegex("noise secret123 noise", rule, nil, nil, "response_body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["matched"] != "secret123" {
		t.Fatalf("wrong match: %v", got)
	}

	// no match returns nil map and nil error
	got, err = m.captureFromRegex("nothing here", rule, nil, nil, "response_body")
	if err != nil || got != nil {
		t.Fatalf("no match must return nil, nil; got %v, %v", got, err)
	}

	// invalid pattern returns an error
	bad := service.ProxyServiceCaptureRule{Name: "bad", Find: "[unclosed"}
	if _, err := m.captureFromRegex("x", bad, nil, nil, "response_body"); err == nil {
		t.Error("invalid regex must return an error")
	}
}

// --- access control ---

func TestGetClientIP(t *testing.T) {
	m := &ProxyHandler{}

	// X-Forwarded-For wins and its first entry is used
	req := &http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.1:1"}
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	req.Header.Set("X-Real-IP", "9.9.9.9")
	if got := m.getClientIP(req); got != "1.2.3.4" {
		t.Errorf("xff first\n got: %s\nwant: 1.2.3.4", got)
	}

	// X-Real-IP used when no X-Forwarded-For
	req2 := &http.Request{Header: http.Header{}}
	req2.Header.Set("X-Real-IP", "9.9.9.9")
	if got := m.getClientIP(req2); got != "9.9.9.9" {
		t.Errorf("x-real-ip\n got: %s\nwant: 9.9.9.9", got)
	}

	// RemoteAddr with port is stripped
	if got := m.getClientIP(&http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.1:5555"}); got != "10.0.0.1" {
		t.Errorf("remote addr with port\n got: %s\nwant: 10.0.0.1", got)
	}

	// RemoteAddr without port is returned as is
	if got := m.getClientIP(&http.Request{Header: http.Header{}, RemoteAddr: "10.0.0.1"}); got != "10.0.0.1" {
		t.Errorf("remote addr no port\n got: %s\nwant: 10.0.0.1", got)
	}

	// nothing available
	if got := m.getClientIP(&http.Request{Header: http.Header{}}); got != "" {
		t.Errorf("empty\n got: %s\nwant: (empty)", got)
	}
}

func TestCheckAccessRules(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodGet, "https://x/", nil)
	id := uuid.New()

	// nil access control allows everything
	if ok, _ := m.checkAccessRules("/", nil, false, &RequestContext{}, req); !ok {
		t.Error("nil access control must allow")
	}
	// public mode allows everything
	if ok, _ := m.checkAccessRules("/", &service.ProxyServiceAccessControl{Mode: "public"}, false, &RequestContext{}, req); !ok {
		t.Error("public mode must allow")
	}
	// private mode allows a lure request (has campaign recipient id)
	if ok, _ := m.checkAccessRules("/", &service.ProxyServiceAccessControl{Mode: "private"}, false, &RequestContext{CampaignRecipientID: &id}, req); !ok {
		t.Error("private mode must allow a lure request")
	}
	// private mode denies with the default action when there is no lure and no domain
	if ok, action := m.checkAccessRules("/", &service.ProxyServiceAccessControl{Mode: "private"}, false, &RequestContext{}, req); ok || action != "404" {
		t.Errorf("private deny default\n got ok=%v action=%s\nwant ok=false action=404", ok, action)
	}
	// private mode uses the configured on_deny action
	if ok, action := m.checkAccessRules("/", &service.ProxyServiceAccessControl{Mode: "private", OnDeny: "redirect:https://e.com"}, false, &RequestContext{}, req); ok || action != "redirect:https://e.com" {
		t.Errorf("private deny custom\n got ok=%v action=%s", ok, action)
	}
	// unknown mode falls back to allow
	if ok, _ := m.checkAccessRules("/", &service.ProxyServiceAccessControl{Mode: "weird"}, false, &RequestContext{}, req); !ok {
		t.Error("unknown mode must allow")
	}
}

func TestApplyDefaultPrivateMode(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodGet, "https://x/", nil)
	id := uuid.New()

	if ok, _ := m.applyDefaultPrivateMode(&RequestContext{CampaignRecipientID: &id}, req); !ok {
		t.Error("lure request must be allowed")
	}
	if ok, action := m.applyDefaultPrivateMode(&RequestContext{}, req); ok || action != "404" {
		t.Errorf("no lure must deny 404\n got ok=%v action=%s", ok, action)
	}
}

func TestEvaluatePathAccess(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodGet, "https://x/dashboard", nil)

	// domain specific rule (public) allows
	rcDomain := &RequestContext{
		Domain:      &database.Domain{},
		PhishDomain: testPhishHost,
		ProxyConfig: &service.ProxyServiceConfigYAML{
			Hosts: map[string]*service.ProxyServiceDomainConfig{
				testOriginalHost: {To: testPhishHost, Access: &service.ProxyServiceAccessControl{Mode: "public"}},
			},
		},
	}
	if ok, _ := m.evaluatePathAccess("/dashboard", rcDomain, false, req); !ok {
		t.Error("domain public rule must allow")
	}

	// no matching domain rule, global public allows
	rcGlobal := &RequestContext{
		ProxyConfig: &service.ProxyServiceConfigYAML{
			Global: &service.ProxyServiceRules{Access: &service.ProxyServiceAccessControl{Mode: "public"}},
		},
	}
	if ok, _ := m.evaluatePathAccess("/x", rcGlobal, false, req); !ok {
		t.Error("global public rule must allow")
	}

	// no configuration at all denies via default private mode
	if ok, action := m.evaluatePathAccess("/x", &RequestContext{}, false, req); ok || action != "404" {
		t.Errorf("no config must deny 404\n got ok=%v action=%s", ok, action)
	}
}

// --- forwarding rules ---

func TestCheckForwardInRules(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodGet, "https://x/api/data", nil)

	global := &service.ProxyServiceRules{Response: []service.ProxyServiceResponseRule{
		{Path: "^/api", PathRe: regexp.MustCompile("^/api"), Forward: true},
	}}
	if !m.checkForwardInGlobalRules(global, req) {
		t.Error("matching global rule with forward true must forward")
	}

	other, _ := http.NewRequest(http.MethodGet, "https://x/other", nil)
	if m.checkForwardInGlobalRules(global, other) {
		t.Error("non matching path must not forward")
	}
	if m.checkForwardInGlobalRules(nil, req) {
		t.Error("nil rules must not forward")
	}

	// a matching domain rule returns its own Forward value, not a constant
	domainForward := &service.ProxyServiceDomainConfig{Response: []service.ProxyServiceResponseRule{
		{Path: "^/api", PathRe: regexp.MustCompile("^/api"), Forward: true},
	}}
	if !m.checkForwardInDomainRules(domainForward, req) {
		t.Error("matching domain rule with forward true must forward")
	}
	domainNoForward := &service.ProxyServiceDomainConfig{Response: []service.ProxyServiceResponseRule{
		{Path: "^/api", PathRe: regexp.MustCompile("^/api"), Forward: false},
	}}
	if m.checkForwardInDomainRules(domainNoForward, req) {
		t.Error("matching domain rule with forward false must not forward")
	}
}

func TestRewriteRuleMatchesRequest(t *testing.T) {
	m := &ProxyHandler{}
	req, _ := http.NewRequest(http.MethodPost, "https://x/login", nil)

	// nil request matches only an unconstrained rule
	if !m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{}, nil) {
		t.Error("nil req, unconstrained rule must match")
	}
	if m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{Method: http.MethodGet}, nil) {
		t.Error("nil req with a method constraint must not match")
	}

	// method mismatch and case insensitive match
	if m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{Method: http.MethodGet}, req) {
		t.Error("method mismatch must not match")
	}
	if !m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{Method: "post"}, req) {
		t.Error("method match is case insensitive")
	}

	// path set but not compiled is skipped safely
	if m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{Path: "^/login"}, req) {
		t.Error("path without compiled regex must not match")
	}
	// compiled path match
	if !m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{Path: "^/login", PathRe: regexp.MustCompile("^/login")}, req) {
		t.Error("compiled path must match")
	}
	// unconstrained matches
	if !m.rewriteRuleMatchesRequest(service.ProxyServiceReplaceRule{}, req) {
		t.Error("unconstrained rule must match")
	}
}

// --- response header rewrites ---

func TestRemoveSecurityHeaders(t *testing.T) {
	m := &ProxyHandler{}
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Content-Security-Policy", "default-src 'self'")
	resp.Header.Set("Strict-Transport-Security", "max-age=63072000")
	resp.Header.Set("X-Frame-Options", "DENY")
	resp.Header.Set("Content-Type", "text/html")

	m.removeSecurityHeaders(resp)

	for _, h := range []string{"Content-Security-Policy", "Strict-Transport-Security", "X-Frame-Options"} {
		if resp.Header.Get(h) != "" {
			t.Errorf("%s must be removed", h)
		}
	}
	if resp.Header.Get("Content-Type") != "text/html" {
		t.Error("non security header must be kept")
	}
}

func TestRewriteLocationHeaderWithoutSession(t *testing.T) {
	m := &ProxyHandler{}
	cfg := testHostConfig()

	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Location", "https://"+testOriginalHost+"/next")
	m.rewriteLocationHeaderWithoutSession(resp, cfg)
	if got, want := resp.Header.Get("Location"), "https://"+testPhishHost+"/next"; got != want {
		t.Errorf("location rewrite\n got: %s\nwant: %s", got, want)
	}

	// empty Location is a no-op
	resp2 := &http.Response{Header: http.Header{}}
	m.rewriteLocationHeaderWithoutSession(resp2, cfg)
	if resp2.Header.Get("Location") != "" {
		t.Error("empty location must stay empty")
	}

	// unmatched host stays unchanged
	resp3 := &http.Response{Header: http.Header{}}
	resp3.Header.Set("Location", "https://other.example.org/x")
	m.rewriteLocationHeaderWithoutSession(resp3, cfg)
	if resp3.Header.Get("Location") != "https://other.example.org/x" {
		t.Error("unmatched host must stay unchanged")
	}
}

func TestRewriteCORSHeaderWithoutSession(t *testing.T) {
	m := &ProxyHandler{}
	cfg := testHostConfig()

	// matched origin is rewritten and credentials are enabled
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Access-Control-Allow-Origin", "https://"+testOriginalHost)
	m.rewriteCORSHeaderWithoutSession(resp, cfg)
	if got, want := resp.Header.Get("Access-Control-Allow-Origin"), "https://"+testPhishHost; got != want {
		t.Errorf("cors origin rewrite\n got: %s\nwant: %s", got, want)
	}
	if resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials must be enabled for a specific origin")
	}

	// wildcard origin is left alone and does not enable credentials
	resp2 := &http.Response{Header: http.Header{}}
	resp2.Header.Set("Access-Control-Allow-Origin", "*")
	m.rewriteCORSHeaderWithoutSession(resp2, cfg)
	if resp2.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("wildcard origin must stay wildcard")
	}
	if resp2.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Error("wildcard origin must not enable credentials")
	}
}

// --- body rewrites and variables ---

func TestApplyRegexReplacement(t *testing.T) {
	m := newTestHandler()

	rule := service.ProxyServiceReplaceRule{Find: "foo", Replace: "bar"}
	if got := string(m.applyRegexReplacement([]byte("a foo b foo"), rule, "sid")); got != "a bar b bar" {
		t.Errorf("replacement\n got: %s\nwant: a bar b bar", got)
	}

	// no match returns the original body
	if got := string(m.applyRegexReplacement([]byte("nothing"), rule, "sid")); got != "nothing" {
		t.Errorf("no match must be unchanged, got: %s", got)
	}

	// invalid regex returns the body unchanged
	bad := service.ProxyServiceReplaceRule{Find: "[unclosed", Replace: "x"}
	if got := string(m.applyRegexReplacement([]byte("keep me"), bad, "sid")); got != "keep me" {
		t.Errorf("invalid regex must be unchanged, got: %s", got)
	}
}

func TestRewritePathsInContent(t *testing.T) {
	m := newTestHandler()

	// anchors are stripped so the pattern matches mid string
	rule := service.ProxyServiceURLRewriteRule{Find: "^/old/", Replace: "/new/"}
	if got := m.rewritePathsInContent(`<a href="/old/page">x</a>`, rule); got != `<a href="/new/page">x</a>` {
		t.Errorf("path rewrite\n got: %s", got)
	}

	// invalid regex returns the content unchanged
	bad := service.ProxyServiceURLRewriteRule{Find: "[unclosed", Replace: "y"}
	if got := m.rewritePathsInContent("keep", bad); got != "keep" {
		t.Errorf("invalid regex must be unchanged, got: %s", got)
	}
}

func TestInterpolateVariables(t *testing.T) {
	m := &ProxyHandler{}
	data := map[string]string{"FirstName": "Alice", "Email": "a@e.com"}

	// disabled context returns the input unchanged
	if got := m.interpolateVariables("Hi {{.FirstName}}", &VariablesContext{Enabled: false, Data: data}); got != "Hi {{.FirstName}}" {
		t.Error("disabled context must not interpolate")
	}
	// nil context returns the input unchanged
	if got := m.interpolateVariables("Hi {{.FirstName}}", nil); got != "Hi {{.FirstName}}" {
		t.Error("nil context must not interpolate")
	}
	// no template syntax returns the input unchanged
	if got := m.interpolateVariables("plain", &VariablesContext{Enabled: true, Data: data}); got != "plain" {
		t.Error("input without template syntax must be unchanged")
	}
	// with no allow list all known variables are replaced
	if got := m.interpolateVariables("Hi {{.FirstName}} <{{.Email}}>", &VariablesContext{Enabled: true, Data: data}); got != "Hi Alice <a@e.com>" {
		t.Errorf("all variables must interpolate, got: %s", got)
	}
	// an allow list restricts interpolation to the allowed variable
	ctx := &VariablesContext{
		Enabled: true,
		Data:    data,
		Config:  &service.ProxyServiceVariablesConfig{Enabled: true, Allowed: []string{"FirstName"}},
	}
	if got := m.interpolateVariables("Hi {{.FirstName}} <{{.Email}}>", ctx); got != "Hi Alice <{{.Email}}>" {
		t.Errorf("only allowed variable must interpolate, got: %s", got)
	}
}

// --- body decompression ---

func gzipBytes(t *testing.T, in []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func deflateBytes(t *testing.T, in []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := flate.NewWriter(&b, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func brotliBytes(t *testing.T, in []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := brotli.NewWriter(&b)
	if _, err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func zstdBytes(t *testing.T, in []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := zstd.NewWriter(&b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func makeEncodedResponse(encoding string, body []byte) *http.Response {
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}
	if encoding != "" {
		resp.Header.Set("Content-Encoding", encoding)
	}
	return resp
}

func TestReadAndDecompressBody(t *testing.T) {
	m := newTestHandler()
	payload := []byte("hello world, hello world, hello world")

	cases := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"gzip", "gzip", gzipBytes(t, payload)},
		{"deflate", "deflate", deflateBytes(t, payload)},
		{"brotli", "br", brotliBytes(t, payload)},
		{"zstd", "zstd", zstdBytes(t, payload)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := makeEncodedResponse(tc.encoding, tc.body)
			got, wasCompressed, err := m.readAndDecompressBody(resp, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !wasCompressed {
				t.Error("must report the body was compressed")
			}
			if string(got) != string(payload) {
				t.Fatalf("decompressed body\n got: %s\nwant: %s", got, payload)
			}
		})
	}

	// no encoding is returned as is and reported not compressed
	resp := makeEncodedResponse("", payload)
	got, wasCompressed, err := m.readAndDecompressBody(resp, false)
	if err != nil || wasCompressed || string(got) != string(payload) {
		t.Fatalf("plain body\n got: %s was: %v err: %v", got, wasCompressed, err)
	}
}

func TestReadAndDecompressBody_AlreadyDecompressed(t *testing.T) {
	m := newTestHandler()

	// header claims gzip but the body is plain, so the reader fails and the
	// stale Content-Encoding header is removed and the body returned as is
	resp := makeEncodedResponse("gzip", []byte("not actually gzip"))
	got, wasCompressed, err := m.readAndDecompressBody(resp, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasCompressed {
		t.Error("must report not compressed")
	}
	if string(got) != "not actually gzip" {
		t.Errorf("body must be unchanged, got: %s", got)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Error("stale content-encoding header must be removed")
	}
}

func TestUpdateResponseBody_RecompressGzip(t *testing.T) {
	m := newTestHandler()
	payload := []byte("hello world round trip")

	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Content-Encoding", "gzip")

	m.updateResponseBody(resp, payload, true)

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Error("content-encoding must be kept when recompressing")
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("body must be valid gzip: %v", err)
	}
	out, _ := io.ReadAll(gz)
	if string(out) != string(payload) {
		t.Fatalf("round trip mismatch\n got: %s\nwant: %s", out, payload)
	}
}

func TestUpdateResponseBody_UncompressedWhenEncodingRemoved(t *testing.T) {
	m := newTestHandler()
	payload := []byte("plain body")

	// wasCompressed is true but the encoding header was already removed, so the
	// body must be sent uncompressed with a matching content length
	resp := &http.Response{Header: http.Header{}}
	m.updateResponseBody(resp, payload, true)

	out, _ := io.ReadAll(resp.Body)
	if string(out) != string(payload) {
		t.Fatalf("body\n got: %s\nwant: %s", out, payload)
	}
	if resp.ContentLength != int64(len(payload)) {
		t.Errorf("content length\n got: %d\nwant: %d", resp.ContentLength, len(payload))
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(payload)) {
		t.Errorf("content-length header\n got: %s\nwant: %d", resp.Header.Get("Content-Length"), len(payload))
	}
}

func TestUpdateResponseBody_NotCompressedStripsEncoding(t *testing.T) {
	m := newTestHandler()
	payload := []byte("plain")

	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Content-Encoding", "gzip") // stale header

	m.updateResponseBody(resp, payload, false)

	out, _ := io.ReadAll(resp.Body)
	if string(out) != string(payload) {
		t.Fatalf("body\n got: %s\nwant: %s", out, payload)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Error("stale content-encoding must be stripped for an uncompressed body")
	}
}
