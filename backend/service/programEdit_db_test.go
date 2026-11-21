package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// The tests in this file change a running program through the service update
// and check what happens to the people in it.

// template stores a usable template that is not training
func (p *programTest) template() uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id, urlID := uuid.New(), uuid.New()
	p.create(&database.CampaignTemplate{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "template " + id.String(),
		URLIdentifierID: &urlID, IsUsable: true,
	})
	return id
}

// trainingTemplate stores a usable awareness training template
func (p *programTest) trainingTemplate() uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id, urlID := uuid.New(), uuid.New()
	p.create(&database.CampaignTemplate{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "training " + id.String(),
		URLIdentifierID: &urlID, IsUsable: true, IsTraining: true,
	})
	return id
}

// TestTrainingOnlyProgramAccepted checks a program may hold only training
// templates: a create with only a training step is stored, and an update of a
// program to a step list of only training templates is stored too
func TestTrainingOnlyProgramAccepted(t *testing.T) {
	p := newProgramWriteTest(t)
	training := p.trainingTemplate()
	req := newProgramRequest(t, "training only", training, p.group("training staff"))
	id, err := p.svc.Create(p.ctx, authorizedSession(t), req)
	if err != nil {
		t.Fatalf("a training only program must be accepted on create: %v", err)
	}
	if steps := p.loadProgram(*id).Steps; len(steps) != 1 || steps[0].CampaignTemplateID.MustGet() != training {
		t.Fatalf("want the one training step stored, got %d steps", len(steps))
	}

	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	other := p.trainingTemplate()
	p.updateSteps(programID, newStep(training, 0), newStep(other, 1))
	steps := p.loadProgram(programID).Steps
	if len(steps) != 2 {
		t.Fatalf("want two training steps after the update, got %d", len(steps))
	}
	for _, st := range steps {
		if tid := st.CampaignTemplateID.MustGet(); tid != training && tid != other {
			t.Fatalf("a step kept a template that is not training: %v", tid)
		}
	}
}

// keepStep is a step of an update request that keeps a stored step at a
// position
func (p *programTest) keepStep(stepID uuid.UUID, position int) *model.ProgramStep {
	st := &model.ProgramStep{}
	st.ID.Set(stepID)
	st.CampaignTemplateID.Set(p.stepTemplate(stepID))
	st.Position.Set(position)
	return st
}

// newStep is a step of an update request with a template at a position
func newStep(templateID uuid.UUID, position int) *model.ProgramStep {
	st := &model.ProgramStep{}
	st.CampaignTemplateID.Set(templateID)
	st.Position.Set(position)
	return st
}

// update stores a change through the service update, with the stored name as
// the update asks for one
func (p *programTest) update(programID uuid.UUID, update *model.Program) error {
	p.t.Helper()
	update.Name = p.loadProgram(programID).Name
	return p.svc.UpdateByID(p.ctx, authorizedSession(p.t), &programID, update)
}

// updateSteps stores a new step list through the service update
func (p *programTest) updateSteps(programID uuid.UUID, steps ...*model.ProgramStep) {
	p.t.Helper()
	if err := p.update(programID, &model.Program{Steps: steps}); err != nil {
		p.t.Fatalf("update steps: %v", err)
	}
}

// updateGroups stores a new group list through the service update
func (p *programTest) updateGroups(programID uuid.UUID, groupIDs ...uuid.UUID) {
	p.t.Helper()
	ids := make([]*uuid.UUID, 0, len(groupIDs))
	for i := range groupIDs {
		ids = append(ids, &groupIDs[i])
	}
	update := &model.Program{RecipientGroupIDs: nullable.NewNullableWithValue(ids)}
	if err := p.update(programID, update); err != nil {
		p.t.Fatalf("update groups: %v", err)
	}
}

// addToGroup puts a recipient in a group
func (p *programTest) addToGroup(groupID, recipientID uuid.UUID) {
	p.t.Helper()
	p.create(&database.RecipientGroupRecipient{RecipientID: &recipientID, RecipientGroupID: &groupID})
}

