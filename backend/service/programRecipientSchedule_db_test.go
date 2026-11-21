package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// TestRecipientScheduleSameOrder checks a recipient plan in the same order
// lists every template in order: sent, scheduled and the rest with a window
func TestRecipientScheduleSameOrder(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	p.setProgram(programID, map[string]any{"spread_minutes": 3 * 24 * 60})
	first := p.step(programID, 0, true)
	second := p.step(programID, 1, true)
	third := p.step(programID, 2, true)
	fourth := p.step(programID, 3, true)
	anna := p.recipient("anna@example.com")
	p.enroll(programID, anna, 0)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	p.linkTemplate(p.sentWithEvent(programID, first, anna, 0, sentAt, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, sentAt), first)
	queuedAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	p.queue(programID, second, anna, 0, queuedAt)

	plan, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &anna)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if plan.Round != 1 || plan.Sent != 1 || plan.TemplateCount != 4 || len(plan.Rows) != 4 {
		t.Fatalf("unexpected plan %+v", plan)
	}
	want := []struct {
		status string
		step   any
	}{{ProgramScheduleSent, first}, {ProgramScheduleScheduled, second}, {ProgramScheduleNotSent, third}, {ProgramScheduleNotSent, fourth}}
	for i, w := range want {
		r := plan.Rows[i]
		if r.Status != w.status {
			t.Fatalf("row %d: want %s, got %+v", i, w.status, r)
		}
	}
	if plan.Rows[0].SentAt == nil || plan.Rows[0].NotableEventName != data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED {
		t.Fatalf("a sent row has its time and outcome, got %+v", plan.Rows[0])
	}
	if plan.Rows[1].ScheduledAt == nil || !plan.Rows[1].ScheduledAt.Equal(queuedAt) {
		t.Fatalf("a scheduled row has its planned time, got %+v", plan.Rows[1])
	}
	w3, w4 := plan.Rows[2].Window, plan.Rows[3].Window
	if w3 == nil || w4 == nil || w3.FirstDay == "" || w3.LastDay < w3.FirstDay || !(w4.FirstDay > w3.FirstDay) {
		t.Fatalf("templates not sent get windows of the coming rounds, got %+v %+v", w3, w4)
	}
	if plan.Rows[2].TemplateName != p.templateNameOf(third) {
		t.Fatalf("rows keep the template order, got %s", plan.Rows[2].TemplateName)
	}
	if len(plan.NextWindows) != 0 {
		t.Fatal("the same order puts the windows on the rows")
	}
}

// TestRecipientScheduleRandomOrder checks a random order plan lists the
// remaining templates without times and the windows of the coming rounds
func TestRecipientScheduleRandomOrder(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	p.setProgram(programID, map[string]any{"order_mode": model.ProgramOrderShuffle})
	first := p.step(programID, 0, true)
	p.step(programID, 1, true)
	p.step(programID, 2, true)
	anna := p.recipient("anna@example.com")
	p.enroll(programID, anna, 0)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	p.linkTemplate(p.sentWithEvent(programID, first, anna, 0, sentAt, "", sentAt), first)

	plan, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &anna)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(plan.Rows) != 3 || plan.Rows[0].Status != ProgramScheduleSent {
		t.Fatalf("expected the sent template first, got %+v", plan.Rows)
	}
	for _, r := range plan.Rows[1:] {
		if r.Status != ProgramScheduleNotSent || r.Window != nil || r.ScheduledAt != nil {
			t.Fatalf("a template not sent in random order has no time, got %+v", r)
		}
	}
	if len(plan.NextWindows) != 2 {
		t.Fatalf("expected the windows of the 2 coming rounds, got %d", len(plan.NextWindows))
	}
}

// TestRecipientScheduleCompleted checks a completed recipient shows only what
// was sent
func TestRecipientScheduleCompleted(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	first := p.step(programID, 0, true)
	second := p.step(programID, 1, true)
	anna := p.recipient("anna@example.com")
	e := p.enroll(programID, anna, 0)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	p.linkTemplate(p.sentWithEvent(programID, first, anna, 0, sentAt, "", sentAt), first)
	p.linkTemplate(p.sentWithEvent(programID, second, anna, 0, sentAt.Add(time.Minute), "", sentAt), second)
	if err := p.db.Model(e).Updates(map[string]any{"status": model.ProgramEnrollmentCompleted, "next_due_at": nil}).Error; err != nil {
		t.Fatalf("complete: %v", err)
	}
	plan, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &anna)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if plan.EnrollmentStatus != model.ProgramEnrollmentCompleted || plan.Sent != 2 || len(plan.Rows) != 2 || len(plan.NextWindows) != 0 {
		t.Fatalf("expected the history only, got %+v", plan)
	}
	for _, r := range plan.Rows {
		if r.Status != ProgramScheduleSent {
			t.Fatalf("a completed recipient has no future rows, got %+v", r)
		}
	}
}

// TestRecipientScheduleRefused checks an anonymous program, a recipient who is
// not enrolled and an unauthorized session read nothing
func TestRecipientScheduleRefused(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	anna := p.recipient("anna@example.com")
	bob := p.recipient("bob@example.com")
	p.enroll(programID, anna, 0)
	if _, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &bob); err == nil {
		t.Fatal("a recipient who is not enrolled must be refused")
	}
	if _, err := p.svc.GetRecipientSchedule(p.ctx, unauthorizedSession(t), &programID, &anna); err == nil {
		t.Fatal("an unauthorized session must be refused")
	}
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	if plan, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &anna); err == nil {
		t.Fatalf("an anonymous program must give no plan, got %+v", plan)
	}
}

// TestRecipientScheduleTemplateAddedAgain checks a template removed and added
// again in the same round shows as sent on its new step, as the scheduler
// counts it as given and does not send it again
func TestRecipientScheduleTemplateAddedAgain(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	first := p.step(programID, 0, true)
	second := p.step(programID, 1, true)
	anna := p.recipient("anna@example.com")
	p.enroll(programID, anna, 0)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	p.linkTemplate(p.sentWithEvent(programID, first, anna, 0, sentAt, "", sentAt), first)
	// the template of the first step is removed and added again as a new step
	templateID := p.stepTemplate(first)
	if err := p.db.Where("id = ?", first).Delete(&database.ProgramStep{}).Error; err != nil {
		t.Fatalf("remove step: %v", err)
	}
	now := time.Now().UTC()
	again := uuid.New()
	p.create(&database.ProgramStep{
		ID: &again, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &programID, CampaignTemplateID: &templateID, Position: 2,
	})

	plan, err := p.svc.GetRecipientSchedule(p.ctx, session, &programID, &anna)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if plan.Sent != 1 || len(plan.Rows) != 2 {
		t.Fatalf("unexpected plan %+v", plan)
	}
	byName := map[string]*ProgramScheduleRow{}
	for _, r := range plan.Rows {
		byName[r.TemplateName] = r
	}
	if r := byName[p.templateNameOf(again)]; r == nil || r.Status != ProgramScheduleSent || r.SentAt == nil {
		t.Fatalf("the template added again must show as sent, got %+v", plan.Rows)
	}
	if r := byName[p.templateNameOf(second)]; r == nil || r.Status != ProgramScheduleNotSent || r.Window == nil {
		t.Fatalf("the other template is not sent yet, got %+v", plan.Rows)
	}
}
