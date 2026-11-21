package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
)

// TestExportProgramData checks the recipient export reads only program sends
// of the recipient's company or global programs, never an anonymous program,
// and the company export gives events for a named program only
func TestExportProgramData(t *testing.T) {
	p := newProgramServiceTest(t)
	now := time.Now().UTC()
	companyID, otherCompany := uuid.New(), uuid.New()
	alice := p.recipient("alice@example.com")
	p.db.Model(&database.Recipient{}).Where("id = ?", alice).Update("company_id", companyID)

	mk := func(name string, company *uuid.UUID, anonymous bool) (uuid.UUID, uuid.UUID) {
		programID := p.program(model.ProgramLoopComplete)
		p.db.Model(&database.Program{}).Where("id = ?", programID).Updates(map[string]any{
			"name": name, "company_id": company, "is_anonymous": anonymous,
		})
		step := p.step(programID, 0, true)
		campaignID := p.stepCampaign(programID, step, 0)
		// a hidden campaign sends the template of its step
		var stepRow database.ProgramStep
		p.db.Where("id = ?", step).First(&stepRow)
		p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", stepRow.CampaignTemplateID)
		p.send(campaignID, alice, true)
		eventID := uuid.New()
		p.create(&database.CampaignEvent{
			ID: &eventID, CreatedAt: &now, UpdatedAt: &now,
			CampaignID: &campaignID, RecipientID: &alice, EventID: &eventID,
		})
		return programID, campaignID
	}
	mk("Own", &companyID, false)
	mk("Shared", nil, false)
	mk("Elsewhere", &otherCompany, false)
	mk("Anonymous", &companyID, true)

	recipients := &Recipient{
		Common:              Common{Logger: zap.NewNop().Sugar()},
		RecipientRepository: &repository.Recipient{DB: p.db},
	}
	sends, events, err := recipients.GetProgramExport(p.ctx, authorizedSession(t), &alice)
	if err != nil {
		t.Fatalf("recipient export: %v", err)
	}
	names := map[string]bool{}
	for _, s := range sends {
		names["send "+s.ProgramName] = true
	}
	for _, e := range events {
		names["event "+e.ProgramName] = true
	}
	for _, want := range []string{"send Own", "send Shared", "event Own", "event Shared"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	if len(names) != 4 {
		t.Fatalf("unexpected program data %v", names)
	}
	if _, _, err := recipients.GetProgramExport(p.ctx, unauthorizedSession(t), &alice); err == nil {
		t.Fatal("expected an unauthorized session to be refused")
	}

	sections, err := p.svc.CampaignService.GetProgramEventsExport(p.ctx, authorizedSession(t), &companyID)
	if err != nil {
		t.Fatalf("company export: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want the two company programs", len(sections))
	}
	for _, s := range sections {
		switch s.ProgramName {
		case "Anonymous":
			if !s.Anonymous || len(exportEvents(t, p.ctx, s)) != 0 {
				t.Fatalf("an anonymous program must have no events: %+v", s)
			}
		case "Own":
			events := exportEvents(t, p.ctx, s)
			if len(events) != 1 || !events[0].HasRecipient {
				t.Fatalf("named program events = %+v", events)
			}
			if name := s.TemplateByCampaign[events[0].CampaignID]; name == "" {
				t.Fatal("the event template is not named")
			}
		default:
			t.Fatalf("unexpected program %q", s.ProgramName)
		}
	}
}

// exportEvents reads every event of a program section of the company export
func exportEvents(t *testing.T, ctx context.Context, s *ProgramEventsExport) []repository.ProgramEventExportRow {
	t.Helper()
	out := []repository.ProgramEventExportRow{}
	err := s.EachEvent(ctx, true, func(row *repository.ProgramEventExportRow) error {
		out = append(out, *row)
		return nil
	})
	if err != nil {
		t.Fatalf("export events: %v", err)
	}
	return out
}
