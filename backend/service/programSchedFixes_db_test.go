package service

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// workWeekProgram sends Monday to Friday 09:00 to 17:00 UTC, a new template
// every two weeks spread over one day
func workWeekProgram() *model.Program {
	return &model.Program{
		// weekday bits: sunday 1, monday 2 ... saturday 64
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(2 + 4 + 8 + 16 + 32)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
		StepIntervalMinutes: nullable.NewNullableWithValue(14 * 24 * 60),
		SpreadMinutes:       nullable.NewNullableWithValue(24 * 60),
	}
}

// TestPickSendTimeSpreadsOverNextSendingHours checks a round that starts
// outside the sending hours or just before they end is spread over the next
// sending hours instead of sending everyone at the next opening
func TestPickSendTimeSpreadsOverNextSendingHours(t *testing.T) {
	p := workWeekProgram()
	rng := rand.New(rand.NewSource(7))
	monday9 := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	monday17 := time.Date(2024, 1, 8, 17, 0, 0, 0, time.UTC)

	// resumed on Friday 18:00, after the sending hours
	friday18 := time.Date(2024, 1, 5, 18, 0, 0, 0, time.UTC)
	hours := map[int]bool{}
	atOpening := 0
	for i := 0; i < 500; i++ {
		got := programPickSendTimeAfter(p, friday18, friday18, time.Time{}, rng)
		if got.Before(monday9) || !got.Before(monday17) {
			t.Fatalf("send %v is outside the Monday sending hours", got)
		}
		if got.Equal(monday9) {
			atOpening++
		}
		hours[got.Hour()] = true
	}
	if len(hours) != 8 || atOpening > 5 {
		t.Fatalf("sends are not spread over Monday: %d distinct hours, %d at the opening", len(hours), atOpening)
	}

	// resumed on Friday 16:50, ten minutes before the sending hours end
	friday1650 := time.Date(2024, 1, 5, 16, 50, 0, 0, time.UTC)
	friday17 := time.Date(2024, 1, 5, 17, 0, 0, 0, time.UTC)
	onFriday, onMonday := 0, 0
	for i := 0; i < 500; i++ {
		got := programPickSendTimeAfter(p, friday1650, friday1650, time.Time{}, rng)
		switch {
		case !got.Before(friday1650) && got.Before(friday17):
			onFriday++
		case !got.Before(monday9) && got.Before(monday17):
			onMonday++
		default:
			t.Fatalf("send %v is outside the sending hours", got)
		}
	}
	// the round holds eight sending hours, ten minutes of them on Friday
	if onFriday == 0 || onFriday > 40 || onMonday < 450 {
		t.Fatalf("expected most sends on Monday, got %d on Friday and %d on Monday", onFriday, onMonday)
	}

	// a round that starts at the opening keeps its own day
	tuesday9 := time.Date(2024, 1, 9, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		got := programPickSendTimeAfter(p, tuesday9, tuesday9, time.Time{}, rng)
		if got.Before(tuesday9) || !got.Before(tuesday9.Add(8*time.Hour)) {
			t.Fatalf("send %v left the Tuesday sending hours", got)
		}
	}
}

// TestHeldSendMovesSpreadAfterHours checks held sends whose time passed during
// a pause are not all moved to the same second when the resume is after hours
func TestHeldSendMovesSpreadAfterHours(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	friday18 := time.Date(2024, 1, 5, 18, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		p.sendAt(campaignID, p.recipient(uuid.NewString()+"@example.com"), friday18.Add(-24*time.Hour), false)
	}
	program := workWeekProgram()
	program.ID = nullable.NewNullableWithValue(programID)
	moves, err := p.svc.heldSendMoves(p.ctx, program, friday18)
	if err != nil || len(moves) != 50 {
		t.Fatalf("expected 50 moves, got %d %v", len(moves), err)
	}
	distinct := map[time.Time]bool{}
	for _, m := range moves {
		distinct[m.SendAt] = true
	}
	if len(distinct) < 40 {
		t.Fatalf("held sends were moved to %d distinct times only", len(distinct))
	}
}

