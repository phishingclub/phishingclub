package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// randomProgram creates a started program in random order with n templates
func (p *programTest) randomProgram(n int) (uuid.UUID, []uuid.UUID) {
	p.t.Helper()
	programID := p.program(model.ProgramLoopLoop)
	p.setProgram(programID, map[string]any{"order_mode": model.ProgramOrderShuffle})
	steps := make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		steps = append(steps, p.step(programID, i, true))
	}
	return programID, steps
}

// sentSteps marks the given steps sent to a recipient in loop 0
func (p *programTest) sentSteps(programID, recipientID uuid.UUID, steps ...uuid.UUID) {
	p.t.Helper()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for i, st := range steps {
		at := base.Add(time.Duration(i) * time.Minute)
		p.linkTemplate(p.sentWithEvent(programID, st, recipientID, 0, at, "", at), st)
	}
}

// nextTemplateOf returns the next template name of a recipient row
func (p *programTest) nextTemplateOf(programID, recipientID uuid.UUID) *string {
	p.t.Helper()
	view, err := p.svc.GetRecipientsView(p.ctx, authorizedSession(p.t), &programID, &vo.QueryArgs{Limit: 10})
	if err != nil {
		p.t.Fatalf("recipients: %v", err)
	}
	for _, r := range view.Rows {
		if r.RecipientID == recipientID.String() {
			return r.NextTemplateName
		}
	}
	p.t.Fatalf("recipient %s not listed", recipientID)
	return nil
}

// TestRandomOrderOneLeftIsDecided checks that in random order the next
// template is named when only one is left in the round, is left open while two
// or more are, opens again when the round starts over and is always named in
// a one template program
func TestRandomOrderOneLeftIsDecided(t *testing.T) {
	t.Run("two left", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(3)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps[0])
		if n := p.nextTemplateOf(programID, anna); n != nil {
			t.Fatalf("two left is picked at send time, got %s", *n)
		}
	})
	t.Run("one left", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(3)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps[2], steps[0])
		n := p.nextTemplateOf(programID, anna)
		if n == nil || *n != p.templateNameOf(steps[1]) {
			t.Fatalf("the one left is decided, got %v", n)
		}
	})
	t.Run("start over", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(3)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps...)
		if n := p.nextTemplateOf(programID, anna); n != nil {
			t.Fatalf("a round that starts over is picked at send time, got %s", *n)
		}
	})
	t.Run("one template", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(1)
		anna := p.recipient("anna@example.com")
		bob := p.recipient("bob@example.com")
		p.enroll(programID, anna, 0)
		p.enroll(programID, bob, 0)
		p.sentSteps(programID, anna, steps[0])
		want := p.templateNameOf(steps[0])
		if n := p.nextTemplateOf(programID, bob); n == nil || *n != want {
			t.Fatalf("a one template program is decided, got %v", n)
		}
		if n := p.nextTemplateOf(programID, anna); n == nil || *n != want {
			t.Fatalf("a one template program is decided when it starts over, got %v", n)
		}
	})
}

// TestRecipientScheduleOneLeft checks the random order plan gives the last
// template left its window and lists no open windows, while two left keep the
// open order, and a round that is complete lists only what was sent
func TestRecipientScheduleOneLeft(t *testing.T) {
	session := func(t *testing.T) *model.Session { return authorizedSession(t) }
	t.Run("one left", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(3)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps[0], steps[2])
		plan, err := p.svc.GetRecipientSchedule(p.ctx, session(t), &programID, &anna)
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if len(plan.Rows) != 3 || len(plan.NextWindows) != 0 {
			t.Fatalf("expected 3 rows and no open windows, got %+v", plan)
		}
		last := plan.Rows[2]
		if last.Status != ProgramScheduleNotSent || last.Window == nil || last.Window.FirstDay == "" ||
			last.TemplateName != p.templateNameOf(steps[1]) {
			t.Fatalf("the one left has its window, got %+v", last)
		}
	})
	t.Run("two left", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(3)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps[0])
		plan, err := p.svc.GetRecipientSchedule(p.ctx, session(t), &programID, &anna)
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		for _, r := range plan.Rows[1:] {
			if r.Window != nil {
				t.Fatalf("two left have no window, got %+v", r)
			}
		}
		if len(plan.NextWindows) != 2 {
			t.Fatalf("expected 2 open windows, got %d", len(plan.NextWindows))
		}
	})
	t.Run("one template", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(1)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		plan, err := p.svc.GetRecipientSchedule(p.ctx, session(t), &programID, &anna)
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if len(plan.Rows) != 1 || plan.Rows[0].Window == nil || len(plan.NextWindows) != 0 ||
			plan.Rows[0].TemplateName != p.templateNameOf(steps[0]) {
			t.Fatalf("a one template program is decided, got %+v", plan)
		}
	})
	t.Run("start over", func(t *testing.T) {
		p := newProgramServiceTest(t)
		programID, steps := p.randomProgram(2)
		anna := p.recipient("anna@example.com")
		p.enroll(programID, anna, 0)
		p.sentSteps(programID, anna, steps...)
		plan, err := p.svc.GetRecipientSchedule(p.ctx, session(t), &programID, &anna)
		if err != nil {
			t.Fatalf("schedule: %v", err)
		}
		if len(plan.Rows) != 2 || len(plan.NextWindows) != 0 {
			t.Fatalf("a complete round lists what was sent only, got %+v", plan)
		}
		for _, r := range plan.Rows {
			if r.Status != ProgramScheduleSent {
				t.Fatalf("expected sent rows, got %+v", r)
			}
		}
	})
}
