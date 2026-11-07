package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// newProgramLifecycleTest is a program service test with the tables and
// repositories a resume and a delete use as well
func newProgramLifecycleTest(t *testing.T) *programTest {
	t.Helper()
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.CampaignWebhook{},
		&database.CampaignScript{},
		&database.CampaignAllowDeny{},
		&database.CampaignRecipientGroup{},
		&database.MicrosoftDeviceCode{},
		&database.ProgramAllowDeny{},
		&database.ProgramWebhook{},
		&database.CampaignStats{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.svc.RecipientGroupRepository = &repository.RecipientGroup{DB: p.db}
	p.svc.CampaignService.MicrosoftDeviceCodeRepository = &repository.MicrosoftDeviceCode{DB: p.db}
	return p
}

// cancelledContext returns a context that has already ended
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func mustString64(t *testing.T, v string) *vo.String64 {
	t.Helper()
	s, err := vo.NewString64(v)
	if err != nil {
		t.Fatalf("string64: %v", err)
	}
	return s
}

func (p *programTest) campaignRow(id uuid.UUID) database.Campaign {
	p.t.Helper()
	var row database.Campaign
	if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
		p.t.Fatalf("load campaign: %v", err)
	}
	return row
}

// TestProgramLifecyclePauseResumeCloseDelete walks a program through pause,
// resume, close and delete through the service, the way the API calls it
func TestProgramLifecyclePauseResumeCloseDelete(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)
	pseudonym := uuid.New()
	p.setEnrollment(e.ID, map[string]any{"anonymized_id": pseudonym})
	held := p.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)

	// a refused pause changes nothing
	if err := p.svc.Pause(p.ctx, unauthorizedSession(t), &programID); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused pause, got %v", err)
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatalf("a refused pause must not pause")
	}

	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if p.loadProgram(programID).Status != model.ProgramStatusPaused {
		t.Fatalf("expected paused")
	}
	if p.campaignRow(campaignID).PausedAt == nil {
		t.Fatalf("the hidden campaign must be paused with the program")
	}

	if err := p.svc.Resume(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if p.loadProgram(programID).Status != model.ProgramStatusActive {
		t.Fatalf("expected active after resume")
	}
	if p.campaignRow(campaignID).PausedAt != nil {
		t.Fatalf("the hidden campaign must send again after resume")
	}

	if err := p.svc.Close(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	program := p.loadProgram(programID)
	if !program.IsClosed() || program.IsPaused() {
		t.Fatalf("expected closed and not paused, got %s", program.Status)
	}
	if p.campaignRow(campaignID).ClosedAt == nil {
		t.Fatalf("the hidden campaign must be closed")
	}
	if p.campaignRecipient(held).CancelledAt == nil {
		t.Fatalf("a send not yet sent must be cancelled on close")
	}
	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentStopped || row.NextDueAt != nil || row.AnonymizedID != nil {
		t.Fatalf("expected the person stopped, not due and without pseudonym: %s %v %v", row.Status, row.NextDueAt, row.AnonymizedID)
	}
	if n, err := p.svc.ProgramRepository.CountActiveEnrollments(p.ctx, &programID); err != nil || n != 0 {
		t.Fatalf("nobody may be active in a closed program: %d %v", n, err)
	}

	// a closed program can not be paused, resumed, started or closed again
	if err := p.svc.Pause(p.ctx, session, &programID); err == nil {
		t.Fatalf("a closed program can not be paused")
	}
	if err := p.svc.Resume(p.ctx, session, &programID); err == nil {
		t.Fatalf("a closed program can not be resumed")
	}
	if err := p.svc.Start(p.ctx, session, &programID); err == nil {
		t.Fatalf("a closed program can not be started")
	}
	if err := p.svc.Close(p.ctx, session, &programID); err == nil {
		t.Fatalf("a closed program can not be closed again")
	}

	if err := p.svc.DeleteByID(p.ctx, session, &programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := p.svc.ProgramRepository.GetByID(p.ctx, &programID, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected the program gone, got %v", err)
	}
	var count int64
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Count(&count)
	if count != 0 {
		t.Fatalf("the hidden campaign must be deleted with the program")
	}
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ?", programID).Count(&count)
	if count != 0 {
		t.Fatalf("the enrollments must be deleted with the program")
	}
}

// TestProgramLifecycleWaitsForContext checks the lifecycle calls give up when
// the request ends while another call holds the lock
func TestProgramLifecycleWaitsForContext(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.svc.lifecycleMu.Lock()
	defer p.svc.lifecycleMu.Unlock()
	calls := map[string]func() error{
		"start":  func() error { return p.svc.Start(cancelledContext(), session, &programID) },
		"pause":  func() error { return p.svc.Pause(cancelledContext(), session, &programID) },
		"resume": func() error { return p.svc.Resume(cancelledContext(), session, &programID) },
		"close":  func() error { return p.svc.Close(cancelledContext(), session, &programID) },
		"delete": func() error { return p.svc.DeleteByID(cancelledContext(), session, &programID) },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("%s must give up when the request ends", name)
		}
	}
	if p.loadProgram(programID).Status != model.ProgramStatusActive {
		t.Fatalf("nothing may change")
	}
}

// TestProgramStartRefusesPassedEnd checks a draft starts now, and a program
// whose end has passed is not started
func TestProgramStartRefusesPassedEnd(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	groupID := uuid.New()
	now := time.Now().UTC()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group"})
	draft := func() uuid.UUID {
		id := p.program(model.ProgramLoopComplete)
		p.setProgram(id, map[string]any{"start_at": nil, "started_at": nil})
		p.create(&database.ProgramRecipientGroup{ProgramID: &id, RecipientGroupID: &groupID})
		return id
	}
	ended := draft()
	p.setProgram(ended, map[string]any{"end_at": now.Add(-time.Minute)})
	err := p.svc.Start(p.ctx, session, &ended)
	if err == nil || !strings.Contains(err.Error(), "The end has passed") {
		t.Fatalf("expected a start after the end refused, got %v", err)
	}
	if p.loadProgram(ended).StartAtTime() != nil {
		t.Fatalf("the refused start must not be stored")
	}
	ok := draft()
	p.setProgram(ok, map[string]any{"end_at": now.Add(48 * time.Hour)})
	if err := p.svc.Start(p.ctx, session, &ok); err != nil {
		t.Fatalf("start: %v", err)
	}
	if p.loadProgram(ok).Status != model.ProgramStatusActive {
		t.Fatalf("expected the draft started")
	}
}

// TestTickClosesDraftPastEnd checks a draft whose end passes is closed like
// any program
func TestTickClosesDraftPastEnd(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"start_at": nil, "started_at": nil, "end_at": time.Now().UTC().Add(-time.Minute)})
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !p.loadProgram(programID).IsClosed() {
		t.Fatalf("expected the draft closed at its end")
	}
}