// TestUpdateLocksAnonymousZoneOnceStarted checks the time zone of an anonymous
// program can not change once people are enrolled, as the months of its
// results are cut in that zone, while the same zone, a program that is not
// anonymous and an anonymous program without people may change it
func TestUpdateLocksAnonymousZoneOnceStarted(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	zone := func(name string) *model.Program {
		return &model.Program{
			Name:     nullable.NewNullableWithValue(*mustString64(t, "program "+uuid.NewString()[:8])),
			TimeZone: nullable.NewNullableWithValue(name),
		}
	}
	anon := p.program(model.ProgramLoopComplete)
	p.setProgram(anon, map[string]any{"is_anonymous": true, "time_zone": "Europe/Copenhagen"})
	if err := p.svc.UpdateByID(p.ctx, session, &anon, zone("America/New_York")); err == nil || !isValidation(err) {
		t.Fatalf("a zone change of a started anonymous program must be refused, got %v", err)
	}
	if got := p.loadProgram(anon).TimeZone.MustGet(); got != "Europe/Copenhagen" {
		t.Fatalf("the zone must not change, got %s", got)
	}
	if err := p.svc.UpdateByID(p.ctx, session, &anon, zone("Europe/Copenhagen")); err != nil {
		t.Fatalf("sending the same zone must pass: %v", err)
	}

	// not started, but people enrolled by a first pass that stopped
	enrolled := p.program(model.ProgramLoopComplete)
	p.setProgram(enrolled, map[string]any{"is_anonymous": true, "started_at": nil})
	p.enroll(enrolled, p.recipient("alice@example.com"), 0)
	if err := p.svc.UpdateByID(p.ctx, session, &enrolled, zone("America/New_York")); err == nil {
		t.Fatal("a zone change of an anonymous program with people must be refused")
	}

	empty := p.program(model.ProgramLoopComplete)
	p.setProgram(empty, map[string]any{"is_anonymous": true, "started_at": nil, "start_at": nil})
	if err := p.svc.UpdateByID(p.ctx, session, &empty, zone("America/New_York")); err != nil {
		t.Fatalf("an anonymous program without people may change its zone: %v", err)
	}

	open := p.program(model.ProgramLoopComplete)
	if err := p.svc.UpdateByID(p.ctx, session, &open, zone("America/New_York")); err != nil {
		t.Fatalf("a program that is not anonymous may change its zone: %v", err)
	}
}

