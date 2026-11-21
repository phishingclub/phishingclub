package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestUpgradeGlobalProgramDefault(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "report.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&database.ReportTemplate{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	add := func(content string, companyID *uuid.UUID, program bool) *uuid.UUID {
		id := uuid.New()
		now := time.Now()
		if err := db.Create(&database.ReportTemplate{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, Content: content,
			CompanyID: companyID, IsProgram: program,
		}).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
		return &id
	}
	content := func(id *uuid.UUID) string {
		var row database.ReportTemplate
		if err := db.Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).First(&row).Error; err != nil {
			t.Fatalf("read: %v", err)
		}
		return row.Content
	}
	earlier := "<p>earlier default</p>"
	current := "<p>current default</p>"
	known := map[string]bool{embedded.ReportContentHash(earlier): true}
	company := uuid.New()
	global := add("\n"+earlier+"\n", nil, true)
	edited := add(earlier+"<p>edited</p>", nil, true)
	companyRow := add(earlier, &company, true)
	campaign := add(earlier, nil, false)
	repo := &ReportTemplate{DB: db}
	if err := repo.UpgradeGlobalProgramDefault(context.Background(), known, embedded.ReportContentHash, current); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if content(global) != current {
		t.Fatalf("an unedited global program template must get the new default")
	}
	if content(edited) != earlier+"<p>edited</p>" {
		t.Fatalf("an edited global program template must be kept")
	}
	if content(companyRow) != earlier {
		t.Fatalf("a company program template must be kept")
	}
	if content(campaign) != earlier {
		t.Fatalf("a campaign report template must be kept")
	}
}
