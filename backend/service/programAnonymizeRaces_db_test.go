package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// exitedMember is a program with alice and bob in its group, alice with an
// exited enrollment who is back in the group and bob active. It returns the
// program and alice's enrollment.
func (p *programTest) exitedMember() (uuid.UUID, uuid.UUID, *database.ProgramEnrollment) {
	p.t.Helper()
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	programID, _ := p.membershipProgram(alice, bob)
	e := p.enroll(programID, alice, 0)
	exited := time.Now().UTC().Add(-24 * time.Hour)
	if err := p.db.Model(&database.ProgramEnrollment{}).Where("id = ?", e.ID).Updates(map[string]any{
		"status": model.ProgramEnrollmentExited, "exited_at": exited, "next_due_at": nil,
	}).Error; err != nil {
		p.t.Fatalf("exit: %v", err)
	}
	p.enroll(programID, bob, 0)
	return programID, alice, e
}

// checkUnlinkedStaysOut checks an enrollment of a deleted person stays
// exited without a person, is never read as due, and that a due batch and an
// advance of such a row, as from an older build, do not panic or send
func (p *programTest) checkUnlinkedStaysOut(programID uuid.UUID, enrollmentID *uuid.UUID) {
	p.t.Helper()
	row := p.enrollment(enrollmentID)
	if row.RecipientID != nil || row.Status != model.ProgramEnrollmentExited || row.NextDueAt != nil {
		p.t.Fatalf("the plan from the stale copy must not make the unlinked row active: %+v", row)
	}
	// a row made active without a person is still never read as due
	past := time.Now().UTC().Add(-time.Hour)
	if err := p.db.Model(&database.ProgramEnrollment{}).Where("id = ?", enrollmentID).Updates(map[string]any{
		"status": model.ProgramEnrollmentActive, "next_due_at": past,
	}).Error; err != nil {
		p.t.Fatalf("force active: %v", err)
	}
	due, err := p.svc.ProgramRepository.GetDueEnrollments(p.ctx, time.Now().Add(time.Hour), 100)
	if err != nil {
		p.t.Fatalf("due: %v", err)
	}
	for _, e := range due {
		if e.RecipientID == nil {
			p.t.Fatalf("an unlinked row must not be due: %+v", e)
		}
	}
	if callPanics(func() { _, _ = p.svc.advanceDueBatch(p.ctx, nil, time.Now()) }) {
		p.t.Fatal("a due batch must not panic")
	}
	forced := p.enrollment(enrollmentID)
	if callPanics(func() {
		_ = p.svc.advanceEnrollment(p.ctx, nil, p.loadProgram(programID), &forced, time.Now(), nil, nil)
	}) {
		p.t.Fatal("an advance of an unlinked row must not panic")
	}
	var unlinkedSends int64
	p.db.Model(&database.CampaignRecipient{}).Where("recipient_id IS NULL AND anonymized_id IS NULL").Count(&unlinkedSends)
	if unlinkedSends != 0 {
		p.t.Fatalf("no send may be made for nobody, %d made", unlinkedSends)
	}
	if after := p.enrollment(enrollmentID); after.Status != model.ProgramEnrollmentExited {
		p.t.Fatalf("an advance of an unlinked row must exit it: %+v", after)
	}
}

// TestProgramRecipientDeleteRacesMembershipPass checks a membership pass
// that read the enrollments before a person was deleted does not make the
// unlinked enrollment active again from its stale copy
func TestProgramRecipientDeleteRacesMembershipPass(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID, alice, e := p.exitedMember()
	listed := p.listedProgram(programID)
	members, err := p.svc.resolveMembership(p.ctx, listed)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	existing, err := p.svc.ProgramRepository.GetEnrollmentPlansByProgramID(p.ctx, &programID)
	if err != nil || len(existing) != 2 {
		t.Fatalf("existing: %d %v", len(existing), err)
	}
	// the person is deleted between the read and the writes of the pass
	p.deleteRecipient(alice)
	p.passMembers(listed, members, existing, time.Now())
	p.checkUnlinkedStaysOut(programID, e.ID)
}

