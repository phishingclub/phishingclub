package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
)

// TestRepeatOffenderLeavesOutPrograms checks that clicks in a program's hidden
// campaigns do not make a person a repeat offender
func TestRepeatOffenderLeavesOutPrograms(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.Recipient{}, &database.Option{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	if err := db.Create(&database.Option{ID: ptrUUID(), Key: data.OptionKeyRepeatOffenderMonths, Value: "12"}).Error; err != nil {
		t.Fatalf("create option: %v", err)
	}
	clickID := ptrUUID()
	prev := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = clickID
	t.Cleanup(func() { cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED] = prev })

	alice, bob := ptrUUID(), ptrUUID()
	for id, address := range map[*uuid.UUID]string{alice: "alice@example.com", bob: "bob@example.com"} {
		email := address
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email}).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
	}
	// alice clicked in two plain campaigns, bob in two program campaigns
	programID := createProgram(t, db, "p", nil)
	for i := 0; i < 2; i++ {
		plain := ptrUUID()
		if err := db.Create(&database.Campaign{ID: plain, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "plain " + plain.String()}).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		addEvent(t, db, plain, alice, nil, clickID)
		hidden := ptrUUID()
		createHiddenCampaign(t, db, hidden, programID)
		addEvent(t, db, hidden, bob, nil, clickID)
	}

	repo := &Recipient{DB: db, OptionRepository: &Option{DB: db}}
	count, err := repo.GetRepeatOffenderCount(ctx, nil)
	if err != nil || count != 1 {
		t.Fatalf("expected only alice as repeat offender, got %d (%v)", count, err)
	}
	all, err := repo.GetAll(ctx, nil, &RecipientOption{})
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	for _, r := range all.Rows {
		if r.ID.MustGet() == *bob && r.IsRepeatOffender {
			t.Fatal("bob must not be flagged by program clicks")
		}
		if r.ID.MustGet() == *alice && !r.IsRepeatOffender {
			t.Fatal("alice must be flagged by plain campaign clicks")
		}
	}
}
