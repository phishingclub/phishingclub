package service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// manySendsAt adds n unsent sends of new people to a campaign, all planned at
// the given time
func (p *programTest) manySendsAt(campaignID uuid.UUID, n int, at time.Time) {
	p.t.Helper()
	now := time.Now().UTC()
	rows := make([]*database.CampaignRecipient, 0, n)
	for i := 0; i < n; i++ {
		id, rid := uuid.New(), uuid.New()
		rows = append(rows, &database.CampaignRecipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, RecipientID: &rid})
	}
	if err := p.db.CreateInBatches(rows, 500).Error; err != nil {
		p.t.Fatalf("create sends: %v", err)
	}
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("send_at", utils.RFC3339UTC(at))
}

// unsentIn counts the sends of a campaign that are neither sent nor cancelled
func (p *programTest) unsentIn(campaignID uuid.UUID) int {
	p.t.Helper()
	n := 0
	for _, cr := range p.sendsIn(campaignID) {
		if cr.SentAt == nil && cr.CancelledAt == nil {
			n++
		}
	}
	return n
}

// lockFreeBetweenWrites reports, once the work is done, whether another
// caller got the lifecycle lock between two writes of campaign recipients.
// At the first write a waiter asks for the lock, a later write that finds the
// waiter done saw the lock let go in between.
func (p *programTest) lockFreeBetweenWrites() func() bool {
	p.t.Helper()
	var started, sawFree atomic.Bool
	got := make(chan struct{})
	name := "test:lock_waiter"
	hook := func(db *gorm.DB) {
		if db.Statement.Table != database.CAMPAIGN_RECIPIENT_TABLE_NAME {
			return
		}
		if !started.Swap(true) {
			go func() {
				p.svc.lifecycleMu.Lock()
				close(got)
				p.svc.lifecycleMu.Unlock()
			}()
			return
		}
		select {
		case <-got:
			sawFree.Store(true)
		default:
		}
	}
	if err := p.db.Callback().Update().Before("gorm:begin_transaction").Register(name, hook); err != nil {
		p.t.Fatalf("register hook: %v", err)
	}
	p.t.Cleanup(func() { _ = p.db.Callback().Update().Remove(name) })
	return func() bool {
		if started.Load() {
			select {
			case <-got:
			case <-time.After(5 * time.Second):
				p.t.Fatal("the lock waiter did not get the lock")
			}
		}
		return sawFree.Load()
	}
}

// TestUpdateEndCancelsWithLockFree checks a new end cancels every send planned
// at or after it in chunks, with the lifecycle lock let go between them, and
// keeps the sends before it
func TestUpdateEndCancelsWithLockFree(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	end := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	n := repository.ProgramWriteChunk + 5
	p.manySendsAt(campaignID, n, end.Add(24*time.Hour))
	before := p.sendAt(campaignID, p.recipient("before@example.com"), end.Add(-time.Hour), false)
	sawFree := p.lockFreeBetweenWrites()

	incoming := &model.Program{}
	incoming.Name.Set(*vo.NewString64Must("renamed"))
	incoming.EndAt.Set(end)
	if err := p.svc.UpdateByID(p.ctx, authorizedSession(t), &programID, incoming); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !sawFree() {
		t.Fatal("the cancel must let the lifecycle lock go between its chunks")
	}
	if left := p.unsentIn(campaignID); left != 1 {
		t.Fatalf("expected only the send before the end left, %d unsent", left)
	}
	if p.campaignRecipient(before).CancelledAt != nil {
		t.Fatal("a send before the end must be kept")
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the update must release the lock")
	}
}

// TestEndCancelStopsOnNewEnd checks the chunked cancel of an end stops when
// the program gets another end in between, as that edit cancels for its own
func TestEndCancelStopsOnNewEnd(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	end := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	p.setProgram(programID, map[string]any{"end_at": end})
	n := 2 * repository.ProgramWriteChunk
	p.manySendsAt(campaignID, n, end.Add(time.Hour))
	// the first chunk write moves the end later
	p.probe("update", database.CAMPAIGN_RECIPIENT_TABLE_NAME, func() {
		p.setProgram(programID, map[string]any{"end_at": end.Add(48 * time.Hour)})
	})
	cancelled, done, err := p.svc.cancelSendsFromEnd(p.ctx, &programID)
	if err != nil || done {
		t.Fatalf("expected the rest stopped: done %v err %v", done, err)
	}
	if cancelled != repository.ProgramWriteChunk {
		t.Fatalf("expected one chunk cancelled, got %d", cancelled)
	}
}

// TestResumeCancelsPastEndWhilePaused checks a resume cancels a held send at
// or after the end before it lets the program send again
func TestResumeCancelsPastEndWhilePaused(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID, campaignID := p.pausedWithBacklog(3)
	end := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)
	p.setProgram(programID, map[string]any{"end_at": end})
	late := p.sendAt(campaignID, p.recipient("late@example.com"), end.Add(24*time.Hour), false)
	if err := p.svc.resumeWait(p.ctx, authorizedSession(t), &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if p.campaignRecipient(late).CancelledAt == nil {
		t.Fatal("a held send after the end must be cancelled")
	}
	for _, cr := range p.sendsIn(campaignID) {
		if cr.SentAt == nil && cr.CancelledAt == nil && cr.SendAt != nil && !cr.SendAt.Before(end) {
			t.Fatalf("no send may be left at or after the end, got %v", cr.SendAt)
		}
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatal("expected the program resumed")
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the resume must release the lock")
	}
}

// TestResumeMembershipCancelsOnlyWhileAllowed checks the cancels of a resume
// for the people missing now and the people who left before check the
// program under the lock first, so an edit in between keeps their sends
func TestResumeMembershipCancelsOnlyWhileAllowed(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	missing := p.recipient("missing@example.com")
	left := p.recipient("left@example.com")
	p.enroll(programID, missing, 0)
	el := p.enroll(programID, left, 0)
	p.setEnrollment(el.ID, map[string]any{"status": model.ProgramEnrollmentExited})
	missingSend := p.sendAt(campaignID, missing, time.Now().Add(time.Hour), false)
	leftSend := p.sendAt(campaignID, left, time.Now().Add(time.Hour), false)
	refused := func(*model.Program) bool { return false }

	m := programResumeMembership{gone: []*uuid.UUID{&missing}}
	done, err := p.svc.finishResumeMembership(p.ctx, &programID, m, time.Now(), refused)
	if err != nil || done {
		t.Fatalf("expected the resume stopped: done %v err %v", done, err)
	}
	if p.campaignRecipient(missingSend).CancelledAt != nil {
		t.Fatal("a person missing now keeps the send when the program changed")
	}
	if done, err := p.svc.cancelGoneSends(p.ctx, &programID, refused); err != nil || done {
		t.Fatalf("expected the gone cancel stopped: done %v err %v", done, err)
	}
	if p.campaignRecipient(leftSend).CancelledAt != nil {
		t.Fatal("a person who left keeps the send when the program changed")
	}

	allowed := func(*model.Program) bool { return true }
	done, err = p.svc.finishResumeMembership(p.ctx, &programID, m, time.Now(), allowed)
	if err != nil || !done {
		t.Fatalf("expected the resume membership written: done %v err %v", done, err)
	}
	if p.campaignRecipient(missingSend).CancelledAt == nil || p.campaignRecipient(leftSend).CancelledAt == nil {
		t.Fatal("expected both sends cancelled")
	}
}
