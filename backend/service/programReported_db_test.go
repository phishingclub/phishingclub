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
	"github.com/phishingclub/phishingclub/utils"
)

// reportedRows turns plain records into reported CSV rows, the first record
// being the header on line 1
func reportedRows(records [][]string) ([]string, []ReportedCSVRow) {
	rows := make([]ReportedCSVRow, 0, len(records)-1)
	for i, r := range records[1:] {
		rows = append(rows, ReportedCSVRow{Line: i + 2, Fields: r})
	}
	return records[0], rows
}

func TestProcessReportedCSVRefusesAnonymousProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	p.db.Model(&database.Program{}).Where("id = ?", programID).Update("is_anonymous", true)
	step := p.step(programID, 0, true)
	campaign := p.stepCampaign(programID, step, 0)
	alice := p.recipient("alice@example.com")
	p.enroll(programID, alice, 0)
	p.send(campaign, alice, true)

	header, rows := reportedRows([][]string{{"email"}, {"alice@example.com"}})
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, -1)
	var validation errs.ValidationError
	if out != nil || !errors.As(err, &validation) {
		t.Fatalf("expected a validation error, got %+v %v", out, err)
	}
	var count int64
	p.db.Model(&database.CampaignEvent{}).
		Where("event_id = ?", cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]).
		Count(&count)
	if count != 0 {
		t.Fatalf("a refused upload must record nothing, got %d reports", count)
	}
}

// TestProcessReportedCSVNamedProgram checks the program import gives the
// campaign import result: a time picks the send at or before it, a second
// report is already reported, failed rows carry their real line and reason,
// an ambiguous case is refused and an exact case wins
func TestProcessReportedCSVNamedProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	carlUpper := p.recipient("Carl@example.com")
	carlLower := p.recipient("carl@example.com")
	dora := p.recipient("dora@example.com")
	for _, r := range []uuid.UUID{alice, bob, carlUpper, carlLower, dora} {
		p.enroll(programID, r, 0)
	}
	// alice got step A in January and step B in March
	jan := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	for campaignID, at := range map[uuid.UUID]time.Time{campaignA: jan, campaignB: mar} {
		id := p.send(campaignID, alice, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(at))
	}
	for _, r := range []uuid.UUID{carlUpper, carlLower} {
		id := p.send(campaignA, r, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(jan))
	}

	header, rows := reportedRows([][]string{
		{"email", "reported"},
		{" ALICE@example.com ", "2026-02-01"}, // line 2, recorded on the January send
		{"alice@example.com", "2026-02-02"},   // line 3, already reported
		{"bob@example.com", "2026-02-01"},     // line 4, enrolled but never sent
		{"nobody@example.com", "2026-02-01"},  // line 5, not enrolled
		{"alice@example.com", "not a date"},   // line 6, bad date
		{"CARL@example.com", "2026-02-01"},    // line 7, two people in another case
		{"carl@example.com", "2026-02-01"},    // line 8, exact case wins
		{"not an email", "2026-02-01"},        // line 9, bad email
		{"dora@example.com", "2026-01-01"},    // line 10, no send before the date
		{"alice@example.com", "2026-03-15"},   // line 11, recorded on the March send
		{"alice@example.com"},                 // line 12, short row, the picked date column is missing
		{"alice@example.com", " "},            // line 13, the picked date column is empty
	})
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out.Processed != 3 || out.AlreadyReported != 1 || out.Failed != 8 || out.Skipped != 9 {
		t.Fatalf("got %+v", out)
	}
	wantLines := []int{4, 5, 6, 7, 9}
	if len(out.Errors) != reportedCSVMaxErrors {
		t.Fatalf("errors = %+v, want the first %d", out.Errors, reportedCSVMaxErrors)
	}
	for i, line := range wantLines {
		if out.Errors[i].Line != line || out.Errors[i].Reason == "" {
			t.Fatalf("error %d = %+v, want line %d", i, out.Errors[i], line)
		}
	}
	reported := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	var events []database.CampaignEvent
	p.db.Where("event_id = ?", reported).Find(&events)
	got := map[[2]uuid.UUID]bool{}
	for _, e := range events {
		got[[2]uuid.UUID{*e.CampaignID, *e.RecipientID}] = true
	}
	want := [][2]uuid.UUID{{campaignA, alice}, {campaignA, carlLower}, {campaignB, alice}}
	if len(events) != len(want) {
		t.Fatalf("expected %d reports, got %d", len(want), len(events))
	}
	for _, w := range want {
		if !got[w] {
			t.Fatalf("missing report %v", w)
		}
	}

	// unauthorized sessions are refused
	if _, err := p.svc.ProcessReportedCSV(p.ctx, unauthorizedSession(t), &programID, header, rows, 0, 1); err == nil {
		t.Fatal("expected an unauthorized session to be refused")
	}
	// a column outside the header is refused
	if _, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 5); err == nil {
		t.Fatal("expected a bad date column to be refused")
	}
}

