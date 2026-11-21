package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// anonymizeFixture is a named program with two templates sent to three
// people, with clicks and submitted data
type anonymizeFixture struct {
	*programTest
	programID uuid.UUID
	campaigns []uuid.UUID
	people    []uuid.UUID
}

// newAnonymizeFixture seeds alice, bob and carol sent both templates two
// months ago. Alice clicks both, bob clicks and submits on the first, carol
// does nothing.
func newAnonymizeFixture(t *testing.T) *anonymizeFixture {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	f := &anonymizeFixture{programTest: p, programID: programID}
	for i := 0; i < 2; i++ {
		f.campaigns = append(f.campaigns, p.stepCampaign(programID, p.step(programID, i, true), 0))
	}
	sent := time.Now().UTC().AddDate(0, -2, 0)
	for _, email := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		rid := p.recipient(email)
		f.people = append(f.people, rid)
		p.enroll(programID, rid, 0)
		for i, c := range f.campaigns {
			p.timelineSent(c, rid, sent.Add(time.Duration(i)*time.Hour))
		}
	}
	alice, bob := f.people[0], f.people[1]
	for i, c := range f.campaigns {
		f.trackedEvent(c, alice, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, sent.Add(time.Duration(i)*time.Hour+time.Minute))
	}
	f.trackedEvent(f.campaigns[0], bob, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, sent.Add(time.Minute))
	f.trackedEvent(f.campaigns[0], bob, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA, sent.Add(2*time.Minute))
	return f
}

// trackedEvent adds an event of a person with an IP address, user agent,
// submitted data and browser metadata
func (f *anonymizeFixture) trackedEvent(campaignID, recipientID uuid.UUID, name string, at time.Time) {
	f.t.Helper()
	err := f.db.Table(database.CAMPAIGN_EVENT_TABLE).Create(map[string]any{
		"id": uuid.NewString(), "created_at": utils.RFC3339UTC(at), "updated_at": utils.RFC3339UTC(at),
		"data": "password=secret", "metadata": `{"screen":"1920x1080"}`, "campaign_id": campaignID.String(),
		"event_id": timelineEventID(name).String(), "recipient_id": recipientID.String(),
		"ip_address": "10.0.0.1", "user_agent": "Mozilla/5.0",
	}).Error
	if err != nil {
		f.t.Fatalf("create event: %v", err)
	}
}

