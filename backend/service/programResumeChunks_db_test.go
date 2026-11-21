package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// pausedWithBacklog makes a paused program whose group holds n people, each
// with a held send whose time passed during the pause and a round that came
// due during it
func (p *programTest) pausedWithBacklog(n int) (uuid.UUID, uuid.UUID) {
	p.t.Helper()
	now := time.Now().UTC()
	past := now.Add(-48 * time.Hour)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"paused_at": now.Add(-72 * time.Hour)})
	stepID := p.step(programID, 0, true)
	p.step(programID, 1, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("paused_at", utils.RFC3339UTC(now))
	groupID := uuid.New()
	p.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "group " + groupID.String()})
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	recipients := make([]*database.Recipient, 0, n)
	links := make([]*database.RecipientGroupRecipient, 0, n)
	enrollments := make([]*database.ProgramEnrollment, 0, n)
	sends := make([]*database.CampaignRecipient, 0, n)
	for i := 0; i < n; i++ {
		id, eid, sid := uuid.New(), uuid.New(), uuid.New()
		email := id.String() + "@example.com"
		recipients = append(recipients, &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email})
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &groupID})
		enrollments = append(enrollments, &database.ProgramEnrollment{
			ID: &eid, CreatedAt: &now, UpdatedAt: &now, ProgramID: &programID, RecipientID: &id,
			RoundStartAt: &past, NextDueAt: &past, Status: model.ProgramEnrollmentActive,
		})
		sends = append(sends, &database.CampaignRecipient{ID: &sid, CreatedAt: &now, UpdatedAt: &now, CampaignID: &campaignID, RecipientID: &id})
	}
	for _, rows := range []any{recipients, links, enrollments, sends} {
		if err := p.db.CreateInBatches(rows, 500).Error; err != nil {
			p.t.Fatalf("create: %v", err)
		}
	}
	// stored as RFC3339 text, the same as the campaign send path
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("send_at", utils.RFC3339UTC(past))
	return programID, campaignID
}

// heldBefore counts the held sends of a campaign planned before t
func (p *programTest) heldBefore(campaignID uuid.UUID, t time.Time) int {
	p.t.Helper()
	n := 0
	for _, cr := range p.sendsIn(campaignID) {
		if cr.SentAt == nil && cr.CancelledAt == nil && cr.SendAt != nil && cr.SendAt.Before(t) {
			n++
		}
	}
	return n
}

// dueBefore counts the active people of a program due before t
func (p *programTest) dueBefore(programID uuid.UUID, t time.Time) int64 {
	p.t.Helper()
	var n int64
	p.db.Model(&database.ProgramEnrollment{}).
		Where("program_id = ? AND status = ? AND next_due_at < ?", programID, model.ProgramEnrollmentActive, t).
		Count(&n)
	return n
}

// TestProgramResumeOverChunks checks a resume with more held sends and due
// people than one chunk plans them all again, clears the pause on the program
// and its campaigns and lets the lock go
func TestProgramResumeOverChunks(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	n := repository.ProgramWriteChunk + 5
	programID, campaignID := p.pausedWithBacklog(n)
	before := time.Now().Add(-time.Second)
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if left := p.heldBefore(campaignID, before); left != 0 {
		t.Fatalf("expected every held send planned again, %d left in the past", left)
	}
	if left := p.dueBefore(programID, before); left != 0 {
		t.Fatalf("expected every due person planned again, %d left in the past", left)
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatal("expected the program resumed")
	}
	if p.campaignRow(campaignID).PausedAt != nil {
		t.Fatal("expected the campaign resumed")
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the resume must release the lock")
	}
}

// TestProgramResumeStopsWhenClosed checks a close that begins while a resume
// writes its chunks stops the rest, and the program and its campaigns stay
// paused so no held send goes out on its old time
func TestProgramResumeStopsWhenClosed(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	n := repository.ProgramWriteChunk + 5
	programID, campaignID := p.pausedWithBacklog(n)
	// the first held send write marks the program as being closed
	p.probe("update", database.CAMPAIGN_RECIPIENT_TABLE_NAME, func() {
		p.setProgram(programID, map[string]any{"closing_at": time.Now().UTC()})
	})
	before := time.Now().Add(-time.Second)
	// a close that ends a resume is not a failure of the resume
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("a resume a close ended must not fail, got %v", err)
	}
	if left := p.heldBefore(campaignID, before); left != n-repository.ProgramWriteChunk {
		t.Fatalf("expected only the first chunk moved, %d left in the past", left)
	}
	if left := p.dueBefore(programID, before); left != int64(n) {
		t.Fatalf("expected no person planned again, %d left in the past", left)
	}
	if !p.loadProgram(programID).IsPaused() || p.campaignRow(campaignID).PausedAt == nil {
		t.Fatal("expected the program and its campaign still paused")
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the resume must release the lock")
	}
}

// TestProgramResumeJoinersOutsideLock checks a resume with many people who
// joined the groups during the pause writes them in chunks, each under the
// lock for its own write only, so another writer gets the lock between two
// chunks, and every joiner is enrolled once
func TestProgramResumeJoinersOutsideLock(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	programID, _ := p.pausedWithBacklog(5)
	var link database.ProgramRecipientGroup
	if err := p.db.Where("program_id = ?", programID).First(&link).Error; err != nil {
		t.Fatalf("group link: %v", err)
	}
	joiners := 2*repository.ProgramWriteChunk + 5
	now := time.Now().UTC()
	recipients := make([]*database.Recipient, 0, joiners)
	links := make([]*database.RecipientGroupRecipient, 0, joiners)
	for i := 0; i < joiners; i++ {
		id := uuid.New()
		email := id.String() + "@example.com"
		recipients = append(recipients, &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email})
		links = append(links, &database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: link.RecipientGroupID})
	}
	for _, rows := range []any{recipients, links} {
		if err := p.db.CreateInBatches(rows, 500).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	// on the first enrollment insert another writer waits for the lock. It
	// gets the lock when the first chunk lets it go, before the next chunk
	var seen int64 = -1
	contender := make(chan struct{})
	var started atomic.Bool
	if err := p.db.Callback().Create().Before("gorm:begin_transaction").Register("test:contend", func(db *gorm.DB) {
		if db.Statement.Table != database.PROGRAM_ENROLLMENT_TABLE || started.Swap(true) {
			return
		}
		go func() {
			defer close(contender)
			_ = p.svc.withLifecycleLock(context.Background(), func() error {
				p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ?", programID).Count(&seen)
				return nil
			})
		}()
		time.Sleep(50 * time.Millisecond)
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	t.Cleanup(func() { _ = p.db.Callback().Create().Remove("test:contend") })

	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	<-contender
	total := int64(5 + joiners)
	if seen <= 5 || seen >= total {
		t.Fatalf("expected the lock let go between the joiner chunks, the other writer saw %d of %d people", seen, total)
	}
	var count int64
	p.db.Model(&database.ProgramEnrollment{}).Where("program_id = ? AND status = ?", programID, model.ProgramEnrollmentActive).Count(&count)
	if count != total {
		t.Fatalf("expected %d active people, got %d", total, count)
	}
	if p.loadProgram(programID).IsPaused() {
		t.Fatal("expected the program resumed")
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the resume must release the lock")
	}
}
