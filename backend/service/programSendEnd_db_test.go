package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// TestSenderSkipsEndedProgram checks the campaign sender sends nothing of a
// hidden campaign whose program end has passed, while a program that runs
// goes on to the send
func TestSenderSkipsEndedProgram(t *testing.T) {
	p := newProgramLifecycleTest(t)
	p.svc.CampaignService.ProgramService = p.svc
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 1)
	campaignID := campaigns[0]
	recipients := []*model.CampaignRecipient{}
	for _, id := range sends {
		r, err := p.svc.CampaignRecipientRepository.GetByID(p.ctx, &id, &repository.CampaignRecipientOption{})
		if err != nil {
			t.Fatalf("load send: %v", err)
		}
		recipients = append(recipients, r)
	}

	// a running program goes past the end check, the test campaign has no
	// template so the send stops there
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(time.Hour)})
	err := p.svc.CampaignService.sendCampaignMessages(p.ctx, session, campaignID.String(), recipients)
	if err == nil || !strings.Contains(err.Error(), "no template") {
		t.Fatalf("a running program must reach the send, got %v", err)
	}

	// an end that has passed skips the whole batch of the campaign
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(-time.Minute)})
	if err := p.svc.CampaignService.sendCampaignMessages(p.ctx, session, campaignID.String(), recipients); err != nil {
		t.Fatalf("an ended program must skip the send without an error: %v", err)
	}
	for _, id := range sends {
		row := p.campaignRecipient(id)
		if row.SentAt != nil || row.CancelledAt != nil {
			t.Fatalf("a skipped send stays unsent for the close to cancel: %+v", row)
		}
	}
}

// TestIsEndedByID checks the end read of the sender
func TestIsEndedByID(t *testing.T) {
	p := newProgramServiceTest(t)
	repo := p.svc.ProgramRepository
	now := time.Now()
	programID := p.program(model.ProgramLoopComplete)
	check := func(want bool, id uuid.UUID) {
		t.Helper()
		got, err := repo.IsEndedByID(p.ctx, &id, now)
		if err != nil || got != want {
			t.Fatalf("ended = %v %v, want %v", got, err, want)
		}
	}
	// no end runs on
	check(false, programID)
	p.setProgram(programID, map[string]any{"end_at": now.UTC().Add(time.Minute)})
	check(false, programID)
	p.setProgram(programID, map[string]any{"end_at": now.UTC()})
	check(true, programID)
	// a program that is gone sends nothing more
	check(true, uuid.New())
}

// TestSenderPausesCampaignOfEndedProgram checks the sender pauses the hidden
// campaign of an ended program, so the due read leaves its rows out and they
// can not fill the batch of other campaigns. The tick keeps the pause while
// the program is past its end and not closed, a later end date resumes it,
// and the close clears it.
func TestSenderPausesCampaignOfEndedProgram(t *testing.T) {
	p := newProgramLifecycleTest(t)
	p.svc.CampaignService.ProgramService = p.svc
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	campaigns, sends := p.manyCampaigns(programID, 1)
	campaignID := campaigns[0]
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sends[0]).
		Update("send_at", time.Now().UTC().Add(-time.Minute))
	due := func() int {
		t.Helper()
		rows, err := p.svc.CampaignRecipientRepository.GetUnsendRecipientsForSending(p.ctx, 1000, &repository.CampaignRecipientOption{})
		if err != nil {
			t.Fatalf("due read: %v", err)
		}
		return len(rows)
	}
	pausedAt := func() *time.Time {
		t.Helper()
		var c database.Campaign
		if err := p.db.Where("id = ?", campaignID).First(&c).Error; err != nil {
			t.Fatalf("load campaign: %v", err)
		}
		return c.PausedAt
	}
	if due() != 1 {
		t.Fatal("the send must be due before the end")
	}
	recipient, err := p.svc.CampaignRecipientRepository.GetByID(p.ctx, &sends[0], &repository.CampaignRecipientOption{})
	if err != nil {
		t.Fatalf("load send: %v", err)
	}
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(-time.Minute)})
	if err := p.svc.CampaignService.sendCampaignMessages(p.ctx, session, campaignID.String(), []*model.CampaignRecipient{recipient}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if pausedAt() == nil || due() != 0 {
		t.Fatal("the campaign of an ended program must be paused and its rows left out of the due read")
	}
	// the tick keeps the pause while the program is past its end
	if _, err := p.svc.ProgramRepository.ReconcileCampaignPause(p.ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if pausedAt() == nil {
		t.Fatal("the tick must not resume the campaign of an ended program")
	}
	// an end moved later resumes it on the next tick
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(time.Hour)})
	if _, err := p.svc.ProgramRepository.ReconcileCampaignPause(p.ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if pausedAt() != nil || due() != 1 {
		t.Fatal("a later end date must resume the campaign")
	}
	// the tick also pauses the campaigns of a program found past its end
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(-time.Minute)})
	if _, err := p.svc.ProgramRepository.ReconcileCampaignPause(p.ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if pausedAt() == nil {
		t.Fatal("the tick must pause the campaign of an ended program")
	}
	// the close cancels the send and leaves the closed campaign unpaused
	if err := p.svc.closeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if pausedAt() != nil {
		t.Fatal("a closed campaign must not stay paused")
	}
	if row := p.campaignRecipient(sends[0]); row.CancelledAt == nil {
		t.Fatal("the close must cancel the send")
	}
}
