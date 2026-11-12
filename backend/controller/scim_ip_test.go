package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// a forwarding header that is not an IP is ignored
func TestScimClientIPOnlyIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	g, _ := gin.CreateTestContext(httptest.NewRecorder())
	g.Request = httptest.NewRequest("GET", "/", nil)
	g.Request.RemoteAddr = "198.51.100.7:4444"
	g.Request.Header.Set("X-Forwarded-For", strings.Repeat("x", 500))
	if got := scimClientIP(g, nil); got != "198.51.100.7" {
		t.Fatalf("expected the connection address, got %q", got)
	}
	g.Request.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := scimClientIP(g, nil); got != "203.0.113.9" {
		t.Fatalf("expected the forwarded IP, got %q", got)
	}
}