// TestProgramRecipientDeleteRacesResume checks a resume that read the
// enrollments before a person was deleted does not make the unlinked
// enrollment active again from its stale copy
func TestProgramRecipientDeleteRacesResume(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID, alice, e := p.exitedMember()
	now := time.Now()
	plan, err := p.svc.resumeMembers(p.ctx, &programID, now)
	if err != nil || plan == nil {
		t.Fatalf("plan: %v %v", plan, err)
	}
	p.deleteRecipient(alice)
	m, changed, err := p.svc.resolveMembershipOnResume(p.ctx, &programID, plan, now)
	if err != nil || changed {
		t.Fatalf("resolve: changed %v err %v", changed, err)
	}
	if len(m.returners.returning) != 1 {
		t.Fatalf("the stale copy must plan alice as returning, got %d", len(m.returners.returning))
	}
	always := func(*model.Program) bool { return true }
	if done, err := p.svc.finishResumeMembership(p.ctx, &programID, m, now, always); err != nil || !done {
		t.Fatalf("finish: done %v err %v", done, err)
	}
	p.checkUnlinkedStaysOut(programID, e.ID)
}

// linkAll runs the first part of Anonymize All: the enrollment pseudonyms and
// every send linked to them
func (f *anonymizeFixture) linkAll() {
	f.t.Helper()
	session := authorizedSession(f.t)
	if err := f.svc.ProgramRepository.AssignEnrollmentPseudonyms(f.ctx, &f.programID); err != nil {
		f.t.Fatalf("pseudonyms: %v", err)
	}
	for i := range f.campaigns {
		if _, err := f.svc.linkProgramCampaign(f.ctx, session, &f.programID, &f.campaigns[i]); err != nil {
			f.t.Fatalf("link: %v", err)
		}
	}
}

// begin marks the program as being anonymized the way anonymizeProgram does
func (f *anonymizeFixture) begin() {
	f.t.Helper()
	counts, err := f.svc.ProgramRepository.CountEnrollmentsByStatus(f.ctx, &f.programID)
	if err != nil {
		f.t.Fatalf("counts: %v", err)
	}
	if began, err := f.svc.ProgramRepository.BeginAnonymize(f.ctx, &f.programID, peopleCountsOf(counts)); err != nil || !began {
		f.t.Fatalf("begin: %v %v", began, err)
	}
}

// freshResults reads the results with nothing cached
func (f *anonymizeFixture) freshResults() string {
	f.t.Helper()
	f.svc.forgetProgramResults(&f.programID)
	return f.resultsJSON()
}

