package service

import (
	"strings"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestTickClosesProgramAtEndDate checks a program whose end date has passed is
// closed by the tick like a manual close: sends not yet sent are cancelled,
// sent ones are kept, the hidden campaign is closed and nobody is due. A
// program whose end is ahead keeps running.
func TestTickClosesProgramAtEndDate(t *testing.T) {
	p := newProgramServiceTest(t)
	ended := p.program(model.ProgramLoopComplete)
	running := p.program(model.ProgramLoopComplete)
	now := time.Now().UTC()
	p.setProgram(ended, map[string]any{"end_at": now.Add(-time.Minute)})
	p.setProgram(running, map[string]any{"end_at": now.Add(24 * time.Hour)})

	stepID := p.step(ended, 0, true)
	campaignID := p.stepCampaign(ended, stepID, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	e := p.enroll(ended, alice, 0)
	sent := p.sendAt(campaignID, bob, now.Add(-time.Hour), true)
	// the sender marks a sent row as attempted, which a campaign close checks
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", sent).
		Update("last_attempt_at", now.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("mark attempted: %v", err)
	}
	pending := p.sendAt(campaignID, alice, now.Add(time.Hour), false)

	// keep the tick to the end date and the due scan
	p.svc.lastMembershipAt = time.Now()
	p.svc.lastRetentionAt = time.Now()
	if err := p.svc.Tick(p.ctx, authorizedSession(t)); err != nil {
		t.Fatalf("tick: %v", err)
	}

	program := p.loadProgram(ended)
	if !program.IsClosed() || program.Status != model.ProgramStatusClosed {
		t.Fatalf("expected the program closed at its end date, status %s", program.Status)
	}
	if !program.EndedAutomatically {
		t.Fatalf("expected the program marked as ended by its end date")
	}
	if row := p.campaignRecipient(pending); row.CancelledAt == nil {
		t.Fatalf("a send not yet sent must be cancelled")
	}
	if row := p.campaignRecipient(sent); row.CancelledAt != nil || row.SentAt == nil {
		t.Fatalf("a sent send must be kept: cancelled %v sent %v", row.CancelledAt, row.SentAt)
	}
	var campaign database.Campaign
	if err := p.db.Where("id = ?", campaignID).First(&campaign).Error; err != nil {
		t.Fatalf("load campaign: %v", err)
	}
	if campaign.ClosedAt == nil {
		t.Fatalf("the hidden campaign must be closed")
	}
	if row := p.enrollment(e.ID); row.NextDueAt != nil {
		t.Fatalf("nobody may be due in a closed program")
	}
	if p.loadProgram(running).IsClosed() {
		t.Fatalf("a program whose end is ahead must keep running")
	}
	// a manual close is not marked as ended by date
	if p.loadProgram(running).EndedAutomatically {
		t.Fatalf("a running program is not ended")
	}
}

// TestAdvanceEnrollmentNoSendAtOrAfterEnd checks no send is planned at or after
// the end date. The person waits for the end instead.
func TestAdvanceEnrollmentNoSendAtOrAfterEnd(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	end := time.Now().UTC().Add(-time.Second)
	p.setProgram(programID, map[string]any{"end_at": end})
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	rid := p.recipient("dave@example.com")
	e := p.enroll(programID, rid, 0)

	p.advance(programID, e)
	if rows := p.sendsIn(campaignID); len(rows) != 0 {
		t.Fatalf("no send may be planned at or after the end, got %d", len(rows))
	}
	row := p.enrollment(e.ID)
	if row.NextDueAt == nil || row.NextDueAt.Sub(end).Abs() > time.Millisecond {
		t.Fatalf("expected the person to wait for the end, got %v want %v", row.NextDueAt, end)
	}
	if row.Status != model.ProgramEnrollmentActive || row.Position != 0 {
		t.Fatalf("the person must keep their place, got %s position %d", row.Status, row.Position)
	}

	// before the end the send is planned as usual
	p.setProgram(programID, map[string]any{"end_at": time.Now().UTC().Add(24 * time.Hour)})
	p.advance(programID, &row)
	if rows := p.sendsIn(campaignID); len(rows) != 1 {
		t.Fatalf("expected one send before the end, got %d", len(rows))
	}
}

// TestCancelUnsentFrom checks only sends not yet sent at or after the time are
// cancelled
func TestCancelUnsentFrom(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	end := time.Now().UTC().Add(48 * time.Hour)
	before := p.sendAt(campaignID, p.recipient("a@example.com"), end.Add(-time.Hour), false)
	at := p.sendAt(campaignID, p.recipient("b@example.com"), end, false)
	after := p.sendAt(campaignID, p.recipient("c@example.com"), end.Add(time.Hour), false)
	if err := p.svc.ProgramRepository.CancelUnsentFrom(p.ctx, &programID, end); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if p.campaignRecipient(before).CancelledAt != nil {
		t.Fatalf("a send before the end must be kept")
	}
	if p.campaignRecipient(at).CancelledAt == nil || p.campaignRecipient(after).CancelledAt == nil {
		t.Fatalf("sends at or after the end must be cancelled")
	}
}

// TestValidateEndAtUpdate checks the end of an update against the start the
// program will have and only asks a new end to be in the future
func TestValidateEndAtUpdate(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-30 * 24 * time.Hour)
	existing := &model.Program{
		StartAt: nullable.NewNullableWithValue(started),
		EndAt:   nullable.NewNullableWithValue(now.Add(-time.Minute)),
	}
	// an edit that leaves a passed end alone is allowed
	if changed, err := validateEndAtUpdate(&model.Program{}, existing, now); err != nil || changed {
		t.Fatalf("unchanged end: changed=%v err=%v", changed, err)
	}
	// a new end in the past is refused
	incoming := &model.Program{EndAt: nullable.NewNullableWithValue(now.Add(-time.Hour))}
	if _, err := validateEndAtUpdate(incoming, existing, now); err == nil || !strings.Contains(err.Error(), "in the future") {
		t.Fatalf("expected a past end refused, got %v", err)
	}
	// a new end in the future is a change
	incoming = &model.Program{EndAt: nullable.NewNullableWithValue(now.AddDate(1, 0, 0))}
	if changed, err := validateEndAtUpdate(incoming, existing, now); err != nil || !changed {
		t.Fatalf("future end: changed=%v err=%v", changed, err)
	}
	// clearing the end is allowed and is not a new end
	incoming = &model.Program{EndAt: nullable.NewNullNullable[time.Time]()}
	if changed, err := validateEndAtUpdate(incoming, existing, now); err != nil || changed {
		t.Fatalf("cleared end: changed=%v err=%v", changed, err)
	}
	// a scheduled program moved to start after its end is refused
	scheduled := &model.Program{
		StartAt: nullable.NewNullableWithValue(now.Add(24 * time.Hour)),
		EndAt:   nullable.NewNullableWithValue(now.Add(48 * time.Hour)),
	}
	incoming = &model.Program{StartAt: nullable.NewNullableWithValue(now.Add(72 * time.Hour))}
	if _, err := validateEndAtUpdate(incoming, scheduled, now); err == nil || !strings.Contains(err.Error(), "after the start") {
		t.Fatalf("expected a start after the end refused, got %v", err)
	}
}

// TestUpdateRefusesPastEnd checks the update path refuses a new end that has
// passed and stores nothing
func TestUpdateRefusesPastEnd(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	incoming := &model.Program{}
	incoming.Name.Set(*vo.NewString64Must("renamed"))
	incoming.EndAt.Set(time.Now().Add(-time.Hour))
	err := p.svc.UpdateByID(p.ctx, authorizedSession(t), &programID, incoming)
	if err == nil || !strings.Contains(err.Error(), "The end must be in the future") {
		t.Fatalf("expected a past end refused, got %v", err)
	}
	if p.loadProgram(programID).EndAtTime() != nil {
		t.Fatalf("the end must not be stored")
	}
}

// TestUpdateRefusesClosedProgramEnd checks a closed program cannot get an end
func TestUpdateRefusesClosedProgramEnd(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"closed_at": time.Now().UTC()})
	incoming := &model.Program{}
	incoming.Name.Set(*vo.NewString64Must("renamed"))
	incoming.EndAt.Set(time.Now().Add(24 * time.Hour))
	if err := p.svc.UpdateByID(p.ctx, authorizedSession(t), &programID, incoming); err == nil {
		t.Fatalf("a closed program cannot be changed")
	}
}
