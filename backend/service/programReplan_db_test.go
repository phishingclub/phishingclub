package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// sendAt adds a campaign recipient row planned for the given time, sent or not
func (p *programTest) sendAt(campaignID, recipientID uuid.UUID, at time.Time, sent bool) uuid.UUID {
	p.t.Helper()
	id := p.send(campaignID, recipientID, sent)
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).
		// stored as RFC3339 text, the same as the campaign send path
		Update("send_at", utils.RFC3339UTC(at)).Error; err != nil {
		p.t.Fatalf("set send_at: %v", err)
	}
	return id
}

func (p *programTest) setProgram(id uuid.UUID, fields map[string]any) {
	p.t.Helper()
	if err := p.db.Model(&database.Program{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		p.t.Fatalf("update program: %v", err)
	}
}

func (p *programTest) setEnrollment(id *uuid.UUID, fields map[string]any) {
	p.t.Helper()
	if err := p.svc.ProgramRepository.UpdateEnrollmentFields(p.ctx, id, fields); err != nil {
		p.t.Fatalf("update enrollment: %v", err)
	}
}

func (p *programTest) campaignRecipient(id uuid.UUID) database.CampaignRecipient {
	p.t.Helper()
	var row database.CampaignRecipient
	if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
		p.t.Fatalf("load campaign recipient: %v", err)
	}
	return row
}

// TestProgramBackoff checks the wait doubles from one hour and stops at a day
func TestProgramBackoff(t *testing.T) {
	cases := map[int]time.Duration{
		1: time.Hour, 2: 2 * time.Hour, 3: 4 * time.Hour, 5: 16 * time.Hour,
		6: 24 * time.Hour, 50: 24 * time.Hour,
	}
	for failures, want := range cases {
		if got := programBackoff(failures); got != want {
			t.Errorf("failures %d: got %v want %v", failures, got, want)
		}
	}
}