// TestProgramAnonymizeMidRunResults checks the results read in the middle of
// an Anonymize All, with one campaign done and the other still naming its
// people, are the results from before, read fresh and from a store kept from
// before, and the per send export names no one
func TestProgramAnonymizeMidRunResults(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	before := f.resultsJSON()
	f.linkAll()
	if mid := f.freshResults(); mid != before {
		t.Fatalf("linked sends must not change the results\nbefore %s\nafter  %s", before, mid)
	}
	f.begin()
	if err := f.svc.anonymizeProgramCampaign(f.ctx, session, &f.programID, &f.campaigns[0]); err != nil {
		t.Fatalf("first campaign: %v", err)
	}
	// a store kept from before the mark names people another way, it is not used
	f.svc.resultsCache.invalidateResults(f.programID)
	if mid := f.resultsJSON(); mid != before {
		t.Fatalf("the results must not change during the run\nbefore %s\nafter  %s", before, mid)
	}
	if mid := f.freshResults(); mid != before {
		t.Fatalf("fresh results must not change during the run\nbefore %s\nafter  %s", before, mid)
	}
	rows := [][]string{}
	if err := f.svc.programSendCSV(f.ctx, f.loadProgram(f.programID), func(row []string) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(rows) != 1+len(f.people)*len(f.campaigns) {
		t.Fatalf("expected one row per send, got %d", len(rows))
	}
	for _, row := range rows[1:] {
		if row[0] != "anonymized" || row[1] != "anonymized" {
			t.Fatalf("a send must name no one once the run began: %v", row)
		}
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.checkAnonymized()
	if after := f.freshResults(); after != before {
		t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
	}
}

// TestProgramRecipientDeleteDuringAnonymize checks a person deleted while an
// Anonymize All runs keeps one pseudonym in every hidden campaign, so the
// results do not change. It covers a delete after the sends were linked and
// one campaign was done, a delete while only one campaign was linked, as a
// run of an older build that stopped half way leaves it, a delete before the
// run and a delete after it. A delete before the run counts the person as
// removed, so there the results are compared from after the delete.
func TestProgramRecipientDeleteDuringAnonymize(t *testing.T) {
	cases := []struct {
		name   string
		before func(f *anonymizeFixture)
		after  bool
	}{
		{"linked and one campaign done", func(f *anonymizeFixture) {
			f.linkAll()
			f.begin()
			if err := f.svc.anonymizeProgramCampaign(f.ctx, authorizedSession(f.t), &f.programID, &f.campaigns[0]); err != nil {
				f.t.Fatalf("first campaign: %v", err)
			}
		}, false},
		{"one campaign done and the other not linked", func(f *anonymizeFixture) {
			f.begin()
			if err := f.svc.ProgramRepository.AssignEnrollmentPseudonyms(f.ctx, &f.programID); err != nil {
				f.t.Fatalf("pseudonyms: %v", err)
			}
			if err := f.svc.anonymizeProgramCampaign(f.ctx, authorizedSession(f.t), &f.programID, &f.campaigns[0]); err != nil {
				f.t.Fatalf("first campaign: %v", err)
			}
		}, false},
		{"before the run", func(*anonymizeFixture) {}, false},
		{"after the run", func(*anonymizeFixture) {}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newAnonymizeFixture(t)
			session := authorizedSession(t)
			if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
				t.Fatalf("close: %v", err)
			}
			before := f.resultsJSON()
			c.before(f)
			if c.after {
				if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
					t.Fatalf("anonymize: %v", err)
				}
			}
			// alice clicked both templates
			f.deleteRecipient(f.people[0])
			if c.name == "before the run" {
				before = f.freshResults()
			}
			if !c.after {
				if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
					t.Fatalf("anonymize: %v", err)
				}
			}
			f.checkAnonymized()
			if after := f.freshResults(); after != before {
				t.Fatalf("the results must not change\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

// TestRecipientDeleteClearsEventMetadata checks a deleted person's events of
// a campaign without a program lose their browser metadata with the rest of
// their data and stay on the pseudonym of their send, and that Anonymize All
// of a campaign clears the metadata too
func TestRecipientDeleteClearsEventMetadata(t *testing.T) {
	f := newAnonymizeFixture(t)
	now := time.Now().UTC()
	plain := func() uuid.UUID {
		id := uuid.New()
		f.create(&database.Campaign{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "campaign " + id.String()})
		return id
	}
	deleted, anonymized := plain(), plain()
	carol, bob := f.people[2], f.people[1]
	f.send(deleted, carol, true)
	f.trackedEvent(deleted, carol, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, now)
	f.send(anonymized, bob, true)
	f.trackedEvent(anonymized, bob, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, now)
	f.deleteRecipient(carol)
	if err := f.svc.CampaignRecipientRepository.AnonymizeRowsOfCampaign(f.ctx, &anonymized); err != nil {
		t.Fatalf("anonymize campaign: %v", err)
	}
	for _, campaignID := range []uuid.UUID{deleted, anonymized} {
		sends := f.sendsIn(campaignID)
		if len(sends) != 1 || sends[0].AnonymizedID == nil || sends[0].RecipientID != nil {
			t.Fatalf("the send must be on a pseudonym: %+v", sends)
		}
		var events []database.CampaignEvent
		f.db.Where("campaign_id = ?", campaignID).Find(&events)
		if len(events) != 1 {
			t.Fatalf("expected the event kept, got %d", len(events))
		}
		e := events[0]
		if e.RecipientID != nil || e.AnonymizedID == nil || *e.AnonymizedID != *sends[0].AnonymizedID {
			t.Fatalf("the event must be on the pseudonym of its send: %+v", e)
		}
		if e.Metadata != "" || e.IPAddress != "" || e.UserAgent != "anonymized" || e.Data != "anonymized" {
			t.Fatalf("the event keeps data of the person: %+v", e)
		}
	}
}

// TestProgramReportedImportHoldsAnonymizeClaim checks a reported CSV import
// holds the anonymize claim of the program while it writes, is refused while
// an anonymize runs, and that an anonymize waits for the import
func TestProgramReportedImportHoldsAnonymizeClaim(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	header, rows := reportedRows([][]string{{"email"}, {"carol@example.com"}})
	// an anonymize runs: the import is refused and writes nothing
	if !f.svc.claimAnonymizing(f.programID) {
		t.Fatal("claim")
	}
	_, err := f.svc.ProcessReportedCSV(f.ctx, session, &f.programID, header, rows, 0, -1)
	var v errs.ValidationError
	if !errors.As(err, &v) || v.Err != errProgramAnonymizing {
		t.Fatalf("an import during an anonymize must be refused, got %v", err)
	}
	reported := func() int64 {
		var n int64
		f.db.Model(&database.CampaignEvent{}).
			Where("event_id = ?", cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]).
			Count(&n)
		return n
	}
	if reported() != 0 {
		t.Fatal("a refused import writes nothing")
	}
	f.svc.releaseAnonymizing(f.programID)
	// the import holds the claim while it writes, an anonymize then is skipped
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	checked := false
	if err := f.db.Callback().Create().Before("gorm:begin_transaction").Register("reported_claim", func(db *gorm.DB) {
		if db.Statement.Table != database.CAMPAIGN_EVENT_TABLE || checked {
			return
		}
		checked = true
		if !f.svc.isAnonymizingNow(f.programID) {
			t.Error("the import must hold the anonymize claim while it writes")
		}
		_, err := f.svc.anonymizeProgram(f.ctx, session, &f.programID)
		if !isProgramAnonymizeSkip(err) {
			t.Errorf("an anonymize during an import must be skipped, got %v", err)
		}
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove("reported_claim") })
	out, err := f.svc.ProcessReportedCSV(f.ctx, session, &f.programID, header, rows, 0, -1)
	if err != nil || out.Processed != 1 {
		t.Fatalf("import: %+v %v", out, err)
	}
	if !checked {
		t.Fatal("the import must write an event")
	}
	if f.svc.isAnonymizingNow(f.programID) {
		t.Fatal("the import must let the claim go")
	}
	if f.loadProgram(f.programID).AnonymizeBegun() {
		t.Fatal("the skipped anonymize must change nothing")
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize after the import: %v", err)
	}
	f.checkAnonymized()
}

// TestProgramReportWaitsForAnonymize checks the automated report of a
// program is not sent while an Anonymize All of it runs and goes out once the
// run finished
func TestProgramReportWaitsForAnonymize(t *testing.T) {
	f := newAnonymizeFixture(t)
	session := authorizedSession(t)
	if err := f.db.AutoMigrate(&database.Option{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	optionID := uuid.New()
	f.create(&database.Option{ID: &optionID, Key: data.OptionKeyReportPDFEnabled, Value: "true"})
	f.svc.OptionService = &Option{Common: f.svc.Common, OptionRepository: &repository.Option{DB: f.db}}
	f.setProgram(f.programID, map[string]any{
		"report_enabled": true,
		"start_at":       time.Now().UTC().AddDate(0, -4, 0),
		"started_at":     time.Now().UTC().AddDate(0, -4, 0),
	})
	if err := f.svc.closeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	// the report is not set up, so a try fails and waits to retry
	tried := func() bool {
		f.svc.reports.mu.Lock()
		defer f.svc.reports.mu.Unlock()
		_, ok := f.svc.reports.retryAt[f.programID]
		return ok
	}
	f.linkAll()
	f.begin()
	f.svc.sendDueReports(f.ctx, session, time.Now())
	if tried() {
		t.Fatal("a report must wait while the program is being anonymized")
	}
	if err := f.svc.anonymizeWait(f.ctx, session, &f.programID); err != nil {
		t.Fatalf("anonymize: %v", err)
	}
	f.svc.sendDueReports(f.ctx, session, time.Now())
	if !tried() {
		t.Fatal("the report must be tried once the run finished")
	}
}

// TestTemplateRenameRefreshesAnonymousResults checks the cached results of an
// anonymous program show a renamed template once the template service
// forgets them, without waiting for the day to change
func TestTemplateRenameRefreshesAnonymousResults(t *testing.T) {
	f := newAnonymizeFixture(t)
	f.setProgram(f.programID, map[string]any{"is_anonymous": true})
	var step database.ProgramStep
	if err := f.db.Where("program_id = ? AND position = 0", f.programID).First(&step).Error; err != nil {
		t.Fatalf("template: %v", err)
	}
	templateID := *step.CampaignTemplateID
	// the results name a campaign by the template it sent
	f.db.Model(&database.Campaign{}).Where("id = ?", f.campaigns[0]).Update("campaign_template_id", templateID)
	if before := f.resultsJSON(); strings.Contains(before, "Renamed lure") {
		t.Fatalf("the fixture must not hold the new name: %s", before)
	}
	f.db.Model(&database.CampaignTemplate{}).Where("id = ?", templateID).Update("name", "Renamed lure")
	if cached := f.resultsJSON(); strings.Contains(cached, "Renamed lure") {
		t.Fatal("the cached anonymous results are expected to be kept until told")
	}
	f.svc.ForgetTemplateResults(&templateID)
	if after := f.resultsJSON(); !strings.Contains(after, "Renamed lure") {
		t.Fatalf("the results must show the new name: %s", after)
	}
	// a template no program uses leaves the results cached
	other := uuid.New()
	f.svc.ForgetTemplateResults(&other)
	var nilProgram *Program
	nilProgram.ForgetTemplateResults(&templateID)
}
