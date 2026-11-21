package utils

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractClientIP(t *testing.T) {
	headers := []string{
		"CF-Connecting-IP", "True-Client-IP", "X-Forwarded-For", "X-Real-IP",
		"X-Client-IP", "Connecting-IP", "Client-IP",
	}
	cases := []struct {
		name    string
		trusted []string
		header  string
		want    string
	}{
		{"no proxies, no header", nil, "", "198.51.100.7"},
		{"untrusted remote, header ignored", []string{"10.0.0.1"}, "203.0.113.9", "198.51.100.7"},
		{"trusted remote by ip", []string{"198.51.100.7"}, "203.0.113.9", "203.0.113.9"},
		{"trusted remote by cidr", []string{"198.51.100.0/24"}, "203.0.113.9", "203.0.113.9"},
		{"trusted remote, no header", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"trusted remote, header not an ip", []string{"198.51.100.7"}, "evil", "198.51.100.7"},
		{"trusted remote, overlong header", []string{"198.51.100.7"}, strings.Repeat("1", 500), "198.51.100.7"},
		{"trusted remote, ip with port", []string{"198.51.100.7"}, "203.0.113.9:1234", "203.0.113.9"},
		{"trusted remote, ipv6 with port", []string{"198.51.100.7"}, "[2001:db8::1]:1234", "2001:db8::1"},
		{"trusted remote, comma list", []string{"198.51.100.7"}, "203.0.113.9, 10.0.0.1", "203.0.113.9"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "198.51.100.7:4444"
		if tc.header != "" {
			req.Header.Set("X-Forwarded-For", tc.header)
		}
		if got := ExtractClientIP(req, IPTrust{Proxies: tc.trusted}); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	// with no trusted proxies every forwarding header is ignored
	for _, h := range headers {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "198.51.100.7:4444"
		req.Header.Set(h, "203.0.113.9")
		for _, trusted := range [][]string{nil, {}} {
			if got := ExtractClientIP(req, IPTrust{Proxies: trusted, Header: h}); got != "198.51.100.7" {
				t.Errorf("%s with no trusted proxies: got %q, want the connection address", h, got)
			}
		}
	}

	// an invalid header falls back to the next valid one
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.7:4444"
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := ExtractClientIP(req, IPTrust{Proxies: []string{"198.51.100.7"}}); got != "203.0.113.9" {
		t.Errorf("invalid first header: got %q, want 203.0.113.9", got)
	}
	// no headers and no connection address gives an empty IP
	empty := httptest.NewRequest("GET", "/", nil)
	empty.RemoteAddr = ""
	if got := ExtractClientIP(empty, IPTrust{}); got != "" {
		t.Errorf("empty remote: got %q, want empty", got)
	}
}

func TestExtractClientIPTrustedHeader(t *testing.T) {
	trust := IPTrust{Proxies: []string{"198.51.100.7", "10.0.0.0/8"}, Header: "X-Real-IP"}
	newReq := func(remote string, headers map[string]string) *http.Request {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = remote
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req
	}
	cases := []struct {
		name    string
		trust   IPTrust
		remote  string
		headers map[string]string
		want    string
	}{
		{"configured header is used", trust, "198.51.100.7:1",
			map[string]string{"X-Real-IP": "203.0.113.9"}, "203.0.113.9"},
		{"other headers passed through are ignored", trust, "198.51.100.7:1",
			map[string]string{"X-Real-IP": "203.0.113.9", "CF-Connecting-IP": "1.2.3.4", "X-Forwarded-For": "1.2.3.4"}, "203.0.113.9"},
		{"configured header missing gives the connection address", trust, "198.51.100.7:1",
			map[string]string{"CF-Connecting-IP": "1.2.3.4"}, "198.51.100.7"},
		{"configured header not an ip gives the connection address", trust, "198.51.100.7:1",
			map[string]string{"X-Real-IP": "evil"}, "198.51.100.7"},
		{"untrusted connection ignores the configured header", trust, "192.0.2.1:1",
			map[string]string{"X-Real-IP": "203.0.113.9"}, "192.0.2.1"},
		{"header without proxies is ignored", IPTrust{Header: "X-Real-IP"}, "192.0.2.1:1",
			map[string]string{"X-Real-IP": "203.0.113.9"}, "192.0.2.1"},
		{"list is read from the right", IPTrust{Proxies: trust.Proxies, Header: "X-Forwarded-For"}, "198.51.100.7:1",
			map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.9"}, "203.0.113.9"},
		{"trusted hops on the right are skipped", IPTrust{Proxies: trust.Proxies, Header: "X-Forwarded-For"}, "198.51.100.7:1",
			map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.9, 10.0.0.5"}, "203.0.113.9"},
		{"only trusted hops gives the leftmost", IPTrust{Proxies: trust.Proxies, Header: "X-Forwarded-For"}, "198.51.100.7:1",
			map[string]string{"X-Forwarded-For": "10.0.0.6, 10.0.0.5"}, "10.0.0.6"},
		{"junk entry on the right gives the connection address", IPTrust{Proxies: trust.Proxies, Header: "X-Forwarded-For"}, "198.51.100.7:1",
			map[string]string{"X-Forwarded-For": "203.0.113.9, evil"}, "198.51.100.7"},
		{"header name is case insensitive", IPTrust{Proxies: trust.Proxies, Header: "x-real-ip"}, "198.51.100.7:1",
			map[string]string{"X-Real-IP": "203.0.113.9"}, "203.0.113.9"},
	}
	for _, tc := range cases {
		if got := ExtractClientIP(newReq(tc.remote, tc.headers), tc.trust); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIPMatchesListNormalForm(t *testing.T) {
	if !IPMatchesList("2001:db8::1", []string{"2001:0db8:0:0:0:0:0:1"}) {
		t.Error("expected long form ipv6 entry to match")
	}
	if IPMatchesList("2001:db8::2", []string{"2001:0db8:0:0:0:0:0:1"}) {
		t.Error("expected a different ipv6 address not to match")
	}
}
