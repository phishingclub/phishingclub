package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type assetServeFixture struct {
	t    *testing.T
	db   *gorm.DB
	repo *Asset
	now  time.Time
}

func newAssetServeFixture(t *testing.T) *assetServeFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "assetserve.db") + "?_foreign_keys=ON&_busy_timeout=5000"
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
	return &assetServeFixture{t: t, db: db, repo: &Asset{DB: db}, now: time.Now()}
}

func (f *assetServeFixture) company(key string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.Company{
		ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "company " + key, AssetsKey: key,
	}).Error; err != nil {
		f.t.Fatalf("insert company: %v", err)
	}
	return id
}

func (f *assetServeFixture) domain(name string, companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.Domain{
		ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, CompanyID: companyID,
	}).Error; err != nil {
		f.t.Fatalf("insert domain: %v", err)
	}
	return id
}

// asset inserts an asset row. Each row is created a little later than the
// one before so the order between rows is fixed.
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

func TestSplitCompanyAssetPath(t *testing.T) {
	tests := []struct {
		in, key, rest string
		ok            bool
	}{
		{"logo.png", "", "", false},
		{"abc/logo.png", "abc", "logo.png", true},
		{"abc/img/logo.png", "abc", "img/logo.png", true},
		{"/logo.png", "", "", false},
		{"abc/", "", "", false},
	}
	for _, tt := range tests {
		key, rest, ok := SplitCompanyAssetPath(tt.in)
		if key != tt.key || rest != tt.rest || ok != tt.ok {
			t.Fatalf("SplitCompanyAssetPath(%q) = %q, %q, %v", tt.in, key, rest, ok)
		}
	}
}

// TestAssetServeSettingsDomainLookup checks that a domain request maps only to
// the asset of that domain at the exact path
func TestAssetServeSettingsDomainLookup(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	companyID := f.company("ck1")
	a := f.domain("a.test", nil)
	b := f.domain("b.test", &companyID)
	onA := f.asset(&a, nil, "img/logo.png", "image/x-a")
	f.header(onA, "X-First", "1")
	f.header(onA, "Set-Cookie", "a=1")
	f.header(onA, "Set-Cookie", "b=2")
	onB := f.asset(&b, &companyID, "img/logo.png", "image/x-b")
	f.header(onB, "X-B", "b")
	// a shared asset at the same path must never apply to a domain asset
	f.asset(nil, nil, "img/logo.png", "image/x-shared")

	got, err := f.repo.GetServeSettingsForDomainAsset(ctx, &a, "img/logo.png")
	if err != nil || got == nil {
		t.Fatalf("lookup a: %v %v", got, err)
	}
	if got.AssetID != onA || got.ContentType != "image/x-a" {
		t.Fatalf("lookup a picked %v %q", got.AssetID, got.ContentType)
	}
	if len(got.Headers) != 3 ||
		got.Headers[0].Key != "X-First" ||
		got.Headers[1].Value != "a=1" ||
		got.Headers[2].Value != "b=2" {
		t.Fatalf("headers of a = %+v", got.Headers)
	}

	got, err = f.repo.GetServeSettingsForDomainAsset(ctx, &b, "img/logo.png")
	if err != nil || got == nil || got.AssetID != onB || len(got.Headers) != 1 {
		t.Fatalf("lookup b = %+v, %v", got, err)
	}

	// other path, other case, and a path with a leading slash do not match
	for _, p := range []string{"img/other.png", "IMG/logo.png", "/img/logo.png", "logo.png"} {
		got, err = f.repo.GetServeSettingsForDomainAsset(ctx, &a, p)
		if err != nil || got != nil {
			t.Fatalf("lookup %q = %+v, %v, want nothing", p, got, err)
		}
	}

	// an asset with no headers still gives its content type
	plain := f.asset(&a, nil, "plain.txt", "")
	got, err = f.repo.GetServeSettingsForDomainAsset(ctx, &a, "plain.txt")
	if err != nil || got == nil || got.AssetID != plain || got.ContentType != "" || len(got.Headers) != 0 {
		t.Fatalf("lookup plain = %+v, %v", got, err)
	}
}

