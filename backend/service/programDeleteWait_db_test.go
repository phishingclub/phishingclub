package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

// withShortDeleteWait makes DeleteByIDAndWait look often and give up after
// wait, for the length of a test
func withShortDeleteWait(t *testing.T, wait time.Duration) {
	t.Helper()
	oldWait, oldPoll := programDeleteBusyWait, programDeleteBusyPoll
	programDeleteBusyWait, programDeleteBusyPoll = wait, 5*time.Millisecond
	t.Cleanup(func() {
		programDeleteBusyWait, programDeleteBusyPoll = oldWait, oldPoll
	})
}

// TestDeleteAndWaitGoneIsDone checks a program that is already gone counts as
// deleted, as when a background delete finished it first
func TestDeleteAndWaitGoneIsDone(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	gone := uuid.New()
	if err := p.svc.DeleteByIDAndWait(p.ctx, session, &gone); err != nil {
		t.Fatalf("a program that is gone must count as deleted: %v", err)
	}
}

// TestDeleteAndWaitWaitsForRunningDelete checks a delete that finds another
// delete of the same program running waits for it, whether that one removes
// the program or stops half way
func TestDeleteAndWaitWaitsForRunningDelete(t *testing.T) {
	withShortDeleteWait(t, 10*time.Second)
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)

	// the running delete removes the program
	finished := p.program(model.ProgramLoopComplete)
	p.manyCampaigns(finished, 2)
	p.setProgram(finished, map[string]any{"deleting_at": time.Now().UTC(), "paused_at": time.Now().UTC()})
	p.svc.claimStopping(finished)
	go func() {
		defer p.svc.releaseStopping(finished)
		time.Sleep(50 * time.Millisecond)
		_ = p.svc.deleteProgramData(p.ctx, session, &finished)
	}()
	if err := p.svc.DeleteByIDAndWait(p.ctx, session, &finished); err != nil {
		t.Fatalf("delete after a running delete finished: %v", err)
	}
	if !p.programGone(finished) {
		t.Fatal("expected the program gone")
	}

	// the running one ends without removing the program, the wait deletes it
	left := p.program(model.ProgramLoopComplete)
	p.manyCampaigns(left, 2)
	p.svc.claimStopping(left)
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.svc.releaseStopping(left)
	}()
	if err := p.svc.DeleteByIDAndWait(p.ctx, session, &left); err != nil {
		t.Fatalf("delete after a running one ended: %v", err)
	}
	if !p.programGone(left) {
		t.Fatal("expected the program and its campaigns deleted")
	}
	if !lockIsFree(p.svc) || p.svc.isStoppingNow(left) {
		t.Fatal("the delete must release the lock and its claim")
	}
}

// TestDeleteAndWaitBounded checks the wait for a running delete ends after
// programDeleteBusyWait with the busy refusal
func TestDeleteAndWaitBounded(t *testing.T) {
	withShortDeleteWait(t, 50*time.Millisecond)
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.svc.claimStopping(programID)
	defer p.svc.releaseStopping(programID)
	start := time.Now()
	err := p.svc.DeleteByIDAndWait(p.ctx, session, &programID)
	if !isProgramBusy(err) {
		t.Fatalf("expected the busy refusal, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the wait must be bounded")
	}
	if p.programGone(programID) {
		t.Fatal("a busy program must not be deleted")
	}
}
