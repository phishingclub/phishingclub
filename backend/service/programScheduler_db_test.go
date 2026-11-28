package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// programTest holds a program service on a temp SQLite database
type programTest struct {
	t   *testing.T
	ctx context.Context
	db  *gorm.DB
	svc *Program
}

func newProgramServiceTest(t *testing.T) *programTest {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "program.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Campaign{},
		&database.CampaignLanguageRoute{},
		&database.CampaignRecipient{},
		&database.CampaignEvent{},
		&database.CampaignTemplate{},
		&database.CampaignTemplateLanguage{},
		&database.CampaignTemplatePool{},
		&database.Recipient{},
		&database.Event{},
		&database.Program{},
		&database.ProgramLanguageRoute{},
		&database.ProgramStep{},
		&database.ProgramCampaign{},
		&database.ProgramRecipientGroup{},
		&database.ProgramEnrollment{},
		&database.ProgramAllowDeny{},
		&database.ProgramWebhook{},
		&database.MicrosoftDeviceCode{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// the many to many join of the campaign makes the webhook table without
	// its settings columns, as the scheduler reads them they are added here
	for _, field := range []string{"WebhookIncludeData", "WebhookEvents"} {
		if !db.Migrator().HasColumn(&database.CampaignWebhook{}, field) {
			if err := db.Migrator().AddColumn(&database.CampaignWebhook{}, field); err != nil {
				t.Fatalf("add webhook column: %v", err)
			}
		}
	}
	for _, name := range []string{
		data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED,
		data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
		data.EVENT_CAMPAIGN_CLOSED,
	} {
		if cache.EventIDByName[name] == nil {
			id := uuid.New()
			cache.EventIDByName[name] = &id
		}
	}
	log := zap.NewNop().Sugar()
	campaignService := &Campaign{
		Common:                      Common{Logger: log},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
	}
	svc := &Program{
		Common:                      Common{Logger: log},
		ProgramRepository:           &repository.Program{DB: db},
		CampaignRepository:          &repository.Campaign{DB: db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: db},
		RecipientRepository:         &repository.Recipient{DB: db},
		CampaignService:             campaignService,
	}
	// a background delete or close pass ends before the database is removed
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = svc.settle(ctx)
	})
	return &programTest{t: t, ctx: context.Background(), db: db, svc: svc}
}

func (p *programTest) create(row any) {
	p.t.Helper()
	if err := p.db.Create(row).Error; err != nil {
		p.t.Fatalf("create %T: %v", row, err)
	}
}

// program creates a started program and returns its id
func (p *programTest) program(loopMode string) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	started := now.Add(-time.Hour)
	p.create(&database.Program{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "program " + id.String(),
		LoopMode: loopMode, OrderMode: model.ProgramOrderSequential,
		StartAt: &started, StartedAt: &started,
	})
	return id
}

// step adds a scheduled step whose template is usable or not
func (p *programTest) step(programID uuid.UUID, position int, usable bool) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	templateID, urlID, stepID := uuid.New(), uuid.New(), uuid.New()
	p.create(&database.CampaignTemplate{
		ID: &templateID, CreatedAt: &now, UpdatedAt: &now, Name: "template " + templateID.String(),
		URLIdentifierID: &urlID, IsUsable: usable,
	})
	p.create(&database.ProgramStep{
		ID: &stepID, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &programID, CampaignTemplateID: &templateID, Position: position,
	})
	return stepID
}

// stepCampaign creates the open hidden campaign of a step and loop
func (p *programTest) stepCampaign(programID, stepID uuid.UUID, loop int) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	campaignID := uuid.New()
	p.create(&database.Campaign{
		ID: &campaignID, CreatedAt: &now, UpdatedAt: &now,
		Name: "campaign " + campaignID.String(), ProgramID: &programID,
	})
	linkID := uuid.New()
	p.create(&database.ProgramCampaign{
		ID: &linkID, CreatedAt: &now,
		ProgramID: &programID, ProgramStepID: &stepID, LoopIndex: loop, CampaignID: &campaignID,
	})
	return campaignID
}

func (p *programTest) recipient(email string) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	p.create(&database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email})
	return id
}

