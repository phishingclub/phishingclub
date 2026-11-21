package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
)

// eventIDFor returns the cached id of an event name, adding one when missing
func eventIDFor(name string) uuid.UUID {
	if id := cache.EventIDByName[name]; id != nil {
		cache.EventNameByID[id.String()] = name
		return *id
	}
	id := uuid.New()
	cache.EventIDByName[name] = &id
	cache.EventNameByID[id.String()] = name
	return id
}

// sentWithEvent adds a send at sentAt to a new campaign of the step and,
// when event is set, the event at eventAt as its notable event
func (p *programTest) sentWithEvent(programID, stepID, recipientID uuid.UUID, loop int, sentAt time.Time, event string, eventAt time.Time) uuid.UUID {
	p.t.Helper()
	campaignID := p.stepCampaign(programID, stepID, loop)
	id := p.send(campaignID, recipientID, true)
	fields := map[string]any{"sent_at": sentAt}
	if event != "" {
		eventID := eventIDFor(event)
		fields["notable_event_id"] = eventID
		now := time.Now().UTC()
		rowID := uuid.New()
		rid := recipientID
		p.create(&database.CampaignEvent{
			ID: &rowID, CreatedAt: &eventAt, UpdatedAt: &now,
			CampaignID: &campaignID, RecipientID: &rid, EventID: &eventID,
		})
	}
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		p.t.Fatalf("update send: %v", err)
	}
	return campaignID
}

// linkTemplate sets the template a hidden campaign sends to the template of
// its step, as a real step campaign has
func (p *programTest) linkTemplate(campaignID, stepID uuid.UUID) {
	p.t.Helper()
	templateID := p.stepTemplate(stepID)
	if err := p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("campaign_template_id", templateID).Error; err != nil {
		p.t.Fatalf("link template: %v", err)
	}
}

// templateNameOf returns the stored name of the template of a step
func (p *programTest) templateNameOf(stepID uuid.UUID) string {
	p.t.Helper()
	var row database.CampaignTemplate
	if err := p.db.Where("id = ?", p.stepTemplate(stepID)).First(&row).Error; err != nil {
		p.t.Fatalf("load template: %v", err)
	}
	return row.Name
}

// queue adds a send of a step that waits in the queue
func (p *programTest) queue(programID, stepID, recipientID uuid.UUID, loop int, sendAt time.Time) uuid.UUID {
	p.t.Helper()
	campaignID := p.stepCampaign(programID, stepID, loop)
	p.linkTemplate(campaignID, stepID)
	id := p.send(campaignID, recipientID, false)
	if err := p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("send_at", sendAt).Error; err != nil {
		p.t.Fatalf("queue: %v", err)
	}
	return campaignID
}

// TestGetRecipientProgramsTotals checks the program totals of a recipient
// count their own outcomes and an anonymous program gives only its name and
// status
func TestGetRecipientProgramsTotals(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	stepID := p.step(programID, 0, true)
	anna := p.recipient("anna@example.com")
	p.enroll(programID, anna, 0)
	base := time.Now().UTC().AddDate(0, 0, -30).Truncate(time.Second)
	for i := 0; i < 12; i++ {
		sentAt := base.AddDate(0, 0, i)
		event := data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT
		eventAt := sentAt
		switch i {
		case 11:
			event = data.EVENT_CAMPAIGN_RECIPIENT_REPORTED
			eventAt = sentAt.Add(2 * time.Hour)
		case 10:
			event = data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED
			eventAt = sentAt.Add(time.Hour)
		}
		p.sentWithEvent(programID, stepID, anna, i, sentAt, event, eventAt)
	}
	programs, err := p.svc.GetRecipientPrograms(p.ctx, session, &anna, nil)
	if err != nil {
		t.Fatalf("programs: %v", err)
	}
	if len(programs.Rows) != 1 || programs.Rows[0].Totals == nil {
		t.Fatalf("expected one program with totals, got %+v", programs)
	}
	if tot := programs.Rows[0].Totals; tot.Sent != 12 || tot.Clicked != 1 || tot.Reported != 1 || tot.Submitted != 0 {
		t.Fatalf("unexpected totals %+v", tot)
	}
	if _, err := p.svc.GetRecipientPrograms(p.ctx, unauthorizedSession(t), &anna, nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected an unauthorized session refused, got %v", err)
	}
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	programs, err = p.svc.GetRecipientPrograms(p.ctx, session, &anna, nil)
	if err != nil {
		t.Fatalf("anonymous programs: %v", err)
	}
	if len(programs.Rows) != 1 || !programs.Rows[0].Anonymous || programs.Rows[0].Totals != nil || programs.Rows[0].NextDueAt != nil {
		t.Fatalf("expected only the name and status of an anonymous program, got %+v", programs.Rows[0])
	}
}

