package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAbortRecovery checks http.ErrAbortHandler passes the recovery and any
// other panic is answered with 500
func TestAbortRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.CustomRecovery(AbortRecovery))
	router.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	router.GET("/other", func(*gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/other", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("other panic: got %d", rec.Code)
	}
	func() {
		defer func() {
			if err := recover(); err != http.ErrAbortHandler {
				t.Fatalf("abort panic must pass the recovery, got %v", err)
			}
		}()
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
	}()
}
