package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// TestDashboardEventsAndStats checks the dashboard event list names a program
// event by its program and drops the hidden campaign, leaves anonymous program
// events out, filters on event type, and that the stats count paused
// campaigns and failed sends in the same scope. Both reads are refused
// without permission.
func TestDashboardEventsAndStats(t *testing.T) {
	p := newProgramServiceTest(t)
	failedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED]
	if failedID == nil {
		id := uuid.New()
		failedID = &id
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED] = failedID
		t.Cleanup(func() { delete(cache.EventIDByName, data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED) })
	}
	closedID := cache.EventIDByName[data.EVENT_CAMPAIGN_CLOSED]
	companyA := uuid.New()
	alice := p.recipient("alice@example.com")
	now := time.Now().UTC()
	past := now.AddDate(0, 0, -2)

	campaign := func(programID *uuid.UUID, paused bool) uuid.UUID {
		id := uuid.New()
		row := &database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "c " + id.String(), CompanyID: &companyA, ProgramID: programID, SendStartAt: &past}
		if paused {
			row.PausedAt = &now
		}
		p.create(row)
		return id
	}
	event := func(campaignID uuid.UUID, eventID *uuid.UUID) {
		id := uuid.New()
		p.create(&database.CampaignEvent{ID: &id, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, RecipientID: &alice, EventID: eventID})
	}
	program := p.statsProgram(&companyA, &past, &past, nil, nil)
	if err := p.db.Model(&database.Program{}).Where("id = ?", program).Update("name", "Quarterly").Error; err != nil {
		t.Fatalf("name program: %v", err)
	}
	anon := p.statsProgram(&companyA, &past, &past, nil, nil)
	if err := p.db.Model(&database.Program{}).Where("id = ?", anon).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	plain := campaign(nil, false)
	paused := campaign(nil, true)
	hidden := campaign(&program, false)
	anonHidden := campaign(&anon, false)
	event(plain, failedID)
	event(paused, failedID)
	event(hidden, failedID)
	event(hidden, closedID)
	event(anonHidden, failedID)

	svc := p.svc.CampaignService
	session := authorizedSession(t)
	q := func() *vo.QueryArgs {
		return &vo.QueryArgs{Limit: 50, OrderBy: repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Desc: true}
	}
	page, err := svc.GetAllEvents(p.ctx, session, &companyA, q(), false, nil)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(page.Rows) != 3 {
		t.Fatalf("expected 3 events, got %d", len(page.Rows))
	}
	programRows := 0
	for _, ev := range page.Rows {
		if ev.ProgramID == nil {
			continue
		}
		programRows++
		if *ev.ProgramID != program || ev.ProgramName != "Quarterly" {
			t.Fatalf("program event named %v %q", ev.ProgramID, ev.ProgramName)
		}
		if ev.CampaignID != nil || ev.Campaign != nil {
			t.Fatalf("hidden campaign leaked on a program event: %+v", ev.Campaign)
		}
	}
	if programRows != 1 {
		t.Fatalf("expected 1 program event, got %d", programRows)
	}
	filtered, err := svc.GetAllEvents(p.ctx, session, &companyA, q(), false, closedID)
	if err != nil || len(filtered.Rows) != 0 {
		t.Fatalf("closed is not a program recipient event, got %v (%v)", filtered, err)
	}

	stats, err := svc.GetStats(p.ctx, session, &companyA, false)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Paused != 1 || stats.Active != 1 || stats.FailedSending != 3 || stats.FailedSendingDays != FailedSendingDays {
		t.Fatalf("stats %+v", stats)
	}

	if _, err := svc.GetAllEvents(p.ctx, unauthorizedSession(t), nil, q(), false, nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused event read, got %v", err)
	}
	if _, err := svc.GetStats(p.ctx, unauthorizedSession(t), nil, false); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused stats read, got %v", err)
	}
}