// resultsJSON reads the results of the program through the service
func (f *anonymizeFixture) resultsJSON() string {
	f.t.Helper()
	results, err := f.svc.GetResults(f.ctx, authorizedSession(f.t), &f.programID, "")
	if err != nil {
		f.t.Fatalf("results: %v", err)
	}
	b, err := json.Marshal(results)
	if err != nil {
		f.t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func (f *anonymizeFixture) programCampaigns() any {
	return f.db.Model(&database.Campaign{}).Select("id").Where("program_id = ?", f.programID)
}

// checkAnonymized checks no send or event of the program names a person,
// every person has one pseudonym in every campaign, the events lost their
// data and the enrollments their pseudonyms
func (f *anonymizeFixture) checkAnonymized() {
	f.t.Helper()
	var sends []database.CampaignRecipient
	f.db.Where("campaign_id IN (?)", f.programCampaigns()).Find(&sends)
	if len(sends) != len(f.people)*len(f.campaigns) {
		f.t.Fatalf("expected every send kept, got %d", len(sends))
	}
	pseudonyms := map[uuid.UUID]int{}
	for _, s := range sends {
		if s.RecipientID != nil || s.AnonymizedID == nil {
			f.t.Fatalf("a send keeps a person or has no pseudonym: %+v", s)
		}
		pseudonyms[*s.AnonymizedID]++
	}
	if len(pseudonyms) != len(f.people) {
		f.t.Fatalf("expected one pseudonym per person, got %d", len(pseudonyms))
	}
	for id, n := range pseudonyms {
		if n != len(f.campaigns) {
			f.t.Fatalf("pseudonym %v must hold one send per campaign, holds %d", id, n)
		}
	}
	var events []database.CampaignEvent
	f.db.Where("campaign_id IN (?)", f.programCampaigns()).Find(&events)
	if len(events) == 0 {
		f.t.Fatal("expected the events kept")
	}
	personEvents := 0
	for _, e := range events {
		if e.RecipientID != nil {
			f.t.Fatalf("an event keeps a person: %+v", e)
		}
		// an event of the campaign itself, such as its close, has no person
		if e.AnonymizedID == nil {
			continue
		}
		personEvents++
		if pseudonyms[*e.AnonymizedID] == 0 {
			f.t.Fatalf("an event is not on a send pseudonym: %+v", e)
		}
		if e.IPAddress != "" || e.Data != "anonymized" || e.UserAgent != "anonymized" || e.Metadata != "" {
			f.t.Fatalf("an event keeps its data: %+v", e)
		}
	}
	if personEvents < 4 {
		f.t.Fatalf("expected the events of the people kept, got %d", personEvents)
	}
	// no enrollment is left to tie a person to the program or a pseudonym
	var enrolled int64
	f.db.Model(&database.ProgramEnrollment{}).Where("program_id = ?", f.programID).Count(&enrolled)
	if enrolled != 0 {
		f.t.Fatalf("the enrollments must be removed, %d left", enrolled)
	}
	if f.loadProgram(f.programID).AnonymizingAt != nil {
		f.t.Fatal("the anonymizing mark must be cleared")
	}
	for _, c := range f.campaigns {
		if f.campaignRow(c).AnonymizedAt == nil {
			f.t.Fatalf("every hidden campaign must be marked anonymized")
		}
	}
	if !f.loadProgram(f.programID).IsAnonymized() {
		f.t.Fatal("the program must be marked anonymized")
	}
}

// TestProgramAnonymizeKeepsCounts checks Anonymize All of a closed program
// keeps every result count, gives each person one pseudonym in every hidden
// campaign, removes the names from every view and refuses a second run
func TestProgramAnonymizeKeepsCounts(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := f.resultsJSON()
	if !strings.Contains(before, `"repeatOffenders":1`) {
		t.Fatalf("the fixture must have one repeat offender: %s", before)
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.checkAnonymized()
	if after := f.resultsJSON(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
	view, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
	if err != nil {
		t.Fatalf("recipients: %v", err)
	}
	if !view.Anonymized || len(view.Rows) != 0 || view.Total != len(f.people) {
		t.Fatalf("expected counts only: %+v", view)
	}
	programs, err := f.svc.GetRecipientPrograms(f.ctx, session, &f.people[0], nil)
	if err != nil {
		t.Fatalf("recipient programs: %v", err)
	}
	if len(programs.Rows) != 0 {
		t.Fatalf("an anonymized program is not listed on the recipient page: %+v", programs.Rows)
	}
	if _, err := f.svc.GetRecipientSchedule(f.ctx, session, &f.programID, &f.people[0]); err == nil {
		t.Fatal("an anonymized program has no plan per person")
	}
	err = f.svc.anonymizeWait(f.ctx, session, &f.programID)
	var v errs.ValidationError
	if !errors.As(err, &v) || v.Err != errProgramAlreadyAnonymized {
		t.Fatalf("expected already anonymized, got %v", err)
	}
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAlreadyAnonymized {
		t.Fatalf("expected data anonymize refused on an anonymized program, got %v", err)
	}
	// a delete still removes everything
	if err := f.svc.DeleteByIDAndWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !f.programGone(f.programID) {
		t.Fatal("the anonymized program must be deleted")
	}
}

// TestProgramAnonymizeResumes checks a run cut short after one campaign is
// finished by the next, with the same pseudonym per person in both campaigns
func TestProgramAnonymizeResumes(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := f.resultsJSON()
	// the first part of a run: pseudonyms and the first campaign only
	if err := f.svc.ProgramRepository.AssignEnrollmentPseudonyms(f.ctx, &f.programID); err != nil {
		t.Fatalf("pseudonyms: %v", err)
	}
	if err := f.svc.anonymizeProgramCampaign(f.ctx, session, &f.programID, &f.campaigns[0]); err != nil {
		t.Fatalf("first campaign: %v", err)
	}
	if f.loadProgram(f.programID).IsAnonymized() {
		t.Fatal("the program is marked only when every campaign is done")
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.checkAnonymized()
	if after := f.resultsJSON(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
}

// TestProgramAnonymizeManualRunResumes checks a run started by hand that stops
// while the sends get their pseudonyms, before it is marked as begun, is found
// by the background pass and finished there
func TestProgramAnonymizeManualRunResumes(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := f.db.Exec(`CREATE TRIGGER fail_link BEFORE UPDATE OF anonymized_id ON campaign_recipients
		BEGIN SELECT RAISE(ABORT, 'link refused'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	// the request sets the run going, the background run then fails
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	if f.loadProgram(f.programID).IsAnonymized() {
		t.Fatal("the run must fail while the sends can not get their pseudonyms")
	}
	failed := f.loadProgram(f.programID)
	f.svc.applyRunState(failed)
	if failed.ActionFailure == nil || failed.ActionFailure.Action != programActionAnonymize {
		t.Fatalf("the failure must be shown, got %+v", failed.ActionFailure)
	}
	if err := f.db.Exec(`DROP TRIGGER fail_link`).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if f.loadProgram(f.programID).AnonymizingAt != nil {
		t.Fatal("the run stopped before it was marked as begun")
	}
	ids, err := f.svc.ProgramRepository.GetIDsToAnonymize(f.ctx, time.Now(), nil)
	if err != nil || len(ids) != 1 || *ids[0] != f.programID {
		t.Fatalf("the background pass must find the run: %v %v", ids, err)
	}
	// the failed run waits programAnonymizeRetryEvery, the wait is skipped here
	f.svc.anonymizes.retryAt = nil
	if err := f.svc.HandleAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("background: %v", err)
	}
	f.checkAnonymized()
	done := f.loadProgram(f.programID)
	f.svc.applyRunState(done)
	if done.ActionFailure != nil {
		t.Fatal("a finished run must clear the failure")
	}
}

// TestProgramAnonymizeRefusals checks Anonymize All refuses an open program,
// an anonymous program and a session without access, and changes nothing
func TestProgramAnonymizeRefusals(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	var v errs.ValidationError
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAnonymizeNotClosed {
		t.Fatalf("expected an open program refused, got %v", err)
	}
	if err := f.svc.anonymizeWait(f.ctx, unauthorizedSession(t), &f.programID); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused session, got %v", err)
	}
	if err := f.svc.anonymizeDataWait(f.ctx, unauthorizedSession(t), &f.programID); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("expected a refused session, got %v", err)
	}
	var linked int64
	f.db.Model(&database.CampaignRecipient{}).Where("campaign_id IN (?) AND recipient_id IS NOT NULL", f.programCampaigns()).Count(&linked)
	if linked != int64(len(f.people)*len(f.campaigns)) {
		t.Fatalf("a refused anonymize must change nothing, %d linked", linked)
	}
	f.setProgram(f.programID, map[string]any{"is_anonymous": true, "closed_at": time.Now().UTC()})
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAnonymizeAnonymous {
		t.Fatalf("expected an anonymous program refused, got %v", err)
	}
}

// TestProgramAnonymizeDataKeepsPeople checks Anonymize Data removes the data
// of every event and keeps who it is about, can run again while the program
// is open and is refused once the program is closed and done
func TestProgramAnonymizeDataKeepsPeople(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	before := f.resultsJSON()
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize data: %v", err)
	}
	var events []database.CampaignEvent
	f.db.Where("campaign_id IN (?) AND recipient_id IS NOT NULL", f.programCampaigns()).Find(&events)
	if len(events) != 4 {
		t.Fatalf("every event must keep its person, got %d", len(events))
	}
	for _, e := range events {
		if e.RecipientID == nil {
			continue
		}
		if e.IPAddress != "" || e.Data != "anonymized" || e.UserAgent != "anonymized" || e.Metadata != "" {
			t.Fatalf("an event keeps its data: %+v", e)
		}
	}
	if !f.loadProgram(f.programID).IsDataAnonymized() || f.campaignRow(f.campaigns[0]).DataAnonymizedAt == nil {
		t.Fatal("the program and its campaigns must be marked")
	}
	if after := f.resultsJSON(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
	// a newer event of an open program is removed by a new run
	f.trackedEvent(f.campaigns[1], f.people[2], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, time.Now().UTC())
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize data again: %v", err)
	}
	var left int64
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id IN (?) AND data != ?", f.programCampaigns(), "anonymized").Count(&left)
	if left != 0 {
		t.Fatalf("expected every event scrubbed, %d left", left)
	}
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	// the last run was before the close, data from up to the close is removed
	f.trackedEvent(f.campaigns[0], f.people[2], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, time.Now().UTC())
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize data after the close: %v", err)
	}
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id IN (?) AND data != ?", f.programCampaigns(), "anonymized").Count(&left)
	if left != 0 {
		t.Fatalf("expected the events up to the close scrubbed, %d left", left)
	}
	var v errs.ValidationError
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramDataAlreadyAnonymized {
		t.Fatalf("expected a closed and done program refused, got %v", err)
	}
	// Anonymize All still works after the data is anonymized
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.checkAnonymized()
}

// TestProgramAnonymizeScheduled checks the tasks run the scheduled dates:
// the data of an open program once its date passed, a closed program once
// its anonymize date passed, and nothing before or after
func TestProgramAnonymizeScheduled(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	buf := f.withAuditBuffer()
	future := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Minute)
	f.setProgram(f.programID, map[string]any{"data_anonymize_at": future})
	if err := f.svc.HandleDataAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("data task: %v", err)
	}
	if f.loadProgram(f.programID).IsDataAnonymized() {
		t.Fatal("a date not reached must not run")
	}
	f.setProgram(f.programID, map[string]any{"data_anonymize_at": past, "anonymize_at": past})
	if err := f.svc.HandleDataAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("data task: %v", err)
	}
	if !f.loadProgram(f.programID).IsDataAnonymized() {
		t.Fatal("the data must be anonymized once the date passed")
	}
	// an open program is not anonymized by the date, only once closed
	if err := f.svc.HandleAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("anonymize task: %v", err)
	}
	if f.loadProgram(f.programID).IsAnonymized() {
		t.Fatal("an open program must not be anonymized")
	}
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := f.svc.HandleAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("anonymize task: %v", err)
	}
	f.checkAnonymized()
	lines := auditLines(t, buf)
	if len(lines["Program.HandleDataAnonymizePrograms"]) != 1 || len(lines["Program.HandleAnonymizePrograms"]) != 1 {
		t.Fatalf("expected one event per task that changed something: %v", lines)
	}
	// nothing left to do writes nothing
	buf.Reset()
	if err := f.svc.HandleAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("anonymize task: %v", err)
	}
	if err := f.svc.HandleDataAnonymizePrograms(f.ctx, session); err != nil {
		t.Fatalf("data task: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("a task that changes nothing writes nothing: %s", buf.String())
	}
}

// TestProgramAnonymizeAudit checks the actions write one success event each
// with the program id and a refused session writes a denied event
func TestProgramAnonymizeAudit(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	buf := f.withAuditBuffer()
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize data: %v", err)
	}
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	_ = f.svc.anonymizeWait(f.ctx, unauthorizedSession(t), &f.programID)
	lines := auditLines(t, buf)
	for _, name := range []string{"Program.AnonymizeData", "Program.Anonymize"} {
		ev := lines[name]
		if len(ev) == 0 || ev[0]["result"] != "success" {
			t.Fatalf("%s: expected a success event, got %v", name, ev)
		}
		if eventDetails(t, ev[0])["id"] != f.programID.String() {
			t.Fatalf("%s: expected the program id: %v", name, ev[0])
		}
	}
	// the request writes the start, the background pass the finish with who
	// asked, and the refused session a denied event
	ev := lines["Program.Anonymize"]
	if len(ev) != 3 || ev[2]["result"] != "denied" {
		t.Fatalf("expected a denied event, got %v", ev)
	}
	if d := eventDetails(t, ev[1]); d["reason"] != "anonymize finished" || d["requestedBy"] == nil {
		t.Fatalf("expected the finish with who asked, got %v", ev[1])
	}
	if strings.Contains(buf.String(), "alice@example.com") {
		t.Fatal("the events must not name a person")
	}
}

// TestProgramAnonymizeDates checks the anonymize dates of a create follow
// the start and end, and an update checks them with the stored values
func TestProgramAnonymizeDates(t *testing.T) {
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(24 * time.Hour)
	at := func(t time.Time) *model.Program {
		p := &model.Program{}
		p.AnonymizeAt.Set(t)
		return p
	}
	data := func(t time.Time) *model.Program {
		p := &model.Program{}
		p.DataAnonymizeAt.Set(t)
		return p
	}
	cases := []struct {
		name      string
		p         *model.Program
		anonymous bool
		end       *time.Time
		ok        bool
	}{
		{"anonymize needs an end", at(end), false, nil, false},
		{"anonymize after the end", at(end.Add(time.Hour)), false, &end, true},
		{"anonymize at the end", at(end), false, &end, true},
		{"anonymize before the end", at(end.Add(-time.Hour)), false, &end, false},
		{"anonymous takes no anonymize date", at(end.Add(time.Hour)), true, &end, false},
		{"data without an end after the start", data(start.Add(time.Hour)), false, nil, true},
		{"data before the start", data(start.Add(-time.Minute)), false, nil, false},
		{"data before the end", data(end.Add(-time.Minute)), false, &end, false},
	}
	for _, c := range cases {
		err := c.p.ValidateAnonymizeDates(c.anonymous, &start, c.end)
		if (err == nil) != c.ok {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	// an update keeps a stored end the date is checked against
	existing := &model.Program{}
	existing.EndAt.Set(end)
	existing.StartAt.Set(start)
	if err := validateAnonymizeDatesUpdate(at(end.Add(-time.Hour)), existing, false); err == nil {
		t.Fatal("an update must check the date against the stored end")
	}
	if err := validateAnonymizeDatesUpdate(at(end.Add(time.Hour)), existing, false); err != nil {
		t.Fatalf("a date after the stored end is fine: %v", err)
	}
}

// TestProgramAnonymizeKeepsCountsWithoutEnrollments checks the people counts
// of the program page, the list and the results come from the counts stored
// when Anonymize All began, with the enrollments removed
func TestProgramAnonymizeKeepsCountsWithoutEnrollments(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	// one person left before the close, one completed
	f.db.Model(&database.ProgramEnrollment{}).Where("recipient_id = ?", f.people[1]).Updates(map[string]any{"status": model.ProgramEnrollmentExited})
	f.db.Model(&database.ProgramEnrollment{}).Where("recipient_id = ?", f.people[2]).Updates(map[string]any{"status": model.ProgramEnrollmentCompleted})
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := f.resultsJSON()
	viewBefore, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
	if err != nil {
		t.Fatalf("recipients: %v", err)
	}
	listBefore, err := f.svc.ProgramRepository.GetListSummaries(f.ctx, []*uuid.UUID{&f.programID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.checkAnonymized()
	if after := f.resultsJSON(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
	view, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
	if err != nil {
		t.Fatalf("recipients: %v", err)
	}
	if view.Total != viewBefore.Total || view.Exited != viewBefore.Exited || view.Completed != viewBefore.Completed || view.Stopped != viewBefore.Stopped {
		t.Fatalf("the people counts must be kept: before %+v after %+v", viewBefore, view)
	}
	list, err := f.svc.ProgramRepository.GetListSummaries(f.ctx, []*uuid.UUID{&f.programID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	b, a := listBefore[f.programID], list[f.programID]
	if a.Total != b.Total || a.People != b.People || a.Stopped != b.Stopped || a.Enrolled != 0 {
		t.Fatalf("the list counts must be kept: before %+v after %+v", b, a)
	}
}

// TestProgramAnonymizeFinishesMarkedRun checks an Anonymize All that stopped
// half way on a program without an anonymize date is finished by the task,
// with the counts taken before the stop
func TestProgramAnonymizeFinishesMarkedRun(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := f.resultsJSON()
	// the first part of a run: the mark, the pseudonyms, one campaign and
	// some enrollments removed
	counts, err := f.svc.ProgramRepository.CountEnrollmentsByStatus(f.ctx, &f.programID)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if began, err := f.svc.ProgramRepository.BeginAnonymize(f.ctx, &f.programID, peopleCountsOf(counts)); err != nil || !began {
		t.Fatalf("begin: %v %v", began, err)
	}
	if err := f.svc.ProgramRepository.AssignEnrollmentPseudonyms(f.ctx, &f.programID); err != nil {
		t.Fatalf("pseudonyms: %v", err)
	}
	if err := f.svc.anonymizeProgramCampaign(f.ctx, session, &f.programID, &f.campaigns[0]); err != nil {
		t.Fatalf("first campaign: %v", err)
	}
	program := f.loadProgram(f.programID)
	if program.AnonymizingAt == nil || program.IsAnonymized() {
		t.Fatal("the program must be marked as being anonymized")
	}
	// the campaigns the run has not reached still name people, so no events
	// are listed while it runs
	if view, err := f.svc.GetEvents(f.ctx, session, &f.programID, &vo.QueryArgs{Limit: 50}, nil); err != nil || len(view.Rows) != 0 {
		t.Fatalf("events while the anonymize runs: %v %v", view, err)
	}
	// a second begin keeps the counts taken first
	if began, _ := f.svc.ProgramRepository.BeginAnonymize(f.ctx, &f.programID, repository.ProgramPeopleCounts{Total: 99}); began {
		t.Fatal("a begun anonymize must keep its mark and counts")
	}
	ids, err := f.svc.ProgramRepository.GetIDsToAnonymize(f.ctx, time.Now(), nil)
	if err != nil || len(ids) != 1 || *ids[0] != f.programID {
		t.Fatalf("the task must find the marked program: %v %v", ids, err)
	}
	// a program whose last try failed waits before it is listed again
	st := &f.svc.anonymizes
	st.failed(&st.retryAt, f.programID, time.Now())
	if ids, _ := f.svc.ProgramRepository.GetIDsToAnonymize(f.ctx, time.Now(), st.held(&st.retryAt, time.Now())); len(ids) != 0 {
		t.Fatalf("a held program must be left out: %v", ids)
	}
	if held := st.held(&st.retryAt, time.Now().Add(programAnonymizeRetryEvery)); len(held) != 0 {
		t.Fatalf("the wait must end after programAnonymizeRetryEvery: %v", held)
	}
	if err := f.svc.AnonymizeDueInBackground(f.ctx, session); err != nil {
		t.Fatalf("background: %v", err)
	}
	if err := f.svc.waitForAnonymizes(f.ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	f.checkAnonymized()
	if after := f.resultsJSON(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
}

// TestProgramAnonymizeDataNewDate checks a data anonymize date runs again
// when it is later than the last run, by hand or by an earlier date, and a
// new date after a run is checked and accepted on an update
func TestProgramAnonymizeDataNewDate(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	// a run by hand while the program is open
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize data: %v", err)
	}
	ran := *f.loadProgram(f.programID).DataAnonymizedAt
	// data collected after the run
	f.trackedEvent(f.campaigns[1], f.people[2], data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, time.Now().UTC())
	// a date before the last run is done already
	f.setProgram(f.programID, map[string]any{"data_anonymize_at": ran.Add(-time.Minute)})
	if ids, _ := f.svc.ProgramRepository.GetIDsToDataAnonymize(f.ctx, time.Now(), nil); len(ids) != 0 {
		t.Fatalf("a date before the last run must not run again: %v", ids)
	}
	// a new date later than the last run is accepted by an update and runs
	existing := f.loadProgram(f.programID)
	later := ran.Add(time.Second)
	incoming := &model.Program{}
	incoming.DataAnonymizeAt.Set(later)
	if err := validateAnonymizeDatesUpdate(incoming, existing, false); err != nil {
		t.Fatalf("a new date after the last run must be accepted: %v", err)
	}
	f.setProgram(f.programID, map[string]any{"data_anonymize_at": later})
	time.Sleep(1100 * time.Millisecond)
	if err := f.svc.AnonymizeDueInBackground(f.ctx, session); err != nil {
		t.Fatalf("background: %v", err)
	}
	if err := f.svc.waitForAnonymizes(f.ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	var left int64
	f.db.Model(&database.CampaignEvent{}).Where("campaign_id IN (?) AND recipient_id IS NOT NULL AND data != ?", f.programCampaigns(), "anonymized").Count(&left)
	if left != 0 {
		t.Fatalf("the data collected after the first run must be removed, %d left", left)
	}
	if ids, _ := f.svc.ProgramRepository.GetIDsToDataAnonymize(f.ctx, time.Now(), nil); len(ids) != 0 {
		t.Fatalf("a date that ran must not run again: %v", ids)
	}
	// a stored date before the start is not checked again once it ran
	f.setProgram(f.programID, map[string]any{"data_anonymize_at": time.Now().UTC().AddDate(-1, 0, 0)})
	if err := validateAnonymizeDatesUpdate(&model.Program{}, f.loadProgram(f.programID), false); err != nil {
		t.Fatalf("a date that ran is not checked again: %v", err)
	}
}

// TestProgramAnonymizeClaims checks two anonymize runs of one program never
// overlap and a close or delete during one is refused with a message that
// says so, and a waiting delete goes on once it ends
func TestProgramAnonymizeClaims(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if !f.svc.claimAnonymizing(f.programID) {
		t.Fatal("claim")
	}
	var v errs.ValidationError
	if err := f.svc.anonymizeDataWait(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAnonymizing {
		t.Fatalf("a second anonymize must be refused, got %v", err)
	}
	err := f.svc.closeWait(f.ctx, session, &f.programID)
	if !errors.As(err, &v) || v.Err != errProgramAnonymizing || !strings.Contains(err.Error(), "anonymized") {
		t.Fatalf("a close during an anonymize must say so, got %v", err)
	}
	if err := f.svc.DeleteByID(f.ctx, session, &f.programID); !errors.As(err, &v) || v.Err != errProgramAnonymizing {
		t.Fatalf("a delete during an anonymize must say so, got %v", err)
	}
	if f.loadProgram(f.programID).IsClosed() {
		t.Fatal("a refused close changes nothing")
	}
	// a delete that waits goes on once the anonymize ends
	go func() {
		time.Sleep(300 * time.Millisecond)
		f.svc.releaseAnonymizing(f.programID)
	}()
	if err := f.svc.DeleteByIDAndWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("delete and wait: %v", err)
	}
	if !f.programGone(f.programID) {
		t.Fatal("the program must be deleted")
	}
}

// deleteRecipient deletes a recipient the way the recipient delete and the
// SCIM prune do
func (f *programTest) deleteRecipient(id uuid.UUID) {
	f.t.Helper()
	svc := &Recipient{
		Common:                      Common{Logger: f.svc.Logger},
		RecipientRepository:         &repository.Recipient{DB: f.db},
		RecipientGroupRepository:    &repository.RecipientGroup{DB: f.db},
		CampaignRepository:          &repository.Campaign{DB: f.db},
		CampaignRecipientRepository: &repository.CampaignRecipient{DB: f.db},
		ProgramRepository:           f.svc.ProgramRepository,
		ProgramService:              f.svc,
	}
	if err := svc.deleteRecipientByID(f.ctx, &id); err != nil {
		f.t.Fatalf("delete recipient: %v", err)
	}
}

// checkUnlinkedEnrollments checks no enrollment of the program names a
// deleted person or holds a detail of theirs
func (f *anonymizeFixture) checkUnlinkedEnrollments(deleted ...uuid.UUID) {
	f.t.Helper()
	var rows []database.ProgramEnrollment
	f.db.Where("program_id = ? AND recipient_id IS NULL", f.programID).Find(&rows)
	if len(rows) != len(deleted) {
		f.t.Fatalf("expected %d unlinked enrollments, got %d", len(deleted), len(rows))
	}
	for _, e := range rows {
		if e.AnonymizedID != nil || e.Status != model.ProgramEnrollmentExited || e.Position != 0 || e.LoopCount != 0 ||
			e.ExitedAt != nil || e.NextDueAt != nil || e.LastSendAt != nil || e.RoundStartAt != nil ||
			e.MissingSince != nil || e.CompletedAt != nil || e.LastStepID != nil || e.AdvanceFailures != 0 {
			f.t.Fatalf("an unlinked enrollment keeps a detail of the person: %+v", e)
		}
		if e.CreatedAt == nil || e.CreatedAt.UTC().Day() != 1 || e.CreatedAt.UTC().Hour() != 0 {
			f.t.Fatalf("the enrollment time must be moved to the start of its month: %v", e.CreatedAt)
		}
	}
	var named int64
	f.db.Model(&database.ProgramEnrollment{}).Where("recipient_id IN ?", deleted).Count(&named)
	if named != 0 {
		f.t.Fatalf("an enrollment still names a deleted person")
	}
}

// TestProgramRecipientDeleteKeepsCounts checks a deleted recipient stays
// counted once in the people of an active and of a closed program, as a
// removed person, with nothing on the enrollment that leads back to them, and
// that a new recipient with the same email is a new person
func TestProgramRecipientDeleteKeepsCounts(t *testing.T) {
	for _, closed := range []bool{false, true} {
		f := newAnonymizeFixture(t)
		session := authorizedSession(t)
		if closed {
			if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
				t.Fatalf("close: %v", err)
			}
		}
		results := func() *ProgramResults {
			f.svc.forgetProgramResults(&f.programID)
			r, err := f.svc.GetResults(f.ctx, session, &f.programID, "")
			if err != nil {
				t.Fatalf("results: %v", err)
			}
			return r
		}
		before := results()
		viewBefore, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
		if err != nil {
			t.Fatalf("recipients: %v", err)
		}
		// alice clicked twice, bob once
		f.deleteRecipient(f.people[0])
		f.deleteRecipient(f.people[1])
		f.checkUnlinkedEnrollments(f.people[0], f.people[1])
		after := results()
		b, _ := json.Marshal(before.Summary)
		a, _ := json.Marshal(after.Summary)
		if string(a) != string(b) {
			t.Fatalf("closed=%v the totals must not change\nbefore %s\nafter  %s", closed, b, a)
		}
		if after.Summary.Recipients != len(f.people) {
			t.Fatalf("closed=%v every person must still count once: %d", closed, after.Summary.Recipients)
		}
		view, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
		if err != nil {
			t.Fatalf("recipients: %v", err)
		}
		if view.Total != viewBefore.Total || view.Exited != viewBefore.Exited+2 || len(view.Rows) != len(f.people)-2 || view.Count != int64(len(f.people)-2) {
			t.Fatalf("closed=%v the deleted people count as removed and have no row: before %+v after %+v", closed, viewBefore, view)
		}
		plans, err := f.svc.ProgramRepository.GetEnrollmentPlansByProgramID(f.ctx, &f.programID)
		if err != nil || len(plans) != len(f.people)-2 {
			t.Fatalf("closed=%v the planners must not see an unlinked enrollment: %d %v", closed, len(plans), err)
		}
		// the same email again is a new person with a new enrollment
		again := f.recipient("alice@example.com")
		f.enroll(f.programID, again, 0)
		var total int64
		f.db.Model(&database.ProgramEnrollment{}).Where("program_id = ?", f.programID).Count(&total)
		if total != int64(len(f.people)+1) {
			t.Fatalf("closed=%v a recipient made again must get a new enrollment: %d", closed, total)
		}
		if closed {
			// Anonymize All counts the unlinked people too
			f.db.Where("recipient_id = ?", again).Delete(&database.ProgramEnrollment{})
			if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
				t.Fatalf("anonymize: %v", err)
			}
			view, err := f.svc.GetRecipientsView(f.ctx, session, &f.programID, nil)
			if err != nil || view.Total != len(f.people) {
				t.Fatalf("the stored counts must hold every person: %+v %v", view, err)
			}
		}
	}
}
