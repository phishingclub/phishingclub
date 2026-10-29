package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPackagePreviewCSP(t *testing.T) {
	csp := packagePreviewCSP
	if !strings.HasPrefix(csp, "sandbox allow-scripts allow-forms allow-popups;") {
		t.Fatalf("sandbox flags changed: %q", csp)
	}
	for _, bad := range []string{"allow-modals", "allow-same-origin", "allow-top-navigation"} {
		if strings.Contains(csp, bad) {
			t.Errorf("package preview must not carry %s", bad)
		}
	}
	for _, want := range []string{
		"connect-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-ancestors 'self'",
		"img-src data: blob: https:",
		"font-src data: https:",
		"style-src 'unsafe-inline' data: https:",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("package preview is missing %q", want)
		}
	}
	// scripts never load from a remote host.
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "script-src") && strings.Contains(d, "https:") {
			t.Errorf("script-src allows remote hosts: %q", d)
		}
	}
}

func TestAdminPreviewCSPUnchanged(t *testing.T) {
	if adminPreviewCSP != "sandbox allow-scripts allow-forms allow-popups allow-modals" {
		t.Fatalf("admin preview policy changed: %q", adminPreviewCSP)
	}
}

func TestPackagePreviewHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// the security headers middleware sets these first; it cannot be imported
	// here as it imports this package.
	r.Use(func(g *gin.Context) {
		g.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		g.Header("Content-Security-Policy", "frame-ancestors 'none'")
		g.Next()
	})
	r.POST("/pkg", func(g *gin.Context) { writePreview(g, packagePreviewCSP, packagePreviewReferrer) })
	r.POST("/admin", func(g *gin.Context) { writePreview(g, adminPreviewCSP, "") })

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("content=%3Cimg%3E"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := post("/pkg")
	if got := w.Header().Values("Referrer-Policy"); len(got) != 1 || got[0] != "no-referrer" {
		t.Fatalf("package preview Referrer-Policy = %q, want no-referrer", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != packagePreviewCSP {
		t.Fatalf("package preview CSP = %q", got)
	}
	if w.Body.String() != "<img>" {
		t.Fatalf("body = %q", w.Body.String())
	}

	w = post("/admin")
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("admin preview Referrer-Policy = %q", got)
	}
}
