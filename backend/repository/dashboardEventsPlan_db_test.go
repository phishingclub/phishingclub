package repository

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// dashboardPlanSeed is the data of the dashboard event plan tests
type dashboardPlanSeed struct {
	companyA, companyB, companyEmpty *uuid.UUID
	sent, failed, closed             *uuid.UUID
	programTypes                     []string
}

// seedDashboardPlan stores events of two companies, a company with no events
// and a global campaign, with test campaigns, a program and an anonymous
// program. Many events share a time, so the order of ties is tested.
func seedDashboardPlan(t *testing.T, db *gorm.DB) *dashboardPlanSeed {
	t.Helper()
	if err := (database.CampaignEvent{}).Migrate(db); err != nil {
		t.Fatalf("event indexes: %v", err)
	}
	s := &dashboardPlanSeed{
		companyA: ptrUUID(), companyB: ptrUUID(), companyEmpty: ptrUUID(),
		sent: ptrUUID(), failed: ptrUUID(), closed: ptrUUID(),
	}
	s.programTypes = []string{s.sent.String(), s.failed.String()}
	campaign := func(companyID *uuid.UUID, isTest bool, programID *uuid.UUID) *uuid.UUID {
		id := ptrUUID()
		row := &database.Campaign{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + id.String(), CompanyID: companyID, IsTest: isTest, ProgramID: programID}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create campaign: %v", err)
		}
		return id
	}
	program := createProgram(t, db, "p", s.companyA)
	anonProgram := createProgram(t, db, "anon", s.companyA)
	if err := db.Model(&database.Program{}).Where("id = ?", anonProgram).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	recipients := []*uuid.UUID{}
	for i := 0; i < 5; i++ {
		id := ptrUUID()
		// each recipient has many events, so the email sort has ties
		email := fmt.Sprintf("r%d@example.com", i)
		if err := db.Create(&database.Recipient{ID: id, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Email: &email}).Error; err != nil {
			t.Fatalf("create recipient: %v", err)
		}
		recipients = append(recipients, id)
	}
	campaigns := []*uuid.UUID{
		campaign(s.companyA, false, nil),
		campaign(s.companyA, true, nil),
		campaign(s.companyA, false, program),
		campaign(s.companyA, false, anonProgram),
		campaign(s.companyB, false, nil),
		campaign(nil, false, nil),
	}
	campaign(s.companyEmpty, false, nil)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	add := func(campaignID, recipientID, eventID *uuid.UUID, at time.Time) {
		row := map[string]any{
			"id": ptrUUID().String(), "created_at": utils.RFC3339UTC(at), "updated_at": utils.RFC3339UTC(at),
			"data": "", "campaign_id": campaignID.String(), "event_id": eventID.String(),
		}
		if recipientID != nil {
			row["recipient_id"] = recipientID.String()
		}
		if err := db.Model(&database.CampaignEvent{}).Create(row).Error; err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	types := []*uuid.UUID{s.sent, s.sent, s.failed, s.closed}
	for i := 0; i < 60; i++ {
		// every four events share a time
		at := base.Add(time.Duration(i/4) * time.Minute)
		add(campaigns[i%len(campaigns)], recipients[i%len(recipients)], types[i%len(types)], at)
	}
	return s
}

// dashboardPages reads every page of dashboard events with the given sort
// limit, which picks the plan, and returns the event ids in order
func dashboardPages(
	t *testing.T,
	r *Campaign,
	companyID *uuid.UUID,
	options func() *CampaignEventAllOption,
	limit int,
) []uuid.UUID {
	t.Helper()
	dashboardEventsSortLimit = limit
	out := []uuid.UUID{}
	for offset := 0; ; offset += 4 {
		o := options()
		o.QueryArgs.Limit, o.QueryArgs.Offset = 4, offset
		res, err := r.GetAllEvents(context.Background(), companyID, o)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		for _, e := range res.Rows {
			out = append(out, *e.ID)
		}
		if !res.HasNextPage {
			return out
		}
	}
}

