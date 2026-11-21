package service

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// TestProgramAttentionReasonCompleted checks a started program whose people
// all completed needs no attention, while one with nobody left in it does
func TestProgramAttentionReasonCompleted(t *testing.T) {
	now := time.Now()
	started := &model.Program{StartedAt: &now}
	completed := &repository.ProgramListSummary{Enrolled: 0, People: 5}
	if got := programAttentionReason(started, completed, nil, now); got != "" {
		t.Fatalf("a completed program must not need attention, got %q", got)
	}
	empty := &repository.ProgramListSummary{}
	if got := programAttentionReason(started, empty, nil, now); got == "" {
		t.Fatal("a started program with nobody in it must need attention")
	}
	if got := programAttentionReason(&model.Program{}, empty, nil, now); got != "" {
		t.Fatalf("a program that has not started must not need attention, got %q", got)
	}
}

// TestProgramStalledReason checks a running program needs attention when a
// next send is more than a day overdue, a queued send waits more than a day
// past its send time, or an active person has no next send and nothing is
// queued, and a paused one or one that waits for a due time ahead does not
func TestProgramStalledReason(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	startedAt := now.Add(-30 * day)
	program := &model.Program{StartedAt: &startedAt}
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	sum := &repository.ProgramListSummary{Enrolled: 3, People: 3, NextDueAt: at(-3 * day)}
	want := "Sends are 3 days overdue. Check the sender and sending hours."
	if got := programAttentionReason(program, sum, nil, now); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	queued := &repository.ProgramListSummary{Enrolled: 3, People: 3, NextDueAt: at(day), Queued: 1, OldestQueuedAt: at(-2 * day)}
	if got := programStalledReason(program, queued, now); got != "Sends are 2 days overdue. Check the sender and sending hours." {
		t.Fatalf("a queued send two days late is stalled, got %q", got)
	}
	unplanned := &repository.ProgramListSummary{Enrolled: 3, People: 3, Unplanned: 1}
	if got := programStalledReason(program, unplanned, now); got == "" {
		t.Fatal("an active person without a next send and nothing queued is stalled")
	}
	for name, ok := range map[string]*repository.ProgramListSummary{
		"due after the weekend":         {Enrolled: 3, People: 3, NextDueAt: at(2 * day)},
		"due after the join delay":      {Enrolled: 3, People: 3, NextDueAt: at(20 * day)},
		"due after a skip range":        {Enrolled: 3, People: 3, NextDueAt: at(14 * day)},
		"due inside the grace":          {Enrolled: 3, People: 3, NextDueAt: at(-day + time.Hour)},
		"no due time but a queued send": {Enrolled: 3, People: 3, Unplanned: 1, Queued: 1, OldestQueuedAt: at(time.Hour)},
	} {
		if got := programStalledReason(program, ok, now); got != "" {
			t.Fatalf("%s is not stalled, got %q", name, got)
		}
	}
	program.PausedAt.Set(now)
	if got := programStalledReason(program, sum, now); got != "" {
		t.Fatalf("a paused program is not stalled, got %q", got)
	}
}

// TestProgramEmptyGroupsAttention checks a running program with nobody in it
// needs attention once marked started or past its start by more than the
// start grace, and one that just started does not yet
func TestProgramEmptyGroupsAttention(t *testing.T) {
	now := time.Now()
	empty := &repository.ProgramListSummary{}
	want := "The groups are empty, so nobody is enrolled."
	notMarked := &model.Program{}
	notMarked.StartAt.Set(now.Add(-2 * time.Hour))
	if got := programAttentionReason(notMarked, empty, nil, now); got != want {
		t.Fatalf("a program past its start with nobody in it, got %q", got)
	}
	justStarted := &model.Program{}
	justStarted.StartAt.Set(now.Add(-10 * time.Minute))
	if got := programAttentionReason(justStarted, empty, nil, now); got != "" {
		t.Fatalf("a program that just started is not empty yet, got %q", got)
	}
	everyoneLeft := &repository.ProgramListSummary{Total: 4}
	started := &model.Program{StartedAt: &now}
	if got := programAttentionReason(started, everyoneLeft, nil, now); got != want {
		t.Fatalf("a program everyone left, got %q", got)
	}
}

// TestProgramWeekendIsNotStalled checks a program sending on weekdays with a
// day between templates, whose last send went out on a Friday, draws its
// next due time on Monday and is not stalled on Saturday or Sunday
func TestProgramWeekendIsNotStalled(t *testing.T) {
	program := &model.Program{}
	program.ConstraintWeekDays.Set(*vo.NewCampaignWeekDaysMust(62))
	program.ConstraintStartTime.Set(*vo.NewCampaignTimeConstraintMust("09:00"))
	program.ConstraintEndTime.Set(*vo.NewCampaignTimeConstraintMust("17:00"))
	program.StepIntervalMinutes.Set(24 * 60)
	program.SpreadMinutes.Set(0)
	// Friday 2026-11-13 at 16:00 UTC
	friday := time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC)
	started := friday.AddDate(0, 0, -30)
	program.StartedAt = &started
	next := programNextRoundStart(program, friday, friday)
	due := programPickSendTimeAfter(program, next, friday, programMinNextSend(program, &friday), rand.New(rand.NewSource(1)))
	if due.Weekday() != time.Monday {
		t.Fatalf("the next due time must fall on Monday, got %v", due)
	}
	for _, now := range []time.Time{friday.Add(32 * time.Hour), friday.Add(56 * time.Hour), due.Add(-time.Minute)} {
		sum := &repository.ProgramListSummary{Enrolled: 1, People: 1, NextDueAt: &due}
		if got := programAttentionReason(program, sum, nil, now); got != "" {
			t.Fatalf("%v: a weekend is not a stall, got %q", now, got)
		}
	}
}

