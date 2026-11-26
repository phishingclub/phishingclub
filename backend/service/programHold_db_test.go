package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// setTemplate writes fields of a template row
func (p *programTest) setTemplate(id uuid.UUID, fields map[string]any) {
	p.t.Helper()
	if err := p.db.Model(&database.CampaignTemplate{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		p.t.Fatalf("update template: %v", err)
	}
}

// templateRow loads a template row
func (p *programTest) templateRow(id uuid.UUID) database.CampaignTemplate {
	p.t.Helper()
	var row database.CampaignTemplate
	if err := p.db.Where("id = ?", id).First(&row).Error; err != nil {
		p.t.Fatalf("load template: %v", err)
	}
	return row
}

// TestSenderHeldSendIsReplanned checks a send the sender can not make, as its
// template is not usable, is cancelled and its person planned again: the step
// counts as not given, the next advance gives another template and nothing is
// burned, and once the template is fixed the held step is offered again and
// its row put back in the queue
func TestSenderHeldSendIsReplanned(t *testing.T) {
	p := newProgramSenderTest(t)
	if cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED] == nil {
		id := uuid.New()
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED] = &id
	}
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopComplete)
	broken := p.step(programID, 0, false)
	working := p.step(programID, 1, true)
	brokenCampaign := p.stepCampaign(programID, broken, 0)
	workingCampaign := p.stepCampaign(programID, working, 0)
	interval := p.loadProgram(programID).IntervalOrDefault()
	now := time.Now().UTC()

	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 0)
	held := p.sendAt(brokenCampaign, alice, now.Add(-time.Minute), false)
	// the advance that planned the held send moved her a round on
	round := now.Add(interval)
	p.setEnrollment(e.ID, map[string]any{
		"round_start_at": round, "next_due_at": round.Add(time.Hour),
		"position": 1, "last_step_id": broken, "last_send_at": now.Add(-time.Minute),
	})

	if err := p.sendWith(brokenCampaign, p.stepTemplate(broken), held); err != nil {
		t.Fatalf("a held program send is no error: %v", err)
	}
	if row := p.campaignRow(brokenCampaign); row.ClosedAt != nil {
		t.Fatal("the hidden campaign must stay open")
	}
	if row := p.campaignRecipient(held); row.CancelledAt == nil || row.SentAt != nil {
		t.Fatalf("the held send must be cancelled, cancelled %v sent %v", row.CancelledAt, row.SentAt)
	}
	row := p.enrollment(e.ID)
	if row.NextDueAt == nil || row.NextDueAt.Before(now.Add(programHoldRetryDelay-time.Minute)) ||
		row.NextDueAt.After(now.Add(programHoldRetryDelay+time.Minute)) {
		t.Fatalf("the person must be due again after the retry wait, got %v", row.NextDueAt)
	}
	if row.RoundStartAt == nil || row.RoundStartAt.After(now.Add(time.Minute)) {
		t.Fatalf("the person must get the round of the held send back, got %v", row.RoundStartAt)
	}

	// the next advance skips the broken step and gives the working one
	due := *row.NextDueAt
	if err := p.svc.advanceEnrollment(p.ctx, session, p.loadProgram(programID), &row, due, nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	sends := p.sendsOf(workingCampaign, alice)
	if len(sends) != 1 || sends[0].SendAt == nil || sends[0].SendAt.After(due.Add(time.Minute)) {
		t.Fatalf("expected the working template sent at the due time, got %+v", sends)
	}
	if len(p.sendsOf(brokenCampaign, alice)) != 1 {
		t.Fatal("the broken step must get no new row")
	}
	row = p.enrollment(e.ID)
	if row.LastStepID == nil || *row.LastStepID != working || row.LastSendAt == nil || row.LastSendAt.After(due.Add(time.Minute)) {
		t.Fatalf("expected the working step as the last send, got %v at %v", row.LastStepID, row.LastSendAt)
	}
	if row.Status != model.ProgramEnrollmentActive {
		t.Fatalf("the person still has the held step to get, got %s", row.Status)
	}

	// fixed, the held step is offered on the next round and its row revived
	p.setTemplate(p.stepTemplate(broken), map[string]any{"is_usable": true})
	next := *row.NextDueAt
	if err := p.svc.advanceEnrollment(p.ctx, session, p.loadProgram(programID), &row, next, nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	revived := p.campaignRecipient(held)
	if revived.CancelledAt != nil || revived.SendAt == nil || revived.SendAt.Before(next.Add(-time.Minute)) {
		t.Fatalf("the held row must be back in the queue, cancelled %v send at %v", revived.CancelledAt, revived.SendAt)
	}
	if revived.NotableEventID == nil || *revived.NotableEventID != *cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED] {
		t.Fatalf("a revived row must be scheduled again, got %v", revived.NotableEventID)
	}
	if row := p.enrollment(e.ID); row.Status != model.ProgramEnrollmentCompleted {
		t.Fatalf("expected the person completed with both steps, got %s", row.Status)
	}
}

