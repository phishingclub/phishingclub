package service

import (
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
)

// TestEndMovedLaterPlansCancelledLastSend checks a completed person whose last
// send an earlier end cancelled is planned again when the end moves later, so
// they still get that template
func TestEndMovedLaterPlansCancelledLastSend(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	now := time.Now().UTC().Truncate(time.Second)
	rid := p.recipient("pat@example.com")
	e := p.enroll(programID, rid, 0)
	p.sendAt(campaignA, rid, now.Add(-20*24*time.Hour), true)
	last := p.sendAt(campaignB, rid, now.Add(5*24*time.Hour), false)
	// the earlier end cancelled the last send of the completed person
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", last).Update("cancelled_at", utils.NowRFC3339UTC())
	p.setEnrollment(e.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
		"position": 2, "last_step_id": stepB, "round_start_at": now.Add(-20 * 24 * time.Hour),
	})
	before := p.loadProgram(programID)
	before.EndAt = nullable.NewNullableWithValue(now.Add(2 * 24 * time.Hour))
	after := *before
	after.EndAt = nullable.NewNullableWithValue(now.Add(60 * 24 * time.Hour))
	if err := p.svc.replanPeople(p.ctx, before, &after, false, nil); err != nil {
		t.Fatalf("replan: %v", err)
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("an end moved later must plan the cancelled template again, got %s", row.Status)
	}
}
