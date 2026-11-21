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

// TestReportSendLogDeleteByCompanyID checks the logs of one company are
// removed and the others kept, so the company row can then be deleted with
// foreign keys on
func TestReportSendLogDeleteByCompanyID(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "logs.db") + "?_foreign_keys=ON&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Company{}, &database.ReportSendLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	company := func(name string) uuid.UUID {
		id := uuid.New()
		if err := db.Create(&database.Company{ID: id, CreatedAt: &now, UpdatedAt: &now, Name: name, AssetsKey: name}).Error; err != nil {
			t.Fatalf("create company: %v", err)
		}
		logID := uuid.New()
		if err := db.Create(&database.ReportSendLog{ID: &logID, CreatedAt: &now, CompanyID: &id}).Error; err != nil {
			t.Fatalf("create log: %v", err)
		}
		return id
	}
	gone, kept := company("gone"), company("kept")
	if err := db.Where("id = ?", gone).Delete(&database.Company{}).Error; err == nil {
		t.Fatal("the test needs foreign keys on, a company with logs must not delete")
	}
	repo := &ReportSendLog{DB: db}
	if err := repo.DeleteByCompanyID(ctx, &gone); err != nil {
		t.Fatalf("delete logs: %v", err)
	}
	if err := db.Where("id = ?", gone).Delete(&database.Company{}).Error; err != nil {
		t.Fatalf("the company must delete once its logs are gone: %v", err)
	}
	var left int64
	if err := db.Model(&database.ReportSendLog{}).Where("company_id = ?", kept).Count(&left).Error; err != nil || left != 1 {
		t.Fatalf("the logs of another company must stay: %d %v", left, err)
	}
}
