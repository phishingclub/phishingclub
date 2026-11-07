package service

import (
	"errors"
	"testing"

	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
)

func TestProcessReportedCSVRefusesAnonymousProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.db.Model(&database.Program{}).Where("id = ?", programID).Update("is_anonymous", true)
	step := p.step(programID, 0, true)
	campaign := p.stepCampaign(programID, step, 0)
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	p.send(campaign, alice, true)

	records := [][]string{{"email"}, {"alice@example.com"}}
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, records, 0, -1)
	var validation errs.ValidationError
	if out != nil || !errors.As(err, &validation) {
		t.Fatalf("expected a validation error, got %+v %v", out, err)
	}
	var count int64
	p.db.Model(&database.CampaignEvent{}).
		Where("event_id = ?", cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]).
		Count(&count)
	if count != 0 {
		t.Fatalf("a refused upload must record nothing, got %d reports", count)
	}
}
