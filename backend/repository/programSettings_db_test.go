package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

func TestApplyCampaignSettingsAndOpenCampaigns(t *testing.T) {
	db := newProgramTestDB(t)
	// the campaign migration made a bare join table, the real one has settings
	if err := db.Migrator().DropTable(&database.CampaignWebhook{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := db.Migrator().CreateTable(&database.CampaignWebhook{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.AutoMigrate(&database.CampaignAllowDeny{}, &database.ProgramAllowDeny{}, &database.ProgramWebhook{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	repo := &Program{DB: db}
	programID := createProgram(t, db, "settings", nil)
	open := ptrUUID()
	closed := ptrUUID()
	for i, id := range []*uuid.UUID{open, closed} {
		row := &database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c" + string(rune('a'+i)), ProgramID: programID}
		if id == closed {
			row.ClosedAt = ptrNow()
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
	}
	ids, err := repo.GetOpenCampaignIDs(ctx, programID)
	if err != nil || len(ids) != 1 || *ids[0] != *open {
		t.Fatalf("only the open campaign must be returned: %v %v", ids, err)
	}
	filter := ptrUUID()
	hook := uuid.New()
	deny := ptrUUID()
	settings := &ProgramCampaignSettings{
		DenyPageID:   deny,
		AllowDenyIDs: []*uuid.UUID{filter},
		Webhooks: []*model.CampaignWebhook{
			{WebhookID: nullable.NewNullableWithValue(hook), WebhookEvents: nullable.NewNullableWithValue(4)},
			{WebhookID: nullable.NewNullableWithValue(hook)},
		},
	}
	// applying twice replaces, it never duplicates
	for i := 0; i < 2; i++ {
		if err := repo.ApplyCampaignSettings(ctx, ids, settings); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	var c database.Campaign
	db.First(&c, "id = ?", open)
	if c.DenyPageID == nil || *c.DenyPageID != *deny || c.EvasionPageID != nil {
		t.Fatal("pages not applied")
	}
	var adCount, whCount int64
	db.Model(&database.CampaignAllowDeny{}).Where("campaign_id = ?", open).Count(&adCount)
	db.Model(&database.CampaignWebhook{}).Where("campaign_id = ?", open).Count(&whCount)
	if adCount != 1 || whCount != 1 {
		t.Fatalf("want 1 filter and 1 webhook, got %d %d", adCount, whCount)
	}
	// the closed campaign is left alone
	db.Model(&database.CampaignAllowDeny{}).Where("campaign_id = ?", closed).Count(&adCount)
	if adCount != 0 {
		t.Fatal("closed campaign must not be changed")
	}
	// clearing removes them again
	if err := repo.ApplyCampaignSettings(ctx, ids, &ProgramCampaignSettings{}); err != nil {
		t.Fatalf("apply clear: %v", err)
	}
	db.Model(&database.CampaignAllowDeny{}).Where("campaign_id = ?", open).Count(&adCount)
	db.Model(&database.CampaignWebhook{}).Where("campaign_id = ?", open).Count(&whCount)
	var cleared database.Campaign
	db.First(&cleared, "id = ?", open)
	if adCount != 0 || whCount != 0 || cleared.DenyPageID != nil {
		t.Fatal("clear must remove the settings")
	}

	// program join rows replace and delete
	if err := repo.ReplaceAllowDenies(ctx, programID, []*uuid.UUID{filter, filter}); err != nil {
		t.Fatalf("replace allow deny: %v", err)
	}
	if err := repo.ReplaceWebhooks(ctx, programID, settings.Webhooks); err != nil {
		t.Fatalf("replace webhooks: %v", err)
	}
	gotIDs, _ := repo.GetAllowDenyIDs(ctx, programID)
	gotHooks, _ := repo.GetWebhooks(ctx, programID)
	if len(gotIDs) != 1 || len(gotHooks) != 1 || gotHooks[0].GetWebhookEventsOrDefault() != 4 {
		t.Fatalf("join rows wrong: %d %d", len(gotIDs), len(gotHooks))
	}
	if err := repo.RemoveAllowDenyFromPrograms(ctx, filter); err != nil {
		t.Fatal(err)
	}
	if gotIDs, _ = repo.GetAllowDenyIDs(ctx, programID); len(gotIDs) != 0 {
		t.Fatal("deleted filter must be removed from programs")
	}
	if err := repo.DeleteSettings(ctx, programID); err != nil {
		t.Fatal(err)
	}
	if gotHooks, _ = repo.GetWebhooks(ctx, programID); len(gotHooks) != 0 {
		t.Fatal("settings must be deleted")
	}
}