// TestCancelPeopleSendsOnlyExited checks the chunked cancel of leavers skips a
// person who came back before their chunk was written
func TestCancelPeopleSendsOnlyExited(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	gone, back := p.recipient("gone@example.com"), p.recipient("back@example.com")
	eg := p.enroll(programID, gone, 0)
	// back is still active, as after a membership pass that found them again
	p.enroll(programID, back, 0)
	goneSend, backSend := p.send(campaignID, gone, false), p.send(campaignID, back, false)
	p.setEnrollment(eg.ID, map[string]any{"status": model.ProgramEnrollmentExited})
	if _, err := p.svc.cancelPeopleSends(p.ctx, &programID, []*uuid.UUID{&gone, &back}, true, nil); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if row := p.campaignRecipient(goneSend); row.CancelledAt == nil {
		t.Fatal("the send of the person who left must be cancelled")
	}
	if row := p.campaignRecipient(backSend); row.CancelledAt != nil {
		t.Fatal("the send of the person who came back must be kept")
	}
}

// TestResolveListedProgramExitsAfterGrace checks the tick membership pass
// stores joiners, exits a leaver after the grace period and cancels their send
// after the lock is let go
func TestResolveListedProgramExitsAfterGrace(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	leaver, joiner := p.recipient("leaver@example.com"), p.recipient("joiner@example.com")
	e := p.enroll(programID, leaver, 0)
	p.setEnrollment(e.ID, map[string]any{"missing_since": time.Now().Add(-2 * programMissingGrace).UTC()})
	unsent := p.send(campaignID, leaver, false)
	program := p.loadProgram(programID)
	if err := p.svc.resolveListedProgramWith(p.ctx, program, []*uuid.UUID{&joiner}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentExited {
		t.Fatalf("expected the leaver exited, got %s", row.Status)
	}
	if row := p.campaignRecipient(unsent); row.CancelledAt == nil {
		t.Fatal("the unsent send of the leaver must be cancelled")
	}
	if got := p.enrollmentOf(programID, joiner); got == nil || got.Status != model.ProgramEnrollmentActive {
		t.Fatal("the joiner must be enrolled")
	}
	// a second pass with the same members changes nothing
	if err := p.svc.resolveListedProgramWith(p.ctx, program, []*uuid.UUID{&joiner}); err != nil {
		t.Fatalf("resolve again: %v", err)
	}
	var n int64
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ?", programID).Count(&n)
	if n != 2 {
		t.Fatalf("expected two enrollments, got %d", n)
	}
}

// TestAnonymousJoinerGetsPseudonym checks a person who joins an anonymous
// program gets a pseudonym at enrollment, and a person of a normal program
// does not
func TestAnonymousJoinerGetsPseudonym(t *testing.T) {
	p := newProgramServiceTest(t)
	anon := p.program(model.ProgramLoopComplete)
	p.setProgram(anon, map[string]any{"is_anonymous": true})
	normal := p.program(model.ProgramLoopComplete)
	alice := p.recipient("alice@example.com")
	now := time.Now()
	for _, id := range []uuid.UUID{anon, normal} {
		joiners := p.svc.planJoiners(p.loadProgram(id), []*uuid.UUID{&alice}, nil, now)
		if len(joiners) != 1 {
			t.Fatalf("expected one joiner, got %d", len(joiners))
		}
		if (joiners[0].AnonymizedID != nil) != (id == anon) {
			t.Fatalf("pseudonym %v for anonymous %v", joiners[0].AnonymizedID, id == anon)
		}
	}
}

// TestProgramMixSettle checks the picks of a batch stay counted for the next
// batch of the tick only when stored and inside the window
func TestProgramMixSettle(t *testing.T) {
	now := time.Now()
	stepID := uuid.New()
	kept, failed, late := uuid.New(), uuid.New(), uuid.New()
	rid := uuid.New()
	mix := &programMix{
		values: map[uuid.UUID]string{rid: "sales"},
		counts: repository.ProgramMixCounts{},
		from:   now.Add(-time.Hour),
		to:     now.Add(time.Hour),
	}
	mix.record(&kept, &rid, stepID, now)
	mix.record(&failed, &rid, stepID, now)
	mix.record(&late, &rid, stepID, now.Add(48*time.Hour))
	if got := mix.counts["sales"][stepID]; got != 3 {
		t.Fatalf("the batch must see all its picks, got %d", got)
	}
	mix.settle(map[uuid.UUID]bool{failed: true})
	if got := mix.counts["sales"][stepID]; got != 1 {
		t.Fatalf("only the stored pick inside the window stays, got %d", got)
	}
	var none *programMix
	none.record(&kept, &rid, stepID, now)
	none.settle(nil)
}
