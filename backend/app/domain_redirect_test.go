package app

import (
	"net/http"
	"testing"
)

func TestDomainRedirectStatus(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		path   string
		target string
		status int
		ok     bool
	}{
		{"empty target", "a.test", "/", "", 0, false},
		{"relative other path", "a.test", "/x", "/landing", http.StatusFound, true},
		{"relative same path", "a.test", "/landing", "/landing", 0, false},
		{"relative same path with query", "a.test", "/landing", "/landing?a=1", 0, false},
		{"relative same path after clean", "a.test", "/foo/../landing/", "/landing", 0, false},
		{"relative root same path", "a.test", "/", "/", 0, false},
		{"relative root from other path", "a.test", "/x", "/", http.StatusFound, true},
		{"absolute other host", "a.test", "/x", "https://b.test/x", http.StatusMovedPermanently, true},
		{"absolute same host other path", "a.test", "/x", "https://a.test/y", http.StatusMovedPermanently, true},
		{"absolute same host same path", "a.test", "/x", "https://A.test/x", 0, false},
		{"absolute same host root", "a.test", "/", "https://a.test", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, ok := domainRedirectStatus(tt.host, tt.path, tt.target)
			if ok != tt.ok || status != tt.status {
				t.Fatalf("got (%d, %v), want (%d, %v)", status, ok, tt.status, tt.ok)
			}
		})
	}
}
