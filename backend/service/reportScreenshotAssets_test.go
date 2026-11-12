package service

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/repository"
)

func writeTestFile(t *testing.T, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReportAssetsLookup(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "assets")
	writeTestFile(t, filepath.Join(base, "secret.txt"), "secret")
	writeTestFile(t, filepath.Join(root, "Phish.test", "img", "logo.png"), "domain logo")
	writeTestFile(t, filepath.Join(root, "Phish.test", "both.css"), "domain css")
	writeTestFile(t, filepath.Join(root, "shared", "both.css"), "shared css")
	writeTestFile(t, filepath.Join(root, "shared", "fonts", "a.woff2"), "shared font")
	writeTestFile(t, filepath.Join(root, "other.test", "logo.png"), "other company logo")
	if err := os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(root, "Phish.test", "link.txt")); err != nil {
		t.Fatal(err)
	}
	big, err := os.Create(filepath.Join(root, "Phish.test", "big.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(reportAssetMaxSize + 1); err != nil {
		t.Fatal(err)
	}
	big.Close()

	companyA := uuid.New()
	companyB := uuid.New()
	assets := newReportAssets(root, []repository.DomainNameCompany{
		{Name: "Phish.test", CompanyID: &companyA},
		{Name: "global.test"},
		{Name: "other.test", CompanyID: &companyB},
	}, &companyA)

	cases := []struct {
		url       string
		wantLocal bool
		wantBody  string
		wantType  string
	}{
		{"https://phish.test/img/logo.png", true, "domain logo", "image/png"},
		{"http://PHISH.test:8080/img/logo.png?v=1", true, "domain logo", "image/png"},
		{"https://phish.test./img/logo.png", true, "domain logo", "image/png"},
		{"https://phish.test/both.css", true, "domain css", "text/css; charset=utf-8"},
		{"https://phish.test/fonts/a.woff2", true, "shared font", "font/woff2"},
		{"https://global.test/fonts/a.woff2", true, "shared font", "font/woff2"},
		{"https://phish.test/missing.png", true, "", ""},
		{"https://phish.test/", true, "", ""},
		{"https://phish.test/img", true, "", ""},
		{"https://phish.test/../secret.txt", true, "", ""},
		{"https://phish.test/%2e%2e/%2e%2e/secret.txt", true, "", ""},
		{"https://phish.test/link.txt", true, "", ""},
		{"https://phish.test/big.png", true, "", ""},
		{"https://other.test/logo.png", true, "", ""},
		{"https://cdn.example.com/logo.png", false, "", ""},
		{"wss://phish.test/img/logo.png", false, "", ""},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		file, local := assets.lookup(u)
		if local != tc.wantLocal {
			t.Errorf("%s: local %v, want %v", tc.url, local, tc.wantLocal)
			continue
		}
		if tc.wantBody == "" {
			if file != nil {
				t.Errorf("%s: got file %q, want none", tc.url, file.Body)
			}
			continue
		}
		if file == nil {
			t.Errorf("%s: got no file, want %q", tc.url, tc.wantBody)
			continue
		}
		if string(file.Body) != tc.wantBody || file.ContentType != tc.wantType {
			t.Errorf("%s: got %q %q, want %q %q", tc.url, file.Body, file.ContentType, tc.wantBody, tc.wantType)
		}
	}
}

func TestReportAssetsGlobalCampaignServesOnlyGlobalDomains(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "company.test", "a.png"), "company")
	companyA := uuid.New()
	assets := newReportAssets(root, []repository.DomainNameCompany{
		{Name: "company.test", CompanyID: &companyA},
	}, nil)
	u, _ := url.Parse("https://company.test/a.png")
	file, local := assets.lookup(u)
	if !local || file != nil {
		t.Fatalf("company domain must be local and not served for a global campaign, got local %v file %v", local, file)
	}
}

func TestReportAssetsIsLocalHost(t *testing.T) {
	other := uuid.New()
	assets := newReportAssets(t.TempDir(), []repository.DomainNameCompany{
		{Name: "Global.test"},
		{Name: "other.test", CompanyID: &other},
	}, nil)
	// every domain of the install is local, served or not
	for _, host := range []string{"global.test", "GLOBAL.test.", "other.test"} {
		if !assets.isLocalHost(host) {
			t.Fatalf("%q must be local", host)
		}
	}
	if assets.isLocalHost("example.com") {
		t.Fatalf("example.com must not be local")
	}
}

func TestWithBaseURL(t *testing.T) {
	base := `https://phish.test/login?a="b"`
	cases := map[string]string{
		`<html><head><title>x</title></head><body></body></html>`: `<html><head><base href="https://phish.test/login?a=&#34;b&#34;"><title>x</title></head><body></body></html>`,
		`<HTML><HEAD lang="en"><title>x</title></HEAD></HTML>`:    `<HTML><HEAD lang="en"><base href="https://phish.test/login?a=&#34;b&#34;"><title>x</title></HEAD></HTML>`,
		`<body>no head</body>`:             `<base href="https://phish.test/login?a=&#34;b&#34;"><body>no head</body>`,
		`<head><base href="/own/"></head>`: `<head><base href="/own/"></head>`,
		`<header>not a head</header>`:      `<base href="https://phish.test/login?a=&#34;b&#34;"><header>not a head</header>`,
	}
	for in, want := range cases {
		if got := withBaseURL(in, base); got != want {
			t.Errorf("withBaseURL(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestWithInlineImages(t *testing.T) {
	out := withInlineImages(`<img src="cid:logo.png"><img src="cid:logo.png.png"><img src="cid:other.gif">`, map[string][]byte{
		"logo.png":     []byte("a"),
		"logo.png.png": []byte("b"),
	})
	want := `<img src="data:image/png;base64,YQ=="><img src="data:image/png;base64,Yg=="><img src="cid:other.gif">`
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
	if strings.Contains(withInlineImages(`x`, nil), "data:") {
		t.Fatal("no images must leave the content unchanged")
	}
}