// TestSenderHoldsProgramTemplateWithoutLanding checks the sender holds the
// sends of a program template that is marked as not usable or has no landing
// page or proxy, so no email with a dead link goes out, while the campaign
// stays open
func TestSenderHoldsProgramTemplateWithoutLanding(t *testing.T) {
	for name, fields := range map[string]map[string]any{
		"not usable":      {"is_usable": false},
		"no landing page": {"is_usable": true, "landing_page_id": nil, "landing_proxy_id": nil},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProgramSenderTest(t)
			programID := p.program(model.ProgramLoopComplete)
			stepID := p.step(programID, 0, true)
			templateID := p.stepTemplate(stepID)
			p.setTemplate(templateID, fields)
			campaignID := p.stepCampaign(programID, stepID, 0)
			alice := p.recipient("alice@example.com")
			sendID := p.sendAt(campaignID, alice, time.Now().Add(-time.Minute), false)
			if err := p.sendWith(campaignID, templateID, sendID); err != nil {
				t.Fatalf("a held program send is no error: %v", err)
			}
			if row := p.campaignRow(campaignID); row.ClosedAt != nil {
				t.Fatal("the hidden campaign must stay open")
			}
			if row := p.campaignRecipient(sendID); row.SentAt != nil || row.CancelledAt == nil {
				t.Fatalf("the send must be held, sent %v cancelled %v", row.SentAt, row.CancelledAt)
			}
		})
	}
}

// TestPostponedPeopleAreSpread checks people whose templates can all not be
// sent are moved a day on to times drawn over the spread, not all to the same
// window opening
func TestPostponedPeopleAreSpread(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"spread_minutes": 8 * 60})
	p.step(programID, 0, false)
	program := p.loadProgram(programID)
	now := time.Now()
	seen := map[time.Time]bool{}
	for i := 0; i < 20; i++ {
		e := p.enroll(programID, p.recipient(uuid.NewString()+"@example.com"), 0)
		if err := p.svc.advanceEnrollment(p.ctx, nil, program, e, now, nil, nil); err != nil {
			t.Fatalf("advance: %v", err)
		}
		row := p.enrollment(e.ID)
		if row.NextDueAt == nil || row.NextDueAt.Before(now.Add(programPostponeDelay-time.Minute)) ||
			row.NextDueAt.After(now.Add(programPostponeDelay+8*time.Hour+time.Minute)) {
			t.Fatalf("expected a time in the spread a day on, got %v", row.NextDueAt)
		}
		seen[row.NextDueAt.UTC()] = true
	}
	if len(seen) < 2 {
		t.Fatalf("postponed people must not share one time, got %v", seen)
	}
}

// TestSchedulerSkipsTemplateWithBrokenEmail checks the scheduler does not pick
// a template whose email does not parse, which the sender would hold
func TestSchedulerSkipsTemplateWithBrokenEmail(t *testing.T) {
	p := newProgramSenderTest(t)
	p.svc.CampaignTemplateService = p.svc.CampaignService.CampaignTemplateService
	p.svc.CampaignService.MailService = &Email{Common: p.svc.Common, EmailRepository: &repository.Email{DB: p.db}}
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	templateID := p.stepTemplate(stepID)
	now := time.Now().UTC()
	emailID := uuid.New()
	p.create(&database.Email{ID: &emailID, CreatedAt: &now, UpdatedAt: &now, Name: "broken", Content: "{{ .Broken",
		MailFrom: "sender@example.com", From: "Sender <sender@example.com>", Subject: "Hello"})
	p.setTemplate(templateID, map[string]any{"email_id": emailID})
	usable, err := p.svc.templateUsable(p.ctx, templateID, nil)
	if err != nil || usable {
		t.Fatalf("a template whose email does not parse must not be usable: %v %v", usable, err)
	}
	p.db.Model(&database.Email{}).Where("id = ?", emailID).Update("content", "Hello {{.FirstName}}")
	usable, err = p.svc.templateUsable(p.ctx, templateID, nil)
	if err != nil || !usable {
		t.Fatalf("a template whose email parses must be usable: %v %v", usable, err)
	}
}

