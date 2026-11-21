package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
)

// TestRepeatOffenderCountsProgramSends checks that clicks in the hidden
// campaigns of a non anonymous program count toward a repeat offender like
// campaign clicks, that an anonymous program, a test campaign and clicks older
// than the window are left out, and that plain campaigns count as before. The
// per recipient stats behind the recipient page tiles count campaigns only.
func TestRepeatOffenderCountsProgramSends(t *testing.T) {
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

	people := map[string]*uuid.UUID{}
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin", "frank", "gina"} {
		id := ptrUUID()
		email := name + "@example.com"
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email}).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
		people[name] = id
	}
	plain := func(isTest bool) *uuid.UUID {
		id := ptrUUID()
		if err := db.Create(&database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "plain " + id.String(), IsTest: isTest}).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		return id
	}
	programID := createProgram(t, db, "p", nil)
	anonProgramID := createProgram(t, db, "anon", nil)
	if err := db.Model(&database.Program{}).Where("id = ?", anonProgramID).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	hidden := func(program *uuid.UUID) *uuid.UUID {
		id := ptrUUID()
		createHiddenCampaign(t, db, id, program)
		return id
	}

	// alice: two plain campaigns, bob: two program sends, carol: plain and
	// program, dave: two anonymous program sends, erin: two clicks in one
	// campaign, frank: two test campaigns, gina: one recent and one old click
	for i := 0; i < 2; i++ {
		addEvent(t, db, plain(false), people["alice"], nil, clickID)
		addEvent(t, db, hidden(programID), people["bob"], nil, clickID)
		addEvent(t, db, hidden(anonProgramID), people["dave"], nil, clickID)
		addEvent(t, db, plain(true), people["frank"], nil, clickID)
	}
	addEvent(t, db, plain(false), people["carol"], nil, clickID)
	addEvent(t, db, hidden(programID), people["carol"], nil, clickID)
	same := plain(false)
	addEvent(t, db, same, people["erin"], nil, clickID)
	addEvent(t, db, same, people["erin"], nil, clickID)
	addEvent(t, db, plain(false), people["gina"], nil, clickID)
	oldCampaign := plain(false)
	addEvent(t, db, oldCampaign, people["gina"], nil, clickID)
	old := time.Now().AddDate(-2, 0, 0)
	if err := db.Model(&database.CampaignEvent{}).Where("campaign_id = ?", oldCampaign).Update("created_at", old).Error; err != nil {
		t.Fatalf("age event: %v", err)
	}

	want := map[string]bool{"alice": true, "bob": true, "carol": true}
	repo := &Recipient{DB: db, OptionRepository: &Option{DB: db}}
	count, err := repo.GetRepeatOffenderCount(ctx, nil)
	if err != nil || count != int64(len(want)) {
		t.Fatalf("expected %d repeat offenders, got %d (%v)", len(want), count, err)
	}
	all, err := repo.GetAll(ctx, nil, &RecipientOption{})
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all.Rows) != len(people) {
		t.Fatalf("expected %d rows, got %d", len(people), len(all.Rows))
	}
	for name, id := range people {
		for _, r := range all.Rows {
			if r.ID.MustGet() == *id && r.IsRepeatOffender != want[name] {
				t.Fatalf("%s: repeat offender %v, want %v", name, r.IsRepeatOffender, want[name])
			}
		}
	}
	// the per recipient stats leave program sends out: bob has only program
	// clicks and carol one campaign click
	for name, wantClicks := range map[string]int64{"alice": 1, "bob": 0, "carol": 0, "dave": 0, "frank": 0} {
		stats, err := repo.GetStatsByID(ctx, people[name])
		if err != nil {
			t.Fatalf("stats %s: %v", name, err)
		}
		if stats.RepeatLinkClicks != wantClicks {
			t.Fatalf("%s: repeat link clicks %d, want %d", name, stats.RepeatLinkClicks, wantClicks)
		}
	}
	// repeat submissions follow the same rule
	submitID := ptrUUID()
	prevSubmit := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA] = submitID
	t.Cleanup(func() { cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA] = prevSubmit })
	for i := 0; i < 2; i++ {
		addEvent(t, db, plain(false), people["alice"], nil, submitID)
		addEvent(t, db, hidden(programID), people["bob"], nil, submitID)
	}
	for name, wantSubmits := range map[string]int64{"alice": 1, "bob": 0} {
		stats, err := repo.GetStatsByID(ctx, people[name])
		if err != nil {
			t.Fatalf("stats %s: %v", name, err)
		}
		if stats.RepeatSubmissions != wantSubmits {
			t.Fatalf("%s: repeat submissions %d, want %d", name, stats.RepeatSubmissions, wantSubmits)
		}
	}
	// sorting on the computed column still works
	sorted, err := repo.GetAll(ctx, nil, &RecipientOption{QueryArgs: &vo.QueryArgs{OrderBy: "is_repeat_offender", Desc: true}})
	if err != nil || len(sorted.Rows) == 0 || !sorted.Rows[0].IsRepeatOffender {
		t.Fatalf("sort on is_repeat_offender: %v", err)
	}
}

// TestRepeatOffenderCountScope checks the dashboard scope: a company counts
// only its own recipients, no company counts every company and the unowned
// recipients
func TestRepeatOffenderCountScope(t *testing.T) {
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

	companyA := ptrUUID()
	companyB := ptrUUID()
	offender := func(name string, companyID *uuid.UUID) {
		id := ptrUUID()
		email := name + "@example.com"
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email, CompanyID: companyID}).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
		for i := 0; i < 2; i++ {
			campaignID := ptrUUID()
			if err := db.Create(&database.Campaign{ID: campaignID, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + campaignID.String()}).Error; err != nil {
				t.Fatalf("create campaign: %v", err)
			}
			addEvent(t, db, campaignID, id, nil, clickID)
		}
	}
	offender("a1", companyA)
	offender("b1", companyB)
	offender("b2", companyB)
	offender("unowned", nil)

	repo := &Recipient{DB: db, OptionRepository: &Option{DB: db}}
	for _, tc := range []struct {
		name      string
		companyID *uuid.UUID
		want      int64
	}{
		{"company A", companyA, 1},
		{"company B", companyB, 2},
		{"all companies", nil, 4},
	} {
		count, err := repo.GetRepeatOffenderCount(ctx, tc.companyID)
		if err != nil || count != tc.want {
			t.Fatalf("%s: expected %d repeat offenders, got %d (%v)", tc.name, tc.want, count, err)
		}
	}
}