// assignGroup assigns a group to a program
func (p *programTest) assignGroup(programID, groupID uuid.UUID) {
	p.t.Helper()
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
}

// stepCampaignOfStep returns the hidden campaign of a step in a loop, made
// when there is none yet
func (p *programTest) stepCampaignOfStep(programID, stepID uuid.UUID, loop int) uuid.UUID {
	p.t.Helper()
	var link database.ProgramCampaign
	res := p.db.Where("program_step_id = ? AND loop_index = ?", stepID, loop).Limit(1).Find(&link)
	if res.Error != nil {
		p.t.Fatalf("load link: %v", res.Error)
	}
	if res.RowsAffected > 0 {
		return *link.CampaignID
	}
	return p.stepCampaign(programID, stepID, loop)
}

// enrollmentOf returns the enrollment of a recipient in a program
func (p *programTest) enrollmentOf(programID, recipientID uuid.UUID) *database.ProgramEnrollment {
	p.t.Helper()
	var rows []database.ProgramEnrollment
	if err := p.db.Where("program_id = ? AND recipient_id = ?", programID, recipientID).Find(&rows).Error; err != nil {
		p.t.Fatalf("load enrollment: %v", err)
	}
	if len(rows) != 1 {
		p.t.Fatalf("expected one enrollment, got %d", len(rows))
	}
	return &rows[0]
}

// sendsOf returns every send of a recipient in a campaign
func (p *programTest) sendsOf(campaignID, recipientID uuid.UUID) []database.CampaignRecipient {
	p.t.Helper()
	var rows []database.CampaignRecipient
	if err := p.db.Where("campaign_id = ? AND recipient_id = ?", campaignID, recipientID).Find(&rows).Error; err != nil {
		p.t.Fatalf("load sends: %v", err)
	}
	return rows
}

// queuedAfterSend sets an enrollment the way the advance leaves it right
// after it planned a send of step at sendAt in the round that started at
// round: the next round is one interval later
func (p *programTest) queuedAfterSend(e *database.ProgramEnrollment, step uuid.UUID, sendAt, round time.Time, position int) time.Time {
	p.t.Helper()
	next := round.Add(model.ProgramDefaultIntervalMinutes * time.Minute)
	p.setEnrollment(e.ID, map[string]any{
		"last_send_at": sendAt, "last_step_id": step, "round_start_at": next,
		"next_due_at": next, "position": position,
	})
	return next
}