// failEnrollmentStop makes the database refuse to move enrollments to stopped,
// so a close fails after its hidden campaigns are closed. The returned
// function removes the refusal.
func failEnrollmentStop(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	if err := db.Exec(`CREATE TRIGGER fail_stop BEFORE UPDATE OF status ON program_enrollments
		WHEN NEW.status = 'stopped' BEGIN SELECT RAISE(ABORT, 'stop refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	return func() {
		if err := db.Exec(`DROP TRIGGER fail_stop`).Error; err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	}
}

// TestProgramClosePartialFailureRetry checks a close that fails half way
// leaves a paused program that is not closed, and a second close finishes it
func TestProgramClosePartialFailureRetry(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)

	restore := failEnrollmentStop(t, p.db)
	if err := p.svc.Close(p.ctx, session, &programID); err == nil {
		t.Fatalf("expected the close to fail")
	}
	program := p.loadProgram(programID)
	if program.IsClosed() {
		t.Fatalf("a close that failed must not mark the program closed")
	}
	if program.Status != model.ProgramStatusPaused {
		t.Fatalf("a close that failed must leave the program paused, got %s", program.Status)
	}
	if p.campaignRow(campaignID).ClosedAt == nil {
		t.Fatalf("the steps before the failure are kept")
	}
	// the scheduler does not advance the half closed program
	due, err := p.svc.ProgramRepository.GetDueEnrollments(p.ctx, time.Now().Add(time.Hour), 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("a half closed program must not be due: %d %v", len(due), err)
	}

	restore()
	if err := p.svc.Close(p.ctx, session, &programID); err != nil {
		t.Fatalf("second close: %v", err)
	}
	program = p.loadProgram(programID)
	if !program.IsClosed() || program.IsPaused() {
		t.Fatalf("expected the second close to finish, got %s", program.Status)
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentStopped {
		t.Fatalf("expected the person stopped, got %s", row.Status)
	}
}

// TestTickRetriesFailedEndDateClose checks a failed close at the end date is
// tried again by the next tick
func TestTickRetriesFailedEndDateClose(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(-time.Minute)})
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()

	restore := failEnrollmentStop(t, p.db)
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if p.loadProgram(programID).IsClosed() {
		t.Fatalf("the failed close must not mark the program closed")
	}
	if _, ok := p.svc.closeFailureAuditAt[programID]; !ok {
		t.Fatalf("the failed close must be audited")
	}
	restore()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if !p.loadProgram(programID).IsClosed() {
		t.Fatalf("the next tick must finish the close")
	}
	if _, ok := p.svc.closeFailureAuditAt[programID]; ok {
		t.Fatalf("a finished close clears the failure mark")
	}
}

// TestProgramDeletePausesCampaignsFirst checks a delete that stops half way
// leaves the hidden campaigns paused, so nothing queued is sent
func TestProgramDeletePausesCampaignsFirst(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	if err := p.db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON campaigns
		BEGIN SELECT RAISE(ABORT, 'delete refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := p.svc.DeleteByID(p.ctx, session, &programID); err == nil {
		t.Fatalf("expected the delete to fail")
	}
	if p.campaignRow(campaignID).PausedAt == nil {
		t.Fatalf("the hidden campaign must be paused before it is deleted")
	}
	if err := p.db.Exec(`DROP TRIGGER fail_delete`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := p.svc.DeleteByID(p.ctx, session, &programID); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

// TestProgramResultsAfterAnonymousClose checks coverage is not shown for a
// closed program, also an anonymous one whose pseudonyms are gone
func TestProgramResultsAfterAnonymousClose(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	sentAt := time.Now().UTC().AddDate(0, 0, -10)
	for i := 0; i < 6; i++ {
		rid := p.recipient(uuid.NewString() + "@example.com")
		e := p.enroll(programID, rid, 0)
		pseudonym := uuid.New()
		p.setEnrollment(e.ID, map[string]any{"anonymized_id": pseudonym})
		id := p.sendAt(campaignID, rid, sentAt, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).
			Updates(map[string]any{"sent_at": sentAt, "anonymized_id": pseudonym})
	}
	open, err := p.svc.programResults(p.ctx, session, p.loadProgram(programID), time.Now(), nil, nil)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if open.Closed || open.Coverage == nil || open.Coverage.Active != 6 || open.Coverage.Covered != 6 {
		t.Fatalf("expected full coverage while open: %+v", open.Coverage)
	}
	if err := p.svc.Close(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	closed, err := p.svc.programResults(p.ctx, session, p.loadProgram(programID), time.Now(), nil, nil)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if !closed.Closed || closed.Coverage != nil {
		t.Fatalf("a closed program shows no coverage: closed=%v coverage=%+v", closed.Closed, closed.Coverage)
	}
	if closed.Months[0].Sent == nil || *closed.Months[0].Sent != 6 {
		t.Fatalf("the results stay after close: %+v", closed.Months[0])
	}
}

// TestReplanEndMovedLaterRedrawsParked checks people waiting at the old end
// are drawn again over the spread when the end moves later or is cleared, and
// people with a normal next send keep it
func TestReplanEndMovedLaterRedrawsParked(t *testing.T) {
	for _, clear := range []bool{false, true} {
		p := newProgramServiceTest(t)
		programID := p.program(model.ProgramLoopComplete)
		oldEnd := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
		p.setProgram(programID, map[string]any{"end_at": oldEnd, "spread_minutes": 7 * 24 * 60})
		p.step(programID, 0, true)
		p.step(programID, 1, true)
		alice := p.recipient("alice@example.com")
		bob := p.recipient("bob@example.com")
		parked := p.enroll(programID, alice, 0)
		p.setEnrollment(parked.ID, map[string]any{"next_due_at": oldEnd, "round_start_at": time.Now().UTC()})
		normal := p.enroll(programID, bob, 0)
		bobDue := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		p.setEnrollment(normal.ID, map[string]any{"next_due_at": bobDue})

		before := p.loadProgram(programID)
		if clear {
			p.setProgram(programID, map[string]any{"end_at": nil})
		} else {
			p.setProgram(programID, map[string]any{"end_at": oldEnd.AddDate(0, 1, 0)})
		}
		if err := p.svc.replanAfterEdit(p.ctx, before, &programID, false); err != nil {
			t.Fatalf("replan: %v", err)
		}
		row := p.enrollment(parked.ID)
		if row.NextDueAt == nil || row.NextDueAt.Equal(oldEnd) {
			t.Fatalf("clear=%v: the parked person must be drawn again, got %v", clear, row.NextDueAt)
		}
		if row := p.enrollment(normal.ID); row.NextDueAt == nil || !row.NextDueAt.Equal(bobDue) {
			t.Fatalf("clear=%v: a normal next send is kept, got %v", clear, row.NextDueAt)
		}
	}
}

// TestDeliveredStepsSurviveTemplateReadd checks a template removed and added
// again in the same loop is not sent twice to a person who had it
func TestDeliveredStepsSurviveTemplateReadd(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	step2 := p.step(programID, 1, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	program := p.loadProgram(programID)
	template1 := program.Steps[0].CampaignTemplateID.MustGet()
	p.db.Model(&database.Campaign{}).Where("id = ?", campaign1).Update("campaign_template_id", template1)
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	p.sendAt(campaign1, alice, time.Now().Add(-time.Hour), true)

	// the step of template 1 is removed and the template added again as a new
	// step, behind step 2
	readd := &model.ProgramStep{}
	readd.CampaignTemplateID.Set(template1)
	readd.Position.Set(1)
	keep := &model.ProgramStep{}
	keep.ID.Set(step2)
	keep.CampaignTemplateID.Set(program.Steps[1].CampaignTemplateID.MustGet())
	keep.Position.Set(0)
	if err := p.svc.ProgramRepository.ReplaceSteps(p.ctx, &programID, []*model.ProgramStep{keep, readd}); err != nil {
		t.Fatalf("replace steps: %v", err)
	}
	program = p.loadProgram(programID)
	deliveries, err := p.svc.ProgramRepository.GetLoopDeliveries(p.ctx, &programID, &alice, nil, 0)
	if err != nil {
		t.Fatalf("deliveries: %v", err)
	}
	left := undeliveredSteps(program.Steps, deliveredSteps(program.Steps, deliveries))
	if len(left) != 1 || left[0].ID.MustGet() != step2 {
		t.Fatalf("only step 2 is left, got %d steps", len(left))
	}
}

func TestDeliveredStepsCountsEachRemovedStepOnce(t *testing.T) {
	templateID := uuid.New()
	stepA, stepB, removed := uuid.New(), uuid.New(), uuid.New()
	steps := []*model.ProgramStep{
		{ID: nullable.NewNullableWithValue(stepA), CampaignTemplateID: nullable.NewNullableWithValue(templateID), Position: nullable.NewNullableWithValue(0)},
		{ID: nullable.NewNullableWithValue(stepB), CampaignTemplateID: nullable.NewNullableWithValue(templateID), Position: nullable.NewNullableWithValue(1)},
	}
	// one send of a removed step with the same template covers one step only,
	// also when the send is listed twice
	got := deliveredSteps(steps, []repository.ProgramDelivery{
		{StepID: removed, TemplateID: &templateID},
		{StepID: removed, TemplateID: &templateID},
	})
	if len(got) != 1 || !got[stepA] {
		t.Fatalf("expected one step covered, got %v", got)
	}
	// a send of a step still in the list counts for that step only
	got = deliveredSteps(steps, []repository.ProgramDelivery{{StepID: stepB, TemplateID: &templateID}})
	if len(got) != 1 || !got[stepB] {
		t.Fatalf("expected step B only, got %v", got)
	}
}

// TestProgramGroupCap checks the number of groups is capped on create, update
// and the audience count
func TestProgramGroupCap(t *testing.T) {
	ids := make([]*uuid.UUID, model.ProgramMaxRecipientGroups+1)
	for i := range ids {
		id := uuid.New()
		ids[i] = &id
	}
	p := &model.Program{}
	p.Name.Set(*mustString64(t, "capped"))
	p.RecipientGroupIDs = nullable.NewNullableWithValue(ids)
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("expected too many groups refused, got %v", err)
	}
	p.RecipientGroupIDs = nullable.NewNullableWithValue(ids[:model.ProgramMaxRecipientGroups])
	if err := p.Validate(); err != nil {
		t.Fatalf("the cap itself is allowed: %v", err)
	}
	s := newProgramServiceTest(t)
	if _, err := s.svc.CountAudience(s.ctx, authorizedSession(t), nil, ids); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("expected the audience count capped, got %v", err)
	}
}

// TestProgramAnonymousFunnelCountsPeopleOnce checks the anonymous funnel counts
// each person once across the steps of a program, by pseudonym, and leaves out
// sends that have not settled
func TestProgramAnonymousFunnelCountsPeopleOnce(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	step1 := p.step(programID, 0, true)
	step2 := p.step(programID, 1, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	campaign2 := p.stepCampaign(programID, step2, 0)
	settled := time.Now().UTC().AddDate(0, 0, -10)
	fresh := time.Now().UTC().Add(-time.Hour)
	addSend := func(campaignID, rid, pseudonym uuid.UUID, at time.Time) {
		id := p.sendAt(campaignID, rid, at, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).
			Updates(map[string]any{"sent_at": at, "anonymized_id": pseudonym})
	}
	for i := 0; i < 6; i++ {
		rid := p.recipient(uuid.NewString() + "@example.com")
		pseudonym := uuid.New()
		addSend(campaign1, rid, pseudonym, settled)
		addSend(campaign2, rid, pseudonym, settled.Add(time.Hour))
	}
	// a person sent only an hour ago is not counted yet
	addSend(campaign1, p.recipient("fresh@example.com"), uuid.New(), fresh)

	stats, err := p.svc.GetResultStats(p.ctx, session, &programID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Recipients != 6 || stats.EmailsSent != 6 {
		t.Fatalf("expected 6 people counted once, got recipients=%d sent=%d", stats.Recipients, stats.EmailsSent)
	}
}
