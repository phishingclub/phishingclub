package registry

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCheckPreviewImage(t *testing.T) {
	p := pngBytes(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	html := []byte(`<html><script>alert(1)</script></html>`)
	cases := []struct {
		name     string
		declared string
		body     []byte
		ok       bool
	}{
		{"png", "image/png", p, true},
		{"png with params", "image/png; charset=binary", p, true},
		{"declared jpeg sniffed png", "image/jpeg", p, true},
		{"svg declared", "image/svg+xml", svg, false},
		{"svg body as png", "image/png", svg, false},
		{"html body as png", "image/png", html, false},
		{"text type", "text/html", p, false},
		{"empty type", "", p, false},
		{"bmp", "image/bmp", p, false},
	}
	// polyglots: image magic bytes followed by markup sniff as the image type, so they
	// pass, but they are served as that image type with nosniff, a sandbox CSP and
	// inline disposition, where the browser never parses them as a document.
	polyglots := []struct {
		name string
		body []byte
		want string
	}{
		{"gif then html", []byte("GIF89a<html><script>alert(1)</script></html>"), "image/gif"},
		{"png magic then svg", append([]byte("\x89PNG\r\n\x1a\n"), svg...), "image/png"},
	}
	for _, c := range polyglots {
		ct, err := CheckPreviewImage("image/png", c.body)
		if err != nil || ct != c.want {
			t.Errorf("%s: expected %s, got %q %v", c.name, c.want, ct, err)
		}
	}
	for _, c := range cases {
		ct, err := CheckPreviewImage(c.declared, c.body)
		if c.ok && (err != nil || ct != "image/png") {
			t.Errorf("%s: expected ok image/png, got %q %v", c.name, ct, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected rejection", c.name)
		}
	}
}

func TestFetchPreviewImage(t *testing.T) {
	p := pngBytes(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(p)
		case "/evil.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
		case "/lie.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte(`<html><script>alert(1)</script></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "", true)
	if _, ct, err := c.FetchPreviewImage(context.Background(), srv.URL+"/ok.png"); err != nil || ct != "image/png" {
		t.Fatalf("png rejected: %q %v", ct, err)
	}
	if _, _, err := c.FetchPreviewImage(context.Background(), "/evil.svg"); err == nil {
		t.Fatal("svg accepted")
	}
	if _, _, err := c.FetchPreviewImage(context.Background(), "/lie.png"); err == nil {
		t.Fatal("html labelled png accepted")
	}
	if _, _, err := c.FetchPreviewImage(context.Background(), "https://other.example.com/ok.png"); err == nil {
		t.Fatal("foreign host accepted")
	}
}

func TestClientRefusedTokenIsTyped(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		c := NewClient(srv.URL, "bad", true)
		_, err := c.Info(context.Background())
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%d: expected ErrUnauthorized, got %v", code, err)
		}
		if _, _, err := c.FetchVerified(context.Background(), "acme", "x"); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%d: fetch expected ErrUnauthorized, got %v", code, err)
		}
		if _, _, err := c.FetchPreviewImage(context.Background(), "/p.png"); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%d: preview expected ErrUnauthorized, got %v", code, err)
		}
		srv.Close()
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "", true).Info(context.Background())
	if err == nil || errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "/api/") {
		t.Fatalf("404 should be a plain error without the path, got %v", err)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var otherHits atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "secret", true)
	if _, err := c.Info(context.Background()); err == nil {
		t.Fatal("redirected info accepted")
	}
	if _, _, err := c.FetchPreviewImage(context.Background(), "/p.png"); err == nil {
		t.Fatal("redirected preview accepted")
	}
	if _, _, err := c.FetchVerified(context.Background(), "acme", "x"); err == nil {
		t.Fatal("redirected fetch accepted")
	}
	if n := otherHits.Load(); n != 0 {
		t.Fatalf("redirect target was contacted %d times", n)
	}
}

func TestClientUnexpectedAnswerIsTyped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html>login</html>`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "", true)
	_, err := c.Info(context.Background())
	if !errors.Is(err, ErrUnexpectedAnswer) || strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("info: expected ErrUnexpectedAnswer, got %v", err)
	}
	if _, err := c.GetVersion(context.Background(), "acme", "x"); !errors.Is(err, ErrUnexpectedAnswer) {
		t.Fatalf("version: expected ErrUnexpectedAnswer, got %v", err)
	}
	var se *StatusError
	nf := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer nf.Close()
	_, err = NewClient(nf.URL, "", true).Info(context.Background())
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("404: expected a StatusError, got %v", err)
	}
}
