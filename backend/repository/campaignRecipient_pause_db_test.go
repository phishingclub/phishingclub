package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newPauseTestDB opens a sqlite database with the tables the send batch query
// touches: campaigns, campaign_recipients and recipients (the scim guard
// subquery reads the recipients table).
func newPauseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "pause.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Campaign{}, &database.CampaignRecipient{}, &database.Recipient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedSendableRecipient writes a campaign and one recipient row that the send
// batch query should pick up: due in the past, not sent, not cancelled, not
// attempted, not self managed. Times are stored as RFC3339 strings the same way
// production writes them, so the send_at comparison matches.
func seedSendableRecipient(t *testing.T, db *gorm.DB) (campaignID, crID uuid.UUID) {
	t.Helper()
	campaignID = uuid.New()
	crID = uuid.New()
	now := utils.NowRFC3339UTC()
	past := utils.RFC3339UTC(time.Now().Add(-time.Hour))
	if err := db.Model(&database.Campaign{}).Create(map[string]any{
		"id":         campaignID,
		"created_at": now,
		"updated_at": now,
		"name":       "pause test",
	}).Error; err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	if err := db.Model(&database.CampaignRecipient{}).Create(map[string]any{
		"id":           crID,
		"created_at":   now,
		"updated_at":   now,
		"campaign_id":  campaignID,
		"send_at":      past,
		"self_managed": false,
	}).Error; err != nil {
		t.Fatalf("seed campaign recipient: %v", err)
	}
	return campaignID, crID
}

// A paused campaign must deliver to none of its recipients while a recipient of
// a campaign that is not paused is still delivered. This is the core guarantee
// of pausing: sending stops with no change to the recipient rows.
func TestGetUnsendRecipientsForSending_SkipsPausedCampaign(t *testing.T) {
	db := newPauseTestDB(t)
	campaignID, _ := seedSendableRecipient(t, db)
	cr := &CampaignRecipient{DB: db}
	camp := &Campaign{DB: db}
	ctx := context.Background()

	got, err := cr.GetUnsendRecipientsForSending(ctx, 0, &CampaignRecipientOption{})
	if err != nil {
		t.Fatalf("get unsent (active): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("active campaign: want 1 sendable recipient, got %d", len(got))
	}

	if err := camp.SetPausedByID(ctx, &campaignID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, err = cr.GetUnsendRecipientsForSending(ctx, 0, &CampaignRecipientOption{})
	if err != nil {
		t.Fatalf("get unsent (paused): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("paused campaign: want 0 sendable recipients, got %d", len(got))
	}

	if err := camp.ClearPausedByID(ctx, &campaignID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, err = cr.GetUnsendRecipientsForSending(ctx, 0, &CampaignRecipientOption{})
	if err != nil {
		t.Fatalf("get unsent (resumed): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("resumed campaign: want 1 sendable recipient, got %d", len(got))
	}
}

// SetSendAtBatch moves recipients' scheduled send times and leaves everything
// else untouched, which is what resume relies on when it re-spreads recipients.
func TestSetSendAtBatch(t *testing.T) {
	db := newPauseTestDB(t)
	_, crID := seedSendableRecipient(t, db)
	cr := &CampaignRecipient{DB: db}
	ctx := context.Background()

	want := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if err := cr.SetSendAtBatch(ctx, []RecipientSendAt{{ID: &crID, SendAt: want}}); err != nil {
		t.Fatalf("set send at: %v", err)
	}
	var row database.CampaignRecipient
	if err := db.Where("id = ?", crID).First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.SendAt == nil {
		t.Fatalf("send at was not set")
	}
	if !row.SendAt.UTC().Truncate(time.Second).Equal(want) {
		t.Fatalf("send at: want %s, got %s", want, row.SendAt.UTC())
	}
	if row.SentAt != nil {
		t.Fatalf("sent at must stay null")
	}
}

// Resuming an anonymous campaign re-spreads send_at the same way, so the
// reschedule must leave the pseudonym (anonymized_id) and the recipient link
// untouched. If it reassigned the pseudonym the recipient's existing events
// would be orphaned.
func TestSetSendAtBatch_PreservesAnonymizedID(t *testing.T) {
	db := newPauseTestDB(t)
	campaignID := uuid.New()
	crID := uuid.New()
	recipientID := uuid.New()
	anonID := uuid.New()
	now := utils.NowRFC3339UTC()
	past := utils.RFC3339UTC(time.Now().Add(-time.Hour))
	if err := db.Model(&database.Campaign{}).Create(map[string]any{
		"id": campaignID, "created_at": now, "updated_at": now,
		"name": "anon pause test", "is_anonymous": true,
	}).Error; err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	// an anonymous campaign row holds both recipient_id and anonymized_id while live
	if err := db.Model(&database.CampaignRecipient{}).Create(map[string]any{
		"id": crID, "created_at": now, "updated_at": now,
		"campaign_id": campaignID, "recipient_id": recipientID, "anonymized_id": anonID,
		"send_at": past, "self_managed": false,
	}).Error; err != nil {
		t.Fatalf("seed campaign recipient: %v", err)
	}

	cr := &CampaignRecipient{DB: db}
	if err := cr.SetSendAtBatch(context.Background(), []RecipientSendAt{{ID: &crID, SendAt: time.Now().Add(24 * time.Hour)}}); err != nil {
		t.Fatalf("set send at: %v", err)
	}
	var row database.CampaignRecipient
	if err := db.Where("id = ?", crID).First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.AnonymizedID == nil || *row.AnonymizedID != anonID {
		t.Fatalf("anonymized id changed: want %s, got %v", anonID, row.AnonymizedID)
	}
	if row.RecipientID == nil || *row.RecipientID != recipientID {
		t.Fatalf("recipient id changed: want %s, got %v", recipientID, row.RecipientID)
	}
}
