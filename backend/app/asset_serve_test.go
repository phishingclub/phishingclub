package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type assetServeFixture struct {
	t      *testing.T
	db     *gorm.DB
	root   string
	server *Server
	now    time.Time
}

func newAssetServeFixture(t *testing.T) *assetServeFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	dsn := filepath.Join(dir, "serve.db") + "?_foreign_keys=ON&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Domain{},
		&database.Asset{},
		&database.AssetHeader{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	root := filepath.Join(dir, "assets")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &assetServeFixture{
		t:    t,
		db:   db,
		root: root,
		now:  time.Now(),
		server: &Server{
			staticPath:   root,
			logger:       zap.NewNop().Sugar(),
			repositories: &Repositories{Asset: &repository.Asset{DB: db}},
		},
	}
}

func (f *assetServeFixture) file(rel, content string) {
	f.t.Helper()
	full := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		f.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o640); err != nil {
		f.t.Fatalf("write: %v", err)
	}
}

func (f *assetServeFixture) asset(domainID, companyID *uuid.UUID, path, contentType string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.now = f.now.Add(time.Millisecond)
	created := f.now
	if err := f.db.Create(&database.Asset{
		ID: &id, CreatedAt: &created, UpdatedAt: &created,
		DomainID: domainID, CompanyID: companyID,
		Name: path, Path: path, ContentType: contentType,
	}).Error; err != nil {
		f.t.Fatalf("insert asset: %v", err)
	}
	return id
}

func (f *assetServeFixture) header(assetID uuid.UUID, key, value string) {
	f.t.Helper()
	id := uuid.New()
	f.now = f.now.Add(time.Millisecond)
	created := f.now
	if err := f.db.Create(&database.AssetHeader{
		ID: &id, CreatedAt: &created, UpdatedAt: &created,
		AssetID: &assetID, Key: key, Value: value,
	}).Error; err != nil {
		f.t.Fatalf("insert header: %v", err)
	}
}

// serve runs the asset handler for a request on host and returns the
// recorded response and whether the request was served as an asset
func (f *assetServeFixture) serve(host string, domainID *uuid.UUID, path string, preset http.Header) (*httptest.ResponseRecorder, bool) {
	f.t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "https://"+host+path, nil)
	for k, vs := range preset {
		for _, v := range vs {
			c.Writer.Header().Add(k, v)
		}
	}
	ok := f.server.checkAndServeAssets(c, host, domainID)
	return w, ok
}

func TestServeAssetWithoutSettings(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/page.html", "<p>hi</p>")
	f.file("a.test/norow.css", "body{}")
	f.asset(&domainID, nil, "page.html", "")

	for _, p := range []string{"/page.html", "/norow.css"} {
		w, ok := f.serve("a.test", &domainID, p, nil)
		if !ok || w.Code != http.StatusOK {
			t.Fatalf("%s not served: %v %d", p, ok, w.Code)
		}
		want := "text/html; charset=utf-8"
		if p == "/norow.css" {
			want = "text/css; charset=utf-8"
		}
		if got := w.Header().Get("Content-Type"); got != want {
			t.Fatalf("%s content type = %q, want %q", p, got, want)
		}
		if len(w.Header()) != 2 {
			t.Fatalf("%s unexpected headers: %v", p, w.Header())
		}
	}
}