// TestAdvanceAtEndMakesNoCampaign checks a person parked at the program end
// does not make a hidden campaign that would never send
func TestAdvanceAtEndMakesNoCampaign(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, true)
	end := time.Now().UTC().Add(-time.Minute)
	p.setProgram(programID, map[string]any{"end_at": end})
	e := p.enroll(programID, p.recipient("alice@example.com"), 0)
	if err := p.svc.advanceEnrollment(p.ctx, nil, p.loadProgram(programID), e, time.Now(), nil, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	var count int64
	p.db.Model(&database.Campaign{}).Where("program_id = ?", programID).Count(&count)
	if count != 0 {
		t.Fatalf("expected no hidden campaign, got %d", count)
	}
	if row := p.enrollment(e.ID); row.NextDueAt == nil || !row.NextDueAt.Equal(end) {
		t.Fatalf("expected the person parked at the end, got %v", row.NextDueAt)
	}
}

// TestCatchUpIsSpread checks a person overdue by more than the grace, for
// example after downtime, is sent at a time drawn over the spread from now
// and not at once
func TestCatchUpIsSpread(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"spread_minutes": 8 * 60})
	stepID := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, stepID, 0)
	program := p.loadProgram(programID)
	now := time.Now()
	later := 0
	for i := 0; i < 10; i++ {
		alice := p.recipient(uuid.NewString() + "@example.com")
		e := p.enroll(programID, alice, 0)
		overdue := now.Add(-3 * time.Hour)
		e.NextDueAt = &overdue
		if err := p.svc.advanceEnrollment(p.ctx, nil, program, e, now, nil, nil); err != nil {
			t.Fatalf("advance: %v", err)
		}
		sends := p.sendsOf(campaignID, alice)
		if len(sends) != 1 || sends[0].SendAt == nil || sends[0].SendAt.Before(now.Add(-time.Second)) ||
			sends[0].SendAt.After(now.Add(8*time.Hour+time.Minute)) {
			t.Fatalf("expected one send in the spread from now, got %+v", sends)
		}
		if sends[0].SendAt.After(now.Add(time.Minute)) {
			later++
		}
	}
	if later == 0 {
		t.Fatal("overdue people must be spread, not all sent at once")
	}
}

// TestRemoveLandingKeepsUsableRule checks removing a page or proxy only marks
// a template as not usable when it loses its landing and has no other one
func TestRemoveLandingKeepsUsableRule(t *testing.T) {
	p := newProgramServiceTest(t)
	repo := &repository.CampaignTemplate{DB: p.db}
	programID := p.program(model.ProgramLoopComplete)
	page, proxy := uuid.New(), uuid.New()
	afterOnly := p.stepTemplate(p.step(programID, 0, true))
	p.setTemplate(afterOnly, map[string]any{"after_landing_page_id": page, "landing_page_id": uuid.New()})
	withProxy := p.stepTemplate(p.step(programID, 1, true))
	p.setTemplate(withProxy, map[string]any{"landing_page_id": page, "landing_proxy_id": proxy})
	pageOnly := p.stepTemplate(p.step(programID, 2, true))
	p.setTemplate(pageOnly, map[string]any{"landing_page_id": page})
	if err := repo.RemovePageIDFromAll(p.ctx, &page); err != nil {
		t.Fatalf("remove page: %v", err)
	}
	if row := p.templateRow(afterOnly); !row.IsUsable || row.AfterLandingPageID != nil {
		t.Fatalf("an after page removed must keep the template usable: %+v", row)
	}
	if row := p.templateRow(withProxy); !row.IsUsable || row.LandingPageID != nil {
		t.Fatalf("a template with a landing proxy left must stay usable: %+v", row)
	}
	if row := p.templateRow(pageOnly); row.IsUsable || row.LandingPageID != nil {
		t.Fatalf("a template with no landing left must not be usable: %+v", row)
	}
	if err := repo.RemoveProxyIDFromAll(p.ctx, &proxy); err != nil {
		t.Fatalf("remove proxy: %v", err)
	}
	if row := p.templateRow(withProxy); row.IsUsable || row.LandingProxyID != nil {
		t.Fatalf("a template with no landing left must not be usable: %+v", row)
	}
}

