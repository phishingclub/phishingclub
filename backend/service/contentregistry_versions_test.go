package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/phishingclub/phishingclub/registry"
)

func TestIsDowngrade(t *testing.T) {
	cases := []struct {
		installed, latest string
		want              bool
	}{
		{"1.2.0", "1.1.9", true},
		{"1.2.0", "1.2.0", false},
		{"1.2.0", "1.3.0", false},
		{"2.0.0", "2.0.0-rc.1", true},
		{"dev", "1.0.0", false},
	}
	for _, c := range cases {
		if got := isDowngrade(c.installed, c.latest); got != c.want {
			t.Errorf("isDowngrade(%q, %q) = %v, want %v", c.installed, c.latest, got, c.want)
		}
	}
}

// TestResolveVersionsStopsWhenUnreachable: a registry that drops every
// connection is not asked again for the lookups not yet started.
func TestResolveVersionsStopsWhenUnreachable(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("no hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = "acme/p" + strconv.Itoa(i)
	}
	s := &ContentRegistry{}
	got := s.resolveVersions(context.Background(), registry.NewClient(srv.URL, "", true), ids)
	if len(got) != len(ids) {
		t.Fatalf("expected an entry per id, got %d", len(got))
	}
	for id, lk := range got {
		if lk.vr != nil {
			t.Fatalf("%s resolved against a failing registry", id)
		}
	}
	if n := hits.Load(); n > registryLookupConcurrency {
		t.Fatalf("expected at most %d lookups after the first failure, got %d", registryLookupConcurrency, n)
	}
}

// TestResolveVersionsKeepsGoingOnAnswers: a refused or missing package is an
// answer, not an outage, so every other package is still looked up and each
// keeps its own result.
func TestResolveVersionsKeepsGoingOnAnswers(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/acme/locked/"):
			w.WriteHeader(http.StatusForbidden)
		case strings.Contains(r.URL.Path, "/acme/gone/"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/acme/broken/"):
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"version":"1.0.0","contentHash":"aa","archiveSha":"bb"}`))
		}
	}))
	defer srv.Close()
	ids := []string{"acme/locked", "acme/gone", "acme/broken"}
	for i := 0; i < 20; i++ {
		ids = append(ids, "acme/ok"+strconv.Itoa(i))
	}
	s := &ContentRegistry{}
	got := s.resolveVersions(context.Background(), registry.NewClient(srv.URL, "", true), ids)
	if !errors.Is(got["acme/locked"].err, registry.ErrUnauthorized) {
		t.Fatalf("locked: %v", got["acme/locked"].err)
	}
	var se *registry.StatusError
	if !errors.As(got["acme/gone"].err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("gone: %v", got["acme/gone"].err)
	}
	if got["acme/broken"].vr != nil || got["acme/broken"].err == nil {
		t.Fatalf("broken: %+v", got["acme/broken"])
	}
	for _, id := range ids[3:] {
		if got[id].vr == nil || got[id].err != nil {
			t.Fatalf("%s not resolved: %v", id, got[id].err)
		}
	}
}
