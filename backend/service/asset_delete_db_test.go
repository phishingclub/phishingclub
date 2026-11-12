package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type assetDeleteFixture struct {
	t       *testing.T
	db      *gorm.DB
	root    string
	svc     *Asset
	session *model.Session
}

func newAssetDeleteFixture(t *testing.T) *assetDeleteFixture {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "asset.db") + "?_foreign_keys=OFF"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Domain{}, &database.Company{}, &database.Asset{}, &database.AssetHeader{}, &database.AssetAllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	root := filepath.Join(dir, "files")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	common := Common{Logger: zap.NewNop().Sugar()}
	return &assetDeleteFixture{
		t:    t,
		db:   db,
		root: root,
		svc: &Asset{
			Common:            common,
			RootFolder:        root,
			FileService:       &File{Common: common},
			AssetRepository:   &repository.Asset{DB: db},
			DomainRepository:  &repository.Domain{DB: db},
			CompanyRepository: &repository.Company{DB: db},
		},
		session: &model.Session{
			IP: "127.0.0.1",
			User: &model.User{
				ID:   nullable.NewNullableWithValue(uuid.New()),
				Role: &model.Role{ID: uuid.New(), Name: data.RoleSuperAdministrator},
			},
		},
	}
}

func (f *assetDeleteFixture) insert(domainID *uuid.UUID, domainName, p string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	now := time.Now()
	row := &database.Asset{
		ID:         &id,
		CreatedAt:  &now,
		UpdatedAt:  &now,
		DomainID:   domainID,
		DomainName: domainName,
		Name:       "asset",
		Path:       p,
	}
	if err := f.db.Create(row).Error; err != nil {
		f.t.Fatalf("insert asset: %v", err)
	}
	return id
}

func (f *assetDeleteFixture) writeFile(rel string) string {
	f.t.Helper()
	full := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		f.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("x"), 0o640); err != nil {
		f.t.Fatalf("write: %v", err)
	}
	return full
}

func (f *assetDeleteFixture) rowExists(id uuid.UUID) bool {
	var n int64
	f.db.Model(&database.Asset{}).Where("id = ?", id).Count(&n)
	return n > 0
}

