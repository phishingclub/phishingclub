package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
)

// TestGetEventsByCampaignID checks the campaign event reader: only the
// campaign's events, sorted on a joined recipient column, searched, filtered by
// type and time, paged
func TestGetEventsByCampaignID(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	sentID, clickID := ptrUUID(), ptrUUID()
	for id, name := range map[*uuid.UUID]string{sentID: "campaign_recipient_message_sent", clickID: "campaign_recipient_page_visited"} {
		if err := db.Create(&database.Event{ID: id, CreatedAt: ptrNow(), Name: name}).Error; err != nil {
			t.Fatalf("create event type: %v", err)
		}
	}
	alice, bob := ptrUUID(), ptrUUID()
	for id, address := range map[*uuid.UUID]string{alice: "alice@example.com", bob: "bob@example.com"} {
		email := address
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email}).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
	}
	camp, other := ptrUUID(), ptrUUID()
	addEvent(t, db, camp, alice, nil, sentID)
	addEvent(t, db, camp, bob, nil, sentID)
	addEvent(t, db, camp, bob, nil, clickID)
	addEvent(t, db, other, alice, nil, clickID)

	r := &Campaign{DB: db}
	all, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{WithUser: true}, nil)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(all.Rows) != 3 {
		t.Fatalf("events = %d, want 3", len(all.Rows))
	}
	for _, e := range all.Rows {
		if *e.CampaignID != *camp {
			t.Fatalf("an event of another campaign was listed")
		}
	}
	// sorted by recipient email descending, first page of two
	page, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{
		QueryArgs: &vo.QueryArgs{OrderBy: TableColumn(database.RECIPIENT_TABLE, "email"), Desc: true, Limit: 2},
	}, nil)
	if err != nil {
		t.Fatalf("sorted events: %v", err)
	}
	if len(page.Rows) != 2 || !page.HasNextPage || *page.Rows[0].RecipientID != *bob {
		t.Fatalf("sorted page wrong: %d rows, next %v", len(page.Rows), page.HasNextPage)
	}
	// a column outside the allowlist is refused
	if _, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{
		QueryArgs: &vo.QueryArgs{OrderBy: "password"},
	}, nil); err == nil {
		t.Fatal("expected a non allowlisted sort column to be refused")
	}
	// search on the joined event name
	found, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{
		QueryArgs: &vo.QueryArgs{Search: "visited"},
	}, nil)
	if err != nil || len(found.Rows) != 1 {
		t.Fatalf("search: %v rows %d", err, len(found.Rows))
	}
	// event types
	clicks, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{EventTypeIDs: []string{clickID.String()}}, nil)
	if err != nil || len(clicks.Rows) != 1 {
		t.Fatalf("event types: %v rows %d", err, len(clicks.Rows))
	}
	// since in the future leaves nothing
	future := time.Now().Add(time.Hour)
	none, err := r.GetEventsByCampaignID(ctx, camp, &CampaignEventOption{}, &future)
	if err != nil || len(none.Rows) != 0 {
		t.Fatalf("since: %v rows %d", err, len(none.Rows))
	}
}