// TestProgramEventsRecipientFilter checks the program events of one recipient
// list only their events with the template of each send, refuse a recipient
// who is not enrolled and give nothing for an anonymous program
func TestProgramEventsRecipientFilter(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	stepID := p.step(programID, 0, true)
	anna := p.recipient("anna@example.com")
	bob := p.recipient("bob@example.com")
	carl := p.recipient("carl@example.com")
	p.enroll(programID, anna, 0)
	p.enroll(programID, bob, 0)
	now := time.Now().UTC().Truncate(time.Second)
	annaCampaign := p.sentWithEvent(programID, stepID, anna, 0, now, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, now)
	p.linkTemplate(annaCampaign, stepID)
	p.sentWithEvent(programID, stepID, bob, 1, now, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED, now)
	q := &vo.QueryArgs{OrderBy: repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"), Limit: 10}

	all, err := p.svc.GetEvents(p.ctx, session, &programID, q, nil)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(all.Rows) != 2 {
		t.Fatalf("expected the events of both recipients, got %d", len(all.Rows))
	}
	own, err := p.svc.GetEvents(p.ctx, session, &programID, q, &anna)
	if err != nil {
		t.Fatalf("recipient events: %v", err)
	}
	if len(own.Rows) != 1 || own.Rows[0].RecipientID == nil || *own.Rows[0].RecipientID != anna {
		t.Fatalf("expected only the event of anna, got %+v", own.Rows)
	}
	if own.Templates[annaCampaign.String()] != p.templateNameOf(stepID) {
		t.Fatalf("expected the template of the send, got %v", own.Templates)
	}
	if _, err := p.svc.GetEvents(p.ctx, session, &programID, q, &carl); err == nil {
		t.Fatal("a recipient who is not enrolled must be refused")
	}
	if _, err := p.svc.GetEvents(p.ctx, unauthorizedSession(t), &programID, q, &anna); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected an unauthorized session refused, got %v", err)
	}
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	anon, err := p.svc.GetEvents(p.ctx, session, &programID, q, &anna)
	if err != nil {
		t.Fatalf("anonymous events: %v", err)
	}
	if !anon.Anonymous || len(anon.Rows) != 0 {
		t.Fatalf("expected no events for an anonymous program, got %+v", anon)
	}
}

