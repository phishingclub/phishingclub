package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// lockIsFree reports whether the lifecycle lock can be taken within a second
func lockIsFree(s *Program) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return false
	}
	s.lifecycleMu.Unlock()
	return true
}

// callPanics runs fn and reports whether it panicked
func callPanics(fn func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

// TestLifecycleLockReleasedOnPanic checks a panic while the lifecycle lock is
// held never leaves it held
func TestLifecycleLockReleasedOnPanic(t *testing.T) {
	s := &Program{Common: Common{Logger: zap.NewNop().Sugar()}}
	if !callPanics(func() {
		_ = s.withLifecycleLock(context.Background(), func() error { panic("in the locked part") })
	}) {
		t.Fatalf("expected the hook to panic")
	}
	if !lockIsFree(s) {
		t.Fatalf("a panic in withLifecycleLock left the lock held")
	}
	// a repository without a database panics on its first read, inside the
	// locked part of an advance batch and in a tick
	s.ProgramRepository = &repository.Program{}
	if !callPanics(func() { _, _ = s.advanceDueBatch(context.Background(), nil, time.Now()) }) {
		t.Fatalf("expected the advance batch to panic")
	}
	if !lockIsFree(s) {
		t.Fatalf("a panic in the advance batch left the lock held")
	}
	if !callPanics(func() { _ = s.Tick(context.Background(), authorizedSession(t)) }) {
		t.Fatalf("expected the tick to panic")
	}
	if !lockIsFree(s) {
		t.Fatalf("a panic in the tick left the lock held")
	}
}

// probe runs fn once, the first time a statement of the given kind runs on the
// given table, before its transaction starts. It is removed when the test ends.
func (p *programTest) probe(kind string, table string, fn func()) {
	p.t.Helper()
	var done atomic.Bool
	name := "test:probe_" + kind
	hook := func(db *gorm.DB) {
		if db.Statement.Table != table || done.Load() {
			return
		}
		done.Store(true)
		fn()
	}
	var err error
	switch kind {
	case "update":
		err = p.db.Callback().Update().Before("gorm:begin_transaction").Register(name, hook)
		p.t.Cleanup(func() { _ = p.db.Callback().Update().Remove(name) })
	case "delete":
		err = p.db.Callback().Delete().Before("gorm:begin_transaction").Register(name, hook)
		p.t.Cleanup(func() { _ = p.db.Callback().Delete().Remove(name) })
	}
	if err != nil {
		p.t.Fatalf("register probe: %v", err)
	}
}

// stoppingChecks runs the lifecycle calls on the program that is being closed
// or deleted and on another program, while the long part of the close or
// delete runs
type stoppingChecks struct {
	ran         bool
	otherPause  error
	selfPause   error
	selfResume  error
	selfStart   error
	selfClose   error
	selfDelete  error
	lockWasFree bool
}

func (p *programTest) checkWhileStopping(c *stoppingChecks, self, other uuid.UUID) func() {
	return func() {
		session := authorizedSession(p.t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.ran = true
		c.lockWasFree = lockIsFree(p.svc)
		c.otherPause = p.svc.Pause(ctx, session, &other)
		c.selfPause = p.svc.Pause(ctx, session, &self)
		c.selfResume = p.svc.resumeWait(ctx, session, &self)
		c.selfStart = p.svc.startWait(ctx, session, &self)
		c.selfClose = p.svc.closeWait(ctx, session, &self)
		c.selfDelete = p.svc.DeleteByID(ctx, session, &self)
	}
}

// manyCampaigns gives a program several steps, each with a hidden campaign
// holding a send not yet sent
func (p *programTest) manyCampaigns(programID uuid.UUID, n int) ([]uuid.UUID, []uuid.UUID) {
	campaigns, sends := []uuid.UUID{}, []uuid.UUID{}
	alice := p.recipient(uuid.NewString() + "@example.com")
	for i := 0; i < n; i++ {
		stepID := p.step(programID, i, true)
		campaignID := p.stepCampaign(programID, stepID, 0)
		campaigns = append(campaigns, campaignID)
		sends = append(sends, p.sendAt(campaignID, alice, time.Now().Add(time.Duration(i+1)*time.Hour), false))
	}
	return campaigns, sends
}

// TestProgramCloseReleasesLockForBulk checks the sends of a close are
// cancelled and its hidden campaigns closed after the lifecycle lock is let
// go: another program can be paused meanwhile, while the program being closed
// refuses every other change
func TestProgramCloseReleasesLockForBulk(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	other := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 8)

	checks := &stoppingChecks{}
	// the cancel of the sends is the first write after the lock is let go
	p.probe("update", database.CAMPAIGN_RECIPIENT_TABLE_NAME, p.checkWhileStopping(checks, programID, other))
	if err := p.svc.closeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !checks.ran {
		t.Fatalf("the probe did not run during the close")
	}
	if !checks.lockWasFree {
		t.Fatalf("the close held the lifecycle lock while closing the hidden campaigns")
	}
	if checks.otherPause != nil {
		t.Fatalf("another program must be paused while a close runs: %v", checks.otherPause)
	}
	for name, err := range map[string]error{
		"pause": checks.selfPause, "resume": checks.selfResume, "start": checks.selfStart,
		"close": checks.selfClose, "delete": checks.selfDelete,
	} {
		if !isValidationError(err) {
			t.Fatalf("%s of a program being closed must be refused, got %v", name, err)
		}
	}
	if !isProgramBusy(checks.selfClose) || !isProgramBusy(checks.selfDelete) {
		t.Fatalf("a second close or delete must wait for the first: %v %v", checks.selfClose, checks.selfDelete)
	}
	program := p.loadProgram(programID)
	if !program.IsClosed() || program.IsPaused() || program.ClosingAt != nil {
		t.Fatalf("expected the program closed, got %s", program.Status)
	}
	for _, id := range campaigns {
		if p.campaignRow(id).ClosedAt == nil {
			t.Fatalf("every hidden campaign must be closed")
		}
	}
	for _, id := range sends {
		if p.campaignRecipient(id).CancelledAt == nil {
			t.Fatalf("every send not yet sent must be cancelled")
		}
	}
	if !p.loadProgram(other).IsPaused() {
		t.Fatalf("the other program must be paused")
	}
	if p.svc.isStoppingNow(programID) {
		t.Fatalf("the close must release its claim")
	}
}

// failCampaignClose makes the database refuse to close a campaign, so a close
// fails in its long part. The returned function removes the refusal.
func failCampaignClose(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	if err := db.Exec(`CREATE TRIGGER fail_campaign_close BEFORE UPDATE OF closed_at ON campaigns
		WHEN NEW.closed_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'close refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return func() {
		if err := db.Exec(`DROP TRIGGER fail_campaign_close`).Error; err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}
}

// TestProgramCloseBulkFailureRetry checks a close that fails while closing
// the hidden campaigns leaves a program that sends nothing and is marked as
// closing, that no other change is allowed, and that the next tick finishes it
func TestProgramCloseBulkFailureRetry(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 3)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)

	restore := failCampaignClose(t, p.db)
	// the request marks the close, the background close then fails
	if err := p.svc.closeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	program := p.loadProgram(programID)
	if program.IsClosed() || program.ClosingAt == nil || program.Status != model.ProgramStatusPaused {
		t.Fatalf("expected a paused program marked as closing, got %s %v", program.Status, program.ClosingAt)
	}
	if p.svc.isStoppingNow(programID) {
		t.Fatalf("a failed close must release its claim")
	}
	for _, id := range sends {
		if p.campaignRecipient(id).CancelledAt == nil {
			t.Fatalf("the sends must be cancelled before the long part")
		}
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentStopped || row.NextDueAt != nil {
		t.Fatalf("the people must be stopped before the long part: %s %v", row.Status, row.NextDueAt)
	}
	for name, call := range map[string]func() error{
		"pause":  func() error { return p.svc.Pause(p.ctx, session, &programID) },
		"resume": func() error { return p.svc.resumeWait(p.ctx, session, &programID) },
		"start":  func() error { return p.svc.startWait(p.ctx, session, &programID) },
	} {
		if err := call(); !isValidationError(err) {
			t.Fatalf("%s of a closing program must be refused, got %v", name, err)
		}
	}
	// a tick while it still fails keeps the program closing
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	if p.loadProgram(programID).IsClosed() {
		t.Fatalf("the close must not finish while it fails")
	}
	restore()
	// the failed close waits programCloseRetryEvery, the wait is skipped here
	p.svc.closeRetryAt[programID] = time.Now().Add(-time.Second)
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	program = p.loadProgram(programID)
	if !program.IsClosed() || program.IsPaused() || program.ClosingAt != nil {
		t.Fatalf("the next tick must finish the close, got %s", program.Status)
	}
	if program.EndedAutomatically {
		t.Fatalf("a close finished by the scheduler is not an end by date")
	}
	for _, id := range campaigns {
		if p.campaignRow(id).ClosedAt == nil {
			t.Fatalf("every hidden campaign must be closed")
		}
	}
	// an operator close finds it closed
	if err := p.svc.closeWait(p.ctx, session, &programID); !isValidationError(err) {
		t.Fatalf("a closed program can not be closed again, got %v", err)
	}
}

// TestProgramDeleteReleasesLockForBulk checks the hidden campaigns of a
// delete are removed after the lifecycle lock is let go, and a delete that
// fails half way refuses every change but a new delete, which finishes it
func TestProgramDeleteReleasesLockForBulk(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	other := p.program(model.ProgramLoopComplete)
	campaigns, _ := p.manyCampaigns(programID, 6)

	checks := &stoppingChecks{}
	// a campaign delete removes its events first, outside a transaction
	p.probe("delete", database.CAMPAIGN_EVENT_TABLE, p.checkWhileStopping(checks, programID, other))
	// the third campaign can not be deleted, so the delete stops half way
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", campaigns[2]).Update("name", "blocked").Error; err != nil {
		t.Fatalf("name campaign: %v", err)
	}
	if err := p.db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON campaigns
		WHEN OLD.name = 'blocked' BEGIN SELECT RAISE(ABORT, 'delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := p.svc.DeleteByID(p.ctx, session, &programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	p.waitDeletes()
	if p.programGone(programID) {
		t.Fatalf("expected the delete to fail")
	}
	if !checks.ran || !checks.lockWasFree {
		t.Fatalf("the delete held the lifecycle lock while removing the hidden campaigns: ran %v", checks.ran)
	}
	if checks.otherPause != nil {
		t.Fatalf("another program must be paused while a delete runs: %v", checks.otherPause)
	}
	if !isProgramBusy(checks.selfDelete) {
		t.Fatalf("a second delete must wait for the first: %v", checks.selfDelete)
	}
	for name, err := range map[string]error{
		"pause": checks.selfPause, "resume": checks.selfResume, "start": checks.selfStart, "close": checks.selfClose,
	} {
		if !isValidationError(err) {
			t.Fatalf("%s of a program being deleted must be refused, got %v", name, err)
		}
	}
	program := p.loadProgram(programID)
	if program.DeletingAt == nil || !program.IsPaused() {
		t.Fatalf("a delete that stopped half way must leave the program paused and marked")
	}
	if p.campaignRow(campaigns[2]).PausedAt == nil {
		t.Fatalf("the hidden campaigns left must be paused")
	}
	// a half deleted program is not closed by the scheduler or an operator
	if err := p.svc.closeWait(p.ctx, session, &programID); !isValidationError(err) {
		t.Fatalf("a close of a program being deleted must be refused, got %v", err)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); !isValidationError(err) {
		t.Fatalf("a resume of a program being deleted must be refused, got %v", err)
	}
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	p.waitForCloses(t)
	if p.loadProgram(programID).IsClosed() {
		t.Fatalf("the scheduler must not close a program being deleted")
	}
	if err := p.db.Exec(`DROP TRIGGER fail_delete`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := p.svc.DeleteByID(p.ctx, session, &programID); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	p.waitDeletes()
	if _, err := p.svc.ProgramRepository.GetByID(p.ctx, &programID, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected the program gone, got %v", err)
	}
	var count int64
	p.db.Model(&database.Campaign{}).Where("program_id = ?", programID).Count(&count)
	if count != 0 {
		t.Fatalf("every hidden campaign must be deleted, %d left", count)
	}
	if p.svc.isStoppingNow(programID) {
		t.Fatalf("the delete must release its claim")
	}
	// the other program is untouched besides its pause
	if _, err := p.svc.ProgramRepository.GetByID(p.ctx, &other, nil); err != nil {
		t.Fatalf("the other program must stay: %v", err)
	}
}

// TestWriteInChunksStopsWhenProgramStops checks the chunked writes of a start
// or an edit read the program again before each chunk, so a pause, close or
// delete between chunks stops the rest
func TestWriteInChunksStopsWhenProgramStops(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	n := 3 * repository.ProgramWriteChunk
	runs := func(pr *model.Program) bool { return programRunsAt(pr, time.Now()) }
	for name, stop := range map[string]func(){
		"pause":  func() { p.setProgram(programID, map[string]any{"paused_at": time.Now().UTC()}) },
		"close":  func() { p.setProgram(programID, map[string]any{"closing_at": time.Now().UTC()}) },
		"delete": func() { p.db.Where("id = ?", programID).Delete(&database.Program{}) },
	} {
		chunks := 0
		done, err := p.svc.writeInChunks(p.ctx, &programID, n, runs, func(start, end int) error {
			if end-start > repository.ProgramWriteChunk {
				t.Fatalf("a chunk is larger than ProgramWriteChunk: %d", end-start)
			}
			chunks++
			stop()
			return nil
		})
		if err != nil || done || chunks != 1 {
			t.Fatalf("%s: expected the rest skipped after the first chunk: done %v chunks %d err %v", name, done, chunks, err)
		}
		p.setProgram(programID, map[string]any{"paused_at": nil, "closing_at": nil})
		if name == "delete" {
			programID = p.program(model.ProgramLoopComplete)
		}
	}
	chunks := 0
	done, err := p.svc.writeInChunks(p.ctx, &programID, n+1, runs, func(start, end int) error {
		chunks++
		return nil
	})
	if err != nil || !done || chunks != 4 {
		t.Fatalf("expected every chunk written: done %v chunks %d err %v", done, chunks, err)
	}
}

// TestProgramStartEnrollsOverChunks checks a start whose group is larger than
// one chunk enrolls every member once and marks the program started
func TestProgramStartEnrollsOverChunks(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	n := repository.ProgramWriteChunk + 5
	now := time.Now().UTC()
	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group"})
	recipients := make([]*database.Recipient, 0, n)
	links := make([]*database.RecipientGroupRecipient, 0, n)
	for i := 0; i < n; i++ {
		id := uuid.New()
		email := id.String() + "@example.com"
		recipients = append(recipients, &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email})
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &groupID})
	}
	if err := p.db.CreateInBatches(recipients, 500).Error; err != nil {
		t.Fatalf("recipients: %v", err)
	}
	if err := p.db.CreateInBatches(links, 500).Error; err != nil {
		t.Fatalf("links: %v", err)
	}
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"start_at": nil, "started_at": nil})
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	p.step(programID, 0, true)
	p.svc.lastMembershipAt = time.Now()
	if err := p.svc.startWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if count, err := p.svc.ProgramRepository.CountActiveEnrollments(p.ctx, &programID); err != nil || count != int64(n) {
		t.Fatalf("expected %d active people, got %d %v", n, count, err)
	}
	if p.loadProgram(programID).StartedAt == nil {
		t.Fatalf("expected the program marked started")
	}
	if !lockIsFree(p.svc) {
		t.Fatalf("the start must release the lock")
	}
}