func TestAssetDeleteByIDWithUnloadablePath(t *testing.T) {
	f := newAssetDeleteFixture(t)
	domainID := uuid.New()
	// the shared folder and a file in the domain folder must survive
	keep := f.writeFile(filepath.Join("a.test", "keep.txt"))
	id := f.insert(&domainID, "a.test", "bad@name.txt")

	if err := f.svc.DeleteByID(context.Background(), f.session, &id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if f.rowExists(id) {
		t.Fatalf("row was not deleted")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was touched: %v", err)
	}
	if _, err := os.Stat(f.root); err != nil {
		t.Fatalf("root folder was touched: %v", err)
	}
}

func TestAssetDeleteAllByDomainIDMixed(t *testing.T) {
	f := newAssetDeleteFixture(t)
	domainID := uuid.New()
	file := f.writeFile(filepath.Join("a.test", "img", "logo.png"))
	keep := f.writeFile(filepath.Join("a.test", "keep.txt"))
	good := f.insert(&domainID, "a.test", "img/logo.png")
	bad := f.insert(&domainID, "a.test", "bad~name.png")

	if err := f.svc.DeleteAllByDomainID(context.Background(), f.session, &domainID); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if f.rowExists(good) || f.rowExists(bad) {
		t.Fatalf("rows were not deleted")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("asset file was not deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("empty folder was not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was touched: %v", err)
	}
}

func TestAssetDeleteAllByCompanyIDWithUnloadablePath(t *testing.T) {
	f := newAssetDeleteFixture(t)
	companyID := uuid.New()
	id := uuid.New()
	now := time.Now()
	if err := f.db.Create(&database.Asset{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		CompanyID: &companyID, Name: "asset", Path: "+bad.png",
	}).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := f.svc.DeleteAllByCompanyID(context.Background(), f.session, &companyID); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if f.rowExists(id) {
		t.Fatalf("row was not deleted")
	}
}

func (f *assetDeleteFixture) addHeader(assetID uuid.UUID, key, value string) {
	f.t.Helper()
	id := uuid.New()
	now := time.Now()
	if err := f.db.Create(&database.AssetHeader{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		AssetID: &assetID, Key: key, Value: value,
	}).Error; err != nil {
		f.t.Fatalf("insert header: %v", err)
	}
}

func (f *assetDeleteFixture) headerCount(assetID uuid.UUID) int64 {
	var n int64
	f.db.Model(&database.AssetHeader{}).Where("asset_id = ?", assetID).Count(&n)
	return n
}

// TestAssetDeleteRemovesHeaders checks that every delete path removes the
// headers of the deleted assets and leaves the headers of other assets
func TestAssetDeleteRemovesHeaders(t *testing.T) {
	f := newAssetDeleteFixture(t)
	ctx := context.Background()
	domainID := uuid.New()
	otherDomainID := uuid.New()
	f.writeFile(filepath.Join("a.test", "one.html"))
	f.writeFile(filepath.Join("a.test", "two.html"))
	f.writeFile(filepath.Join("b.test", "keep.html"))
	one := f.insert(&domainID, "a.test", "one.html")
	two := f.insert(&domainID, "a.test", "two.html")
	keep := f.insert(&otherDomainID, "b.test", "keep.html")
	f.addHeader(one, "X-One", "1")
	f.addHeader(two, "X-Two", "2")
	f.addHeader(two, "Set-Cookie", "a=b")
	f.addHeader(keep, "X-Keep", "k")

	if err := f.svc.DeleteByID(ctx, f.session, &one); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if f.headerCount(one) != 0 {
		t.Fatalf("headers of deleted asset were kept")
	}
	if err := f.svc.DeleteAllByDomainID(ctx, f.session, &domainID); err != nil {
		t.Fatalf("delete all by domain: %v", err)
	}
	if f.headerCount(two) != 0 {
		t.Fatalf("headers of asset deleted with its domain were kept")
	}
	if f.headerCount(keep) != 1 {
		t.Fatalf("headers of an unrelated asset were deleted")
	}
}

// TestAssetDeleteAllRemovesEmptyContextFolder checks that deleting the assets
// of a domain or a company also removes its now empty asset folder, keeps a
// folder that still holds other files, and never removes the shared folder
func TestAssetDeleteAllRemovesEmptyContextFolder(t *testing.T) {
	f := newAssetDeleteFixture(t)
	ctx := context.Background()
	now := time.Now()

	// a domain whose folder only holds its asset
	domainID := uuid.New()
	if err := f.db.Create(&database.Domain{ID: domainID, CreatedAt: &now, UpdatedAt: &now, Name: "gone.test"}).Error; err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	f.writeFile(filepath.Join("gone.test", "img", "logo.png"))
	f.insert(&domainID, "gone.test", "img/logo.png")
	if err := f.svc.DeleteAllByDomainID(ctx, f.session, &domainID); err != nil {
		t.Fatalf("delete all by domain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "gone.test")); !os.IsNotExist(err) {
		t.Fatalf("empty domain folder was not removed: %v", err)
	}

	// a domain whose folder holds a file that is not an asset keeps its folder
	keptID := uuid.New()
	if err := f.db.Create(&database.Domain{ID: keptID, CreatedAt: &now, UpdatedAt: &now, Name: "kept.test"}).Error; err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	f.writeFile(filepath.Join("kept.test", "a.png"))
	keep := f.writeFile(filepath.Join("kept.test", "other.txt"))
	f.insert(&keptID, "kept.test", "a.png")
	if err := f.svc.DeleteAllByDomainID(ctx, f.session, &keptID); err != nil {
		t.Fatalf("delete all by domain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("folder with other files was touched: %v", err)
	}

	// a company folder under shared is removed, the shared folder stays
	companyID := uuid.New()
	if err := f.db.Create(&database.Company{ID: companyID, CreatedAt: &now, UpdatedAt: &now, Name: "acme", AssetsKey: "k1"}).Error; err != nil {
		t.Fatalf("insert company: %v", err)
	}
	f.writeFile(filepath.Join(data.ASSET_GLOBAL_FOLDER, "k1", "logo.png"))
	id := uuid.New()
	if err := f.db.Create(&database.Asset{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		CompanyID: &companyID, Name: "asset", Path: "logo.png",
	}).Error; err != nil {
		t.Fatalf("insert asset: %v", err)
	}
	if err := f.svc.DeleteAllByCompanyID(ctx, f.session, &companyID); err != nil {
		t.Fatalf("delete all by company: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, data.ASSET_GLOBAL_FOLDER, "k1")); !os.IsNotExist(err) {
		t.Fatalf("empty company folder was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, data.ASSET_GLOBAL_FOLDER)); err != nil {
		t.Fatalf("shared folder was removed: %v", err)
	}
}

// TestAssetDeleteAllKeepsLinkedDomainFolder checks that a link with the name of
// a domain folder is never removed, also when it points at another folder
func TestAssetDeleteAllKeepsLinkedDomainFolder(t *testing.T) {
	f := newAssetDeleteFixture(t)
	now := time.Now()
	domainID := uuid.New()
	if err := f.db.Create(&database.Domain{ID: domainID, CreatedAt: &now, UpdatedAt: &now, Name: "link.test"}).Error; err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	target := filepath.Join(f.root, "target")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(f.root, "link.test")
	if err := os.Symlink("target", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := f.svc.DeleteAllByDomainID(context.Background(), f.session, &domainID); err != nil {
		t.Fatalf("delete all by domain: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("link was removed: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("link target was removed: %v", err)
	}
}