// TestRecipientsViewLastAndNextTemplate checks the recipient rows name the
// last sent template, the next template when it is known and leave it empty
// when it is picked at send time
func TestRecipientsViewLastAndNextTemplate(t *testing.T) {
	p := newProgramWriteTest(t)
	session := authorizedSession(t)
	programID := p.program(model.ProgramLoopLoop)
	first := p.step(programID, 0, true)
	second := p.step(programID, 1, true)
	third := p.step(programID, 2, true)
	anna := p.recipient("anna@example.com")
	bob := p.recipient("bob@example.com")
	p.enroll(programID, anna, 0)
	p.enroll(programID, bob, 0)
	sentAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	p.linkTemplate(p.sentWithEvent(programID, first, anna, 0, sentAt, "", sentAt), first)
	p.linkTemplate(p.sentWithEvent(programID, first, bob, 0, sentAt, "", sentAt), first)
	// bob has the second template waiting in the queue
	queuedAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	p.queue(programID, second, bob, 0, queuedAt)

	rowsOf := func() map[string]ProgramEnrollmentRow {
		view, err := p.svc.GetRecipientsView(p.ctx, session, &programID, &vo.QueryArgs{Limit: 10})
		if err != nil {
			t.Fatalf("recipients: %v", err)
		}
		out := map[string]ProgramEnrollmentRow{}
		for _, r := range view.Rows {
			out[r.Email] = r
		}
		return out
	}
	rows := rowsOf()
	a := rows["anna@example.com"]
	if a.LastTemplateName == nil || *a.LastTemplateName != p.templateNameOf(first) || a.LastSentAt == nil || !a.LastSentAt.Equal(sentAt) {
		t.Fatalf("expected the first template as the last one, got %+v", a)
	}
	if a.NextTemplateName == nil || *a.NextTemplateName != p.templateNameOf(second) {
		t.Fatalf("in the same order the next template is known, got %+v", a.NextTemplateName)
	}
	b := rows["bob@example.com"]
	if b.NextTemplateName == nil || *b.NextTemplateName != p.templateNameOf(second) || b.NextDueAt == nil || !b.NextDueAt.Equal(queuedAt) {
		t.Fatalf("a queued send names the next template and its time, got %+v", b)
	}

	p.setProgram(programID, map[string]any{"order_mode": model.ProgramOrderShuffle})
	rows = rowsOf()
	if rows["anna@example.com"].NextTemplateName != nil {
		t.Fatalf("in random order the next template is picked at send time, got %v", *rows["anna@example.com"].NextTemplateName)
	}
	if n := rows["bob@example.com"].NextTemplateName; n == nil || *n != p.templateNameOf(second) {
		t.Fatalf("a queued send is known in random order too, got %v", n)
	}
	_ = third
}

// TestGetRecipientProgramsPage checks the programs of a recipient are paged
// and sorted by the server, leave out programs of another company, and refuse
// a sort outside the allowlist
func TestGetRecipientProgramsPage(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	anna := p.recipient("anna@example.com")
	company, other := uuid.New(), uuid.New()
	if err := p.db.Model(&database.Recipient{}).Where("id = ?", anna).Update("company_id", company).Error; err != nil {
		t.Fatalf("set recipient company: %v", err)
	}
	for _, def := range []struct {
		name    string
		company *uuid.UUID
	}{{"bravo", &company}, {"alpha", nil}, {"charlie", &company}, {"other", &other}} {
		id := p.program(model.ProgramLoopLoop)
		p.setProgram(id, map[string]any{"name": def.name, "company_id": def.company})
		p.enroll(id, anna, 0)
	}
	names := func(r *model.Result[ProgramRecipientActivity]) []string {
		out := []string{}
		for _, row := range r.Rows {
			out = append(out, row.Name)
		}
		return out
	}
	page, err := p.svc.GetRecipientPrograms(p.ctx, session, &anna, &vo.QueryArgs{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if got := names(page); !page.HasNextPage || len(got) != 2 || got[0] != "alpha" || got[1] != "bravo" {
		t.Fatalf("expected alpha, bravo and a next page, got %v next %v", got, page.HasNextPage)
	}
	page, err = p.svc.GetRecipientPrograms(p.ctx, session, &anna, &vo.QueryArgs{Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if got := names(page); page.HasNextPage || len(got) != 1 || got[0] != "charlie" {
		t.Fatalf("expected only charlie without a next page, got %v next %v", got, page.HasNextPage)
	}
	page, err = p.svc.GetRecipientPrograms(p.ctx, session, &anna, &vo.QueryArgs{OrderBy: "name", Desc: true, Limit: 1})
	if err != nil {
		t.Fatalf("sorted page: %v", err)
	}
	if got := names(page); len(got) != 1 || got[0] != "charlie" {
		t.Fatalf("expected charlie first by name descending, got %v", got)
	}
	if _, err := p.svc.GetRecipientPrograms(p.ctx, session, &anna, &vo.QueryArgs{OrderBy: "next_due_at"}); err == nil {
		t.Fatalf("expected a sort outside the allowlist to be refused")
	}
}
