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

func TestReportTemplateKinds(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "report.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&database.ReportTemplate{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := (&database.ReportTemplate{}).Migrate(db); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	ctx := context.Background()
	repo := &ReportTemplate{DB: db}
	company := uuid.New()
	add := func(content string, companyID *uuid.UUID, training, program bool) error {
		id := uuid.New()
		now := time.Now()
		return db.Create(&database.ReportTemplate{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, Content: content,
			CompanyID: companyID, IsTraining: training, IsProgram: program,
		}).Error
	}
	for _, r := range []struct {
		content           string
		company           *uuid.UUID
		training, program bool
	}{
		{"sim", nil, false, false},
		{"train", nil, true, false},
		{"prog", nil, false, true},
		{"company prog", &company, false, true},
	} {
		if err := add(r.content, r.company, r.training, r.program); err != nil {
			t.Fatalf("add %s: %v", r.content, err)
		}
	}
	// one global and one per company template per kind
	if err := add("second prog", nil, false, true); err == nil {
		t.Fatalf("a second global program template must be refused")
	}
	if err := add("second company prog", &company, false, true); err == nil {
		t.Fatalf("a second company program template must be refused")
	}
	content := func(companyID *uuid.UUID, training, program bool) string {
		var got string
		if program {
			m, err := repo.GetForProgram(ctx, companyID)
			if err != nil {
				t.Fatalf("program: %v", err)
			}
			got = m.Content.MustGet().String()
		} else {
			m, err := repo.GetForCampaign(ctx, companyID, training)
			if err != nil {
				t.Fatalf("campaign: %v", err)
			}
			got = m.Content.MustGet().String()
		}
		return got
	}
	other := uuid.New()
	cases := []struct {
		company           *uuid.UUID
		training, program bool
		want              string
	}{
		{nil, false, false, "sim"},
		{&company, false, false, "sim"},
		{nil, true, false, "train"},
		{nil, false, true, "prog"},
		{&company, false, true, "company prog"},
		{&other, false, true, "prog"},
	}
	for _, c := range cases {
		if got := content(c.company, c.training, c.program); got != c.want {
			t.Fatalf("got %q want %q", got, c.want)
		}
	}
}