// newProgramSenderTest is a lifecycle test whose campaign service sends
// through the template service and plans program sends through the program
func newProgramSenderTest(t *testing.T) *programTest {
	t.Helper()
	p := newProgramLifecycleTest(t)
	if err := p.db.AutoMigrate(
		&database.Domain{},
		&database.Proxy{},
		&database.Page{},
		&database.Email{},
		&database.SMTPConfiguration{},
		&database.APISender{},
		&database.Identifier{},
		&database.Company{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	p.svc.CampaignService.CampaignTemplateService = &CampaignTemplate{
		Common:                     p.svc.Common,
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: p.db},
	}
	p.svc.CampaignService.ProgramService = p.svc
	return p
}

// sendWith runs the sender for the given sends of a program campaign
func (p *programTest) sendWith(campaignID, templateID uuid.UUID, sendIDs ...uuid.UUID) error {
	p.t.Helper()
	campaign, err := p.svc.CampaignRepository.GetByID(p.ctx, &campaignID, &repository.CampaignOption{IncludeProgramManaged: true})
	if err != nil {
		p.t.Fatalf("load campaign: %v", err)
	}
	rows := []*model.CampaignRecipient{}
	for _, id := range sendIDs {
		row := p.campaignRecipient(id)
		cr := &model.CampaignRecipient{
			ID:          nullable.NewNullableWithValue(id),
			CampaignID:  nullable.NewNullableWithValue(campaignID),
			RecipientID: nullable.NewNullableWithValue(*row.RecipientID),
		}
		rows = append(rows, cr)
	}
	return p.svc.CampaignService.sendCampaignMessagesWithTemplate(p.ctx, authorizedSession(p.t), campaign, templateID, rows)
}

// TestSenderHoldsProgramCampaignWithBrokenTemplate checks the sender keeps a
// hidden campaign open when its template has no domain or is gone, and holds
// the sends by cancelling them, so their people are planned again as if the
// step was not given, see TestSenderHeldSendIsReplanned
func TestSenderHoldsProgramCampaignWithBrokenTemplate(t *testing.T) {
	for name, deleted := range map[string]bool{"no domain": false, "deleted": true} {
		t.Run(name, func(t *testing.T) {
			p := newProgramSenderTest(t)
			programID := p.program(model.ProgramLoopComplete)
			stepID := p.step(programID, 0, false)
			templateID := p.stepTemplate(stepID)
			campaignID := p.stepCampaign(programID, stepID, 0)
			alice := p.recipient("alice@example.com")
			now := time.Now()
			sendID := p.sendAt(campaignID, alice, now.Add(-time.Minute), false)
			if deleted {
				p.db.Where("id = ?", templateID).Delete(&database.CampaignTemplate{})
			}
			if err := p.sendWith(campaignID, templateID, sendID); err != nil {
				t.Fatalf("a held program send is no error: %v", err)
			}
			if row := p.campaignRow(campaignID); row.ClosedAt != nil {
				t.Fatal("the hidden campaign must stay open")
			}
			row := p.campaignRecipient(sendID)
			if row.CancelledAt == nil || row.SentAt != nil {
				t.Fatalf("the send must be held, cancelled %v sent %v", row.CancelledAt, row.SentAt)
			}
		})
	}
}

// TestSenderKeepsProgramCampaignOnReadError checks a failed template read does
// not close a hidden campaign or touch its sends, the next run tries again
func TestSenderKeepsProgramCampaignOnReadError(t *testing.T) {
	p := newProgramSenderTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	templateID := p.stepTemplate(stepID)
	campaignID := p.stepCampaign(programID, stepID, 0)
	at := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	sendID := p.sendAt(campaignID, p.recipient("alice@example.com"), at, false)
	if err := p.db.Migrator().DropTable(&database.CampaignTemplate{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := p.sendWith(campaignID, templateID, sendID); err == nil {
		t.Fatal("expected the read error")
	}
	if row := p.campaignRow(campaignID); row.ClosedAt != nil {
		t.Fatal("a read error must not close the hidden campaign")
	}
	row := p.campaignRecipient(sendID)
	if row.CancelledAt != nil || row.SendAt == nil || !row.SendAt.Equal(at) {
		t.Fatalf("the send must be left as it was, cancelled %v send at %v", row.CancelledAt, row.SendAt)
	}
}

// TestCampaignCloseReplansProgramPeople checks a close of a hidden campaign
// that cancels unsent sends puts the people back on schedule: a completed
// person is made active again and an active person whose last planned send
// was cancelled is due again with that round
func TestCampaignCloseReplansProgramPeople(t *testing.T) {
	p := newProgramSenderTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step1 := p.step(programID, 0, true)
	step2 := p.step(programID, 1, true)
	campaign1 := p.stepCampaign(programID, step1, 0)
	campaign2 := p.stepCampaign(programID, step2, 0)
	now := time.Now().UTC()
	interval := p.loadProgram(programID).IntervalOrDefault()

	// alice had both steps, the second is not sent yet, so she is completed
	alice := p.recipient("alice@example.com")
	ea := p.enroll(programID, alice, 0)
	p.sendAt(campaign1, alice, now.Add(-30*24*time.Hour), true)
	aliceSend := p.sendAt(campaign2, alice, now.Add(time.Hour), false)
	p.setEnrollment(ea.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil,
		"position": 2, "last_step_id": step2,
	})
	// bob had the second step planned and waits a whole round for the next one
	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.sendAt(campaign2, bob, now.Add(2*time.Hour), false)
	roundStart := now.Add(interval)
	p.setEnrollment(eb.ID, map[string]any{
		"round_start_at": roundStart, "next_due_at": roundStart.Add(time.Hour),
		"position": 1, "last_step_id": step2,
	})

	campaign, err := p.svc.CampaignRepository.GetByID(p.ctx, &campaign2, &repository.CampaignOption{IncludeProgramManaged: true})
	if err != nil {
		t.Fatalf("load campaign: %v", err)
	}
	if err := p.svc.CampaignService.closeCampaign(p.ctx, authorizedSession(t), &campaign2, campaign, "test"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if row := p.campaignRecipient(aliceSend); row.CancelledAt == nil {
		t.Fatal("expected the unsent send cancelled by the close")
	}
	a := p.enrollment(ea.ID)
	if a.Status != model.ProgramEnrollmentActive || a.NextDueAt == nil || a.NextDueAt.After(now.Add(2*time.Hour)) {
		t.Fatalf("expected alice active and due at about the cancelled send, got %s due %v", a.Status, a.NextDueAt)
	}
	b := p.enrollment(eb.ID)
	if b.NextDueAt == nil || b.NextDueAt.After(now.Add(3*time.Hour)) {
		t.Fatalf("expected bob due at about the cancelled send, got %v", b.NextDueAt)
	}
	if b.RoundStartAt == nil || b.RoundStartAt.After(now.Add(time.Minute)) {
		t.Fatalf("expected bob to get the round of the cancelled send back, got %v", b.RoundStartAt)
	}
}

// membershipProgram is a started program whose one group holds members
func (p *programTest) membershipProgram(members ...uuid.UUID) (uuid.UUID, uuid.UUID) {
	p.t.Helper()
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	groupID := p.group("group " + programID.String())
	p.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	for _, id := range members {
		member := id
		p.create(&database.RecipientGroupRecipient{RecipientID: &member, RecipientGroupID: &groupID})
	}
	return programID, campaignID
}

// listedProgram reads a program the way the membership pass lists it
func (p *programTest) listedProgram(id uuid.UUID) *model.Program {
	p.t.Helper()
	program, err := p.svc.ProgramRepository.GetByID(p.ctx, &id, &repository.ProgramOption{WithSteps: true, WithRecipientGroups: true})
	if err != nil {
		p.t.Fatalf("load program: %v", err)
	}
	return program
}

// TestMembershipPassCancelsSendsOfPeopleWhoLeft checks every membership pass
// cancels the unsent sends left behind by people who exited, for example
// after a cancel that failed, and of completed people who left the groups for
// longer than the grace period, while members and completed people within the
// grace keep theirs
func TestMembershipPassCancelsSendsOfPeopleWhoLeft(t *testing.T) {
	p := newProgramLifecycleTest(t)
	alice := p.recipient("alice@example.com")
	programID, campaignID := p.membershipProgram(alice)
	now := time.Now().UTC()
	ea := p.enroll(programID, alice, 0)
	aliceSend := p.sendAt(campaignID, alice, now.Add(time.Hour), false)

	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.setEnrollment(eb.ID, map[string]any{"status": model.ProgramEnrollmentExited, "exited_at": now, "next_due_at": nil})
	bobSend := p.sendAt(campaignID, bob, now.Add(time.Hour), false)

	carol := p.recipient("carol@example.com")
	ec := p.enroll(programID, carol, 0)
	p.setEnrollment(ec.ID, map[string]any{"status": model.ProgramEnrollmentCompleted, "completed_at": now, "next_due_at": nil})
	carolSend := p.sendAt(campaignID, carol, now.Add(time.Hour), false)

	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	if row := p.campaignRecipient(bobSend); row.CancelledAt == nil {
		t.Fatal("the send left behind by an exited person must be cancelled")
	}
	if row := p.campaignRecipient(aliceSend); row.CancelledAt != nil {
		t.Fatal("a member keeps their send")
	}
	c := p.enrollment(ec.ID)
	if c.MissingSince == nil || c.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("a completed person who left is marked missing and stays completed, got %s %v", c.Status, c.MissingSince)
	}
	if row := p.campaignRecipient(carolSend); row.CancelledAt != nil {
		t.Fatal("a completed person within the grace keeps their send")
	}

	// past the grace the completed person loses their send
	p.setEnrollment(ec.ID, map[string]any{"missing_since": now.Add(-2 * programMissingGrace)})
	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	if row := p.campaignRecipient(carolSend); row.CancelledAt == nil {
		t.Fatal("a completed person who left must lose their unsent send after the grace")
	}
	if c := p.enrollment(ec.ID); c.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("the completed person keeps their status, got %s", c.Status)
	}
	if a := p.enrollment(ea.ID); a.Status != model.ProgramEnrollmentActive || a.MissingSince != nil {
		t.Fatal("the member must be unchanged")
	}
}

// TestCompletedPersonBackClearsMissing checks a completed person who comes
// back in the groups loses the missing mark. Their one template was sent, so
// they have none left and stay completed
func TestCompletedPersonBackClearsMissing(t *testing.T) {
	p := newProgramLifecycleTest(t)
	alice := p.recipient("alice@example.com")
	programID, campaignID := p.membershipProgram(alice)
	p.send(campaignID, alice, true)
	e := p.enroll(programID, alice, 0)
	p.setEnrollment(e.ID, map[string]any{
		"status": model.ProgramEnrollmentCompleted, "next_due_at": nil, "missing_since": time.Now().UTC(),
	})
	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	if row := p.enrollment(e.ID); row.MissingSince != nil || row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("expected completed without the missing mark, got %s %v", row.Status, row.MissingSince)
	}
}

