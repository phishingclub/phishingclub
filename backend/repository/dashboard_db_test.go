package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
)

// TestDashboardEventScope checks the dashboard event scope: company scope, test
// campaigns only when asked, program events only of the given types and never
// from an anonymous program, the event type filter and the since cutoff
func TestDashboardEventScope(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Campaign{DB: db}
	failedID, closedID := ptrUUID(), ptrUUID()
	companyA, companyB := ptrUUID(), ptrUUID()
	campaign := func(companyID *uuid.UUID, isTest bool, programID *uuid.UUID) *uuid.UUID {
		id := ptrUUID()
		row := &database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + id.String(), CompanyID: companyID, IsTest: isTest, ProgramID: programID}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		return id
	}
	program := createProgram(t, db, "p", companyA)
	anonProgram := createProgram(t, db, "anon", companyA)
	if err := db.Model(&database.Program{}).Where("id = ?", anonProgram).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	recipient := ptrUUID()
	addEvent(t, db, campaign(companyA, false, nil), recipient, nil, failedID)
	addEvent(t, db, campaign(companyA, true, nil), recipient, nil, failedID)
	addEvent(t, db, campaign(companyB, false, nil), recipient, nil, failedID)
	addEvent(t, db, campaign(nil, false, nil), recipient, nil, failedID)
	hidden := campaign(companyA, false, program)
	addEvent(t, db, hidden, recipient, nil, failedID)
	// the hidden campaign's own event type is not a program event type
	addEvent(t, db, hidden, nil, nil, closedID)
	addEvent(t, db, campaign(companyA, false, anonProgram), recipient, nil, failedID)
	old := campaign(companyA, false, nil)
	addEvent(t, db, old, recipient, nil, failedID)
	if err := db.Model(&database.CampaignEvent{}).Where("campaign_id = ?", old).Update("created_at", time.Now().AddDate(0, 0, -30)).Error; err != nil {
		t.Fatalf("age event: %v", err)
	}

	since := time.Now().AddDate(0, 0, -7)
	programTypes := []string{failedID.String()}
	for _, tc := range []struct {
		name      string
		companyID *uuid.UUID
		options   CampaignEventAllOption
		want      int64
	}{
		{"all companies", nil, CampaignEventAllOption{ProgramEventTypeIDs: programTypes}, 4},
		{"all companies with test", nil, CampaignEventAllOption{IncludeTestCampaigns: true, ProgramEventTypeIDs: programTypes}, 5},
		{"company A", companyA, CampaignEventAllOption{ProgramEventTypeIDs: programTypes}, 2},
		{"company B", companyB, CampaignEventAllOption{ProgramEventTypeIDs: programTypes}, 1},
		{"without program events", companyA, CampaignEventAllOption{}, 1},
		{"type filter", companyA, CampaignEventAllOption{ProgramEventTypeIDs: programTypes, EventTypeIDs: []string{closedID.String()}}, 0},
	} {
		options := tc.options
		count, err := repo.CountDashboardEvents(ctx, tc.companyID, since, &options)
		if err != nil || count != tc.want {
			t.Fatalf("%s: expected %d events, got %d (%v)", tc.name, tc.want, count, err)
		}
	}

	// the event list uses the same scope
	page, err := repo.GetAllEvents(ctx, companyA, &CampaignEventAllOption{
		QueryArgs:           &vo.QueryArgs{Limit: 50, OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at")},
		WithCampaign:        true,
		ProgramEventTypeIDs: programTypes,
	})
	if err != nil {
		t.Fatalf("get all events: %v", err)
	}
	// the two recent events and the old one of company A, no anonymous program event
	if len(page.Rows) != 3 {
		t.Fatalf("expected 3 listed events, got %d", len(page.Rows))
	}
	for _, ev := range page.Rows {
		if ev.Campaign != nil && ev.Campaign.IsProgramManaged() && ev.Campaign.ProgramID.MustGet() != *program {
			t.Fatalf("an anonymous program event was listed")
		}
	}
	// newest first gives the same events in the reverse order
	newest, err := repo.GetAllEvents(ctx, companyA, &CampaignEventAllOption{
		QueryArgs:           &vo.QueryArgs{Limit: 50, OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Desc: true},
		WithCampaign:        true,
		ProgramEventTypeIDs: programTypes,
	})
	if err != nil || len(newest.Rows) != len(page.Rows) {
		t.Fatalf("newest first: %v", err)
	}
	for i, ev := range newest.Rows {
		if *ev.ID != *page.Rows[len(page.Rows)-1-i].ID {
			t.Fatalf("newest first row %d is not the oldest first row from the end", i)
		}
		if i > 0 && ev.CreatedAt.After(*newest.Rows[i-1].CreatedAt) {
			t.Fatalf("newest first row %d is newer than the row before it", i)
		}
	}
}