// TestProcessReportedCSVSkipsClosedSends checks a row whose send is in a
// hidden campaign that closed before the report time fails with a reason and
// records nothing on a running program, while a report time before the close
// is recorded. On a program that is closing the late report is recorded at
// the close of the send instead.
func TestProcessReportedCSVSkipsClosedSends(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, step, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	jan := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	for _, r := range []uuid.UUID{alice, bob} {
		p.enroll(programID, r, 0)
		id := p.send(campaignID, r, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(jan))
	}
	closed := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	p.db.Model(&database.Campaign{}).Where("id = ?", campaignID).Update("closed_at", closed)

	header, rows := reportedRows([][]string{
		{"email", "reported"},
		{"alice@example.com", "2026-01-20"}, // line 2, before the send closed
		{"bob@example.com", "2026-02-10"},   // line 3, after the send closed
	})
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out.Processed != 1 || out.Failed != 1 || len(out.Errors) != 1 || out.Errors[0].Line != 3 {
		t.Fatalf("got %+v", out)
	}
	if out.Errors[0].Reason != `"bob@example.com" was reported after the send closed` {
		t.Fatalf("reason: %q", out.Errors[0].Reason)
	}
	reported := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	var events []database.CampaignEvent
	p.db.Where("event_id = ?", reported).Find(&events)
	if len(events) != 1 || *events[0].RecipientID != alice {
		t.Fatalf("only the report before the close is recorded: %+v", events)
	}

	// the program is closing: the late row and a row without a time land on
	// the close of the send
	p.setProgram(programID, map[string]any{"closing_at": time.Now().UTC(), "paused_at": time.Now().UTC()})
	header, rows = reportedRows([][]string{
		{"email", "reported"},
		{"bob@example.com", "2026-02-10"},
	})
	out, err = p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1)
	if err != nil || out.Processed != 1 || out.Failed != 0 {
		t.Fatalf("closing program: %+v %v", out, err)
	}
	var e database.CampaignEvent
	if err := p.db.Where("event_id = ? AND recipient_id = ?", reported, bob).First(&e).Error; err != nil {
		t.Fatalf("report: %v", err)
	}
	if !e.CreatedAt.Equal(closed) {
		t.Fatalf("report at %v, want the close of the send %v", e.CreatedAt, closed)
	}
}

// TestProcessReportedCSVReimportKeepsOneReport checks a file imported again
// after a newer send does not record the same report on the newer send: a row
// without a date counts the report already on any send, a row with a date the
// report on any send between the matched send and that date
func TestProcessReportedCSVReimportKeepsOneReport(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	p.enroll(programID, alice, 0)
	p.enroll(programID, bob, 0)
	sentAt := func(campaignID, r uuid.UUID, at time.Time) {
		id := p.send(campaignID, r, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(at))
	}
	jan := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	sentAt(campaignA, alice, jan)
	sentAt(campaignA, bob, jan)

	undated := func() {
		header, rows := reportedRows([][]string{{"email"}, {"alice@example.com"}})
		if _, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, -1); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	undated()
	// bob reported the January send in February, the report is recorded
	header, rows := reportedRows([][]string{{"email", "reported"}, {"bob@example.com", "2026-02-01"}})
	if _, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1); err != nil {
		t.Fatalf("process: %v", err)
	}
	// both get the March send, then the same files are imported again
	sentAt(campaignB, alice, mar)
	sentAt(campaignB, bob, mar)
	undated()
	if _, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1); err != nil {
		t.Fatalf("process: %v", err)
	}
	reported := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	for _, r := range []uuid.UUID{alice, bob} {
		var n int64
		p.db.Model(&database.CampaignEvent{}).Where("event_id = ? AND recipient_id = ?", reported, r).Count(&n)
		if n != 1 {
			t.Fatalf("expected one report of %s, got %d", r, n)
		}
	}
	// a dated row after the March send is a new report of that send
	header, rows = reportedRows([][]string{{"email", "reported"}, {"bob@example.com", "2026-03-15"}})
	if _, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1); err != nil {
		t.Fatalf("process: %v", err)
	}
	var n int64
	p.db.Model(&database.CampaignEvent{}).Where("event_id = ? AND recipient_id = ? AND campaign_id = ?", reported, bob, campaignB).Count(&n)
	if n != 1 {
		t.Fatalf("a dated report of the March send must be recorded, got %d", n)
	}
}