// TestMembershipPassExitsLeaversInChunks checks more leavers than one chunk
// are all exited and the lock is let go after the pass
func TestMembershipPassExitsLeaversInChunks(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID, _ := p.membershipProgram()
	n := repository.ProgramWriteChunk + 7
	now := time.Now().UTC()
	missing := now.Add(-2 * programMissingGrace)
	recipients := make([]*database.Recipient, 0, n)
	enrollments := make([]*database.ProgramEnrollment, 0, n)
	for i := 0; i < n; i++ {
		id, eid := uuid.New(), uuid.New()
		email := id.String() + "@example.com"
		recipients = append(recipients, &database.Recipient{ID: &id, CreatedAt: &now, UpdatedAt: &now, Email: &email})
		enrollments = append(enrollments, &database.ProgramEnrollment{
			ID: &eid, CreatedAt: &now, UpdatedAt: &now, ProgramID: &programID, RecipientID: &id,
			RoundStartAt: &now, NextDueAt: &now, MissingSince: &missing, Status: model.ProgramEnrollmentActive,
		})
	}
	for _, rows := range []any{recipients, enrollments} {
		if err := p.db.CreateInBatches(rows, 500).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	var exited int64
	p.db.Model(&database.ProgramEnrollment{}).
		Where("program_id = ? AND status = ?", programID, model.ProgramEnrollmentExited).Count(&exited)
	if exited != int64(n) {
		t.Fatalf("expected %d exited, got %d", n, exited)
	}
	if !lockIsFree(p.svc) {
		t.Fatal("the pass must let the lock go")
	}
}

// TestMembershipChunksStopWhenGroupsChange checks the chunked writes of a pass
// stop when the program groups change after it was listed
func TestMembershipChunksStopWhenGroupsChange(t *testing.T) {
	p := newProgramLifecycleTest(t)
	alice := p.recipient("alice@example.com")
	programID, _ := p.membershipProgram(alice)
	listed := p.listedProgram(programID)
	running := p.svc.runsWithGroupsOf(p.ctx, listed)
	if !running(p.loadProgram(programID)) {
		t.Fatal("expected the program to run with its groups")
	}
	p.db.Where("program_id = ?", programID).Delete(&database.ProgramRecipientGroup{})
	if running(p.loadProgram(programID)) {
		t.Fatal("a removed group must stop the writes")
	}
	members := []*uuid.UUID{&alice}
	if err := p.svc.resolveListedProgramWith(p.ctx, listed, members); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	if count, _ := p.svc.ProgramRepository.CountEnrollments(p.ctx, &programID); count != 0 {
		t.Fatalf("no one may be enrolled from a group that was removed, got %d", count)
	}
}

// TestFirstPassRedrawsStalePeople checks people stored by a first pass that
// stopped, for example when the program was made a draft, get their first
// send drawn again when the program starts, so they are not all due at once
func TestFirstPassRedrawsStalePeople(t *testing.T) {
	p := newProgramLifecycleTest(t)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	programID, _ := p.membershipProgram(alice, bob)
	p.setProgram(programID, map[string]any{"started_at": nil})
	p.setProgram(programID, map[string]any{"spread_minutes": 24 * 60})
	stale := time.Now().Add(-30 * 24 * time.Hour).UTC()
	e := p.enroll(programID, alice, 0)
	p.setEnrollment(e.ID, map[string]any{"round_start_at": stale, "next_due_at": stale})
	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	now := time.Now()
	row := p.enrollment(e.ID)
	if row.NextDueAt == nil || row.NextDueAt.Before(now.Add(-time.Minute)) || row.RoundStartAt.Before(now.Add(-time.Minute)) {
		t.Fatalf("expected the first send drawn again from now, got round %v due %v", row.RoundStartAt, row.NextDueAt)
	}
	if p.loadProgram(programID).StartedAt == nil {
		t.Fatal("expected the program marked started")
	}
	if count, _ := p.svc.ProgramRepository.CountActiveEnrollments(p.ctx, &programID); count != 2 {
		t.Fatalf("expected both members enrolled, got %d", count)
	}
}

// TestResumeCancelsSendsLeftByPeopleWhoLeft checks a resume cancels the held
// send of a person who exited before, whose cancel did not happen, instead of
// letting it out
func TestResumeCancelsSendsLeftByPeopleWhoLeft(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	alice := p.recipient("alice@example.com")
	programID, campaignID := p.membershipProgram(alice)
	now := time.Now().UTC()
	p.enroll(programID, alice, 0)
	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.setEnrollment(eb.ID, map[string]any{"status": model.ProgramEnrollmentExited, "exited_at": now, "next_due_at": nil})
	bobSend := p.sendAt(campaignID, bob, now.Add(-time.Hour), false)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if row := p.campaignRecipient(bobSend); row.CancelledAt == nil {
		t.Fatal("the held send of a person who left must be cancelled on resume")
	}
}

// TestResumeStaysPausedWhenMembersCanNotBeRead checks a resume whose member
// read fails keeps the program paused and the held sends as they are
func TestResumeStaysPausedWhenMembersCanNotBeRead(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	alice := p.recipient("alice@example.com")
	programID, campaignID := p.membershipProgram(alice)
	p.enroll(programID, alice, 0)
	at := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	sendID := p.sendAt(campaignID, alice, at, false)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := p.db.Migrator().DropTable(&database.RecipientGroupRecipient{}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err == nil {
		t.Fatal("expected the resume to fail")
	}
	if !p.loadProgram(programID).IsPaused() {
		t.Fatal("the program must stay paused")
	}
	if row := p.campaignRecipient(sendID); row.SendAt == nil || !row.SendAt.Equal(at) || row.CancelledAt != nil {
		t.Fatalf("the held send must be left as it was, got %v %v", row.SendAt, row.CancelledAt)
	}
}

// TestResumeStopsWhenEditedBetweenChunks checks an edit stored while a resume
// plans its chunks stops the resume, so the edit is not overwritten and the
// program stays paused
func TestResumeStopsWhenEditedBetweenChunks(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID, _ := p.pausedWithBacklog(repository.ProgramWriteChunk + 5)
	program := p.loadProgram(programID)
	stillPaused := func(pr *model.Program) bool {
		return checkResumable(pr) == nil && sameTime(pr.UpdatedAt, program.UpdatedAt)
	}
	chunks := 0
	done, err := p.svc.writeInChunks(p.ctx, &programID, 3*repository.ProgramWriteChunk, stillPaused, func(start, end int) error {
		chunks++
		// an edit lands between the chunks
		p.setProgram(programID, map[string]any{"updated_at": utils.NowRFC3339UTC(), "name": "edited"})
		return nil
	})
	if err != nil || done || chunks != 1 {
		t.Fatalf("expected the rest skipped after an edit: done %v chunks %d err %v", done, chunks, err)
	}
}

// TestResumeCatchUpKeepsCompleted checks the resume catch up writes only
// people who are still active
func TestResumeCatchUpKeepsCompleted(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	e := p.enroll(programID, p.recipient("alice@example.com"), 0)
	past := time.Now().Add(-time.Hour).UTC()
	p.setEnrollment(e.ID, map[string]any{"next_due_at": past})
	plans, err := p.svc.resumeCatchUpPlans(p.ctx, p.loadProgram(programID), time.Now())
	if err != nil || len(plans) != 1 {
		t.Fatalf("expected one catch up plan, got %d %v", len(plans), err)
	}
	// the person completes before the plan is written
	p.setEnrollment(e.ID, map[string]any{"status": model.ProgramEnrollmentCompleted, "next_due_at": nil})
	if err := p.svc.ProgramRepository.PlanEnrollments(p.ctx, plans, nil, model.ProgramEnrollmentActive); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if row := p.enrollment(e.ID); row.NextDueAt != nil || row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("a completed person must keep their plan, got %s %v", row.Status, row.NextDueAt)
	}
}

// TestPauseAndStartStoreUTC checks the pause and the start mark are written in
// UTC like the other program times
func TestPauseAndStartStoreUTC(t *testing.T) {
	// a server zone that is not UTC
	local := time.Local
	time.Local = time.FixedZone("server", 2*60*60)
	defer func() { time.Local = local }()
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	alice := p.recipient("alice@example.com")
	programID, _ := p.membershipProgram(alice)
	p.setProgram(programID, map[string]any{"started_at": nil})
	if err := p.svc.resolveListedProgram(p.ctx, p.listedProgram(programID)); err != nil {
		t.Fatalf("membership pass: %v", err)
	}
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	var row struct {
		StartedAt string
		PausedAt  string
	}
	p.db.Model(&database.Program{}).Select("started_at", "paused_at").Where("id = ?", programID).Scan(&row)
	for name, v := range map[string]string{"started_at": row.StartedAt, "paused_at": row.PausedAt} {
		if v == "" || !(len(v) > 6 && (v[len(v)-6:] == "+00:00" || v[len(v)-1:] == "Z")) {
			t.Fatalf("%s must be stored in UTC, got %q", name, v)
		}
	}
}

// TestMixKeepsPickOfStoredSend checks a pick whose send was stored right away
// stays in the mix counts when the rest of the person's writes fail
func TestMixKeepsPickOfStoredSend(t *testing.T) {
	stepID := uuid.New()
	stored, failed := uuid.New(), uuid.New()
	rid := uuid.New()
	mix := &programMix{
		values: map[uuid.UUID]string{rid: "sales"},
		counts: map[string]map[uuid.UUID]int{},
		from:   time.Now().Add(-time.Hour),
		to:     time.Now().Add(time.Hour),
	}
	now := time.Now()
	mix.record(&stored, &rid, stepID, now)
	mix.record(&failed, &rid, stepID, now)
	e1 := &database.ProgramEnrollment{ID: &stored}
	e2 := &database.ProgramEnrollment{ID: &failed}
	w := &programWrites{people: []*programPersonWrites{{e: e1, sendStored: true}, {e: e2}}}
	mixFailed := programMixFailed(w, map[uuid.UUID]bool{stored: true, failed: true})
	mix.settle(mixFailed)
	if got := mix.counts["sales"][stepID]; got != 1 {
		t.Fatalf("expected the stored send counted once, got %d", got)
	}
}

// TestResumeCancelsHeldSendOfCompletedLeaver checks a completed person whose
// last send is held by the pause and who leaves the groups during it gets
// nothing on resume
func TestResumeCancelsHeldSendOfCompletedLeaver(t *testing.T) {
	p := newProgramLifecycleTest(t)
	session := authorizedSession(t)
	alice := p.recipient("alice@example.com")
	programID, campaignID := p.membershipProgram(alice)
	now := time.Now().UTC()
	p.enroll(programID, alice, 0)
	bob := p.recipient("bob@example.com")
	eb := p.enroll(programID, bob, 0)
	p.setEnrollment(eb.ID, map[string]any{"status": model.ProgramEnrollmentCompleted, "next_due_at": nil})
	bobSend := p.sendAt(campaignID, bob, now.Add(-time.Hour), false)
	if err := p.svc.Pause(p.ctx, session, &programID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := p.svc.resumeWait(p.ctx, session, &programID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if row := p.campaignRecipient(bobSend); row.CancelledAt == nil {
		t.Fatal("the held last send of a completed person who left must be cancelled on resume")
	}
}
