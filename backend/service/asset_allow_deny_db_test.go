package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type assetFilterFixture struct {
	t         *testing.T
	ctx       context.Context
	db        *gorm.DB
	svc       *Asset
	allowDeny *AllowDeny
	session   *model.Session
	now       time.Time
}

func newAssetFilterFixture(t *testing.T) *assetFilterFixture {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "filter.db") + "?_foreign_keys=ON&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Domain{},
		&database.Asset{},
		&database.AssetHeader{},
		&database.AllowDeny{},
		&database.AssetAllowDeny{},
		&database.Campaign{},
		&database.CampaignAllowDeny{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	assetRepo := &repository.Asset{DB: db}
	allowDenyRepo := &repository.AllowDeny{DB: db}
	return &assetFilterFixture{
		t:   t,
		ctx: context.Background(),
		db:  db,
		svc: &Asset{
			Common:              common,
			RootFolder:          filepath.Join(dir, "files"),
			FileService:         &File{Common: common},
			AssetRepository:     assetRepo,
			DomainRepository:    &repository.Domain{DB: db},
			CompanyRepository:   &repository.Company{DB: db},
			AllowDenyRepository: allowDenyRepo,
		},
		allowDeny: &AllowDeny{
			Common:              common,
			AllowDenyRepository: allowDenyRepo,
			CampaignRepository:  &repository.Campaign{DB: db},
			AssetRepository:     assetRepo,
		},
		session: sessionWithRole(data.RoleSuperAdministrator),
		now:     time.Now(),
	}
}

func isValidationError(err error) bool {
	var v errs.ValidationError
	return errors.As(err, &v)
}

func sessionWithRole(role string) *model.Session {
	return &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uuid.New()),
			Role: &model.Role{ID: uuid.New(), Name: role},
		},
	}
}

func (f *assetFilterFixture) company(key string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.Company{
		ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "company " + key, AssetsKey: key,
	}).Error; err != nil {
		f.t.Fatalf("insert company: %v", err)
	}
	return id
}

func (f *assetFilterFixture) domain(name string, companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.Domain{
		ID: id, CreatedAt: &f.now, UpdatedAt: &f.now, Name: name, CompanyID: companyID,
	}).Error; err != nil {
		f.t.Fatalf("insert domain: %v", err)
	}
	return id
}

func (f *assetFilterFixture) asset(domainID, companyID *uuid.UUID, path string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.Asset{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now,
		DomainID: domainID, CompanyID: companyID, Name: path, Path: path,
	}).Error; err != nil {
		f.t.Fatalf("insert asset: %v", err)
	}
	return id
}

func (f *assetFilterFixture) list(name string, allowed bool, companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.AllowDeny{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, CompanyID: companyID,
		Name: name, Cidrs: "10.0.0.0/8", Allowed: allowed,
	}).Error; err != nil {
		f.t.Fatalf("insert list: %v", err)
	}
	return id
}

func (f *assetFilterFixture) linked(assetID uuid.UUID) []uuid.UUID {
	f.t.Helper()
	rows := []database.AssetAllowDeny{}
	if err := f.db.Where("asset_id = ?", assetID).Find(&rows).Error; err != nil {
		f.t.Fatalf("links: %v", err)
	}
	out := []uuid.UUID{}
	for _, r := range rows {
		out = append(out, *r.AllowDenyID)
	}
	return out
}

func (f *assetFilterFixture) set(assetID uuid.UUID, ids ...uuid.UUID) error {
	return f.svc.SetAllowDenyByID(f.ctx, f.session, &assetID, ids)
}

