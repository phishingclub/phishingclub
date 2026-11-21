package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// statsProgram creates a program in a company with the given start, started,
// paused and closed times, nil leaves a field empty
func (p *programTest) statsProgram(companyID *uuid.UUID, startAt, startedAt, pausedAt, closedAt *time.Time) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	p.create(&database.Program{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "program " + id.String(),
		CompanyID: companyID, LoopMode: model.ProgramLoopComplete, OrderMode: model.ProgramOrderSequential,
		StartAt: startAt, StartedAt: startedAt, PausedAt: pausedAt, ClosedAt: closedAt,
	})
	return id
}

func TestProgramGetStats(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(24 * time.Hour)
	companyA, companyB := uuid.New(), uuid.New()
	alice := p.recipient("alice@example.com")

	// company A
	// running with a usable template and someone enrolled, sent through two
	// hidden campaigns that must not make it count twice
	healthy := p.statsProgram(&companyA, &past, &past, nil, nil)
	healthyStep := p.step(healthy, 0, true)
	p.stepCampaign(healthy, healthyStep, 0)
	p.stepCampaign(healthy, healthyStep, 1)
	p.enroll(healthy, alice, 0)
	// running with a template that cannot be sent
	broken := p.statsProgram(&companyA, &past, &past, nil, nil)
	p.step(broken, 0, false)
	p.enroll(broken, alice, 0)
	// running with nobody enrolled
	empty := p.statsProgram(&companyA, &past, &past, nil, nil)
	p.step(empty, 0, true)
	// paused, also with a broken template, counts as paused only
	paused := p.statsProgram(&companyA, &past, &past, &past, nil)
	p.step(paused, 0, false)
	// paused and then closed counts as closed
	p.statsProgram(&companyA, &past, &past, &past, &past)
	// draft
	p.statsProgram(&companyA, nil, nil, nil, nil)
	// scheduled, one of them with a broken template
	p.statsProgram(&companyA, &future, nil, nil, nil)
	scheduledBroken := p.statsProgram(&companyA, &future, nil, nil, nil)
	p.step(scheduledBroken, 0, false)
	// closed
	p.statsProgram(&companyA, &past, &past, nil, &past)

	// company B, one running
	other := p.statsProgram(&companyB, &past, &past, nil, nil)
	p.step(other, 0, true)
	p.enroll(other, alice, 0)
	// shared, one running
	shared := p.statsProgram(nil, &past, &past, nil, nil)
	p.step(shared, 0, true)
	p.enroll(shared, alice, 0)

	cases := []struct {
		name      string
		companyID *uuid.UUID
		want      model.ProgramsStatView
	}{
		{"company A", &companyA, model.ProgramsStatView{Active: 1, Scheduled: 2, Paused: 1, NeedsAttention: 2, Total: 9}},
		{"company B", &companyB, model.ProgramsStatView{Active: 1, Total: 1}},
		{"every company", nil, model.ProgramsStatView{Active: 3, Scheduled: 2, Paused: 1, NeedsAttention: 2, Total: 11}},
		{"company without programs", ptrTo(uuid.New()), model.ProgramsStatView{}},
	}
	for _, c := range cases {
		got, err := p.svc.GetStats(p.ctx, session, c.companyID)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if *got != c.want {
			t.Fatalf("%s: got %+v, want %+v", c.name, *got, c.want)
		}
	}

	// the running programs split the same way the program list shows them
	list, err := p.svc.GetAll(p.ctx, session, &companyA, &repository.ProgramOption{QueryArgs: &vo.QueryArgs{Limit: 100, OrderBy: "`programs`.`name`"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	attention := 0
	for _, row := range list.Rows {
		if row.Status == model.ProgramStatusActive && !row.IsPaused() && row.NeedsAttention {
			attention++
		}
	}
	if attention != 2 {
		t.Fatalf("the list shows %d programs needing attention, the stats 2", attention)
	}

	// the hidden campaigns stay out of the dashboard campaign count
	active, err := p.svc.CampaignRepository.GetActiveCounts(p.ctx, nil, true)
	if err != nil {
		t.Fatalf("campaign count: %v", err)
	}
	if active.Active != 0 || active.Paused != 0 {
		t.Fatalf("hidden program campaigns counted as active campaigns: %+v", active)
	}

	if _, err := p.svc.GetStats(p.ctx, unauthorizedSession(t), nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused read, got %v", err)
	}
}

func ptrTo[T any](v T) *T { return &v }

func TestProgramGetCalendarEntries(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.Company{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	session := authorizedSession(t)
	now := time.Now().UTC()
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	day := 24 * time.Hour
	companyA, companyB := uuid.New(), uuid.New()
	p.create(&database.Company{ID: companyA, CreatedAt: &now, UpdatedAt: &now, Name: "Company A", AssetsKey: "a"})

	setEnd := func(id uuid.UUID, end *time.Time) {
		p.setProgram(id, map[string]any{"end_at": end})
	}
	running := p.statsProgram(&companyA, at(-30*day), at(-30*day), nil, nil)
	scheduledIn := p.statsProgram(&companyA, at(3*day), nil, nil, nil)
	p.statsProgram(&companyA, at(30*day), nil, nil, nil) // starts after the range
	p.statsProgram(&companyA, nil, nil, nil, nil)        // draft
	p.statsProgram(&companyA, at(-60*day), at(-60*day), nil, at(-20*day))
	closedIn := p.statsProgram(&companyA, at(-60*day), at(-60*day), nil, at(-2*day))
	endedBefore := p.statsProgram(&companyA, at(-60*day), at(-60*day), nil, nil)
	setEnd(endedBefore, at(-10*day))
	endsIn := p.statsProgram(&companyA, at(-60*day), at(-60*day), nil, nil)
	setEnd(endsIn, at(2*day))
	paused := p.statsProgram(&companyA, at(-30*day), at(-30*day), at(-day), nil)
	other := p.statsProgram(&companyB, at(-30*day), at(-30*day), nil, nil)

	options := &repository.ProgramOption{QueryArgs: &vo.QueryArgs{Limit: 100, OrderBy: "`programs`.`name`"}}
	got := func(companyID *uuid.UUID) map[uuid.UUID]*model.ProgramCalendarEntry {
		t.Helper()
		res, err := p.svc.GetCalendarEntries(p.ctx, session, companyID, now.Add(-7*day), now.Add(7*day), options)
		if err != nil {
			t.Fatalf("calendar: %v", err)
		}
		out := map[uuid.UUID]*model.ProgramCalendarEntry{}
		for _, row := range res.Rows {
			out[row.ID] = row
		}
		return out
	}

	a := got(&companyA)
	want := []uuid.UUID{running, scheduledIn, closedIn, endsIn, paused}
	if len(a) != len(want) {
		t.Fatalf("company A: got %d entries, want %d", len(a), len(want))
	}
	for _, id := range want {
		if a[id] == nil {
			t.Fatalf("company A: missing %s", id)
		}
	}
	if a[paused].Status != model.ProgramStatusPaused || a[scheduledIn].Status != model.ProgramStatusScheduled ||
		a[closedIn].Status != model.ProgramStatusClosed || a[running].Status != model.ProgramStatusActive {
		t.Fatalf("unexpected statuses: %+v", a)
	}
	if a[running].CompanyName != "Company A" {
		t.Fatalf("company name: %q", a[running].CompanyName)
	}
	if b := got(&companyB); len(b) != 1 || b[other] == nil {
		t.Fatalf("company B: %+v", b)
	}
	if all := got(nil); len(all) != len(want)+1 {
		t.Fatalf("every company: got %d entries", len(all))
	}

	// paging is bounded like the campaign calendar
	paged, err := p.svc.GetCalendarEntries(p.ctx, session, &companyA, now.Add(-7*day), now.Add(7*day),
		&repository.ProgramOption{QueryArgs: &vo.QueryArgs{Limit: 2, OrderBy: "`programs`.`name`"}})
	if err != nil || len(paged.Rows) != 2 || !paged.HasNextPage {
		t.Fatalf("paging: %v %+v", err, paged)
	}

	if _, err := p.svc.GetCalendarEntries(p.ctx, unauthorizedSession(t), nil, now, now, options); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused read, got %v", err)
	}
	if _, err := p.svc.GetCalendarEntries(p.ctx, session, nil, now, now.Add(-day), options); err == nil {
		t.Fatalf("an end before the start must be refused")
	}
}
