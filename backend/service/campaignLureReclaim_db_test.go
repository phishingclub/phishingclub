package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/lure"
	"github.com/phishingclub/phishingclub/model"
)

// TestLureCodeReclaimSkipsProgramSends checks a lure code held by a program
// send gives the plain conflict without reclaim, is never reclaimed, also
// when the reclaim is asked for, and that the refusal does not name the
// program. A code held by a normal campaign is
// still reclaimed.
func TestLureCodeReclaimSkipsProgramSends(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	campaignID := uuid.New()
	p.create(&database.Campaign{ID: &campaignID, CreatedAt: &now, UpdatedAt: &now, Name: "normal", SendStartAt: &past})
	target := p.send(campaignID, p.recipient("target@example.com"), false)

	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"name": "Secret program name"})
	hidden := p.stepCampaign(programID, p.step(programID, 0, true), 0)
	held := p.send(hidden, p.recipient("held@example.com"), true)
	programCode := lure.NewCustomCode("progcode").Display
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", held).Update("lure_code", programCode)

	// without reclaim the conflict is the plain one, with no detail on the
	// holder, as for a holder of another company
	plain, err := p.svc.CampaignService.SetLureCodeByCampaignRecipientID(p.ctx, session, &target, "progcode", false)
	if !errors.Is(err, ErrLureCodeTaken) || plain == nil || *plain != (LureCodeConflict{}) {
		t.Fatalf("a program send code must give the plain conflict, got %v %+v", err, plain)
	}
	conflict, err := p.svc.CampaignService.SetLureCodeByCampaignRecipientID(p.ctx, session, &target, "progcode", true)
	var ve errs.ValidationError
	if !errors.As(err, &ve) || conflict != nil {
		t.Fatalf("a program send code must be refused, got %v %+v", err, conflict)
	}
	if strings.Contains(err.Error(), "Secret") {
		t.Fatalf("the refusal must not name the program: %v", err)
	}
	if row := p.campaignRecipient(held); row.LureCode == nil || *row.LureCode != programCode {
		t.Fatalf("the program send must keep its code: %+v", row.LureCode)
	}

	other := uuid.New()
	p.create(&database.Campaign{ID: &other, CreatedAt: &now, UpdatedAt: &now, Name: "other", SendStartAt: &past})
	holder := p.send(other, p.recipient("holder@example.com"), true)
	normalCode := lure.NewCustomCode("normcode").Display
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", holder).Update("lure_code", normalCode)
	if _, err := p.svc.CampaignService.SetLureCodeByCampaignRecipientID(p.ctx, session, &target, "normcode", true); err != nil {
		t.Fatalf("a normal campaign code must be reclaimed: %v", err)
	}
	if row := p.campaignRecipient(holder); row.LureCode != nil {
		t.Fatalf("the previous holder must lose the code")
	}
	if row := p.campaignRecipient(target); row.LureCode == nil || *row.LureCode != normalCode {
		t.Fatalf("the target must hold the code: %+v", row.LureCode)
	}
}
