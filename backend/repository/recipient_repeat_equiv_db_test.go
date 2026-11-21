package repository

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// correlatedRepeatOffenderCount is the per row count the repeat offender
// subquery replaced, kept here to check the new query counts the same
func correlatedRepeatOffenderCount(r *Recipient, since time.Time) *gorm.DB {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Select("COUNT(DISTINCT ?)", eventCol("campaign_id")).
		Where(clause.Eq{Column: eventCol("recipient_id"), Value: clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}}).
		Where(clause.Gte{Column: eventCol("created_at"), Value: since}).
		Where(clause.IN{Column: eventCol("event_id"), Values: []interface{}{
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		}}).
		Where(r.repeatOffenderCampaigns())
}

// TestRepeatOffenderMatchesCorrelatedCount fills random campaigns, programs,
// companies and events and checks the repeat offender count and flags equal
// the per row count, per company and over every company
func TestRepeatOffenderMatchesCorrelatedCount(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.Option{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	if err := db.Create(&database.Option{ID: ptrUUID(), Key: data.OptionKeyRepeatOffenderMonths, Value: "6"}).Error; err != nil {
		t.Fatalf("create option: %v", err)
	}
	names := []string{
		data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED,
		data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ,
	}
	ids := []*uuid.UUID{}
	for _, n := range names {
		prev := cache.EventIDByName[n]
		id := ptrUUID()
		cache.EventIDByName[n] = id
		ids = append(ids, id)
		t.Cleanup(func() { cache.EventIDByName[n] = prev })
	}
	rng := rand.New(rand.NewSource(3))
	companies := []*uuid.UUID{nil, ptrUUID(), ptrUUID()}
	recipients := []*uuid.UUID{}
	for i := 0; i < 120; i++ {
		id := ptrUUID()
		email := id.String() + "@example.com"
		row := &database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email, CompanyID: companies[i%3]}
		if i%17 == 0 {
			row.ScimSoftDeletedAt = ptrNow()
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
		recipients = append(recipients, id)
	}
	program := createProgram(t, db, "named", nil)
	anon := createProgram(t, db, "anonymous", nil)
	if err := db.Model(&database.Program{}).Where("id = ?", anon).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	campaigns := []*uuid.UUID{}
	for i := 0; i < 30; i++ {
		id := ptrUUID()
		switch i % 5 {
		case 0:
			createHiddenCampaign(t, db, id, program)
		case 1:
			createHiddenCampaign(t, db, id, anon)
		default:
			row := &database.Campaign{
				ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + id.String(),
				IsTest: i%7 == 0, IsTraining: i%11 == 0,
			}
			if err := db.Create(row).Error; err != nil {
				t.Fatalf("create campaign: %v", err)
			}
		}
		campaigns = append(campaigns, id)
	}
	for i := 0; i < 900; i++ {
		at := time.Now().AddDate(0, -rng.Intn(9), -rng.Intn(28))
		row := &database.CampaignEvent{
			ID: ptrUUID(), CreatedAt: &at, UpdatedAt: &at,
			CampaignID:  campaigns[rng.Intn(len(campaigns))],
			RecipientID: recipients[rng.Intn(len(recipients))],
			EventID:     ids[rng.Intn(len(ids))],
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	repo := &Recipient{DB: db, OptionRepository: &Option{DB: db}}
	since := time.Now().AddDate(0, -6, 0)
	// the expected flags from the per row count
	want := map[uuid.UUID]bool{}
	var rows []struct {
		ID    uuid.UUID
		Count int
	}
	if err := db.Table(database.RECIPIENT_TABLE).
		Select("?, (?) AS count", clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}, correlatedRepeatOffenderCount(repo, since)).
		Scan(&rows).Error; err != nil {
		t.Fatalf("reference: %v", err)
	}
	offenders := 0
	for _, r := range rows {
		want[r.ID] = r.Count > 1
	}
	wantCount := func(companyID *uuid.UUID) int64 {
		n := int64(0)
		for i, id := range recipients {
			if !want[*id] || i%17 == 0 {
				continue
			}
			if companyID != nil && (companies[i%3] == nil || *companies[i%3] != *companyID) {
				continue
			}
			n++
		}
		return n
	}
	for _, v := range want {
		if v {
			offenders++
		}
	}
	if offenders == 0 {
		t.Fatal("the data has no repeat offender to compare")
	}
	for _, companyID := range companies {
		got, err := repo.GetRepeatOffenderCount(ctx, companyID)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if got != wantCount(companyID) {
			t.Fatalf("company %v: count %d, want %d", companyID, got, wantCount(companyID))
		}
	}
	all, err := repo.GetAll(ctx, nil, &RecipientOption{QueryArgs: &vo.QueryArgs{OrderBy: "is_repeat_offender", Desc: true}})
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	seenFalse := false
	for _, r := range all.Rows {
		id := r.ID.MustGet()
		if r.IsRepeatOffender != want[id] {
			t.Fatalf("recipient %s flag %v, want %v", id, r.IsRepeatOffender, want[id])
		}
		if !r.IsRepeatOffender {
			seenFalse = true
		} else if seenFalse {
			t.Fatal("not sorted by the repeat offender flag")
		}
	}
}
