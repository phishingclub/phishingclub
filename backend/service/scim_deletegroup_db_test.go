package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestScimDeleteGroupRemovesItFromPrograms checks that a SCIM group delete is
// never refused because a program uses the group: the group leaves the
// campaigns and programs, other groups of the program stay
func TestScimDeleteGroupRemovesItFromPrograms(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "scim.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.Campaign{},
		&database.CampaignRecipientGroup{},
		&database.Program{},
		&database.ProgramRecipientGroup{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	companyID := uuid.New()
	groupID, otherGroupID := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{groupID, otherGroupID} {
		gid := id
		if err := db.Create(&database.RecipientGroup{ID: &gid, CreatedAt: &now, UpdatedAt: &now, Name: "group " + gid.String(), CompanyID: &companyID}).Error; err != nil {
			t.Fatalf("create group: %v", err)
		}
	}
	programID, campaignID := uuid.New(), uuid.New()
	if err := db.Create(&database.Program{ID: &programID, CreatedAt: &now, UpdatedAt: &now, Name: "program", CompanyID: &companyID}).Error; err != nil {
		t.Fatalf("create program: %v", err)
	}
	if err := db.Create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "campaign", CompanyID: &companyID}).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	for _, row := range []any{
		&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID},
		&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &otherGroupID},
		&database.CampaignRecipientGroup{CampaignID: &campaignID, RecipientGroupID: &groupID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create link: %v", err)
		}
	}

	svc := &Scim{
		Common:                   Common{Logger: zap.NewNop().Sugar()},
		RecipientGroupRepository: &repository.RecipientGroup{DB: db},
		CampaignRepository:       &repository.Campaign{DB: db},
		ProgramRepository:        &repository.Program{DB: db},
	}
	if err := svc.DeleteGroup(ctx, &companyID, nil, &groupID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	var programLinks []database.ProgramRecipientGroup
	if err := db.Find(&programLinks).Error; err != nil {
		t.Fatalf("read program groups: %v", err)
	}
	if len(programLinks) != 1 || *programLinks[0].RecipientGroupID != otherGroupID {
		t.Fatalf("program groups after delete = %+v, want only the other group", programLinks)
	}
	var campaignLinks int64
	db.Model(&database.CampaignRecipientGroup{}).Count(&campaignLinks)
	if campaignLinks != 0 {
		t.Fatalf("campaign groups after delete = %d, want 0", campaignLinks)
	}
	var groups int64
	db.Model(&database.RecipientGroup{}).Where("id = ?", groupID).Count(&groups)
	if groups != 0 {
		t.Fatal("group was not deleted")
	}
}