func (p *programTest) enroll(programID, recipientID uuid.UUID, loop int) *database.ProgramEnrollment {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	e := &database.ProgramEnrollment{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &programID, RecipientID: &recipientID,
		LoopCount: loop, RoundStartAt: &now, NextDueAt: &now,
		Status: model.ProgramEnrollmentActive,
	}
	p.create(e)
	return e
}

// send adds a campaign recipient row, sent or not
func (p *programTest) send(campaignID, recipientID uuid.UUID, sent bool) uuid.UUID {
	p.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	row := &database.CampaignRecipient{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		CampaignID: &campaignID, RecipientID: &recipientID,
	}
	if sent {
		row.SentAt = &now
	}
	p.create(row)
	return id
}

// waitForCloses waits for the background close of the programs at their end
// that a tick started
func (p *programTest) waitForCloses(t *testing.T) {
	t.Helper()
	if err := p.svc.waitForCloses(p.ctx); err != nil {
		t.Fatalf("wait for closes: %v", err)
	}
}

func (p *programTest) loadProgram(id uuid.UUID) *model.Program {
	p.t.Helper()
	program, err := p.svc.ProgramRepository.GetByID(p.ctx, &id, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		p.t.Fatalf("load program: %v", err)
	}
	program.SetReadState(time.Now())
	return program
}

func (p *programTest) enrollment(id *uuid.UUID) database.ProgramEnrollment {
	p.t.Helper()
	var row database.ProgramEnrollment
	if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
		p.t.Fatalf("load enrollment: %v", err)
	}
	return row
}

func (p *programTest) advance(programID uuid.UUID, e *database.ProgramEnrollment) {
	p.t.Helper()
	err := p.svc.advanceEnrollment(p.ctx, nil, p.loadProgram(programID), e, time.Now(), nil, nil)
	if err != nil {
		p.t.Fatalf("advance: %v", err)
	}
}

func (p *programTest) sendsIn(campaignID uuid.UUID) []database.CampaignRecipient {
	p.t.Helper()
	var rows []database.CampaignRecipient
	if err := p.db.Where("campaign_id = ?", campaignID).Find(&rows).Error; err != nil {
		p.t.Fatalf("load sends: %v", err)
	}
	return rows
}

// passMembers runs the writes of one membership pass the way
// resolveListedProgramWith does, from members and existing at now, without
// the group read and the lifecycle checks
func (p *programTest) passMembers(program *model.Program, members []*uuid.UUID, existing []*database.ProgramEnrollment, now time.Time) {
	p.t.Helper()
	programID := program.ID.MustGet()
	always := func(*model.Program) bool { return true }
	r := p.svc.planReturners(program, members, existing, now)
	r.joiners = p.svc.planJoiners(program, members, enrollmentsByRecipient(existing), now)
	if done, err := p.svc.writeReturners(p.ctx, r, always); err != nil || !done {
		p.t.Fatalf("write returners: done %v err %v", done, err)
	}
	if done, err := p.svc.writeLeavers(p.ctx, &programID, planLeavers(members, existing, now), now, always); err != nil || !done {
		p.t.Fatalf("write leavers: done %v err %v", done, err)
	}
	if _, err := p.svc.cancelGoneSends(p.ctx, &programID, nil); err != nil {
		p.t.Fatalf("cancel gone sends: %v", err)
	}
}

