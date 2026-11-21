package service

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// lockFreeBetweenEnrollmentWrites reports, once the work is done, whether
// another caller got the lifecycle lock between two writes of enrollments,
// see lockFreeBetweenWrites
func (p *programTest) lockFreeBetweenEnrollmentWrites() func() bool {
	p.t.Helper()
	var started, sawFree atomic.Bool
	got := make(chan struct{})
	name := "test:enrollment_lock_waiter"
	hook := func(db *gorm.DB) {
		if db.Statement.Table != database.PROGRAM_ENROLLMENT_TABLE {
			return
		}
		if !started.Swap(true) {
			go func() {
				p.svc.lifecycleMu.Lock()
				close(got)
				p.svc.lifecycleMu.Unlock()
			}()
			return
		}
		select {
		case <-got:
			sawFree.Store(true)
		default:
		}
	}
	if err := p.db.Callback().Update().Before("gorm:begin_transaction").Register(name, hook); err != nil {
		p.t.Fatalf("register hook: %v", err)
	}
	p.t.Cleanup(func() { _ = p.db.Callback().Update().Remove(name) })
	return func() bool {
		if started.Load() {
			select {
			case <-got:
			case <-time.After(5 * time.Second):
			}
		}
		return sawFree.Load()
	}
}

// TestMembershipPassWritesReturnersInChunks checks the people who come back
// to the groups are written in chunks after the lock is let go, with the lock
// free between two chunks, and that every one of them is active again
func TestMembershipPassWritesReturnersInChunks(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	groupID := p.group("staff")
	p.assignGroup(programID, groupID)
	n := 2*repository.ProgramWriteChunk + 10
	now := time.Now().UTC()
	exitedAt := now.Add(-2 * time.Hour)
	recipients := make([]*database.Recipient, 0, n)
	links := make([]*database.RecipientGroupRecipient, 0, n)
	enrollments := make([]*database.ProgramEnrollment, 0, n)
	for i := 0; i < n; i++ {
		rid, eid := uuid.New(), uuid.New()
		email := fmt.Sprintf("person%d@example.com", i)
		recipients = append(recipients, &database.Recipient{ID: &rid, CreatedAt: &now, UpdatedAt: &now, Email: &email})
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID})
		enrollments = append(enrollments, &database.ProgramEnrollment{
			ID: &eid, CreatedAt: &now, UpdatedAt: &now, ProgramID: &programID, RecipientID: &rid,
			Status: model.ProgramEnrollmentExited, ExitedAt: &exitedAt, RoundStartAt: &exitedAt,
		})
	}
	for _, rows := range []any{recipients, links, enrollments} {
		if err := p.db.CreateInBatches(rows, 500).Error; err != nil {
			t.Fatalf("create rows: %v", err)
		}
	}
	listed, err := p.svc.ProgramRepository.GetByID(p.ctx, &programID, &repository.ProgramOption{WithSteps: true, WithRecipientGroups: true})
	if err != nil {
		t.Fatalf("load program: %v", err)
	}
	sawFree := p.lockFreeBetweenEnrollmentWrites()
	if err := p.svc.resolveListedProgram(p.ctx, listed); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	var active, missing int64
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND status = ? AND next_due_at IS NOT NULL", programID, model.ProgramEnrollmentActive).Count(&active)
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND missing_since IS NOT NULL", programID).Count(&missing)
	if active != int64(n) || missing != 0 {
		t.Fatalf("expected %d active again, got %d active and %d missing", n, active, missing)
	}
	if !sawFree() {
		t.Fatal("the lifecycle lock must be free between two chunks of returning people")
	}
}

// TestScimDeprovisionKeepsProgramSends checks a SCIM disable cancels the
// unsent sends of a normal campaign at once but keeps the sends of a program,
// which the membership pass cancels once the person is gone past the grace
func TestScimDeprovisionKeepsProgramSends(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	hidden := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)
	programSend := p.send(hidden, alice, false)
	now := time.Now().UTC()
	plainID := uuid.New()
	p.create(&database.Campaign{ID: &plainID, CreatedAt: &now, UpdatedAt: &now, Name: "plain"})
	plainSend := p.send(plainID, alice, false)

	scim := &Scim{
		Common:                      p.svc.Common,
		RecipientRepository:         &repository.Recipient{DB: p.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: p.db},
	}
	if err := scim.deprovisionRecipient(p.ctx, &alice); err != nil {
		t.Fatalf("deprovision: %v", err)
	}
	if p.campaignRecipient(plainSend).CancelledAt == nil {
		t.Fatal("the send of a normal campaign must be cancelled")
	}
	if p.campaignRecipient(programSend).CancelledAt != nil {
		t.Fatal("the send of a program must be kept within the grace")
	}
	// gone past the grace: the membership pass cancels the send
	p.setEnrollment(e.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "next_due_at": nil,
		"missing_since": now.Add(-2 * programMissingGrace),
	})
	if _, err := p.svc.cancelGoneSends(p.ctx, &programID, nil); err != nil {
		t.Fatalf("cancel gone sends: %v", err)
	}
	if p.campaignRecipient(programSend).CancelledAt == nil {
		t.Fatal("the send of a person gone past the grace must be cancelled")
	}
}