// TestDashboardEventsBothPlans checks a page of dashboard events sorted by
// time is the same whether the events of the scope are read campaign by
// campaign and sorted or found by a walk of the created_at index, for one
// company, a company with no events and all companies, with and without an
// event type, newest and oldest first, over every page
func TestDashboardEventsBothPlans(t *testing.T) {
	db := newProgramTestDB(t)
	r := &Campaign{DB: db}
	s := seedDashboardPlan(t, db)
	defer func(v int) { dashboardEventsSortLimit = v }(dashboardEventsSortLimit)

	for _, company := range []*uuid.UUID{s.companyA, s.companyB, s.companyEmpty, nil} {
		for _, eventType := range []*uuid.UUID{nil, s.failed, s.closed} {
			for _, desc := range []bool{true, false} {
				options := func() *CampaignEventAllOption {
					o := &CampaignEventAllOption{
						QueryArgs: &vo.QueryArgs{
							OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
							Desc:    desc,
						},
						WithUser:            true,
						ProgramEventTypeIDs: s.programTypes,
					}
					if eventType != nil {
						o.EventTypeIDs = []string{eventType.String()}
					}
					return o
				}
				name := fmt.Sprintf("company %v type %v desc %v", company, eventType, desc)
				sorted, walked := dashboardPages(t, r, company, options, 1000), dashboardPages(t, r, company, options, -1)
				if len(sorted) != len(walked) {
					t.Fatalf("%s: %d rows sorted, %d walked", name, len(sorted), len(walked))
				}
				for i := range sorted {
					if sorted[i] != walked[i] {
						t.Fatalf("%s: row %d differs", name, i)
					}
				}
				if company == s.companyEmpty && len(sorted) != 0 {
					t.Fatalf("%s: a company with no events listed %d", name, len(sorted))
				}
				if company == s.companyA && eventType == nil && len(sorted) == 0 {
					t.Fatalf("%s: no events", name)
				}
			}
		}
	}

	// the count stops at the limit and keeps the scope: company A lists its
	// plain campaign and its program events of the program types
	all := dashboardPages(t, r, s.companyA, func() *CampaignEventAllOption {
		return &CampaignEventAllOption{
			QueryArgs:           &vo.QueryArgs{OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Desc: true},
			ProgramEventTypeIDs: s.programTypes,
		}
	}, 1000)
	n := len(all)
	for _, tc := range []struct {
		limit int
		few   bool
	}{{n - 1, false}, {n, true}} {
		dashboardEventsSortLimit = tc.limit
		few, err := r.dashboardHasFewEvents(context.Background(), s.companyA, &CampaignEventAllOption{ProgramEventTypeIDs: s.programTypes})
		if err != nil || few != tc.few {
			t.Fatalf("%d events under a limit of %d: few %v (%v)", n, tc.limit, few, err)
		}
	}
}

// TestDashboardEventsSortedByEmail checks a page sorted by something other
// than time lists the same events as the time sort, in email order
func TestDashboardEventsSortedByEmail(t *testing.T) {
	db := newProgramTestDB(t)
	r := &Campaign{DB: db}
	s := seedDashboardPlan(t, db)
	for _, company := range []*uuid.UUID{s.companyA, nil} {
		byTime := dashboardPages(t, r, company, func() *CampaignEventAllOption {
			return &CampaignEventAllOption{
				QueryArgs:           &vo.QueryArgs{OrderBy: TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Desc: true},
				WithUser:            true,
				ProgramEventTypeIDs: s.programTypes,
			}
		}, dashboardEventsSortLimit)
		res, err := r.GetAllEvents(context.Background(), company, &CampaignEventAllOption{
			QueryArgs:           &vo.QueryArgs{OrderBy: TableColumn(database.RECIPIENT_TABLE, "email"), Limit: 1000},
			WithUser:            true,
			ProgramEventTypeIDs: s.programTypes,
		})
		if err != nil {
			t.Fatalf("email sort: %v", err)
		}
		byEmail := []uuid.UUID{}
		emails := []string{}
		for _, e := range res.Rows {
			byEmail = append(byEmail, *e.ID)
			emails = append(emails, utils.NullableToString(e.Recipient.Email))
		}
		if !sort.StringsAreSorted(emails) {
			t.Fatalf("company %v: not sorted by email: %v", company, emails)
		}
		if len(byEmail) == 0 || len(byEmail) != len(byTime) {
			t.Fatalf("company %v: %d events by email, %d by time", company, len(byEmail), len(byTime))
		}
		ids := func(in []uuid.UUID) []string {
			out := []string{}
			for _, id := range in {
				out = append(out, id.String())
			}
			sort.Strings(out)
			return out
		}
		a, b := ids(byTime), ids(byEmail)
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("company %v: the email sort lists other events", company)
			}
		}
	}
}
