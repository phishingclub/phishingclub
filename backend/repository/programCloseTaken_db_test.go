package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
)

// TestCloseKeepsTakenSends checks a send the sender took keeps its row and
// its events when the program closes, as it may have gone out before sent_at
// was written, while a send never taken is cancelled and then deleted with
// its events
func TestCloseKeepsTakenSends(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	programID := createProgram(t, db, "close", nil)
	campaign := ptrUUID()
	createHiddenCampaign(t, db, campaign, programID)
	taken, waiting := ptrUUID(), ptrUUID()
	takenRow := addCampaignRecipient(t, db, campaign, taken, nil, false)
	waitingRow := addCampaignRecipient(t, db, campaign, waiting, nil, false)
	db.Model(&database.CampaignRecipient{}).Where("id = ?", takenRow).
		Update("last_attempt_at", utils.RFC3339UTC(time.Now()))
	eventID := ptrUUID()
	for _, person := range []any{taken, waiting} {
		if err := db.Create(&database.CampaignEvent{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			CampaignID: campaign, RecipientID: person.(*uuid.UUID), EventID: eventID,
		}).Error; err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	if err := r.CancelAllUnsent(ctx, programID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n, err := r.CancelSends(ctx, []*uuid.UUID{takenRow}); err != nil || n != 0 {
		t.Fatalf("a taken send must not be cancelled: %d %v", n, err)
	}
	if err := r.DeleteUnsentSends(ctx, programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var row database.CampaignRecipient
	if err := db.Where("id = ?", takenRow).First(&row).Error; err != nil || row.CancelledAt != nil {
		t.Fatalf("the taken send must stay as it was: %v %+v", err, row)
	}
	if countWhere(t, db, &database.CampaignEvent{}, "recipient_id", taken) != 1 {
		t.Fatal("the events of the taken send must stay")
	}
	if countWhere(t, db, &database.CampaignRecipient{}, "id", waitingRow) != 0 ||
		countWhere(t, db, &database.CampaignEvent{}, "recipient_id", waiting) != 0 {
		t.Fatal("a send never taken is deleted with its events")
	}
}
