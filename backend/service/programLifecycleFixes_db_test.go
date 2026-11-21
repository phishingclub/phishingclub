package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// lockFreeNow reports whether the lifecycle lock can be taken right away
func lockFreeNow(s *Program) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return false
	}
	s.lifecycleMu.Unlock()
	return true
}

// watchWrites runs fn before every update of the given tables until the test
// ends
func (p *programTest) watchWrites(name string, tables map[string]bool, fn func(table string)) {
	p.t.Helper()
	if err := p.db.Callback().Update().Before("gorm:begin_transaction").Register(name, func(db *gorm.DB) {
		if tables[db.Statement.Table] {
			fn(db.Statement.Table)
		}
	}); err != nil {
		p.t.Fatalf("register hook: %v", err)
	}
	p.t.Cleanup(func() { _ = p.db.Callback().Update().Remove(name) })
}

// TestProgramCloseCancelsWithoutLock checks the sends of a close are
// cancelled and its people stopped after the lifecycle lock is let go, while
// the hidden campaigns are already paused, so nothing queued is sent meanwhile
func TestProgramCloseCancelsWithoutLock(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 3)
	bob := p.recipient("bob@example.com")
	e := p.enroll(programID, bob, 0)

	var mu sync.Mutex
	seen := map[string]bool{}
	lockHeld := []string{}
	unpaused := false
	p.watchWrites("test:close_writes", map[string]bool{
		database.CAMPAIGN_RECIPIENT_TABLE_NAME: true,
		database.PROGRAM_ENROLLMENT_TABLE:      true,
	}, func(table string) {
		mu.Lock()
		defer mu.Unlock()
		seen[table] = true
		if !lockFreeNow(p.svc) {
			lockHeld = append(lockHeld, table)
		}
		// a campaign is closed and its pause cleared at the end of the close
		for _, id := range campaigns {
			if row := p.campaignRow(id); row.ClosedAt == nil && row.PausedAt == nil {
				unpaused = true
			}
		}
	})
	if err := p.svc.closeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !seen[database.CAMPAIGN_RECIPIENT_TABLE_NAME] || !seen[database.PROGRAM_ENROLLMENT_TABLE] {
		t.Fatalf("the close must cancel sends and stop people: %v", seen)
	}
	if len(lockHeld) > 0 {
		t.Fatalf("the close held the lifecycle lock while writing %v", lockHeld)
	}
	if unpaused {
		t.Fatalf("the hidden campaigns must be paused before the sends are cancelled")
	}
	for _, id := range sends {
		if p.campaignRecipient(id).CancelledAt == nil {
			t.Fatalf("every send not yet sent must be cancelled")
		}
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentStopped || row.NextDueAt != nil {
		t.Fatalf("expected the person stopped: %s %v", row.Status, row.NextDueAt)
	}
	program := p.loadProgram(programID)
	if !program.IsClosed() || program.IsPaused() || program.ClosingAt != nil {
		t.Fatalf("expected the program closed, got %s", program.Status)
	}
	for _, id := range campaigns {
		row := p.campaignRow(id)
		if row.ClosedAt == nil || row.PausedAt != nil {
			t.Fatalf("every hidden campaign must be closed and not paused")
		}
	}
}

