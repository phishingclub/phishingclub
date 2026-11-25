package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGetHostOnlyLowercases(t *testing.T) {
	s := &Server{}
	cases := map[string]string{
		"micro.test":       "micro.test",
		"MIcro.Test":       "micro.test",
		"MIcro.Test:8000":  "micro.test",
		"10.0.0.1:80":      "10.0.0.1",
		"[FE80::1]:443":    "fe80::1",
		"micro.test.":      "micro.test.", // trailing dot is kept, it does not match a stored name
		"MIcro.Test.:8000": "micro.test.",
	}
	for in, want := range cases {
		got, err := s.getHostOnly(in)
		if err != nil {
			t.Errorf("getHostOnly(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("getHostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHandlerLowercasesRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{remoteBrowserWSPath: "rbpath", logger: zap.NewNop().Sugar()}
	var seen string
	r := gin.New()
	r.Use(s.Handler)
	// the remote browser prefix makes Handler pass through right after the host
	// is lowercased, so the next handler sees the rewritten host
	r.GET("/rbpath/x", func(c *gin.Context) {
		seen = c.Request.Host
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/rbpath/x", nil)
	req.Host = "MIcro.Test:8000"
	r.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "micro.test:8000" {
		t.Fatalf("request host = %q, want %q", seen, "micro.test:8000")
	}
}

func TestNotFoundMatchesMixedCaseHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := filepath.Join(t.TempDir(), "host.db") + "?_foreign_keys=ON&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Company{}, &database.Domain{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	domain := &database.Domain{
		ID:                  uuid.New(),
		Name:                "micro.test",
		Type:                "regular",
		PageNotFoundContent: "nf {{.Domain}}",
	}
	if err := db.Create(domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}
	s := &Server{db: db, logger: zap.NewNop().Sugar()}

	cases := []struct {
		host string
		body string
	}{
		{"micro.test", "nf micro.test"},
		{"MIcro.Test", "nf micro.test"},
		{"MIcro.Test:8000", "nf micro.test"},
		{"micro.test.", ""},
		{"other.test", ""},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/missing", nil)
		c.Request.Host = tc.host
		s.handlerNotFound(c)
		// c.Status without a body is not flushed to the recorder, so read it
		// from the gin writer
		if got := c.Writer.Status(); got != http.StatusNotFound {
			t.Errorf("host %q: status = %d, want 404", tc.host, got)
		}
		if got := w.Body.String(); got != tc.body {
			t.Errorf("host %q: body = %q, want %q", tc.host, got, tc.body)
		}
	}
}
