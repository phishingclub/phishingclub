package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestProgramStalledNeedsAttention checks a running program needs attention
// when its sends do not go out: a next send more than ProgramStallGrace
// overdue, a queued send waiting more than that past its send time, or an
// active person without a next send while nothing is queued. A program that
// sent nothing for long but waits for a due time ahead, as over a weekend, a
// skip range or the delay of a new member, is active. A running program with
// nobody in it needs attention once marked started or past its start by more
// than ProgramStartGrace.
func TestProgramStalledNeedsAttention(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now().UTC().Truncate(time.Second)
	longAgo := now.Add(-60 * 24 * time.Hour)
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	enroll := func(programID *uuid.UUID, due *time.Time) {
		e := &database.ProgramEnrollment{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			ProgramID: programID, RecipientID: ptrUUID(), Status: model.ProgramEnrollmentActive, NextDueAt: due,
		}
		if err := db.Create(e).Error; err != nil {
			t.Fatalf("enroll: %v", err)
		}
	}
	failedAt := map[time.Time]bool{}
	send := func(programID *uuid.UUID, sendAt time.Time, sent bool) {
		campaignID := ptrUUID()
		c := &database.Campaign{ID: campaignID, CreatedAt: ptrNow(), UpdatedAt: ptrNow(), Name: "c " + campaignID.String(), ProgramID: programID}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("campaign: %v", err)
		}
		cr := &database.CampaignRecipient{
			ID: ptrUUID(), CreatedAt: ptrNow(), UpdatedAt: ptrNow(),
			CampaignID: campaignID, RecipientID: ptrUUID(), SendAt: &sendAt,
		}
		if sent {
			cr.SentAt = &sendAt
		}
		if failedAt[sendAt] {
			cr.LastAttemptAt = &sendAt
		}
		if err := db.Create(cr).Error; err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	running := func(name string) *uuid.UUID {
		return statusProgram(t, db, name, &longAgo, &longAgo, nil, nil)
	}

	// last sent on a Friday, the next due time is on Monday
	weekend := running("weekend")
	send(weekend, now.Add(-2*day), true)
	enroll(weekend, at(day))
	// a new member waits the join delay, nothing was ever sent
	joinDelay := running("join delay")
	enroll(joinDelay, at(20*day))
	// a long skip range, the next due time is after it
	skipRange := running("skip range")
	send(skipRange, now.Add(-20*day), true)
	enroll(skipRange, at(14*day))
	// a due time just inside the grace is not yet stalled
	inGrace := running("in grace")
	enroll(inGrace, at(-day+time.Hour))
	// a queued send without a due time is planned
	queuedOnly := running("queued only")
	enroll(queuedOnly, nil)
	send(queuedOnly, now.Add(time.Hour), false)
	// a program that started a few minutes ago with nobody in it yet
	justStarted := statusProgram(t, db, "just started", at(-10*time.Minute), nil, nil, nil)

	// the scheduler stopped advancing
	overdue := running("overdue")
	enroll(overdue, at(-3*day))
	// the sender stopped sending
	queuedLate := running("queued late")
	enroll(queuedLate, at(5*day))
	send(queuedLate, now.Add(-3*day), false)
	// an active person without a next send and nothing queued
	unplanned := running("unplanned")
	enroll(unplanned, nil)
	// started and empty
	emptyStarted := running("empty started")
	// past its start but never marked started, and empty
	emptyNotMarked := statusProgram(t, db, "empty not marked", at(-2*time.Hour), nil, nil, nil)
	// one send failed, for example an address that bounced, while the others
	// went out: the sender works
	bounced := running("bounced")
	enroll(bounced, at(5*day))
	failedAt[now.Add(-3*day)] = true
	send(bounced, now.Add(-3*day), false)
	send(bounced, now.Add(-3*day+time.Minute), true)
	// every send failed in the last week and none went out
	deadSender := running("dead sender")
	enroll(deadSender, at(5*day))
	failedAt[now.Add(-2*day)] = true
	send(deadSender, now.Add(-2*day), false)
	// a failure older than the window is no longer looked at
	oldFailure := running("old failure")
	enroll(oldFailure, at(5*day))
	failedAt[now.Add(-10*day)] = true
	send(oldFailure, now.Add(-10*day), false)
	// paused programs are paused, not stalled
	paused := statusProgram(t, db, "paused", &longAgo, &longAgo, &now, nil)
	enroll(paused, at(-3*day))

	wantAttention := []*uuid.UUID{overdue, queuedLate, unplanned, emptyStarted, emptyNotMarked, deadSender}
	wantActive := []*uuid.UUID{weekend, joinDelay, skipRange, inGrace, queuedOnly, justStarted, bounced, oldFailure}
	check := func(status string, want []*uuid.UUID) {
		t.Helper()
		list, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{}, Status: status}, now)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		got := map[uuid.UUID]string{}
		for _, p := range list.Rows {
			got[p.ID.MustGet()] = p.Name.MustGet().String()
		}
		for _, id := range want {
			if _, ok := got[*id]; !ok {
				t.Errorf("%s: missing %v, got %v", status, *id, got)
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s: expected %d programs, got %v", status, len(want), got)
		}
	}
	check(model.ProgramStatusNeedsAttention, wantAttention)
	check(model.ProgramStatusActive, wantActive)
	counts, err := r.CountByStatus(ctx, nil, now)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.NeedsAttention != int64(len(wantAttention)) || counts.Active != int64(len(wantActive)) || counts.Paused != 1 {
		t.Fatalf("counts: %+v", counts)
	}

	sums, err := r.GetListSummaries(ctx, []*uuid.UUID{queuedLate, unplanned, queuedOnly, bounced, deadSender})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	late := sums[*queuedLate]
	if late.Queued != 1 || late.OldestQueuedAt == nil || late.OldestQueuedAt.Sub(now.Add(-3*day)).Abs() > time.Second {
		t.Fatalf("queued late summary: %+v", late)
	}
	if sums[*unplanned].Unplanned != 1 || sums[*unplanned].Queued != 0 {
		t.Fatalf("unplanned summary: %+v", sums[*unplanned])
	}
	if sums[*queuedOnly].Unplanned != 1 || sums[*queuedOnly].Queued != 1 {
		t.Fatalf("queued only summary: %+v", sums[*queuedOnly])
	}
	// a failed send is not counted as overdue, its attempt and the newest send
	// that went out are given instead
	b := sums[*bounced]
	if b.OldestQueuedAt != nil || b.NewestFailedAt == nil || b.NewestSentAt == nil {
		t.Fatalf("bounced summary: %+v", b)
	}
	d := sums[*deadSender]
	if d.OldestQueuedAt != nil || d.NewestFailedAt == nil || d.NewestSentAt != nil {
		t.Fatalf("dead sender summary: %+v", d)
	}
}

