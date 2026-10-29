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
	if err := db.AutoMigrate(&database.Domain{}, &database.Asset{}); err != nil {
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
			Common:           common,
			RootFolder:       root,
			FileService:      &File{Common: common},
			AssetRepository:  &repository.Asset{DB: db},
			DomainRepository: &repository.Domain{DB: db},
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
