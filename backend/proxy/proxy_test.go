package proxy

import (
	"net/http"
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