// TestEditRemoveTemplateCancelsQueuedSend checks a removed template is never
// sent again, in a running and in a paused program: the queued send of it is
// cancelled, also one moved to a later window opening, the person is due again
// about when that send would have gone out and the next tick gives them a
// template they have left, in the same round. Their history keeps the sent
// sends, a person who had every other template stays completed with one send
// fewer.
func TestEditRemoveTemplateCancelsQueuedSend(t *testing.T) {
	for _, paused := range []bool{false, true} {
		p := newProgramWriteTest(t)
		programID := p.program(model.ProgramLoopComplete)
		stepA := p.step(programID, 0, true)
		stepB := p.step(programID, 1, true)
		stepC := p.step(programID, 2, true)
		campaignA := p.stepCampaign(programID, stepA, 0)
		campaignB := p.stepCampaign(programID, stepB, 0)
		campaignC := p.stepCampaign(programID, stepC, 0)
		now := time.Now().UTC().Truncate(time.Second)
		round := now.Add(-time.Hour)
		interval := model.ProgramDefaultIntervalMinutes * time.Minute

		// alice had A a round ago, B is queued now
		alice := p.recipient("alice@example.com")
		ea := p.enroll(programID, alice, 0)
		sentA := p.sendAt(campaignA, alice, round.Add(-interval), true)
		queuedB := p.sendAt(campaignB, alice, now.Add(-time.Minute), false)
		p.queuedAfterSend(ea, stepB, now.Add(-time.Minute), round, 2)
		// dave's send of B waits for the window to open in ten hours
		dave := p.recipient("dave@example.com")
		ed := p.enroll(programID, dave, 0)
		p.sendAt(campaignA, dave, round.Add(-interval), true)
		laterB := p.sendAt(campaignB, dave, now.Add(10*time.Hour), false)
		p.queuedAfterSend(ed, stepB, now.Add(10*time.Hour), round, 2)
		// bob had A and C, B was his last and is queued, so he is completed
		bob := p.recipient("bob@example.com")
		eb := p.enroll(programID, bob, 0)
		p.sendAt(campaignA, bob, round.Add(-2*interval), true)
		p.sendAt(campaignC, bob, round.Add(-interval), true)
		bobB := p.sendAt(campaignB, bob, now.Add(-time.Minute), false)
		p.setEnrollment(eb.ID, map[string]any{
			"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
			"position": 3, "last_step_id": stepB, "round_start_at": round,
		})
		if paused {
			p.setProgram(programID, map[string]any{"paused_at": now})
		}

		p.updateSteps(programID, p.keepStep(stepA, 0), p.keepStep(stepC, 1))

		for name, id := range map[string]uuid.UUID{"alice": queuedB, "dave": laterB, "bob": bobB} {
			if p.campaignRecipient(id).CancelledAt == nil {
				t.Fatalf("paused=%v: the queued send of the removed template to %s must be cancelled", paused, name)
			}
		}
		if row := p.campaignRecipient(sentA); row.CancelledAt != nil || row.SentAt == nil {
			t.Fatalf("paused=%v: a sent send stays in the history", paused)
		}
		row := p.enrollment(ea.ID)
		if row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil || row.NextDueAt.After(time.Now().Add(time.Second)) {
			t.Fatalf("paused=%v: alice must be due now, got %s %v", paused, row.Status, row.NextDueAt)
		}
		if row.RoundStartAt == nil || !row.RoundStartAt.Equal(round) {
			t.Fatalf("paused=%v: alice keeps the round of the cancelled send, got %v want %v", paused, row.RoundStartAt, round)
		}
		if row := p.enrollment(ed.ID); row.NextDueAt == nil || !row.NextDueAt.Equal(now.Add(10*time.Hour)) {
			t.Fatalf("paused=%v: dave must be due when his cancelled send would have gone out, got %v", paused, row.NextDueAt)
		}
		if row := p.enrollment(eb.ID); row.Status != model.ProgramEnrollmentCompleted {
			t.Fatalf("paused=%v: bob had every template left and stays completed, got %s", paused, row.Status)
		}
		if paused {
			continue
		}

		// the next tick gives alice C and completes her round with one fewer
		row = p.enrollment(ea.ID)
		p.advance(programID, &row)
		if rows := p.sendsOf(campaignC, alice); len(rows) != 1 || rows[0].CancelledAt != nil {
			t.Fatalf("expected alice to get the remaining template, got %d", len(rows))
		}
		if rows := p.sendsOf(campaignB, alice); len(rows) != 1 || rows[0].CancelledAt == nil {
			t.Fatalf("the removed template must not be sent again")
		}
		if row := p.enrollment(ea.ID); row.Status != model.ProgramEnrollmentCompleted || row.Position != 2 {
			t.Fatalf("expected alice completed after two templates, got %s %d", row.Status, row.Position)
		}
		// later picks never choose the removed template
		program := p.loadProgram(programID)
		for _, st := range program.Steps {
			if st.ID.MustGet() == stepB {
				t.Fatalf("the removed step must be gone")
			}
		}
	}
}