// TestGetActiveCounts checks paused campaigns are counted apart from active
// ones, self managed is part of active, and closed, upcoming and program
// campaigns stay out
func TestGetActiveCounts(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	repo := &Campaign{DB: db}
	companyA := ptrUUID()
	// days apart, so the stored time format does not change the order
	past := time.Now().AddDate(0, 0, -2)
	future := time.Now().AddDate(0, 0, 2)
	create := func(companyID *uuid.UUID, start *time.Time, paused, closed bool, programID *uuid.UUID) {
		id := ptrUUID()
		row := &database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + id.String(), CompanyID: companyID, SendStartAt: start, ProgramID: programID}
		if paused {
			row.PausedAt = ptrNow()
		}
		if closed {
			row.ClosedAt = ptrNow()
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
	}
	create(companyA, &past, false, false, nil)
	create(companyA, nil, false, false, nil)
	create(companyA, &past, true, false, nil)
	create(companyA, &past, false, true, nil)
	create(companyA, &future, false, false, nil)
	create(nil, &past, false, false, nil)
	create(companyA, &past, false, false, createProgram(t, db, "p", companyA))
	// paused before its delivery started: paused wins over scheduled
	create(companyA, &future, true, false, nil)

	got, err := repo.GetActiveCounts(ctx, companyA, true)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if got.Active != 2 || got.SelfManaged != 1 || got.Paused != 2 {
		t.Fatalf("company A counts %+v", got)
	}
	all, err := repo.GetActiveCounts(ctx, nil, true)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if all.Active != 3 || all.Paused != 2 {
		t.Fatalf("all companies counts %+v", all)
	}
	upcoming, err := repo.GetUpcomingCount(ctx, companyA, true)
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if upcoming != 1 {
		t.Fatalf("upcoming must leave out the paused campaign, got %d", upcoming)
	}

	// the Campaigns tab tables put each campaign in the same bucket as the tiles
	options := func() *CampaignOption {
		return &CampaignOption{QueryArgs: &vo.QueryArgs{Limit: 100, OrderBy: "name"}, IncludeTestCampaigns: true}
	}
	activeList, err := repo.GetAllActive(ctx, companyA, options())
	if err != nil {
		t.Fatalf("active list: %v", err)
	}
	pausedRows := 0
	for _, c := range activeList.Rows {
		if c.PausedAt.IsSpecified() && !c.PausedAt.IsNull() {
			pausedRows++
		}
	}
	if int64(len(activeList.Rows)) != got.Active+got.Paused || int64(pausedRows) != got.Paused {
		t.Fatalf("active list has %d rows, %d paused, tiles %+v", len(activeList.Rows), pausedRows, got)
	}
	upcomingList, err := repo.GetAllUpcoming(ctx, companyA, options())
	if err != nil {
		t.Fatalf("upcoming list: %v", err)
	}
	if int64(len(upcomingList.Rows)) != upcoming {
		t.Fatalf("upcoming list has %d rows, tile %d", len(upcomingList.Rows), upcoming)
	}
}

// TestReportInboxCountWithError checks the dashboard inbox error count: no
// company counts only the global inbox, a company counts only its own, and
// turned off or healthy inboxes are left out
func TestReportInboxCountWithError(t *testing.T) {
	db := newProgramTestDB(t)
	if err := db.AutoMigrate(&database.ReportInbox{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	repo := &ReportInbox{DB: db}
	companyA, companyB, companyC := ptrUUID(), ptrUUID(), ptrUUID()
	create := func(companyID *uuid.UUID, enabled bool, lastError string) {
		row := &database.ReportInbox{ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(), CompanyID: companyID, Enabled: enabled, LastError: lastError}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create inbox: %v", err)
		}
	}
	create(nil, true, "token expired")
	create(companyA, true, "mailbox not found")
	create(companyB, false, "turned off")
	create(companyC, true, "")

	for _, tc := range []struct {
		name      string
		companyID *uuid.UUID
		want      int64
	}{
		{"global only", nil, 1},
		{"company with error", companyA, 1},
		{"turned off company", companyB, 0},
		{"healthy company", companyC, 0},
	} {
		got, err := repo.CountWithError(ctx, tc.companyID)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
