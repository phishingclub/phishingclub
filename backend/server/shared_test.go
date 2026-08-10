package server

import (
	"net/url"
	"testing"

	"github.com/phishingclub/phishingclub/lure"
)

func TestLastPathSegment(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/4H7K9QM2XR3T", "4H7K9QM2XR3T", true},
		{"/account/login/4H7K9QM2XR3T", "4H7K9QM2XR3T", true},
		// a trailing slash is added by some mail clients and previews
		{"/account/4H7K9QM2XR3T/", "4H7K9QM2XR3T", true},
		{"/logo.png", "logo.png", true},
		{"/", "", false},
		{"", "", false},
		// traversal is refused rather than cleaned
		{"/a/../b", "", false},
	}
	for _, c := range cases {
		got, ok := LastPathSegment(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("LastPathSegment(%q) = %q,%v want %q,%v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestLureCodeFromPath(t *testing.T) {
	// URL.Path is already decoded, so how the path was written can only be judged
	// on the parsed URL and not on a path string
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://example.com/4H7K9QM2XR3T", "4H7K9QM2XR3T", true},
		{"https://example.com/account/4H7K9QM2XR3T/", "4H7K9QM2XR3T", true},
		{"https://example.com/special-42", "special-42", true},
		{"https://example.com/", "", false},
		// an encoded separator decodes into a real one and would move which segment
		// is last, so the request is refused
		{"https://example.com/a%2Fb", "", false},
		{"https://example.com/a%2fb", "", false},
		// characters go escapes when re encoding set RawPath on their own, and
		// IsValidCustom accepts them, so they must still resolve
		{"https://example.com/invoice(1)", "invoice(1)", true},
		{"https://example.com/special!42", "special!42", true},
		{"https://example.com/a%5Bb%5D", "a[b]", true},
		{"https://example.com/caf%C3%A9-42", "café-42", true},
		// a redundant encoding resolves to the code it spells
		{"https://example.com/%41BCDEF", "ABCDEF", true},
		// values IsCandidate rules out never reach a database probe
		{"https://example.com/a%20b", "", false},
		{"https://example.com/..", "", false},
		{"https://example.com/invoice..pdf", "", false},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatalf("failed to parse %q: %v", c.raw, err)
		}
		got, ok := lureCodeFromPath(u)
		if ok != c.ok || got != c.want {
			t.Errorf("lureCodeFromPath(%q) = %q,%v want %q,%v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestStorableCustomCodesResolveFromPath(t *testing.T) {
	// the two rules live in different packages, so a value only one side accepts
	// is a link that gets delivered and never resolves
	for _, code := range []string{
		"special-42",
		"Special_42",
		"HR-Survey-2026",
		"invoice.pdf",
		".hidden",
		"invoice..pdf",
		"v1..2",
		"..",
		".",
		"a b",
	} {
		u, err := url.Parse("https://example.com/login/" + code)
		if err != nil {
			t.Fatalf("failed to parse a URL carrying %q: %v", code, err)
		}
		got, ok := lureCodeFromPath(u)
		storable := lure.IsValidCustom(code)
		if storable != ok {
			t.Errorf("IsValidCustom(%q) = %v but lureCodeFromPath = %v", code, storable, ok)
		}
		if ok && got != code {
			t.Errorf("lureCodeFromPath returned %q for %q", got, code)
		}
	}
}

func TestTrimLastPathSegment(t *testing.T) {
	// a consumed code must leave the path before forwarding, or rewrite rules
	// that compare the path exactly stop matching
	cases := []struct {
		path string
		want string
	}{
		{"/signin/4H7K9QM2XR3T", "/signin"},
		{"/a/b/4H7K9QM2XR3T", "/a/b"},
		{"/4H7K9QM2XR3T", "/"},
		{"/signin/4H7K9QM2XR3T/", "/signin"},
		{"/", "/"},
	}
	for _, c := range cases {
		if got := TrimLastPathSegment(c.path); got != c.want {
			t.Errorf("TrimLastPathSegment(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