// TestRoundStartKeepsWallClockOverClockChange checks an interval of whole days
// moves a round start by calendar days in the program zone
func TestRoundStartKeepsWallClockOverClockChange(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	program := &model.Program{}
	program.TimeZone.Set("Europe/Copenhagen")
	program.StepIntervalMinutes.Set(7 * 24 * 60)
	start := time.Date(2026, 3, 23, 9, 0, 0, 0, loc).UTC()
	next := programNextRoundStart(program, start, start)
	if got := next.In(loc); got.Hour() != 9 || got.Day() != 30 {
		t.Fatalf("expected 09:00 a week later, got %v", got)
	}
	program.StepIntervalMinutes.Set(36 * 60)
	if got := programNextRoundStart(program, start, start); !got.Equal(start.Add(36 * time.Hour)) {
		t.Fatalf("an interval that is not whole days is a fixed duration, got %v", got)
	}
}

// TestSenderHoldsProgramTemplateWithBrokenSubject checks a subject that does
// not parse is found before any row is tried, so the program send is held and
// planned again instead of being saved as a failed send, while a subject that
// parses goes on to the send
func TestSenderHoldsProgramTemplateWithBrokenSubject(t *testing.T) {
	for name, subject := range map[string]string{
		"broken": "Hello {{.FirstName",
		"parses": "Hello {{.FirstName}}",
	} {
		t.Run(name, func(t *testing.T) {
			p := newProgramSenderTest(t)
			for _, event := range []string{
				data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT,
				data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED,
			} {
				if cache.EventIDByName[event] == nil {
					id := uuid.New()
					cache.EventIDByName[event] = &id
				}
				cache.EventNameByID[cache.EventIDByName[event].String()] = event
			}
			p.svc.CampaignService.MailService = &Email{Common: p.svc.Common, EmailRepository: &repository.Email{DB: p.db}}
			p.svc.CampaignService.SMTPConfigService = &SMTPConfiguration{Common: p.svc.Common,
				SMTPConfigurationRepository: &repository.SMTPConfiguration{DB: p.db}}
			programID := p.program(model.ProgramLoopComplete)
			stepID := p.step(programID, 0, true)
			templateID := p.stepTemplate(stepID)
			now := time.Now().UTC()
			domainID, emailID, pageID, smtpID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			p.create(&database.Domain{ID: domainID, CreatedAt: &now, UpdatedAt: &now, Name: "subject.example.com"})
			p.create(&database.Email{ID: &emailID, CreatedAt: &now, UpdatedAt: &now, Name: "subject " + name,
				Content: "Hello {{.FirstName}}", MailFrom: "sender@example.com",
				From: "Sender <sender@example.com>", Subject: subject})
			if err := p.db.AutoMigrate(&database.SMTPConfiguration{}, &database.SMTPHeader{}); err != nil {
				t.Fatalf("migrate: %v", err)
			}
			// nothing listens on port 1, so no mail can leave the test
			p.create(&database.SMTPConfiguration{ID: smtpID, CreatedAt: &now, UpdatedAt: &now,
				Name: "smtp " + name, Host: "127.0.0.1", Port: 1})
			p.setTemplate(templateID, map[string]any{
				"domain_id": domainID, "email_id": emailID, "landing_page_id": pageID,
				"smtp_configuration_id": smtpID,
			})
			campaignID := p.stepCampaign(programID, stepID, 0)
			sendID := p.sendAt(campaignID, p.recipient("alice@example.com"), now.Add(-time.Minute), false)
			// read the row with its recipient, as the sender does
			rows, err := p.svc.CampaignRecipientRepository.GetUnsendRecipientsForSending(
				p.ctx, 1000, &repository.CampaignRecipientOption{WithRecipient: true},
			)
			if err != nil || len(rows) != 1 {
				t.Fatalf("read due row: %v %d", err, len(rows))
			}
			campaign, err := p.svc.CampaignRepository.GetByID(p.ctx, &campaignID, &repository.CampaignOption{IncludeProgramManaged: true})
			if err != nil {
				t.Fatalf("load campaign: %v", err)
			}
			_ = p.svc.CampaignService.sendCampaignMessagesWithTemplate(p.ctx, authorizedSession(t), campaign, templateID, rows)
			if row := p.campaignRow(campaignID); row.ClosedAt != nil {
				t.Fatal("the hidden campaign must stay open")
			}
			row := p.campaignRecipient(sendID)
			if name == "broken" {
				if row.CancelledAt == nil || row.SentAt != nil || row.LastAttemptAt != nil {
					t.Fatalf("the send must be held before it is tried, cancelled %v sent %v tried %v",
						row.CancelledAt, row.SentAt, row.LastAttemptAt)
				}
				return
			}
			if row.LastAttemptAt == nil || row.CancelledAt != nil || row.SentAt != nil {
				t.Fatalf("a subject that parses must go on to the send, cancelled %v tried %v sent %v",
					row.CancelledAt, row.LastAttemptAt, row.SentAt)
			}
		})
	}
}