// TestAssetSetAllowDenyCompanyScope checks which lists each kind of asset can
// use: global assets only global lists, company assets their company lists
// and global lists
func TestAssetSetAllowDenyCompanyScope(t *testing.T) {
	f := newAssetFilterFixture(t)
	c1 := f.company("c1")
	c2 := f.company("c2")
	globalDomain := f.domain("global.test", nil)
	c1Domain := f.domain("c1.test", &c1)

	globalList := f.list("global", false, nil)
	c1List := f.list("c1", false, &c1)
	c2List := f.list("c2", false, &c2)

	tests := []struct {
		name   string
		asset  uuid.UUID
		ok     []uuid.UUID
		refuse []uuid.UUID
	}{
		{"shared global asset", f.asset(nil, nil, "g.txt"), []uuid.UUID{globalList}, []uuid.UUID{c1List, c2List}},
		{"global domain asset", f.asset(&globalDomain, nil, "gd.txt"), []uuid.UUID{globalList}, []uuid.UUID{c1List, c2List}},
		{"company folder asset", f.asset(nil, &c1, "cf.txt"), []uuid.UUID{globalList, c1List}, []uuid.UUID{c2List}},
		{"company domain asset", f.asset(&c1Domain, &c1, "cd.txt"), []uuid.UUID{globalList, c1List}, []uuid.UUID{c2List}},
		// an asset without a company on a company domain uses the domain company
		{"company domain asset without company", f.asset(&c1Domain, nil, "cdn.txt"), []uuid.UUID{globalList, c1List}, []uuid.UUID{c2List}},
		{"company asset on global domain", f.asset(&globalDomain, &c1, "cg.txt"), []uuid.UUID{globalList, c1List}, []uuid.UUID{c2List}},
	}
	for _, tt := range tests {
		if err := f.set(tt.asset, tt.ok...); err != nil {
			t.Fatalf("%s: set allowed lists: %v", tt.name, err)
		}
		if got := f.linked(tt.asset); len(got) != len(tt.ok) {
			t.Fatalf("%s: linked %v, want %v", tt.name, got, tt.ok)
		}
		for _, refused := range tt.refuse {
			err := f.set(tt.asset, append([]uuid.UUID{globalList}, refused)...)
			if err == nil || !isValidationError(err) {
				t.Fatalf("%s: list of other scope was accepted: %v", tt.name, err)
			}
			// a refused set changes nothing
			if got := f.linked(tt.asset); len(got) != len(tt.ok) {
				t.Fatalf("%s: links changed on refused set: %v", tt.name, got)
			}
		}
	}
}

// TestAssetSetAllowDenyValidation checks type mixing, unknown lists,
// duplicates, clearing and permissions
func TestAssetSetAllowDenyValidation(t *testing.T) {
	f := newAssetFilterFixture(t)
	assetID := f.asset(nil, nil, "x.txt")
	allow1 := f.list("allow1", true, nil)
	allow2 := f.list("allow2", true, nil)
	deny := f.list("deny", false, nil)

	if err := f.set(assetID, allow1, deny); err == nil || !isValidationError(err) {
		t.Fatalf("mixed allow and deny was accepted: %v", err)
	}
	if err := f.set(assetID, allow1, uuid.New()); err == nil || !isValidationError(err) {
		t.Fatalf("unknown list was accepted: %v", err)
	}
	if got := f.linked(assetID); len(got) != 0 {
		t.Fatalf("refused sets stored links: %v", got)
	}
	if err := f.set(assetID, allow1, allow2, allow1); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := f.linked(assetID); len(got) != 2 {
		t.Fatalf("duplicates not removed: %v", got)
	}
	// the set replaces the lists, so allow lists can be swapped for a deny list
	if err := f.set(assetID, deny); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := f.linked(assetID); len(got) != 1 || got[0] != deny {
		t.Fatalf("replaced links = %v", got)
	}
	if err := f.set(assetID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := f.linked(assetID); len(got) != 0 {
		t.Fatalf("cleared links = %v", got)
	}
	// unknown asset
	missing := uuid.New()
	if err := f.set(missing, allow1); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown asset err = %v", err)
	}
	// a session without the global permission is refused
	err := f.svc.SetAllowDenyByID(f.ctx, sessionWithRole(data.RoleCompanyUser), &assetID, []uuid.UUID{allow1})
	if !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("unauthorized err = %v", err)
	}
	if got := f.linked(assetID); len(got) != 0 {
		t.Fatalf("unauthorized set stored links: %v", got)
	}
}

