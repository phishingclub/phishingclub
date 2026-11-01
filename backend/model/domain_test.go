package model

import (
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func TestValidateDomainNameAccepts(t *testing.T) {
	valid := []string{
		"example.com",
		"phishing.club.microsoft.test",
		"phishing.club.dark-water.test",
		"zz-e2e-c-1700000000.test",
		"xn--bcher-kva.example",
		"localhost",
		"a.b",
		"10.0.0.1",
		strings.Repeat("a", 63) + ".test",
		strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 61),
	}
	for _, n := range valid {
		if err := ValidateDomainName(n); err != nil {
			t.Errorf("expected %q to be valid, got %v", n, err)
		}
	}
}

func TestValidateDomainNameRejects(t *testing.T) {
	invalid := []string{
		"",
		"Example.com",
		"*.example.com",
		"example.com.",
		".example.com",
		"exa mple.com",
		"example..com",
		"-example.com",
		"example-.com",
		"example.com:443",
		"https://example.com",
		"example.com/path",
		"exa_mple.com",
		"bücher.example",
		"example.com\n",
		strings.Repeat("a", 64) + ".test",
		strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 62),
	}
	for _, n := range invalid {
		if err := ValidateDomainName(n); err == nil {
			t.Errorf("expected %q to be rejected", n)
		}
	}
}

func TestValidateDomainNameReserved(t *testing.T) {
	cases := []struct {
		name     string
		reserved []string
		wantErr  bool
	}{
		{"admin.example.com", []string{"admin.example.com"}, true},
		{"admin.example.com", []string{"ADMIN.Example.com"}, true},
		{"admin.example.com", []string{"admin.example.com:8002"}, true},
		{"admin.example.com", []string{"admin.example.com."}, true},
		{"admin.example.com", []string{"", "other.test", "admin.example.com"}, true},
		{"phish.example.com", []string{"admin.example.com"}, false},
		{"phish.example.com", []string{""}, false},
		{"phish.example.com", nil, false},
		{"127.0.0.1", []string{"127.0.0.1:8002"}, true},
	}
	for _, c := range cases {
		err := ValidateDomainName(c.name, c.reserved...)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateDomainName(%q, %v) err = %v, wantErr %v", c.name, c.reserved, err, c.wantErr)
		}
	}
}

func TestValidateDomainRedirectURL(t *testing.T) {
	ok := []string{
		"",
		"https://example.com",
		"http://example.com/a?b=c",
		"https://localhost:8443/x",
		"/",
		"/landing",
		"/a/b?c=d#e",
	}
	for _, u := range ok {
		if err := ValidateDomainRedirectURL(u); err != nil {
			t.Errorf("expected %q to be valid, got %v", u, err)
		}
	}
	bad := []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"//example.com",
		"//x",
		"/\\example.com",
		"\\\\example.com",
		"/landing\n",
		"/a\tb",
		"/a\x00",
		"/a\x7f",
		"relative",
		"ftp://example.com",
		"file:///etc/passwd",
		"https://",
	}
	for _, u := range bad {
		if err := ValidateDomainRedirectURL(u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
}

func regularDomain(name, redirect string) *Domain {
	return &Domain{
		Name:                nullable.NewNullableWithValue(*vo.NewString255Must(name)),
		HostWebsite:         nullable.NewNullableWithValue(true),
		ManagedTLS:          nullable.NewNullableWithValue(false),
		PageContent:         nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust("")),
		PageNotFoundContent: nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust("")),
		RedirectURL:         nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(redirect)),
	}
}

func TestDomainValidateUsesNameAndRedirectRules(t *testing.T) {
	if err := regularDomain("phishing.club.google.test", "").Validate(); err != nil {
		t.Fatalf("expected valid domain, got %v", err)
	}
	if err := regularDomain("phishing.club.google.test", "https://example.com").Validate(); err != nil {
		t.Fatalf("expected valid redirect, got %v", err)
	}
	if err := regularDomain("Bad_Name.test", "").Validate(); err == nil {
		t.Fatal("expected invalid name to be rejected")
	}
	if err := regularDomain("ok.test", "javascript:alert(1)").Validate(); err == nil {
		t.Fatal("expected javascript redirect to be rejected")
	}
	if err := regularDomain("ok.test", "/landing").Validate(); err != nil {
		t.Fatalf("expected path redirect to be valid, got %v", err)
	}
}

func TestDomainValidateExistingSkipsNameFormat(t *testing.T) {
	// a stored name from before the format rules stays editable
	for _, n := range []string{"Legacy_Name.test", "under_score.example.com", "Upper.test"} {
		if err := regularDomain(n, "").ValidateExisting(); err != nil {
			t.Errorf("expected stored name %q to pass on update, got %v", n, err)
		}
		if err := regularDomain(n, "").Validate(); err == nil {
			t.Errorf("expected new name %q to be rejected", n)
		}
	}
	// a stored redirect is not checked again on update
	if err := regularDomain("ok.test", "legacy").ValidateExisting(); err != nil {
		t.Errorf("expected stored redirect to pass on update, got %v", err)
	}
	missing := regularDomain("ok.test", "")
	missing.Name = nullable.NewNullNullable[vo.String255]()
	if err := missing.ValidateExisting(); err == nil {
		t.Error("expected missing name to be rejected on update")
	}
}

func TestNormalizeDomainName(t *testing.T) {
	cases := map[string]string{
		"Example.COM":    "example.com",
		"  phish.test  ": "phish.test",
		"example.com.":   "example.com",
		" Example.Com. ": "example.com",
		"example.com..":  "example.com.",
		"already.ok":     "already.ok",
		"":               "",
	}
	for in, want := range cases {
		if got := NormalizeDomainName(in); got != want {
			t.Errorf("NormalizeDomainName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDomainNameErrorNamesUnsupportedChars(t *testing.T) {
	for _, n := range []string{"exa_mple.com", "[::1]", "::1"} {
		err := ValidateDomainName(n)
		if err == nil || !strings.Contains(err.Error(), "underscores and IPv6 addresses are not supported") {
			t.Errorf("ValidateDomainName(%q) = %v, want unsupported characters message", n, err)
		}
	}
}

func TestValidateWebhookURL(t *testing.T) {
	ok := []string{"https://hooks.example.com/x", "http://api-test-server/webhook"}
	for _, u := range ok {
		if err := ValidateWebhookURL(u); err != nil {
			t.Errorf("expected %q to be valid, got %v", u, err)
		}
	}
	bad := []string{"", "file:///etc/passwd", "gopher://x", "javascript:alert(1)", "example.com/hook", "//example.com"}
	for _, u := range bad {
		if err := ValidateWebhookURL(u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
	w := &Webhook{
		Name: nullable.NewNullableWithValue(*vo.NewString127Must("hook")),
		URL:  nullable.NewNullableWithValue(*vo.NewString1024Must("ftp://example.com")),
	}
	if err := w.Validate(); err == nil {
		t.Fatal("expected webhook with ftp url to be rejected")
	}
	w.URL = nullable.NewNullableWithValue(*vo.NewString1024Must("https://example.com"))
	if err := w.Validate(); err != nil {
		t.Fatalf("expected webhook to validate, got %v", err)
	}
}