// TestAdvanceDueBacksOffFailures checks a person whose advance fails is not
// due again on the next tick, waits longer after each failure and is reset
// once an advance works
func TestAdvanceDueBacksOffFailures(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	p.stepCampaign(programID, stepID, 0)
	rid := p.recipient("alice@example.com")
	e := p.enroll(programID, rid, 0)

	// the delivery lookup fails while its table is gone
	if err := p.db.Migrator().RenameTable("program_campaigns", "program_campaigns_off"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	now := time.Now()
	if err := p.svc.advanceDue(p.ctx, nil, now); err != nil {
		t.Fatalf("advance due: %v", err)
	}
	row := p.enrollment(e.ID)
	if row.AdvanceFailures != 1 || row.NextDueAt == nil || row.NextDueAt.Before(now.Add(59*time.Minute)) {
		t.Fatalf("expected one failure and a wait of an hour, got %d due %v", row.AdvanceFailures, row.NextDueAt)
	}
	// not due on the next tick
	due, err := p.svc.ProgramRepository.GetDueEnrollments(p.ctx, now.Add(10*time.Second), 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("a failed person must not be due on the next tick, got %d (%v)", len(due), err)
	}
	later := now.Add(61 * time.Minute)
	if err := p.svc.advanceDue(p.ctx, nil, later); err != nil {
		t.Fatalf("advance due: %v", err)
	}
	row = p.enrollment(e.ID)
	if row.AdvanceFailures != 2 || row.NextDueAt.Before(later.Add(119*time.Minute)) {
		t.Fatalf("expected two failures and a wait of two hours, got %d due %v", row.AdvanceFailures, row.NextDueAt)
	}
	// works again: the count is reset
	if err := p.db.Migrator().RenameTable("program_campaigns_off", "program_campaigns"); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	if err := p.svc.advanceDue(p.ctx, nil, later.Add(3*time.Hour)); err != nil {
		t.Fatalf("advance due: %v", err)
	}
	row = p.enrollment(e.ID)
	if row.AdvanceFailures != 0 {
		t.Fatalf("expected the failures reset after a working advance, got %d", row.AdvanceFailures)
	}
}

// TestAdvanceEnrollmentSkipsSendTooSoonAfterLast checks that when the progress
// of a send was not stored, the next attempt does not add a second send right
// after it but plans the person again from that send
func TestAdvanceEnrollmentSkipsSendTooSoonAfterLast(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	step2 := p.step(programID, 1, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	campaign2 := p.stepCampaign(programID, step2, 0)
	rid := p.recipient("alice@example.com")
	// the first step was added a minute ago, the progress write failed so the
	// enrollment still has no last send and is due now
	e := p.enroll(programID, rid, 0)
	sentAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	p.sendAt(campaign1, rid, sentAt, false)

	p.advance(programID, e)
	if rows := p.sendsIn(campaign2); len(rows) != 0 {
		t.Fatalf("expected no second send right after the first, got %d", len(rows))
	}
	row := p.enrollment(e.ID)
	if row.LastSendAt == nil || !row.LastSendAt.Equal(sentAt) || row.LastStepID == nil || *row.LastStepID != step1 {
		t.Fatalf("expected the last send taken from the stored send, got %v %v", row.LastSendAt, row.LastStepID)
	}
	minNext := sentAt.Add(p.loadProgram(programID).IntervalOrDefault() / 2)
	if row.NextDueAt == nil || row.NextDueAt.Before(minNext) {
		t.Fatalf("expected the next send at least half an interval later, got %v", row.NextDueAt)
	}
	if row.Position != 1 {
		t.Fatalf("expected position 1, got %d", row.Position)
	}
}

// TestReplanReactivatesCompletedOnNewStep checks a completed person comes back
// when a step they have not had is added, and one who had every step does not
func TestReplanReactivatesCompletedOnNewStep(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	ea := p.enroll(programID, alice, 0)
	eb := p.enroll(programID, bob, 0)
	p.send(campaign1, alice, true)
	p.send(campaign1, bob, true)
	for _, e := range []*database.ProgramEnrollment{ea, eb} {
		p.setEnrollment(e.ID, map[string]any{
			"status": model.ProgramEnrollmentCompleted, "completed_at": time.Now(), "next_due_at": nil,
			"last_send_at": time.Now().Add(-time.Hour),
		})
	}
	before := p.loadProgram(programID)

	// a step edit with no new step changes nobody
	if err := p.svc.replanAfterEdit(p.ctx, before, &programID, true); err != nil {
		t.Fatalf("replan: %v", err)
	}
	if row := p.enrollment(ea.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("expected still completed, got %s", row.Status)
	}

	// a new step: alice and bob both have it left
	step2 := p.step(programID, 1, true)
	// bob already had the new step, for example from an earlier edit
	campaign2 := p.stepCampaign(programID, step2, 0)
	p.send(campaign2, bob, true)
	now := time.Now()
	if err := p.svc.replanAfterEdit(p.ctx, before, &programID, true); err != nil {
		t.Fatalf("replan: %v", err)
	}
	row := p.enrollment(ea.ID)
	if row.Status != model.ProgramEnrollmentActive || row.CompletedAt != nil || row.NextDueAt == nil {
		t.Fatalf("expected alice active with a send planned, got %s %v %v", row.Status, row.CompletedAt, row.NextDueAt)
	}
	minNext := row.LastSendAt.Add(before.IntervalOrDefault() / 2)
	if row.NextDueAt.Before(minNext) || row.NextDueAt.Before(now.Add(-time.Second)) {
		t.Fatalf("expected the next send after the minimum gap, got %v", row.NextDueAt)
	}
	if row := p.enrollment(eb.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("expected bob still completed, got %s", row.Status)
	}
}

// TestReplanReactivatesCompletedOnLoopMode checks switching to loop mode brings
// every completed person back, and a closed program changes nobody
func TestReplanReactivatesCompletedOnLoopMode(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)
	p.send(campaign1, alice, true)
	p.setEnrollment(e.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": time.Now(), "next_due_at": nil,
	})
	before := p.loadProgram(programID)

	p.setProgram(programID, map[string]any{"loop_mode": model.ProgramLoopLoop, "closed_at": time.Now().UTC()})
	if err := p.svc.replanAfterEdit(p.ctx, before, &programID, false); err != nil {
		t.Fatalf("replan: %v", err)
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("a closed program must change nobody, got %s", row.Status)
	}

	p.setProgram(programID, map[string]any{"closed_at": nil})
	if err := p.svc.replanAfterEdit(p.ctx, before, &programID, false); err != nil {
		t.Fatalf("replan: %v", err)
	}
	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("expected active again in loop mode, got %s %v", row.Status, row.NextDueAt)
	}
}