// TestProgramSearchMatchesWildcardsAsText checks the program search matches
// an underscore, a percent sign and a backslash as written
func TestProgramSearchMatchesWildcardsAsText(t *testing.T) {
	db := newProgramTestDB(t)
	ctx := context.Background()
	r := &Program{DB: db}
	now := time.Now().UTC()
	names := []string{"Q1_2026", "Q1 2026", "Q1X2026", "100% done", "1000 done", `a\b`, "ab"}
	byName := map[string]bool{}
	for _, n := range names {
		statusProgram(t, db, n, nil, nil, nil, nil)
	}
	for search, want := range map[string][]string{
		"Q1_2026": {"Q1_2026"},
		"100%":    {"100% done"},
		`a\b`:     {`a\b`},
		"Q1":      {"Q1_2026", "Q1 2026", "Q1X2026"},
	} {
		list, err := r.GetAll(ctx, nil, &ProgramOption{QueryArgs: &vo.QueryArgs{Search: search}}, now)
		if err != nil {
			t.Fatalf("%s: %v", search, err)
		}
		clear(byName)
		for _, p := range list.Rows {
			byName[p.Name.MustGet().String()] = true
		}
		if len(byName) != len(want) {
			t.Fatalf("%q: got %v, want %v", search, byName, want)
		}
		for _, n := range want {
			if !byName[n] {
				t.Fatalf("%q: got %v, want %v", search, byName, want)
			}
		}
	}
}
