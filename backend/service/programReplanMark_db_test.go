package service

import (
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// TestEditReplanMarkKeptAfterOlderClear checks a second edit whose replan
// fails while an earlier edit still replans keeps a mark of its own, so the
// earlier edit clearing its mark when it ends leaves the program marked and
// the membership pass then plans the people again.
func TestEditReplanMarkKeptAfterOlderClear(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	now := time.Now().UTC().Truncate(time.Second)
	interval := model.ProgramDefaultIntervalMinutes * time.Minute
	rid := p.recipient("bob@example.com")
	bob := p.enroll(programID, rid, 0)
	p.sendAt(campaignA, rid, now.Add(-interval), true)
	p.setEnrollment(bob.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
		"position": 1, "last_step_id": stepA,
	})

	// an earlier edit is still replanning and holds its mark
	older := time.Now().UTC().Add(-time.Minute)
	if err := p.db.Model(&database.Program{}).Where("id = ?", programID).
		Update("replan_pending_at", older).Error; err != nil {
		t.Fatalf("set mark: %v", err)
	}
	restore := failReactivation(t, p.db)
	p.updateSteps(programID, p.keepStep(stepA, 0), newStep(p.template(), 1))
	restore()
	mark := p.loadProgram(programID).ReplanPendingAt
	if mark == nil || mark.Equal(older) {
		t.Fatalf("the second edit must write a mark of its own, got %v", mark)
	}
	// the earlier edit ends and clears the mark it set
	if err := p.svc.ProgramRepository.ClearReplanPending(p.ctx, &programID, older); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if p.loadProgram(programID).ReplanPendingAt == nil {
		t.Fatal("clearing the older mark must keep the mark of the second edit")
	}
	p.svc.finishPendingReplans(p.ctx)
	if row := p.enrollment(bob.ID); row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("the membership pass must make bob active for the new template, got %s", row.Status)
	}
	if p.loadProgram(programID).ReplanPendingAt != nil {
		t.Fatal("the membership pass must clear the mark")
	}
}