// TestResolveEnrollmentsMissingGraceExitReturn walks one person through leaving
// the groups: missing first, kept within the grace period, exited with the
// unsent send cancelled after it, and active again when they come back
func TestResolveEnrollmentsMissingGraceExitReturn(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	rid := p.recipient("alice@example.com")
	program := p.loadProgram(programID)
	members := []*uuid.UUID{&rid}

	now := time.Now()
	p.passMembers(program, members, nil, now)
	existing, err := p.svc.ProgramRepository.GetEnrollmentPlansByProgramID(p.ctx, &programID)
	if err != nil || len(existing) != 1 || existing[0].Status != model.ProgramEnrollmentActive || existing[0].NextDueAt == nil {
		t.Fatalf("expected one active enrollment, got %v (%v)", existing, err)
	}
	e := existing[0]
	unsent := p.send(campaignID, rid, false)

	// gone from the groups: marked missing, still active
	p.passMembers(program, nil, []*database.ProgramEnrollment{e}, now)
	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentActive || row.MissingSince == nil {
		t.Fatalf("expected active and missing, got %s %v", row.Status, row.MissingSince)
	}
	// within the grace period nothing changes
	p.passMembers(program, nil, []*database.ProgramEnrollment{&row}, now.Add(programMissingGrace/2))
	if row = p.enrollment(e.ID); row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("exited within the grace period")
	}
	// after the grace period the person is exited and the unsent send cancelled
	p.passMembers(program, nil, []*database.ProgramEnrollment{&row}, now.Add(2*programMissingGrace))
	row = p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentExited || row.ExitedAt == nil || row.NextDueAt != nil || row.MissingSince != nil {
		t.Fatalf("expected exited, got %s exited %v due %v missing %v", row.Status, row.ExitedAt, row.NextDueAt, row.MissingSince)
	}
	var cr database.CampaignRecipient
	p.db.Where("id = ?", unsent).First(&cr)
	if cr.CancelledAt == nil {
		t.Fatal("the unsent send of a leaver must be cancelled")
	}
	// back in the groups: active again with the same loop
	p.passMembers(program, members, []*database.ProgramEnrollment{&row}, now.Add(3*programMissingGrace))
	row = p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentActive || row.ExitedAt != nil || row.NextDueAt == nil {
		t.Fatalf("expected active again, got %s exited %v due %v", row.Status, row.ExitedAt, row.NextDueAt)
	}
	var count int64
	p.db.Model(&database.ProgramEnrollment{}).Count(&count)
	if count != 1 {
		t.Fatalf("a returning member must keep one enrollment, got %d", count)
	}
}

// TestResolveEnrollmentsBackWithinGrace checks that a member found again before
// the grace period ends only loses the missing mark
func TestResolveEnrollmentsBackWithinGrace(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	rid := p.recipient("bob@example.com")
	e := p.enroll(programID, rid, 0)
	now := time.Now()
	program := p.loadProgram(programID)
	p.passMembers(program, nil, []*database.ProgramEnrollment{e}, now)
	row := p.enrollment(e.ID)
	p.passMembers(program, []*uuid.UUID{&rid}, []*database.ProgramEnrollment{&row}, now)
	if row = p.enrollment(e.ID); row.MissingSince != nil || row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("expected active without missing mark, got %s %v", row.Status, row.MissingSince)
	}
}

// TestAdvanceEnrollmentLoopRollover checks that a looping person who had every
// step starts the next loop in that loop's campaign
func TestAdvanceEnrollmentLoopRollover(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopLoop)
	stepID := p.step(programID, 0, true)
	first := p.stepCampaign(programID, stepID, 0)
	second := p.stepCampaign(programID, stepID, 1)
	rid := p.recipient("carol@example.com")
	e := p.enroll(programID, rid, 0)
	p.send(first, rid, true)

	p.advance(programID, e)
	if rows := p.sendsIn(second); len(rows) != 1 || rows[0].SendAt == nil {
		t.Fatalf("expected one send in the next loop campaign, got %d", len(rows))
	}
	if rows := p.sendsIn(first); len(rows) != 1 {
		t.Fatalf("the first loop campaign must keep its one send, got %d", len(rows))
	}
	row := p.enrollment(e.ID)
	if row.LoopCount != 1 || row.Position != 1 || row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("expected loop 1 position 1 active, got loop %d position %d %s", row.LoopCount, row.Position, row.Status)
	}
	if row.LastStepID == nil || *row.LastStepID != stepID {
		t.Fatalf("last step not stored")
	}
}

// TestAdvanceEnrollmentCompleteMode checks that the last step of a program that
// does not loop is sent and completes the person
func TestAdvanceEnrollmentCompleteMode(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	rid := p.recipient("dave@example.com")
	e := p.enroll(programID, rid, 0)

	p.advance(programID, e)
	if rows := p.sendsIn(campaignID); len(rows) != 1 {
		t.Fatalf("expected the step to be sent, got %d rows", len(rows))
	}
	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentCompleted || row.CompletedAt == nil || row.NextDueAt != nil {
		t.Fatalf("expected completed, got %s due %v", row.Status, row.NextDueAt)
	}
	// a completed person advanced again gets nothing more
	p.db.Model(&database.CampaignRecipient{}).Where("campaign_id = ?", campaignID).Update("sent_at", utils.NowRFC3339UTC())
	p.advance(programID, &row)
	if rows := p.sendsIn(campaignID); len(rows) != 1 {
		t.Fatalf("a completed person must get no new send, got %d rows", len(rows))
	}
}