// TestEditChangeTemplateIsRemoveAndAdd checks a step whose template changes
// in the same position becomes a new step with its own hidden campaign: the
// queued send of the old template is cancelled, the person gets the new one at
// the next tick, and a completed person who had the old one is active again
// for the new one
func TestEditChangeTemplateIsRemoveAndAdd(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	now := time.Now().UTC().Truncate(time.Second)
	round := now.Add(-time.Hour)
	interval := model.ProgramDefaultIntervalMinutes * time.Minute

	alice := p.recipient("alice@example.com")
	ea := p.enroll(programID, alice, 0)
	p.sendAt(campaignA, alice, round.Add(-interval), true)
	queuedB := p.sendAt(campaignB, alice, now.Add(-time.Minute), false)
	p.queuedAfterSend(ea, stepB, now.Add(-time.Minute), round, 2)
	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.sendAt(campaignA, bob, round.Add(-2*interval), true)
	sentB := p.sendAt(campaignB, bob, round.Add(-interval), true)
	p.setEnrollment(eb.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
		"position": 2, "last_step_id": stepB, "round_start_at": round.Add(-interval),
	})

	other := p.template()
	changed := newStep(other, 1)
	changed.ID.Set(stepB)
	p.updateSteps(programID, p.keepStep(stepA, 0), changed)

	program := p.loadProgram(programID)
	if len(program.Steps) != 2 || program.Steps[1].ID.MustGet() == stepB || program.Steps[1].CampaignTemplateID.MustGet() != other {
		t.Fatalf("a new template in the same position must be a new step")
	}
	newStepID := program.Steps[1].ID.MustGet()
	if p.campaignRecipient(queuedB).CancelledAt == nil {
		t.Fatalf("the queued send of the old template must be cancelled")
	}
	if row := p.campaignRecipient(sentB); row.CancelledAt != nil {
		t.Fatalf("a sent send of the old template stays in the history")
	}
	row := p.enrollment(ea.ID)
	if row.NextDueAt == nil || row.NextDueAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("alice must be due now, got %v", row.NextDueAt)
	}
	if row := p.enrollment(eb.ID); row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("bob has the new template left and must be active, got %s", row.Status)
	}

	newCampaign := p.stepCampaignOfStep(programID, newStepID, 0)
	if newCampaign == campaignB {
		t.Fatalf("the new step must have its own hidden campaign")
	}
	p.advance(programID, &row)
	if rows := p.sendsOf(newCampaign, alice); len(rows) != 1 {
		t.Fatalf("expected alice to get the new template, got %d", len(rows))
	}
}

// TestEditAddTemplate checks a template added to a running program: active
// people keep their next send time and get it later in the round, in its
// position in same order. A completed person with the new template left is
// active again in stop mode, every completed person in start over mode.
func TestEditAddTemplate(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	now := time.Now().UTC().Truncate(time.Second)
	interval := model.ProgramDefaultIntervalMinutes * time.Minute

	alice := p.recipient("alice@example.com")
	ea := p.enroll(programID, alice, 0)
	p.sendAt(campaignA, alice, now.Add(-interval), true)
	aliceDue := now.Add(time.Hour)
	p.setEnrollment(ea.ID, map[string]any{
		"last_send_at": now.Add(-interval), "last_step_id": stepA, "round_start_at": aliceDue,
		"next_due_at": aliceDue, "position": 1,
	})
	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.sendAt(campaignA, bob, now.Add(-2*interval), true)
	p.sendAt(campaignB, bob, now.Add(-interval), true)
	p.setEnrollment(eb.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil, "position": 2,
	})

	added := p.template()
	p.updateSteps(programID, p.keepStep(stepA, 0), newStep(added, 1), p.keepStep(stepB, 2))

	row := p.enrollment(ea.ID)
	if row.NextDueAt == nil || !row.NextDueAt.Equal(aliceDue) {
		t.Fatalf("an active person keeps their next send time, got %v want %v", row.NextDueAt, aliceDue)
	}
	if row := p.enrollment(eb.ID); row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("a completed person with the new template left must be active, got %s", row.Status)
	}
	program := p.loadProgram(programID)
	addedStep := program.Steps[1].ID.MustGet()
	addedCampaign := p.stepCampaignOfStep(programID, addedStep, 0)
	// same order: alice gets the new template next, it sits before B
	if err := p.svc.advanceEnrollment(p.ctx, nil, program, &row, aliceDue, nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if rows := p.sendsOf(addedCampaign, alice); len(rows) != 1 {
		t.Fatalf("expected the added template next in its position, got %d", len(rows))
	}
	if rows := p.sendsOf(campaignB, alice); len(rows) != 0 {
		t.Fatalf("B comes after the added template in same order")
	}

	// start over: every completed person comes back, also one who had all
	q := newProgramWriteTest(t)
	loopID := q.program(model.ProgramLoopLoop)
	only := q.step(loopID, 0, true)
	carol := q.recipient("carol@example.com")
	ec := q.enroll(loopID, carol, 0)
	q.sendAt(q.stepCampaign(loopID, only, 0), carol, now.Add(-interval), true)
	q.setEnrollment(ec.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil, "position": 1,
	})
	q.updateSteps(loopID, q.keepStep(only, 0), newStep(q.template(), 1))
	if row := q.enrollment(ec.ID); row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("start over brings every completed person back, got %s", row.Status)
	}
}

