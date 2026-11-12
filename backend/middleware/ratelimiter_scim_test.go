package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// other spellings of the same company id must share one rate limit bucket
func TestScimRateLimiterCanonicalKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/scim/:companyID", NewScimRateLimiterMiddleware(0.0001, 1), func(g *gin.Context) {
		g.Status(http.StatusOK)
	})
	id := "6f1c2a9e-3b7d-4c55-9a1e-2d3f4b5c6d7e"
	spellings := []string{id, strings.ToUpper(id), "urn:uuid:" + id}
	for i, s := range spellings {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/scim/"+s, nil))
		want := http.StatusTooManyRequests
		if i == 0 {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Fatalf("request %d (%s): got %d, want %d", i, s, w.Code, want)
		}
	}
}