// TestAdvanceEnrollmentPostponesWhenAllBroken checks that a person whose every
// remaining step has a broken template waits a day and keeps their round
func TestAdvanceEnrollmentPostponesWhenAllBroken(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, false)
	stepB := p.step(programID, 1, false)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	rid := p.recipient("erin@example.com")
	e := p.enroll(programID, rid, 0)
	before := time.Now()

	p.advance(programID, e)
	if len(p.sendsIn(campaignA))+len(p.sendsIn(campaignB)) != 0 {
		t.Fatal("a broken template must not be sent")
	}
	row := p.enrollment(e.ID)
	if row.Status != model.ProgramEnrollmentActive || row.NextDueAt == nil {
		t.Fatalf("expected active with a due time, got %s %v", row.Status, row.NextDueAt)
	}
	if row.NextDueAt.Before(before.Add(programPostponeDelay - time.Minute)) {
		t.Fatalf("expected the send postponed about a day, got %v", row.NextDueAt)
	}
	if row.Position != 0 || row.LoopCount != 0 || !row.RoundStartAt.Equal(*e.RoundStartAt) {
		t.Fatalf("a postponed person must keep their round, got position %d loop %d", row.Position, row.LoopCount)
	}
}

// TestAdvanceEnrollmentKeepsTakenCancelledSend checks a cancelled row the
// sender already took is never put back in the queue, as it may have gone out
// before a crash kept sent_at from being written, and its step counts as given
func TestAdvanceEnrollmentKeepsTakenCancelledSend(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignID := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	rid := p.recipient("frank@example.com")
	e := p.enroll(programID, rid, 0)
	taken := p.send(campaignID, rid, false)
	now := utils.NowRFC3339UTC()
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", taken).
		Updates(map[string]any{"cancelled_at": now, "last_attempt_at": now})

	p.advance(programID, e)
	rows := p.sendsIn(campaignID)
	if len(rows) != 1 || *rows[0].ID != taken {
		t.Fatalf("expected only the taken row in the campaign, got %d rows", len(rows))
	}
	if rows[0].CancelledAt == nil {
		t.Fatal("a row the sender took must not be put back in the queue")
	}
	// the taken step counts as given, so the person gets the other step
	if rows := p.sendsIn(campaignB); len(rows) != 1 || rows[0].CancelledAt != nil {
		t.Fatalf("expected the other step planned, got %d rows", len(rows))
	}
}

// TestAdvanceEnrollmentRevivesCancelledSend checks that a cancelled unsent row
// of the picked step goes back in the queue instead of a second row
func TestAdvanceEnrollmentRevivesCancelledSend(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	p.step(programID, 1, true)
	campaignID := p.stepCampaign(programID, stepA, 0)
	rid := p.recipient("frank@example.com")
	e := p.enroll(programID, rid, 0)
	cancelled := p.send(campaignID, rid, false)
	p.db.Model(&database.CampaignRecipient{}).Where("id = ?", cancelled).
		Updates(map[string]any{"cancelled_at": utils.NowRFC3339UTC(), "language": "fr"})

	p.advance(programID, e)
	rows := p.sendsIn(campaignID)
	if len(rows) != 1 || *rows[0].ID != cancelled {
		t.Fatalf("expected the cancelled row to be reused, got %d rows", len(rows))
	}
	if rows[0].CancelledAt != nil || rows[0].SendAt == nil {
		t.Fatal("the cancelled send was not put back in the queue")
	}
	// put back is a new planning decision: the template has no language, so
	// the old stamp is not kept
	if rows[0].Language != nil {
		t.Fatalf("a revived row gets the language decided now, got %v", *rows[0].Language)
	}
	row := p.enrollment(e.ID)
	if row.Position != 1 || row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("expected position 1 active, got %d %s", row.Position, row.Status)
	}
}