// TestProcessReportedCSVDatedRowRules checks a dated row is skipped only for a
// report already recorded on a phishing send on the same day: a report of a
// training message does not count, a report of an older send on another day
// does not stop a report of a newer send, and a report earlier on the same
// day does, as a row of today first read at the import time
func TestProcessReportedCSVDatedRowRules(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	stepA := p.step(programID, 0, true)
	stepB := p.step(programID, 1, true)
	stepT := p.step(programID, 2, true)
	campaignA := p.stepCampaign(programID, stepA, 0)
	campaignB := p.stepCampaign(programID, stepB, 0)
	training := p.stepCampaign(programID, stepT, 0)
	p.db.Model(&database.Campaign{}).Where("id = ?", training).Update("is_training", true)
	reported := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	sentAt := func(campaignID, r uuid.UUID, at time.Time) {
		id := p.send(campaignID, r, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(at))
	}
	report := func(campaignID, r uuid.UUID, at time.Time) {
		id := uuid.New()
		at = at.UTC()
		if err := p.db.Create(&database.CampaignEvent{
			ID: &id, CreatedAt: &at, UpdatedAt: &at, CampaignID: &campaignID, EventID: reported, RecipientID: &r,
		}).Error; err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	day := func(d, h int) time.Time { return time.Date(2026, 3, d, h, 0, 0, 0, time.UTC) }
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	carol := p.recipient("carol@example.com")
	for _, r := range []uuid.UUID{alice, bob, carol} {
		p.enroll(programID, r, 0)
	}
	// alice reported a training message, the simulation row still counts
	sentAt(campaignA, alice, day(10, 9))
	sentAt(training, alice, day(11, 9))
	report(training, alice, day(12, 9))
	// bob reported the older send after the newer one went out
	sentAt(campaignA, bob, day(1, 9))
	sentAt(campaignB, bob, day(10, 9))
	report(campaignA, bob, day(12, 9))
	// carol was recorded on the older send earlier on the day the newer one
	// went out, the same row read again lands on the newer send
	sentAt(campaignA, carol, day(1, 9))
	report(campaignA, carol, day(20, 10))
	sentAt(campaignB, carol, day(20, 14))

	header, rows := reportedRows([][]string{
		{"email", "reported"},
		{"alice@example.com", "2026-03-15"},
		{"bob@example.com", "2026-03-15"},
		{"carol@example.com", "2026-03-20"},
	})
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out.Processed != 2 || out.AlreadyReported != 1 {
		t.Fatalf("got %+v", out)
	}
	count := func(campaignID, r uuid.UUID) int64 {
		var n int64
		p.db.Model(&database.CampaignEvent{}).Where("event_id = ? AND recipient_id = ? AND campaign_id = ?", reported, r, campaignID).Count(&n)
		return n
	}
	if count(campaignA, alice) != 1 {
		t.Fatal("a report of a training message must not stop the simulation report")
	}
	if count(campaignB, bob) != 1 {
		t.Fatal("a report of an older send on another day must not stop the newer report")
	}
	if count(campaignB, carol) != 0 {
		t.Fatal("a report on the same day must not land on a second send")
	}
}
