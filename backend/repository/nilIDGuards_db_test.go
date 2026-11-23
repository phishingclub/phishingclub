package repository

import (
	"errors"
	"testing"

	"github.com/phishingclub/phishingclub/database"
)

// TestNilIDGuardsWriteNothing checks that a nil campaign id given to
// GetUnsendRecipients and a nil recipient id given to
// AnonymizeCampaignEventsByRecipientID are refused, so close never cancels
// the rows of other campaigns and a recipient delete never rewrites the events
// of other people
func TestNilIDGuardsWriteNothing(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := t.Context()
	eventType := ptrUUID()
	if err := db.Create(&database.Event{ID: eventType, CreatedAt: ptrNow(), Name: "campaign_recipient_page_visited"}).Error; err != nil {
		t.Fatalf("create event type: %v", err)
	}
	campaignID, recipientID, pseudonym := ptrUUID(), ptrUUID(), ptrUUID()
	if err := db.Model(&database.CampaignRecipient{}).Create(map[string]any{
		"id":           ptrUUID(),
		"created_at":   ptrNow(),
		"updated_at":   ptrNow(),
		"campaign_id":  campaignID,
		"recipient_id": recipientID,
	}).Error; err != nil {
		t.Fatalf("create campaign recipient: %v", err)
	}
	// a campaign level event and an anonymized event both have no recipient
	addEvent(t, db, campaignID, nil, nil, eventType)
	addEvent(t, db, campaignID, nil, pseudonym, eventType)

	rows, err := (&CampaignRecipient{DB: db}).GetUnsendRecipients(ctx, nil, NO_LIMIT, &CampaignRecipientOption{})
	if !errors.Is(err, errNilCampaignID) {
		t.Fatalf("nil campaign id: want errNilCampaignID, got %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("nil campaign id returned %d rows", len(rows))
	}
	// the row of the campaign is still found by its own id
	rows, err = (&CampaignRecipient{DB: db}).GetUnsendRecipients(ctx, campaignID, NO_LIMIT, &CampaignRecipientOption{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("campaign id: want 1 row, got %d %v", len(rows), err)
	}

	err = (&Campaign{DB: db}).AnonymizeCampaignEventsByRecipientID(ctx, nil, ptrUUID())
	if !errors.Is(err, errNilRecipientID) {
		t.Fatalf("nil recipient id: want errNilRecipientID, got %v", err)
	}
	// the campaign level event keeps no pseudonym and the anonymized event
	// keeps its own
	var withoutPseudonym, withOwnPseudonym int64
	db.Model(&database.CampaignEvent{}).Where("anonymized_id IS NULL").Count(&withoutPseudonym)
	db.Model(&database.CampaignEvent{}).Where("anonymized_id = ?", pseudonym).Count(&withOwnPseudonym)
	if withoutPseudonym != 1 || withOwnPseudonym != 1 {
		t.Fatalf("a nil recipient id moved events: %d without, %d with own pseudonym", withoutPseudonym, withOwnPseudonym)
	}
	var scrubbed int64
	db.Model(&database.CampaignEvent{}).Where("data = ?", "anonymized").Count(&scrubbed)
	if scrubbed != 0 {
		t.Fatalf("a nil recipient id scrubbed %d events", scrubbed)
	}
}
