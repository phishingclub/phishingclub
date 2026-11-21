package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

const programDataSavingRefused = "submitted data cannot be saved on an anonymous program"

// newProgramDataSavingTest is a write test that can also create hidden
// campaigns, which read the template through the campaign service
func newProgramDataSavingTest(t *testing.T) *programTest {
	t.Helper()
	p := newProgramWriteTest(t)
	p.svc.CampaignService.CampaignTemplateService = p.svc.CampaignTemplateService
	if cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA] == nil {
		id := uuid.New()
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA] = &id
	}
	return p
}

// createDataSavingProgram creates a draft program through the service
func (p *programTest) createDataSavingProgram(name string, anonymous bool, save bool) uuid.UUID {
	p.t.Helper()
	templateID := p.stepTemplate(p.step(p.program(model.ProgramLoopComplete), 0, true))
	req := newProgramRequest(p.t, name, templateID, p.group(name+" group"))
	req.IsAnonymous = nullable.NewNullableWithValue(anonymous)
	req.SaveSubmittedData = nullable.NewNullableWithValue(save)
	id, err := p.svc.Create(p.ctx, authorizedSession(p.t), req)
	if err != nil {
		p.t.Fatalf("create: %v", err)
	}
	return *id
}

// stepCampaignOf creates the hidden campaign of the first step of a program
func (p *programTest) stepCampaignOf(programID uuid.UUID, loop int) uuid.UUID {
	p.t.Helper()
	program := p.loadProgram(programID)
	c, err := p.svc.getOrCreateStepCampaign(p.ctx, authorizedSession(p.t), program, program.Steps[0], loop, nil)
	if err != nil {
		p.t.Fatalf("step campaign: %v", err)
	}
	return c.ID.MustGet()
}

func (p *programTest) campaignSavesData(campaignID uuid.UUID) bool {
	p.t.Helper()
	var row database.Campaign
	if err := p.db.Where("id = ?", campaignID).First(&row).Error; err != nil {
		p.t.Fatalf("load campaign: %v", err)
	}
	return row.SaveSubmittedData
}

// TestProgramDataSavingAnonymousRule checks submitted data can not be saved on
// an anonymous program, refused on create, when turned on for an anonymous
// program and when a program that saves data is made anonymous
func TestProgramDataSavingAnonymousRule(t *testing.T) {
	p := newProgramDataSavingTest(t)
	session := authorizedSession(t)

	both := &model.Program{
		Name:              nullable.NewNullableWithValue(*mustString64(t, "both")),
		IsAnonymous:       nullable.NewNullableWithValue(true),
		SaveSubmittedData: nullable.NewNullableWithValue(true),
	}
	if err := both.Validate(); err == nil || !strings.Contains(err.Error(), programDataSavingRefused) {
		t.Fatalf("validate must refuse data saving on an anonymous program, got %v", err)
	}

	templateID := p.stepTemplate(p.step(p.program(model.ProgramLoopComplete), 0, true))
	req := newProgramRequest(t, "anonymous saving", templateID, p.group("staff"))
	req.IsAnonymous = nullable.NewNullableWithValue(true)
	req.SaveSubmittedData = nullable.NewNullableWithValue(true)
	if _, err := p.svc.Create(p.ctx, session, req); err == nil || !strings.Contains(err.Error(), programDataSavingRefused) {
		t.Fatalf("create must refuse data saving on an anonymous program, got %v", err)
	}

	anonID := p.createDataSavingProgram("anonymous", true, false)
	turnOn := &model.Program{Name: p.loadProgram(anonID).Name, SaveSubmittedData: nullable.NewNullableWithValue(true)}
	if err := p.svc.UpdateByID(p.ctx, session, &anonID, turnOn); err == nil || !strings.Contains(err.Error(), programDataSavingRefused) {
		t.Fatalf("turning on data saving of an anonymous program must be refused, got %v", err)
	}
	if p.loadProgram(anonID).SaveSubmittedDataOn() {
		t.Fatal("a refused update must not store data saving")
	}

	savingID := p.createDataSavingProgram("saving", false, true)
	makeAnon := &model.Program{Name: p.loadProgram(savingID).Name, IsAnonymous: nullable.NewNullableWithValue(true)}
	if err := p.svc.UpdateByID(p.ctx, session, &savingID, makeAnon); err == nil || !strings.Contains(err.Error(), programDataSavingRefused) {
		t.Fatalf("making a program that saves data anonymous must be refused, got %v", err)
	}
	if p.loadProgram(savingID).IsAnonymousOn() {
		t.Fatal("a refused update must not make the program anonymous")
	}
	// both changed together in the allowed direction is fine
	switchBoth := &model.Program{
		Name:              p.loadProgram(savingID).Name,
		IsAnonymous:       nullable.NewNullableWithValue(true),
		SaveSubmittedData: nullable.NewNullableWithValue(false),
	}
	if err := p.svc.UpdateByID(p.ctx, session, &savingID, switchBoth); err != nil {
		t.Fatalf("anonymous with data saving off must be allowed: %v", err)
	}
}