// TestReplanRedrawsOnIntervalChange checks a shorter interval moves the stored
// next send of an active person closer, never under half the new interval
// after the last send, and leaves a person who is due now alone
func TestReplanRedrawsOnIntervalChange(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	p.step(programID, 1, true)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	ea := p.enroll(programID, alice, 0)
	eb := p.enroll(programID, bob, 0)
	before := p.loadProgram(programID)
	now := time.Now()
	lastSend := now.Add(-time.Hour)
	round := lastSend.Add(before.IntervalOrDefault())
	p.setEnrollment(ea.ID, map[string]any{
		"last_send_at": lastSend, "round_start_at": round, "next_due_at": round, "position": 1,
	})
	bobDue := now.Add(-time.Minute).UTC().Truncate(time.Second)
	p.setEnrollment(eb.ID, map[string]any{"next_due_at": bobDue})

	p.setProgram(programID, map[string]any{"step_interval_minutes": 2 * 24 * 60})
	if err := p.svc.replanAfterEdit(p.ctx, before, &programID, false); err != nil {
		t.Fatalf("replan: %v", err)
	}
	row := p.enrollment(ea.ID)
	want := lastSend.Add(2 * 24 * time.Hour)
	if row.NextDueAt == nil || row.NextDueAt.Sub(want).Abs() > time.Second {
		t.Fatalf("expected the next send two days after the last, got %v want %v", row.NextDueAt, want)
	}
	if row := p.enrollment(eb.ID); row.NextDueAt == nil || !row.NextDueAt.Equal(bobDue) {
		t.Fatalf("a person due now must be left to the tick, got %v", row.NextDueAt)
	}
}

// TestRespreadHeldSends checks a held send whose time passed is moved to now or
// later, a training send too, and a send still ahead stays
func TestRespreadHeldSends(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	now := time.Now()
	past := p.sendAt(campaign1, alice, now.Add(-48*time.Hour), false)
	ahead := now.Add(48 * time.Hour).UTC().Truncate(time.Second)
	future := p.sendAt(campaign1, bob, ahead, false)

	if err := p.svc.respreadHeldSends(p.ctx, p.loadProgram(programID), now); err != nil {
		t.Fatalf("respread: %v", err)
	}
	if row := p.campaignRecipient(past); row.SendAt == nil || row.SendAt.Before(now.Add(-time.Second)) {
		t.Fatalf("expected the held send moved to now or later, got %v", row.SendAt)
	}
	if row := p.campaignRecipient(future); row.SendAt == nil || !row.SendAt.Equal(ahead) {
		t.Fatalf("expected the send ahead unchanged, got %v want %v", row.SendAt, ahead)
	}
}