// TestAssetGetByIDIncludesAllowDeny checks the asset response carries its
// lists and the company whose lists it can use
func TestAssetGetByIDIncludesAllowDeny(t *testing.T) {
	f := newAssetFilterFixture(t)
	c1 := f.company("c1")
	c1Domain := f.domain("c1.test", &c1)
	assetID := f.asset(&c1Domain, nil, "x.txt")
	list := f.list("deny", false, &c1)
	if err := f.set(assetID, list); err != nil {
		t.Fatalf("set: %v", err)
	}
	asset, err := f.svc.GetByID(f.ctx, f.session, &assetID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(asset.AllowDeny) != 1 || asset.AllowDeny[0].ID.MustGet() != list {
		t.Fatalf("lists = %v", asset.AllowDeny)
	}
	if got, err := asset.AllowDenyCompanyID.Get(); err != nil || got != c1 {
		t.Fatalf("allow deny company = %v, %v", got, err)
	}
	global := f.asset(nil, nil, "g.txt")
	asset, err = f.svc.GetByID(f.ctx, f.session, &global)
	if err != nil || asset.AllowDeny == nil || len(asset.AllowDeny) != 0 || !asset.AllowDenyCompanyID.IsNull() {
		t.Fatalf("global asset = %+v, %v", asset, err)
	}
}

// TestAllowDenyDeleteRemovesFromAssets checks deleting a list removes it from
// the assets that use it, and deleting an asset removes its links
func TestAllowDenyDeleteRemovesFromAssets(t *testing.T) {
	f := newAssetFilterFixture(t)
	one := f.asset(nil, nil, "one.txt")
	two := f.asset(nil, nil, "two.txt")
	l1 := f.list("l1", false, nil)
	l2 := f.list("l2", false, nil)
	if err := f.set(one, l1, l2); err != nil {
		t.Fatalf("set one: %v", err)
	}
	if err := f.set(two, l1); err != nil {
		t.Fatalf("set two: %v", err)
	}
	if err := f.allowDeny.DeleteByID(f.ctx, f.session, &l1); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	if got := f.linked(one); len(got) != 1 || got[0] != l2 {
		t.Fatalf("one links = %v", got)
	}
	if got := f.linked(two); len(got) != 0 {
		t.Fatalf("two links = %v", got)
	}
	shared := filepath.Join(f.svc.RootFolder, data.ASSET_GLOBAL_FOLDER)
	if err := os.MkdirAll(shared, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, "one.txt"), []byte("1"), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.svc.DeleteByID(f.ctx, f.session, &one); err != nil {
		t.Fatalf("delete asset: %v", err)
	}
	var n int64
	f.db.Model(&database.AssetAllowDeny{}).Count(&n)
	if n != 0 {
		t.Fatalf("links left: %d", n)
	}
	// the list itself is kept
	if _, err := f.allowDeny.AllowDenyRepository.GetByID(f.ctx, &l2, &repository.AllowDenyOption{}); err != nil {
		t.Fatalf("list was deleted with the asset: %v", err)
	}
}

func TestValidateAllowDenySameType(t *testing.T) {
	allow := &model.AllowDeny{Allowed: nullable.NewNullableWithValue(true)}
	deny := &model.AllowDeny{Allowed: nullable.NewNullableWithValue(false)}
	if err := model.ValidateAllowDenySameType(nil); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := model.ValidateAllowDenySameType([]*model.AllowDeny{allow, allow}); err != nil {
		t.Fatalf("all allow: %v", err)
	}
	if err := model.ValidateAllowDenySameType([]*model.AllowDeny{deny, allow}); err == nil {
		t.Fatalf("mixed accepted")
	}
	untyped := &model.AllowDeny{}
	if err := model.ValidateAllowDenySameType([]*model.AllowDeny{allow, untyped}); err == nil {
		t.Fatalf("untyped accepted")
	}
}