// TestAssetServeSettingsSharedLookup checks how a path in the shared folder
// maps to global assets and company folder assets
func TestAssetServeSettingsSharedLookup(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	companyID := f.company("ck1")
	f.company("ck2")
	a := f.domain("a.test", nil)

	global := f.asset(nil, nil, "css/site.css", "text/x-global")
	f.header(global, "X-Global", "g")
	// company folder asset, stored without the folder name
	companyAsset := f.asset(nil, &companyID, "doc.pdf", "application/x-company")
	f.header(companyAsset, "X-Company", "c")
	// a global asset at the same path as the company asset name
	rootDoc := f.asset(nil, nil, "doc.pdf", "application/x-root")
	// a domain asset at the shared path must not be used for shared files
	f.asset(&a, nil, "css/site.css", "text/x-domain")

	tests := []struct {
		path string
		want *uuid.UUID
		ct   string
	}{
		{"css/site.css", &global, "text/x-global"},
		{"ck1/doc.pdf", &companyAsset, "application/x-company"},
		{"doc.pdf", &rootDoc, "application/x-root"},
		// the company folder of another company has no such asset
		{"ck2/doc.pdf", nil, ""},
		// an unknown folder name is not a company folder
		{"nope/doc.pdf", nil, ""},
		{"ck1/missing.pdf", nil, ""},
	}
	for _, tt := range tests {
		got, err := f.repo.GetServeSettingsForSharedAsset(ctx, tt.path)
		if err != nil {
			t.Fatalf("lookup %q: %v", tt.path, err)
		}
		if tt.want == nil {
			if got != nil {
				t.Fatalf("lookup %q = %+v, want nothing", tt.path, got)
			}
			continue
		}
		if got == nil || got.AssetID != *tt.want || got.ContentType != tt.ct {
			t.Fatalf("lookup %q = %+v, want %v %q", tt.path, got, *tt.want, tt.ct)
		}
	}

	// a global asset stored under a company folder name points at the same
	// file as the company asset, the company asset is used
	f.asset(nil, nil, "ck1/doc.pdf", "application/x-collide")
	got, err := f.repo.GetServeSettingsForSharedAsset(ctx, "ck1/doc.pdf")
	if err != nil || got == nil || got.AssetID != companyAsset {
		t.Fatalf("collision lookup = %+v, %v", got, err)
	}
	if len(got.Headers) != 1 || got.Headers[0].Key != "X-Company" {
		t.Fatalf("collision headers = %+v", got.Headers)
	}
}

// TestAssetHeaderRemoveScopedToAsset checks a header can only be removed
// through the asset it belongs to, and that deleting an asset removes its
// headers
func TestAssetHeaderRemoveScopedToAsset(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	one := f.asset(nil, nil, "one.txt", "")
	two := f.asset(nil, nil, "two.txt", "")
	f.header(one, "X-One", "1")
	headers, err := f.repo.GetHeadersByAssetID(ctx, &one)
	if err != nil || len(headers) != 1 {
		t.Fatalf("headers = %v, %v", headers, err)
	}
	headerID := headers[0].ID
	if err := f.repo.RemoveHeader(ctx, &two, &headerID); err != gorm.ErrRecordNotFound {
		t.Fatalf("remove through other asset err = %v, want not found", err)
	}
	if headers, _ := f.repo.GetHeadersByAssetID(ctx, &one); len(headers) != 1 {
		t.Fatalf("header was removed through another asset")
	}
	if err := f.repo.RemoveHeader(ctx, &one, &headerID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	f.header(two, "X-Two", "2")
	if err := f.repo.DeleteByID(ctx, &two); err != nil {
		t.Fatalf("delete asset: %v", err)
	}
	var n int64
	f.db.Model(&database.AssetHeader{}).Where("asset_id = ?", two).Count(&n)
	if n != 0 {
		t.Fatalf("headers left after asset delete: %d", n)
	}
}