// TestRespreadHeldSendsKeepsBlackout checks a held send is not let out on a
// blackout day
func TestRespreadHeldSendsKeepsBlackout(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	now := time.Now().UTC()
	today := now.Format(model.ProgramDateLayout)
	tomorrow := now.Add(24 * time.Hour).Format(model.ProgramDateLayout)
	blackout := `[{"from":"` + today + `","to":"` + tomorrow + `"}]`
	p.setProgram(programID, map[string]any{"blackout_dates": blackout, "time_zone": "UTC"})
	step1 := p.step(programID, 0, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	alice := p.recipient("alice@example.com")
	held := p.sendAt(campaign1, alice, now.Add(-time.Hour), false)

	if err := p.svc.respreadHeldSends(p.ctx, p.loadProgram(programID), now); err != nil {
		t.Fatalf("respread: %v", err)
	}
	open := time.Date(now.Year(), now.Month(), now.Day()+2, 0, 0, 0, 0, time.UTC)
	if row := p.campaignRecipient(held); row.SendAt == nil || row.SendAt.Before(open) {
		t.Fatalf("expected the held send after the blackout, got %v", row.SendAt)
	}
}

// TestResumeMembershipCancelsHeldSendsOfLeavers checks that people gone from
// the groups when a program resumes get none of the held sends, while members
// keep theirs
func TestResumeMembershipCancelsHeldSendsOfLeavers(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.RecipientGroup{}, &database.RecipientGroupRecipient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.svc.RecipientGroupRepository = &repository.RecipientGroup{DB: p.db}
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	now := time.Now().UTC()
	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group"})
	p.create(&database.RecipientGroupRecipient{RecipientID: &alice, RecipientGroupID: &groupID})
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	ea := p.enroll(programID, alice, 0)
	eb := p.enroll(programID, bob, 0)
	aliceSend := p.sendAt(campaign1, alice, now.Add(-time.Hour), false)
	bobSend := p.sendAt(campaign1, bob, now.Add(-time.Hour), false)

	p.svc.resolveMembershipOnResume(p.ctx, &programID, now)
	if row := p.campaignRecipient(bobSend); row.CancelledAt == nil {
		t.Fatalf("expected the held send of the leaver cancelled")
	}
	if row := p.campaignRecipient(aliceSend); row.CancelledAt != nil {
		t.Fatalf("expected the held send of the member kept")
	}
	if row := p.enrollment(eb.ID); row.MissingSince == nil {
		t.Fatalf("expected the leaver marked missing")
	}
	if row := p.enrollment(ea.ID); row.MissingSince != nil || row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("expected the member unchanged")
	}
}

// TestUpdateLocksAnonymousOnceEnrolled checks anonymity can not be flipped when
// people are enrolled, also before the start mark is stored
func TestUpdateLocksAnonymousOnceEnrolled(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"started_at": nil})
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	incoming := &model.Program{}
	incoming.Name.Set(*vo.NewString64Must("renamed"))
	incoming.IsAnonymous.Set(true)
	err := p.svc.UpdateByID(p.ctx, authorizedSession(t), &programID, incoming)
	if err == nil {
		t.Fatalf("expected anonymity locked once people are enrolled")
	}
	if p.loadProgram(programID).IsAnonymousOn() {
		t.Fatalf("anonymity must not change")
	}
}

// TestProgramResultStatsComplement checks an anonymous funnel hides a count that
// leaves fewer than programMinGroup people out, keeps zero and keeps a count
// with enough people on both sides
func TestProgramResultStatsComplement(t *testing.T) {
	raw := &model.CampaignResultView{
		Recipients: 20, EmailsSent: 20, TrackingPixelLoaded: 18, WebsiteLoaded: 15,
		SubmittedData: 0, Reported: 20, TrainingStarted: 10, TrainingCompleted: 15,
	}
	anon := programResultStats(raw, true)
	if !anon.Suppressed || anon.TrackingPixelLoaded != nil || anon.Reported != nil {
		t.Fatalf("counts that leave under %d out must be null: %+v", programMinGroup, anon)
	}
	if anon.WebsiteLoaded == nil || *anon.WebsiteLoaded != 15 || anon.TrainingStarted == nil ||
		anon.TrainingCompleted == nil || *anon.TrainingCompleted != 15 {
		t.Fatalf("counts with enough people on both sides stay: %+v", anon)
	}
	if anon.SubmittedData == nil || *anon.SubmittedData != 0 {
		t.Fatalf("zero stays: %+v", anon)
	}
	named := programResultStats(raw, false)
	if named.Suppressed || named.Reported == nil || *named.Reported != 20 {
		t.Fatalf("a named program keeps every count: %+v", named)
	}
}