func TestServeDomainAssetSettings(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	otherDomainID := uuid.New()
	f.file("a.test/files/report.html", "<p>report</p>")
	id := f.asset(&domainID, nil, "files/report.html", "application/pdf")
	f.header(id, "Content-Disposition", "attachment; filename=report.pdf")
	f.header(id, "Set-Cookie", "a=1; Path=/")
	f.header(id, "Set-Cookie", "b=2; Path=/")
	f.header(id, "x-custom", "replaced")
	// the same path on another domain has its own settings
	other := f.asset(&otherDomainID, nil, "files/report.html", "text/plain")
	f.header(other, "X-Other", "o")

	preset := http.Header{"X-Custom": {"default"}}
	w, ok := f.serve("a.test", &domainID, "/files/report.html", preset)
	if !ok || w.Code != http.StatusOK {
		t.Fatalf("not served: %v %d", ok, w.Code)
	}
	h := w.Header()
	if got := h.Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("content type = %q", got)
	}
	if got := h.Get("Content-Disposition"); got != "attachment; filename=report.pdf" {
		t.Fatalf("content disposition = %q", got)
	}
	if got := h.Values("Set-Cookie"); len(got) != 2 || got[0] != "a=1; Path=/" || got[1] != "b=2; Path=/" {
		t.Fatalf("set cookie = %v", got)
	}
	if got := h.Values("X-Custom"); len(got) != 1 || got[0] != "replaced" {
		t.Fatalf("custom header did not replace the default: %v", got)
	}
	if h.Get("X-Other") != "" {
		t.Fatalf("settings of another domain were applied")
	}
	if w.Body.String() != "<p>report</p>" {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestServeSharedAndCompanyAssetSettings(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	companyID := uuid.New()
	if err := f.db.Create(&database.Company{
		ID: companyID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "c", AssetsKey: "ckey",
	}).Error; err != nil {
		t.Fatalf("insert company: %v", err)
	}
	f.file("shared/js/app.js", "1")
	f.file("shared/ckey/doc.txt", "2")
	global := f.asset(nil, nil, "js/app.js", "text/x-global")
	f.header(global, "X-Global", "g")
	company := f.asset(nil, &companyID, "doc.txt", "text/x-company")
	f.header(company, "X-Company", "c")
	// a domain asset row at the same path is not the shared file
	other := f.asset(&domainID, nil, "js/app.js", "text/x-domain")
	f.header(other, "X-Domain", "d")

	w, ok := f.serve("a.test", &domainID, "/js/app.js", nil)
	if !ok || w.Header().Get("Content-Type") != "text/x-global" || w.Header().Get("X-Global") != "g" ||
		w.Header().Get("X-Domain") != "" {
		t.Fatalf("global asset served with %v %v", ok, w.Header())
	}
	// shared settings apply on every domain that serves the file
	w, ok = f.serve("b.test", nil, "/js/app.js", nil)
	if !ok || w.Header().Get("X-Global") != "g" {
		t.Fatalf("global asset on other domain served with %v %v", ok, w.Header())
	}
	w, ok = f.serve("a.test", &domainID, "/ckey/doc.txt", nil)
	if !ok || w.Header().Get("Content-Type") != "text/x-company" || w.Header().Get("X-Company") != "c" {
		t.Fatalf("company asset served with %v %v", ok, w.Header())
	}
}

func TestServeAssetSkipsInvalidStoredHeader(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/x.txt", "xyz")
	id := f.asset(&domainID, nil, "x.txt", "")
	// rows changed outside the application are not sent
	f.header(id, "Content-Length", "999")
	f.header(id, "X-Bad", "a\r\nX-Injected: 1")
	f.header(id, "X-Good", "ok")
	w, ok := f.serve("a.test", &domainID, "/x.txt", nil)
	if !ok {
		t.Fatalf("not served")
	}
	if w.Header().Get("X-Bad") != "" || w.Header().Get("X-Injected") != "" {
		t.Fatalf("invalid header was sent: %v", w.Header())
	}
	if got := w.Header().Get("Content-Length"); got != "3" {
		t.Fatalf("content length = %q, want 3", got)
	}
	if w.Header().Get("X-Good") != "ok" {
		t.Fatalf("valid header missing: %v", w.Header())
	}
}

func TestServeAssetLookupErrorServesDefaults(t *testing.T) {
	f := newAssetServeFixture(t)
	domainID := uuid.New()
	f.file("a.test/x.txt", "x")
	if err := f.db.Migrator().DropTable(&database.AssetHeader{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	w, ok := f.serve("a.test", &domainID, "/x.txt", nil)
	if !ok || w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("lookup error broke serving: %v %d %v", ok, w.Code, w.Header())
	}
}