// TestEditAddGroup checks members of a group added to a running program are
// enrolled on the next membership pass after the new member delay, and a
// member already enrolled through another group keeps their enrollment
func TestEditAddGroup(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"new_member_delay_minutes": 3 * 24 * 60})
	p.step(programID, 0, true)
	first := p.group("first")
	second := p.group("second")
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	p.addToGroup(first, alice)
	p.addToGroup(second, alice)
	p.addToGroup(second, bob)
	p.assignGroup(programID, first)
	ea := p.enroll(programID, alice, 0)
	aliceDue := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	p.setEnrollment(ea.ID, map[string]any{"next_due_at": aliceDue})

	p.updateGroups(programID, first, second)
	if err := p.svc.resolveEnrollments(p.ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	eb := p.enrollmentOf(programID, bob)
	if eb.Status != model.ProgramEnrollmentActive || eb.NextDueAt == nil ||
		eb.NextDueAt.Before(time.Now().Add(3*24*time.Hour-time.Minute)) {
		t.Fatalf("a new member waits the new member delay, got %s %v", eb.Status, eb.NextDueAt)
	}
	row := p.enrollmentOf(programID, alice)
	if *row.ID != *ea.ID || row.NextDueAt == nil || !row.NextDueAt.Equal(aliceDue) {
		t.Fatalf("a member already enrolled is unchanged, got %v", row.NextDueAt)
	}
}

