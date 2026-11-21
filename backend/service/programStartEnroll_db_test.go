package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// TestProgramStartEnrollsNow checks a start enrolls every group member right
// away with a planned send, without waiting for a tick, and that a later tick
// does not enroll anyone twice
func TestProgramStartEnrollsNow(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group"})
	for _, email := range []string{"alice@example.com", "bob@example.com"} {
		rid := p.recipient(email)
		p.create(&database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID})
	}
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{
		"start_at": nil, "started_at": nil, "new_member_delay_minutes": 7 * 24 * 60,
	})
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	p.step(programID, 0, true)

	// the scheduler ran a moment ago, so only the start itself can enroll
	p.svc.lastMembershipAt = time.Now()
	if err := p.svc.startWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("start: %v", err)
	}
	enrollments, err := p.svc.ProgramRepository.GetEnrollmentPlansByProgramID(p.ctx, &programID)
	if err != nil {
		t.Fatalf("enrollments: %v", err)
	}
	if len(enrollments) != 2 {
		t.Fatalf("expected both members enrolled on start, got %d", len(enrollments))
	}
	for _, e := range enrollments {
		if e.Status != model.ProgramEnrollmentActive || e.NextDueAt == nil {
			t.Fatalf("expected an active enrollment with a planned send: %s %v", e.Status, e.NextDueAt)
		}
		// the starting audience does not wait the new member delay
		if e.NextDueAt.After(now.Add(24 * time.Hour)) {
			t.Fatalf("the starting audience must not wait the new member delay: %v", e.NextDueAt)
		}
	}
	if p.loadProgram(programID).StartedAt == nil {
		t.Fatalf("expected the program marked started")
	}

	// a membership pass right after finds nobody new
	err = p.svc.resolveEnrollments(p.ctx)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if n, err := p.svc.ProgramRepository.CountEnrollments(p.ctx, &programID); err != nil || n != 2 {
		t.Fatalf("expected no double enrollment, got %d %v", n, err)
	}
}

// TestProgramStartPausedWaitsForResume checks a paused draft is not enrolled
// on start, and is enrolled when resumed
func TestProgramStartPausedWaitsForResume(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group"})
	rid := p.recipient("carol@example.com")
	p.create(&database.RecipientGroupRecipient{RecipientID: &rid, RecipientGroupID: &groupID})
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"start_at": nil, "started_at": nil, "paused_at": now})
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	p.step(programID, 0, true)
	p.svc.lastMembershipAt = time.Now()

	if err := p.svc.startWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if n, _ := p.svc.ProgramRepository.CountEnrollments(p.ctx, &programID); n != 0 {
		t.Fatalf("a paused program must not enroll, got %d", n)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n, _ := p.svc.ProgramRepository.CountEnrollments(p.ctx, &programID); n != 1 {
		t.Fatalf("expected the member enrolled on resume, got %d", n)
	}
}