// failReactivation makes every write that turns a completed person active
// fail until the returned func is called
func failReactivation(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	if err := db.Exec(`CREATE TRIGGER fail_reactivate BEFORE UPDATE OF status ON program_enrollments
		WHEN OLD.status = 'completed' AND NEW.status = 'active' BEGIN SELECT RAISE(ABORT, 'reactivate refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return func() {
		if err := db.Exec(`DROP TRIGGER fail_reactivate`).Error; err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}
}

// TestEditReplanBackstop checks an edit whose replan fails keeps a replan
// mark, the membership pass then makes the completed person with the new
// template left active and clears the mark. A completed person gone from the
// groups past the grace stays completed. An edit whose replan works leaves no
// mark.
func TestEditReplanBackstop(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	now := time.Now().UTC().Truncate(time.Second)
	interval := model.ProgramDefaultIntervalMinutes * time.Minute
	completed := func(email string, missing *time.Time) *database.ProgramEnrollment {
		rid := p.recipient(email)
		e := p.enroll(programID, rid, 0)
		p.sendAt(campaignA, rid, now.Add(-interval), true)
		fields := map[string]any{
			"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
			"position": 1, "last_step_id": stepA,
		}
		if missing != nil {
			fields["missing_since"] = *missing
		}
		p.setEnrollment(e.ID, fields)
		return e
	}
	bob := completed("bob@example.com", nil)
	goneSince := now.Add(-2 * programMissingGrace)
	gone := completed("gone@example.com", &goneSince)

	restore := failReactivation(t, p.db)
	p.updateSteps(programID, p.keepStep(stepA, 0), newStep(p.template(), 1))
	if row := p.enrollment(bob.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("the failed replan must leave bob completed, got %s", row.Status)
	}
	if p.loadProgram(programID).ReplanPendingAt == nil {
		t.Fatal("a failed replan must keep the replan mark")
	}
	// a pass while the replan still fails keeps the mark
	p.svc.finishPendingReplans(p.ctx)
	if p.loadProgram(programID).ReplanPendingAt == nil {
		t.Fatal("a failed backstop must keep the replan mark")
	}
	restore()
	p.svc.finishPendingReplans(p.ctx)
	if row := p.enrollment(bob.ID); row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("the backstop must make bob active, got %s", row.Status)
	}
	if row := p.enrollment(gone.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("a person gone past the grace must stay completed, got %s", row.Status)
	}
	if p.loadProgram(programID).ReplanPendingAt != nil {
		t.Fatal("the backstop must clear the replan mark")
	}

	// an edit whose replan works clears its own mark
	program := p.loadProgram(programID)
	steps := []*model.ProgramStep{}
	for i, st := range program.Steps {
		steps = append(steps, p.keepStep(st.ID.MustGet(), i))
	}
	steps = append(steps, newStep(p.template(), len(steps)))
	p.updateSteps(programID, steps...)
	if p.loadProgram(programID).ReplanPendingAt != nil {
		t.Fatal("an edit whose replan works must leave no mark")
	}
}

// TestStalledWithoutActivePeople checks the attention reason and the list
// filter agree: a queued send long overdue needs attention also when nobody
// is active, as the last send of a program that does not loop completes the
// person when it is queued, and a person whose advance keeps failing needs
// attention although their next try is ahead
func TestStalledWithoutActivePeople(t *testing.T) {
	p := newProgramServiceTest(t)
	now := time.Now().UTC()
	day := 24 * time.Hour
	person := func(programID uuid.UUID, email string, fields map[string]any) uuid.UUID {
		rid := p.recipient(email)
		e := p.enroll(programID, rid, 0)
		p.setEnrollment(e.ID, fields)
		return rid
	}
	completedFields := map[string]any{"status": model.ProgramEnrollmentCompleted, "next_due_at": nil}
	running := func() (uuid.UUID, uuid.UUID) {
		programID := p.program(model.ProgramLoopComplete)
		stepID := p.step(programID, 0, true)
		return programID, p.stepCampaign(programID, stepID, 0)
	}
	// one step, the sender never sent the last send
	deadSender, deadCampaign := running()
	rid := person(deadSender, "dead@example.com", completedFields)
	p.sendAt(deadCampaign, rid, now.Add(-3*day), false)
	// everyone completed and nothing queued
	finished, _ := running()
	person(finished, "done@example.com", completedFields)
	// the advance keeps failing, the next try is ahead
	failing, _ := running()
	person(failing, "failing@example.com", map[string]any{"advance_failures": repository.ProgramStallFailures, "next_due_at": now.Add(day)})
	// a few failures are not yet a stall
	fewFailures, _ := running()
	person(fewFailures, "few@example.com", map[string]any{"advance_failures": repository.ProgramStallFailures - 1, "next_due_at": now.Add(day)})

	want := map[uuid.UUID]bool{deadSender: true, failing: true, finished: false, fewFailures: false}
	ids := []*uuid.UUID{}
	for id := range want {
		ids = append(ids, &id)
	}
	sums, err := p.svc.ProgramRepository.GetListSummaries(p.ctx, ids)
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	list, err := p.svc.ProgramRepository.GetAll(p.ctx, nil, &repository.ProgramOption{
		QueryArgs: &vo.QueryArgs{}, Status: model.ProgramStatusNeedsAttention,
	}, now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	listed := map[uuid.UUID]bool{}
	for _, row := range list.Rows {
		listed[row.ID.MustGet()] = true
	}
	for id, stalled := range want {
		reason := programStalledReason(p.loadProgram(id), sums[id], now)
		if (reason != "") != stalled {
			t.Errorf("program %s: reason %q, want stalled %v", id, reason, stalled)
		}
		if listed[id] != stalled {
			t.Errorf("program %s: listed as needs attention %v, want %v", id, listed[id], stalled)
		}
	}
}

// TestPauseStopsResume checks a pause of a paused program stops a resume that
// is under way and keeps the program paused, and that a pause of a paused
// program with no resume is refused
func TestPauseStopsResume(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"paused_at": time.Now().UTC()})
	err := p.svc.Pause(p.ctx, session, &programID)
	var v errs.ValidationError
	if !errors.As(err, &v) || v.Err != errProgramAlreadyPaused {
		t.Fatalf("expected already paused, got %v", err)
	}
	token := p.svc.startResume(programID)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause during resume: %v", err)
	}
	if p.svc.resumeLive(programID, token) {
		t.Fatal("the pause must stop the resume")
	}
	if err := p.svc.resumeStopped(p.ctx, &programID, token); !errors.As(err, &v) || v.Err != errProgramPausedOnResume {
		t.Fatalf("expected the resume stopped by the pause, got %v", err)
	}
	if !p.loadProgram(programID).IsPaused() {
		t.Fatal("the program must stay paused")
	}
	p.svc.endResume(programID, token)
}

// TestPausedProgramKeepsItsStart checks a paused program that has not
// enrolled anyone can not be made a draft or given a later start
func TestPausedProgramKeepsItsStart(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"started_at": nil, "paused_at": time.Now().UTC()})
	draft := true
	if err := p.update(programID, &model.Program{Draft: &draft}); err == nil || !strings.Contains(err.Error(), "paused program") {
		t.Fatalf("a paused program must not be made a draft, got %v", err)
	}
	later := &model.Program{}
	later.StartAt.Set(time.Now().Add(48 * time.Hour))
	if err := p.update(programID, later); err == nil || !strings.Contains(err.Error(), "paused program") {
		t.Fatalf("a paused program must not be given a later start, got %v", err)
	}
	if p.loadProgram(programID).StartAtTime() == nil {
		t.Fatal("the start must be kept")
	}
}

// TestProcessReportedCSVRefusesDeletingProgram checks a reported CSV is not
// imported on a program that is being deleted
func TestProcessReportedCSVRefusesDeletingProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"deleting_at": time.Now().UTC(), "paused_at": time.Now().UTC()})
	header, rows := reportedRows([][]string{{"email"}, {"alice@example.com"}})
	_, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, -1)
	var v errs.ValidationError
	if !errors.As(err, &v) || v.Err != errProgramStopping {
		t.Fatalf("expected the import refused, got %v", err)
	}
}