// TestEditRemoveGroup checks members only in a removed group are marked
// missing, exited after the grace period with their unsent sends cancelled
// and their history kept, members still in another group of the program are
// unaffected, and a member whose group is added again comes back
func TestEditRemoveGroup(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	first := p.group("first")
	second := p.group("second")
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	p.addToGroup(first, alice)
	p.addToGroup(first, bob)
	p.addToGroup(second, bob)
	p.assignGroup(programID, first)
	p.assignGroup(programID, second)
	ea := p.enroll(programID, alice, 0)
	eb := p.enroll(programID, bob, 0)
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	p.setEnrollment(ea.ID, map[string]any{"next_due_at": future})
	p.setEnrollment(eb.ID, map[string]any{"next_due_at": future})
	aliceSent := p.sendAt(campaignA, alice, now.Add(-24*time.Hour), true)
	aliceQueued := p.sendAt(campaignB, alice, future, false)
	bobQueued := p.sendAt(campaignB, bob, future, false)

	p.updateGroups(programID, second)
	if err := p.svc.resolveEnrollments(p.ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	row := p.enrollment(ea.ID)
	if row.Status != model.ProgramEnrollmentActive || row.MissingSince == nil {
		t.Fatalf("alice must be marked missing first, got %s %v", row.Status, row.MissingSince)
	}
	if p.campaignRecipient(aliceQueued).CancelledAt != nil {
		t.Fatalf("within the grace period the send is kept")
	}
	// the grace period passed
	p.setEnrollment(ea.ID, map[string]any{"missing_since": now.Add(-2 * programMissingGrace)})
	if err := p.svc.resolveEnrollments(p.ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if row := p.enrollment(ea.ID); row.Status != model.ProgramEnrollmentExited {
		t.Fatalf("alice must be exited after the grace period, got %s", row.Status)
	}
	if p.campaignRecipient(aliceQueued).CancelledAt == nil {
		t.Fatalf("the unsent send of a leaver must be cancelled")
	}
	if row := p.campaignRecipient(aliceSent); row.SentAt == nil || row.CancelledAt != nil {
		t.Fatalf("the history of a leaver is kept")
	}
	if row := p.enrollment(eb.ID); row.Status != model.ProgramEnrollmentActive || row.MissingSince != nil {
		t.Fatalf("bob is still in a program group and must be unaffected, got %s %v", row.Status, row.MissingSince)
	}
	if p.campaignRecipient(bobQueued).CancelledAt != nil {
		t.Fatalf("bob keeps his queued send")
	}

	// the group is added again: alice returns with the same enrollment
	p.updateGroups(programID, first, second)
	if err := p.svc.resolveEnrollments(p.ctx); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	row = *p.enrollmentOf(programID, alice)
	if *row.ID != *ea.ID || row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("alice must return and continue, got %s %v", row.Status, row.NextDueAt)
	}
}

// TestEditTimingKeepsQueuedSends checks a timing change draws the next send of
// an active person again but leaves a send already queued at its time
func TestEditTimingKeepsQueuedSends(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	alice := p.recipient("alice@example.com")
	ea := p.enroll(programID, alice, 0)
	now := time.Now().UTC().Truncate(time.Second)
	queuedAt := now.Add(30 * time.Minute)
	queued := p.sendAt(campaignA, alice, queuedAt, false)
	oldDue := p.queuedAfterSend(ea, stepA, queuedAt, now, 1)

	update := &model.Program{}
	update.StepIntervalMinutes.Set(2 * 24 * 60)
	if err := p.update(programID, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	row := p.enrollment(ea.ID)
	if row.NextDueAt == nil || row.NextDueAt.Equal(oldDue) {
		t.Fatalf("the next send must be drawn again, got %v", row.NextDueAt)
	}
	if minNext := queuedAt.Add(24 * time.Hour); row.NextDueAt.Before(minNext) {
		t.Fatalf("the next send keeps half the new interval after the queued one, got %v", row.NextDueAt)
	}
	if got := p.campaignRecipient(queued); got.CancelledAt != nil || got.SendAt == nil || !got.SendAt.Equal(queuedAt) {
		t.Fatalf("a queued send keeps its time, got %v", got.SendAt)
	}
}

// TestEditEndEarlierCancelsLaterSends checks an earlier end set through the
// update cancels the sends planned at or after it and keeps those before it
func TestEditEndEarlierCancelsLaterSends(t *testing.T) {
	p := newProgramWriteTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	end := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	kept := p.sendAt(campaignA, p.recipient("a@example.com"), end.Add(-time.Hour), false)
	dropped := p.sendAt(campaignA, p.recipient("b@example.com"), end.Add(time.Hour), false)

	update := &model.Program{}
	update.EndAt.Set(end)
	if err := p.update(programID, update); err != nil {
		t.Fatalf("update: %v", err)
	}
	if p.campaignRecipient(kept).CancelledAt != nil {
		t.Fatalf("a send before the new end must be kept")
	}
	if p.campaignRecipient(dropped).CancelledAt == nil {
		t.Fatalf("a send after the new end must be cancelled")
	}
}