// TestProgramCloseResumesAfterCancelFailure checks a close that fails while
// it cancels the sends is finished by the next tick
func TestProgramCloseResumesAfterCancelFailure(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 2)
	if err := p.db.Exec(`CREATE TRIGGER fail_cancel BEFORE UPDATE OF cancelled_at ON campaign_recipients
		BEGIN SELECT RAISE(ABORT, 'cancel refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	// the request marks the close, the background close then fails
	if err := p.svc.closeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	program := p.loadProgram(programID)
	if program.IsClosed() || program.ClosingAt == nil || !program.IsPaused() {
		t.Fatalf("expected a paused program marked as closing, got %s", program.Status)
	}
	for _, id := range campaigns {
		if p.campaignRow(id).PausedAt == nil {
			t.Fatalf("the hidden campaigns must stay paused while the close is not done")
		}
	}
	if err := p.db.Exec(`DROP TRIGGER fail_cancel`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	// the failed close waits programCloseRetryEvery, the wait is skipped here
	p.svc.closeRetryAt[programID] = time.Now().Add(-time.Second)
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	if !p.loadProgram(programID).IsClosed() {
		t.Fatalf("the tick must finish the close")
	}
	for _, id := range sends {
		if p.campaignRecipient(id).CancelledAt == nil {
			t.Fatalf("every send not yet sent must be cancelled")
		}
	}
}

// TestEditRemoveTemplateCancelsAfterRequestEnds checks the queued sends of a
// removed template are cancelled also when the request ends right after the
// new step list is stored
func TestEditRemoveTemplateCancelsAfterRequestEnds(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignB := p.stepCampaign(programID, stepB, 0)
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	queued := p.sendAt(campaignB, alice, time.Now().Add(time.Hour), false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// the first read of the sends comes after the new step list is stored
	endRequest := func(db *gorm.DB) {
		if db.Statement.Table == database.CAMPAIGN_RECIPIENT_TABLE_NAME {
			cancel()
		}
	}
	if err := p.db.Callback().Query().After("gorm:query").Register("test:end_request", endRequest); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	if err := p.db.Callback().Row().After("gorm:row").Register("test:end_request", endRequest); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	t.Cleanup(func() {
		_ = p.db.Callback().Query().Remove("test:end_request")
		_ = p.db.Callback().Row().Remove("test:end_request")
	})
	update := &model.Program{Name: p.loadProgram(programID).Name, Steps: []*model.ProgramStep{p.keepStep(stepA, 0)}}
	if err := p.svc.UpdateByID(ctx, authorizedSession(t), &programID, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatalf("the request must have ended during the edit")
	}
	if p.campaignRecipient(queued).CancelledAt == nil {
		t.Fatalf("the queued send of the removed template must be cancelled")
	}
}

// TestTickCancelsSendsOfRemovedSteps checks the tick cancels the queued sends
// of a step that is no longer on its program and plans the person again,
// while a send of a step still on the program stays
func TestTickCancelsSendsOfRemovedSteps(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	alice := p.recipient("alice@example.com")
	ea := p.enroll(programID, alice, 0)
	later := time.Now().UTC().Add(48 * time.Hour)
	p.setEnrollment(ea.ID, map[string]any{"next_due_at": later})
	bob := p.recipient("bob@example.com")
	p.enroll(programID, bob, 0)
	removedSend := p.sendAt(campaignB, alice, time.Now().Add(time.Hour), false)
	keptSend := p.sendAt(campaignA, bob, time.Now().Add(time.Hour), false)
	// an edit stored the new list and stopped before it cancelled the sends
	if err := p.db.Where("id = ?", stepB).Delete(&database.ProgramStep{}).Error; err != nil {
		t.Fatalf("remove step: %v", err)
	}

	p.svc.lastMembershipAt = time.Time{}
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	if p.campaignRecipient(removedSend).CancelledAt == nil {
		t.Fatalf("the send of the removed step must be cancelled")
	}
	if p.campaignRecipient(keptSend).CancelledAt != nil {
		t.Fatalf("the send of a step on the program must stay")
	}
	if row := p.enrollment(ea.ID); row.NextDueAt == nil || !row.NextDueAt.Before(later) {
		t.Fatalf("alice must be due again about when the cancelled send was planned, got %v", row.NextDueAt)
	}
	// nothing is left for the next pass
	left, err := p.svc.ProgramRepository.GetUnsentSendsOfRemovedSteps(p.ctx, 10)
	if err != nil || len(left) != 0 {
		t.Fatalf("expected no sends of removed steps left: %d %v", len(left), err)
	}
}

// failCampaignPauseWrite makes the database refuse to set or clear the pause
// mark of a campaign. The returned function removes the refusal.
func failCampaignPauseWrite(t *testing.T, db *gorm.DB, set bool) func() {
	t.Helper()
	trigger := `CREATE TRIGGER fail_pause_write BEFORE UPDATE OF paused_at ON campaigns
		WHEN NEW.paused_at IS NULL BEGIN SELECT RAISE(ABORT, 'resume refused'); END`
	if set {
		trigger = `CREATE TRIGGER fail_pause_write BEFORE UPDATE OF paused_at ON campaigns
		WHEN NEW.paused_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'pause refused'); END`
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return func() {
		if err := db.Exec(`DROP TRIGGER fail_pause_write`).Error; err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}
}

// TestProgramPauseResumeAllOrNothing checks a pause or resume that fails on
// the hidden campaigns changes nothing, so a retry works
func TestProgramPauseResumeAllOrNothing(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, _ := p.manyCampaigns(programID, 2)

	restore := failCampaignPauseWrite(t, p.db, true)
	if err := p.svc.Pause(p.ctx, session, &programID); err == nil {
		t.Fatalf("expected the pause to fail")
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatalf("a failed pause must not pause the program")
	}
	restore()
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("a retried pause must work: %v", err)
	}
	for _, id := range campaigns {
		if p.campaignRow(id).PausedAt == nil {
			t.Fatalf("every hidden campaign must be paused")
		}
	}

	restore = failCampaignPauseWrite(t, p.db, false)
	if err := p.svc.resumeWait(p.ctx, session, &programID); err == nil {
		t.Fatalf("expected the resume to fail")
	}
	if !p.loadProgram(programID).IsPaused() {
		t.Fatalf("a failed resume must leave the program paused")
	}
	for _, id := range campaigns {
		if p.campaignRow(id).PausedAt == nil {
			t.Fatalf("a failed resume must leave every hidden campaign paused")
		}
	}
	restore()
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("a retried resume must work: %v", err)
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatalf("expected the program running")
	}
	for _, id := range campaigns {
		if p.campaignRow(id).PausedAt != nil {
			t.Fatalf("every hidden campaign must send again")
		}
	}
}

// TestTickMatchesCampaignPauseToProgram checks the tick pauses the hidden
// campaigns of a paused program and resumes those of a running one, and
// leaves other campaigns alone
func TestTickMatchesCampaignPauseToProgram(t *testing.T) {
	p := newProgramLifecycleTest(t)
	paused := p.program(model.ProgramLoopComplete)
	running := p.program(model.ProgramLoopComplete)
	pausedCampaigns, _ := p.manyCampaigns(paused, 2)
	runningCampaigns, _ := p.manyCampaigns(running, 2)
	now := time.Now().UTC()
	p.setProgram(paused, map[string]any{"paused_at": now})
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", runningCampaigns[0]).Update("paused_at", now).Error; err != nil {
		t.Fatalf("pause campaign: %v", err)
	}
	// a closed hidden campaign of the paused program is not paused
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", pausedCampaigns[1]).Update("closed_at", now).Error; err != nil {
		t.Fatalf("close campaign: %v", err)
	}
	// a campaign of no program keeps its pause
	other := uuid.New()
	p.create(&database.Campaign{ID: &other, CreatedAt: &now, UpdatedAt: &now, Name: "other", PausedAt: &now})

	p.svc.lastMembershipAt = time.Time{}
	p.svc.lastRetentionAt = now
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	if p.campaignRow(pausedCampaigns[0]).PausedAt == nil {
		t.Fatalf("the open campaign of a paused program must be paused")
	}
	if p.campaignRow(pausedCampaigns[1]).PausedAt != nil {
		t.Fatalf("a closed campaign must not be paused")
	}
	if p.campaignRow(runningCampaigns[0]).PausedAt != nil {
		t.Fatalf("the campaign of a running program must not be paused")
	}
	if p.campaignRow(other).PausedAt == nil {
		t.Fatalf("a campaign of no program keeps its pause")
	}
}

// TestAdvanceSkipsProgramPastEnd checks a program past its end or being
// closed is never advanced, also before its close has finished
func TestAdvanceSkipsProgramPastEnd(t *testing.T) {
	p := newProgramLifecycleTest(t)
	now := time.Now().UTC()
	ended := p.program(model.ProgramLoopComplete)
	p.step(ended, 0, true)
	p.setProgram(ended, map[string]any{"end_at": now.Add(-time.Minute)})
	closing := p.program(model.ProgramLoopComplete)
	p.step(closing, 0, true)
	p.setProgram(closing, map[string]any{"closing_at": now})
	people := []*database.ProgramEnrollment{
		p.enroll(ended, p.recipient("alice@example.com"), 0),
		p.enroll(closing, p.recipient("bob@example.com"), 0),
	}
	if _, err := p.svc.advanceDueBatch(p.ctx, authorizedSession(t), time.Now()); err != nil {
		t.Fatalf("advance: %v", err)
	}
	var sends int64
	p.db.Model(&database.CampaignRecipient{}).Count(&sends)
	if sends != 0 {
		t.Fatalf("nothing may be sent for a program past its end or being closed, got %d", sends)
	}
	for _, e := range people {
		if row := p.enrollment(e.ID); row.Position != 0 || row.Status != model.ProgramEnrollmentActive {
			t.Fatalf("the person must not advance: %d %s", row.Position, row.Status)
		}
	}
}

// TestProgramPauseOnlyRunning checks a draft or scheduled program can not be
// paused, a running one can
func TestProgramPauseOnlyRunning(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	draft := p.program(model.ProgramLoopComplete)
	p.setProgram(draft, map[string]any{"start_at": nil, "started_at": nil})
	scheduled := p.program(model.ProgramLoopComplete)
	p.setProgram(scheduled, map[string]any{"start_at": time.Now().UTC().Add(time.Hour), "started_at": nil})
	for name, id := range map[string]uuid.UUID{"draft": draft, "scheduled": scheduled} {
		if err := p.svc.Pause(p.ctx, session, &id); !isValidationError(err) {
			t.Fatalf("a %s program must not be paused, got %v", name, err)
		}
		if p.loadProgram(id).IsPaused() {
			t.Fatalf("a refused pause of a %s program must change nothing", name)
		}
	}
	running := p.program(model.ProgramLoopComplete)
	if err := p.svc.Pause(p.ctx, session, &running); err != nil {
		t.Fatalf("a running program must be paused: %v", err)
	}
}

// TestProgramStartAfterPassedStart checks a scheduled program whose start
// passed before the scheduler picked it up starts now
func TestProgramStartAfterPassedStart(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	groupID := p.group("group")
	alice := p.recipient("alice@example.com")
	p.addToGroup(groupID, alice)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	p.assignGroup(programID, groupID)
	start := now.Add(-time.Minute)
	p.setProgram(programID, map[string]any{"start_at": start, "started_at": nil})
	if err := p.svc.startWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("a passed start must start now: %v", err)
	}
	program := p.loadProgram(programID)
	if program.StartedAt == nil {
		t.Fatalf("the program must be marked started")
	}
	p.enrollmentOf(programID, alice)
	if err := p.svc.startWait(p.ctx, session, &programID); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("a started program can not start again, got %v", err)
	}
}

// TestProgramCloneRefusedWhileDeleting checks a program that is being
// deleted can not be copied
func TestProgramCloneRefusedWhileDeleting(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"deleting_at": time.Now().UTC(), "paused_at": time.Now().UTC()})
	name := mustString64(t, "copy")
	if _, err := p.svc.Clone(p.ctx, authorizedSession(t), &programID, *name, nil); !isValidationError(err) {
		t.Fatalf("a copy of a program being deleted must be refused, got %v", err)
	}
}

// TestCountAudienceRefusesNullGroup checks a null group entry is a
// validation error, not a crash
func TestCountAudienceRefusesNullGroup(t *testing.T) {
	p := newProgramLifecycleTest(t)
	if _, err := p.svc.CountAudience(p.ctx, authorizedSession(t), nil, []*uuid.UUID{nil}); !isValidationError(err) {
		t.Fatalf("expected a validation error, got %v", err)
	}
}

// TestProgramNextSendHiddenWhenAnonymous checks the next send of an
// anonymous program is never given out, while a normal program shows it
func TestProgramNextSendHiddenWhenAnonymous(t *testing.T) {
	p := newProgramLifecycleTest(t)
	next := time.Now().UTC().Add(5 * time.Hour)
	normal := p.program(model.ProgramLoopComplete)
	anonymous := p.program(model.ProgramLoopComplete)
	p.setProgram(anonymous, map[string]any{"is_anonymous": true})
	for _, id := range []uuid.UUID{normal, anonymous} {
		p.step(id, 0, true)
		e := p.enroll(id, p.recipient(uuid.NewString()+"@example.com"), 0)
		p.setEnrollment(e.ID, map[string]any{"next_due_at": next})
	}
	programs := []*model.Program{p.loadProgram(normal), p.loadProgram(anonymous)}
	if err := p.svc.applyListSummaries(p.ctx, programs); err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if programs[0].NextSendAt == nil {
		t.Fatalf("a normal program shows its next send")
	}
	if programs[1].NextSendAt != nil {
		t.Fatalf("an anonymous program must not show its next send")
	}
	if programs[1].Enrolled != 1 {
		t.Fatalf("the other summaries stay, got %d enrolled", programs[1].Enrolled)
	}
}

// TestReturningCompletedGetsCancelledStep checks a completed person who left
// and whose last send was cancelled is made active again when they come
// back, while one whose send still waits stays completed
func TestReturningCompletedGetsCancelledStep(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	now := time.Now().UTC()
	completed := func(email string, missing time.Time, cancelled bool) *database.ProgramEnrollment {
		rid := p.recipient(email)
		e := p.enroll(programID, rid, 0)
		p.sendAt(campaignA, rid, now.Add(-72*time.Hour), true)
		last := p.sendAt(campaignB, rid, now.Add(time.Hour), false)
		if cancelled {
			if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", last).Update("cancelled_at", now).Error; err != nil {
				t.Fatalf("cancel: %v", err)
			}
		}
		p.setEnrollment(e.ID, map[string]any{
			"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
			"position": 2, "last_step_id": stepB, "missing_since": missing,
		})
		row := p.enrollment(e.ID)
		return &row
	}
	alice := completed("alice@example.com", now.Add(-2*programMissingGrace), true)
	bob := completed("bob@example.com", now.Add(-10*time.Minute), false)

	p.passMembers(p.loadProgram(programID),
		[]*uuid.UUID{alice.RecipientID, bob.RecipientID},
		[]*database.ProgramEnrollment{alice, bob}, now)

	row := p.enrollment(alice.ID)
	if row.Status != model.ProgramEnrollmentActive || row.CompletedAt != nil || row.MissingSince != nil || row.NextDueAt == nil {
		t.Fatalf("alice must be active again with a next send: %s %v %v %v", row.Status, row.CompletedAt, row.MissingSince, row.NextDueAt)
	}
	row = p.enrollment(bob.ID)
	if row.Status != model.ProgramEnrollmentCompleted || row.MissingSince != nil {
		t.Fatalf("bob still has his send, he stays completed and is no longer missing: %s %v", row.Status, row.MissingSince)
	}
}

// TestRemoveDomainKeepsProgramCampaignsOpen checks removing a domain closes
// the normal campaigns of its templates but leaves the hidden campaigns of a
// program open, whose sends wait for the template to be fixed
func TestRemoveDomainKeepsProgramCampaignsOpen(t *testing.T) {
	f := newPoolFixture(t)
	domainID := uuid.New()
	if err := f.db.Create(&database.Domain{ID: domainID, CreatedAt: &f.now, UpdatedAt: &f.now, Name: "example.test"}).Error; err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	templateID := f.template(templateOpts{domainID: &domainID})
	normal := f.campaign([]uuid.UUID{templateID}, false, false)
	hidden := f.campaign([]uuid.UUID{templateID}, false, false)
	programID := uuid.New()
	if err := f.db.Model(&database.Campaign{}).Where("id = ?", hidden).Update("program_id", programID).Error; err != nil {
		t.Fatalf("link campaign: %v", err)
	}
	if err := f.svc.CampaignTemplateService.RemoveDomainByDomainID(f.ctx, f.session, &domainID); err != nil {
		t.Fatalf("remove domain: %v", err)
	}
	read := func(id uuid.UUID) database.Campaign {
		var row database.Campaign
		if err := f.db.Where("id = ?", id).First(&row).Error; err != nil {
			t.Fatalf("load campaign: %v", err)
		}
		return row
	}
	if read(normal).CloseAt == nil {
		t.Fatalf("the normal campaign must be set to close")
	}
	if row := read(hidden); row.CloseAt != nil || row.ClosedAt != nil {
		t.Fatalf("the hidden campaign of a program must stay open")
	}
}

// TestScheduleWeekDaysWithLocalZone checks a campaign without a time zone
// sends on its week days in UTC also when the server zone is not UTC and the
// send window arrives in the server zone. Auckland is ahead of UTC, so its
// Tuesday morning is Monday evening in UTC.
func TestScheduleWeekDaysWithLocalZone(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skipf("no zone data: %v", err)
	}
	saved := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = saved })

	f := newPoolFixture(t)
	templateID := f.template(templateOpts{})
	groupID := f.group([]string{"a", "b", "c", "d", "e", "f"})
	campaignID := f.campaign([]uuid.UUID{templateID}, false, false)
	campaign := f.loadCampaign(campaignID, groupID)
	start := time.Date(2027, 3, 2, 10, 0, 0, 0, time.Local)
	campaign.SendStartAt = nullable.NewNullableWithValue(start)
	campaign.SendEndAt = nullable.NewNullableWithValue(start.AddDate(0, 0, 7))
	// Tuesday only, 09:00 to 17:00
	campaign.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(1 << 2))
	campaign.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00"))
	campaign.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00"))
	if err := f.svc.schedule(f.ctx, f.session, campaign); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	var rows []database.CampaignRecipient
	if err := f.db.Where("campaign_id = ?", campaignID).Find(&rows).Error; err != nil {
		t.Fatalf("read sends: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 sends, got %d", len(rows))
	}
	for _, row := range rows {
		at := row.SendAt.UTC()
		if at.Weekday() != time.Tuesday || at.Hour() < 9 || at.Hour() > 17 {
			t.Fatalf("a send must be on Tuesday 09:00 to 17:00 UTC, got %v", at)
		}
		if at.Before(start) {
			t.Fatalf("a send must not be before the start, got %v", at)
		}
	}
}

// TestProgramCloseClearsAnonymousSendTimes checks the close of an anonymous
// program removes the send times and round state of its enrollments, as they
// match the send times of the rows, while a named program keeps them
func TestProgramCloseClearsAnonymousSendTimes(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	at := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	fields := map[string]any{
		"last_send_at":   at,
		"last_step_id":   uuid.New(),
		"round_start_at": at,
		"next_due_at":    at.Add(72 * time.Hour),
		"completed_at":   at,
	}
	anonymous := p.program(model.ProgramLoopComplete)
	p.setProgram(anonymous, map[string]any{"is_anonymous": true})
	named := p.program(model.ProgramLoopComplete)
	// one person was sent to and has a pseudonym, one has a planned round only
	a := p.enroll(anonymous, p.recipient("anon@example.com"), 0)
	planned := p.enroll(anonymous, p.recipient("planned@example.com"), 0)
	n := p.enroll(named, p.recipient("named@example.com"), 0)
	p.setEnrollment(a.ID, fields)
	p.setEnrollment(a.ID, map[string]any{"anonymized_id": uuid.New()})
	p.setEnrollment(planned.ID, map[string]any{"round_start_at": at})
	p.setEnrollment(n.ID, fields)
	for _, id := range []uuid.UUID{anonymous, named} {
		if err := p.svc.closeWait(p.ctx, session, &id); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	row := p.enrollment(a.ID)
	if row.LastSendAt != nil || row.LastStepID != nil || row.RoundStartAt != nil ||
		row.NextDueAt != nil || row.CompletedAt != nil || row.AnonymizedID != nil {
		t.Fatalf("the anonymous enrollment keeps send state: %+v", row)
	}
	if row := p.enrollment(planned.ID); row.RoundStartAt != nil || row.NextDueAt != nil {
		t.Fatalf("the enrollment without a pseudonym keeps its round: %+v", row)
	}
	if kept := p.enrollment(n.ID); kept.LastSendAt == nil || kept.LastStepID == nil || kept.CompletedAt == nil {
		t.Fatalf("a named program keeps its send state: %+v", kept)
	}
}
