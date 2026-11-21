package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// newProgramWriteTest is a lifecycle test with the template service a create
// and an update read the templates through
func newProgramWriteTest(t *testing.T) *programTest {
	t.Helper()
	p := newProgramLifecycleTest(t)
	p.svc.CampaignTemplateService = &CampaignTemplate{
		Common:                     p.svc.Common,
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: p.db},
	}
	return p
}

// stepTemplate returns the template of a step
func (p *programTest) stepTemplate(stepID uuid.UUID) uuid.UUID {
	p.t.Helper()
	var row database.ProgramStep
	if err := p.db.Where("id = ?", stepID).First(&row).Error; err != nil {
		p.t.Fatalf("load step: %v", err)
	}
	return *row.CampaignTemplateID
}

// group stores an empty static recipient group
func (p *programTest) group(name string) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.RecipientGroup{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: name})
	return id
}

// newProgramRequest is a create request with one template and one group
func newProgramRequest(t *testing.T, name string, templateID, groupID uuid.UUID) *model.Program {
	t.Helper()
	draft := true
	return &model.Program{
		Name:              nullable.NewNullableWithValue(*mustString64(t, name)),
		CompanyID:         nullable.NewNullNullable[uuid.UUID](),
		Draft:             &draft,
		Steps:             []*model.ProgramStep{{CampaignTemplateID: nullable.NewNullableWithValue(templateID)}},
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&groupID}),
	}
}

// TestProgramPauseResumeGuards checks a pause of a paused program and a
// resume of a program that is not paused are refused and change nothing
func TestProgramPauseResumeGuards(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)

	if err := p.svc.resumeWait(p.ctx, session, &programID); err == nil || !strings.Contains(err.Error(), "not paused") {
		t.Fatalf("a resume of a running program must be refused, got %v", err)
	}
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	pausedAt := p.loadProgram(programID).PausedAt.MustGet()
	if err := p.svc.Pause(p.ctx, session, &programID); err == nil || !strings.Contains(err.Error(), "already paused") {
		t.Fatalf("a second pause must be refused, got %v", err)
	}
	if got := p.loadProgram(programID).PausedAt.MustGet(); !got.Equal(pausedAt) {
		t.Fatalf("a refused pause must keep the pause time: %v %v", got, pausedAt)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatal("expected the program running again")
	}
}