// TestProgramDataSavingStepCampaigns checks a hidden campaign gets data saving
// from its program when it is created and follows a change while it is open,
// while a closed campaign keeps what it had
func TestProgramDataSavingStepCampaigns(t *testing.T) {
	p := newProgramDataSavingTest(t)
	session := authorizedSession(t)

	offID := p.createDataSavingProgram("off", false, false)
	if p.campaignSavesData(p.stepCampaignOf(offID, 0)) {
		t.Fatal("a program without data saving must create campaigns that do not save data")
	}

	programID := p.createDataSavingProgram("on", false, true)
	open := p.stepCampaignOf(programID, 0)
	if !p.campaignSavesData(open) {
		t.Fatal("the hidden campaign must save data when the program does")
	}
	closed := p.stepCampaignOf(programID, 1)
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", closed).Update("closed_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("close: %v", err)
	}

	off := &model.Program{Name: p.loadProgram(programID).Name, SaveSubmittedData: nullable.NewNullableWithValue(false)}
	if err := p.svc.UpdateByID(p.ctx, session, &programID, off); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	if p.campaignSavesData(open) {
		t.Fatal("an open hidden campaign must stop saving data when the program does")
	}
	if !p.campaignSavesData(closed) {
		t.Fatal("a closed hidden campaign must be left as it was")
	}

	on := &model.Program{Name: p.loadProgram(programID).Name, SaveSubmittedData: nullable.NewNullableWithValue(true)}
	if err := p.svc.UpdateByID(p.ctx, session, &programID, on); err != nil {
		t.Fatalf("turn on: %v", err)
	}
	if !p.campaignSavesData(open) {
		t.Fatal("an open hidden campaign must save data again when the program does")
	}
}

// TestProgramEventsSubmittedData checks the program events carry the
// submitted data only when the program saves it
func TestProgramEventsSubmittedData(t *testing.T) {
	for _, save := range []bool{true, false} {
		p := newProgramDataSavingTest(t)
		session := authorizedSession(t)
		programID := p.createDataSavingProgram("events", false, save)
		campaignID := p.stepCampaignOf(programID, 0)
		rid := p.recipient("alice@example.com")
		p.send(campaignID, rid, true)

		err := p.svc.CampaignService.emitScriptEvent(
			p.ctx, &campaignID, &rid, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA,
			map[string]interface{}{"username": "alice"},
		)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		view, err := p.svc.GetEvents(p.ctx, session, &programID, &vo.QueryArgs{
			OrderBy: repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
			Limit:   10,
		}, nil)
		if err != nil {
			t.Fatalf("events: %v", err)
		}
		submittedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
		found := false
		for _, ev := range view.Rows {
			if ev.EventID == nil || *ev.EventID != *submittedID {
				continue
			}
			found = true
			got := ""
			if ev.Data != nil {
				got = ev.Data.String()
			}
			if save && !strings.Contains(got, "alice") {
				t.Fatalf("saved data must be in the event, got %q", got)
			}
			if !save && got != "" {
				t.Fatalf("data must not be stored when saving is off, got %q", got)
			}
		}
		if !found {
			t.Fatalf("expected the submitted data event, save %v", save)
		}
	}
}

// TestBuildProgramCloneKeepsDataSaving checks a copy keeps data saving
func TestBuildProgramCloneKeepsDataSaving(t *testing.T) {
	companyA := uuid.New()
	src, _ := cloneTestSource(&companyA)
	src.Program.IsAnonymous = nullable.NewNullableWithValue(false)
	src.Program.SaveSubmittedData = nullable.NewNullableWithValue(true)
	clone, _ := BuildProgramClone(src, *vo.NewString64Must("copy"), &companyA)
	if !clone.SaveSubmittedDataOn() {
		t.Fatal("a copy must keep data saving")
	}
}