// TestProgramCreateUpdateAtomic checks a failed child write leaves no program
// on create and the stored program unchanged on update
func TestProgramCreateUpdateAtomic(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	existingID := p.program(model.ProgramLoopComplete)
	templateID := p.stepTemplate(p.step(existingID, 0, true))
	groupID := p.group("staff")

	// the settings table is gone, so the last child write fails
	if err := p.db.Migrator().DropTable(&database.ProgramWebhook{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	req := newProgramRequest(t, "half written", templateID, groupID)
	req.Webhooks = nullable.NewNullableWithValue([]*model.CampaignWebhook{})
	if _, err := p.svc.Create(p.ctx, session, req); err == nil {
		t.Fatal("expected the create to fail")
	}
	var count int64
	p.db.Model(&database.Program{}).Where("name = ?", "half written").Count(&count)
	if count != 0 {
		t.Fatal("a program must not be left without its settings")
	}
	var steps int64
	p.db.Model(&database.ProgramStep{}).Count(&steps)
	if steps != 1 {
		t.Fatalf("the steps of the failed create must be rolled back, got %d", steps)
	}

	before := p.loadProgram(existingID)
	update := &model.Program{
		Name:     nullable.NewNullableWithValue(*mustString64(t, "renamed")),
		Webhooks: nullable.NewNullableWithValue([]*model.CampaignWebhook{}),
	}
	if err := p.svc.UpdateByID(p.ctx, session, &existingID, update); err == nil {
		t.Fatal("expected the update to fail")
	}
	after := p.loadProgram(existingID)
	if after.Name.MustGet().String() != before.Name.MustGet().String() {
		t.Fatalf("the program row must be rolled back, name %q", after.Name.MustGet().String())
	}
}

// TestProgramNameUnique checks a create, an update and a clone refuse a name
// another program of the scope has with a validation error
func TestProgramNameUnique(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	firstID := p.program(model.ProgramLoopComplete)
	templateID := p.stepTemplate(p.step(firstID, 0, true))
	groupID := p.group("staff")
	p.create(&database.ProgramRecipientGroup{ProgramID: &firstID, RecipientGroupID: &groupID})
	firstName := p.loadProgram(firstID).Name.MustGet().String()

	if _, err := p.svc.Create(p.ctx, session, newProgramRequest(t, firstName, templateID, groupID)); err == nil ||
		!isValidation(err) {
		t.Fatalf("a duplicate name on create must be a validation error, got %v", err)
	}
	secondID, err := p.svc.Create(p.ctx, session, newProgramRequest(t, "second", templateID, groupID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rename := &model.Program{Name: nullable.NewNullableWithValue(*mustString64(t, firstName))}
	if err := p.svc.UpdateByID(p.ctx, session, secondID, rename); err == nil || !isValidation(err) {
		t.Fatalf("a duplicate name on update must be a validation error, got %v", err)
	}
	// keeping its own name is no conflict
	same := &model.Program{Name: nullable.NewNullableWithValue(*mustString64(t, "second"))}
	if err := p.svc.UpdateByID(p.ctx, session, secondID, same); err != nil {
		t.Fatalf("an update keeping the name: %v", err)
	}
	if _, err := p.svc.Clone(p.ctx, session, &firstID, *vo.NewString64Must("second"), nil); err == nil || !isValidation(err) {
		t.Fatalf("a duplicate name on clone must be a validation error, got %v", err)
	}
}

// TestProgramListAndSingleReadAgree checks a single program read carries the
// same summary fields as the list
func TestProgramListAndSingleReadAgree(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, false)
	p.step(programID, 1, true)

	single, err := p.svc.GetByID(p.ctx, session, &programID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	list, err := p.svc.GetAll(p.ctx, session, nil, &repository.ProgramOption{QueryArgs: &vo.QueryArgs{}})
	if err != nil || len(list.Rows) != 1 {
		t.Fatalf("list: %v", err)
	}
	row := list.Rows[0]
	if single.SimulationCount != 2 || single.SimulationCount != row.SimulationCount {
		t.Fatalf("simulations: single %d list %d", single.SimulationCount, row.SimulationCount)
	}
	if !single.NeedsAttention || single.AttentionReason != row.AttentionReason || single.Status != row.Status {
		t.Fatalf("attention: single %v %q list %v %q", single.NeedsAttention, single.AttentionReason, row.NeedsAttention, row.AttentionReason)
	}
}

// TestProgramProgressAtCompletion checks a person who had the last template
// is shown with every template of the loop had
func TestProgramProgressAtCompletion(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	p.stepCampaign(programID, stepID, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)
	p.advance(programID, e)

	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentCompleted || row.Position != 1 || row.LastStepID == nil {
		t.Fatalf("a completed person must keep the template count: %s %d %v", row.Status, row.Position, row.LastStepID)
	}
	// a row stored before the count was kept shows it from the sends
	p.setEnrollment(e.ID, map[string]any{"position": 0})
	view, err := p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Rows) != 1 || view.Rows[0].Position != 1 || view.StepCount != 1 {
		t.Fatalf("progress: %+v", view.Rows)
	}
}

// TestProgramTemplateDeletedAfterClose checks a closed program keeps its
// results once a template it used is removed from it
func TestProgramTemplateDeletedAfterClose(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	templateID := p.stepTemplate(stepID)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID)
	for i := 0; i < 6; i++ {
		p.send(campaignID, p.recipient("p"+string(rune('a'+i))+"@example.com"), true)
	}
	p.setProgram(programID, map[string]any{"closed_at": time.Now().UTC()})
	if names, _ := p.svc.ProgramRepository.GetNamesByCampaignTemplateID(p.ctx, &templateID); len(names) != 0 {
		t.Fatalf("a closed program must not block the delete: %v", names)
	}
	// the template delete removes the closed program's step, then the
	// template and the campaign link to it
	if err := p.svc.ProgramRepository.RemoveCampaignTemplateFromClosedPrograms(p.ctx, &templateID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", nil)
	p.db.Where("id = ?", templateID).Delete(&database.CampaignTemplate{})

	results, err := p.svc.GetResults(p.ctx, session, &programID, "")
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if *results.Summary.Sent != 6 || len(results.Templates) != 1 || results.Templates[0].Name != programDeletedTemplateName {
		t.Fatalf("results after the delete: %+v %+v", results.Summary, results.Templates)
	}
	if _, err := p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{}); err != nil {
		t.Fatalf("people after the delete: %v", err)
	}
}
