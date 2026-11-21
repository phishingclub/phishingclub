package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
)

// anonCloseFixture is an anonymous program with sends over several months
type anonCloseFixture struct {
	*programTest
	programID  uuid.UUID
	loc        *time.Location
	pseudonyms []uuid.UUID
	unsent     []uuid.UUID
	months     []time.Time
	enrolled   []*database.ProgramEnrollment
}

// newAnonCloseFixture seeds 12 people sent one template per month for four
// settled months, with clicks and reports, two never sent rows with an event,
// and the department and title kept on every send
func newAnonCloseFixture(t *testing.T) *anonCloseFixture {
	p := newProgramLifecycleTest(t)
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		loc = time.UTC
	}
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true, "time_zone": loc.String()})
	f := &anonCloseFixture{programTest: p, programID: programID, loc: loc}
	now := time.Now().In(loc)
	for i := 5; i >= 2; i-- {
		f.months = append(f.months, time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, loc))
	}
	steps := []uuid.UUID{p.step(programID, 0, true), p.step(programID, 1, true)}
	campaigns := []uuid.UUID{}
	for i := range f.months {
		c := p.stepCampaign(programID, steps[i%2], i/2)
		p.db.Model(&database.Campaign{}).Where("id = ?", c).Update("is_anonymous", true)
		campaigns = append(campaigns, c)
	}
	open := p.stepCampaign(programID, steps[0], 2)
	p.db.Model(&database.Campaign{}).Where("id = ?", open).Update("is_anonymous", true)
	visit := timelineEventID(data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	reported := timelineEventID(data.EVENT_CAMPAIGN_RECIPIENT_REPORTED)
	scheduled := timelineEventID(data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED)
	event := func(campaignID, pseudonym, eventID uuid.UUID, at time.Time) {
		id := uuid.New()
		p.create(&database.CampaignEvent{
			ID: &id, CreatedAt: &at, UpdatedAt: &at,
			CampaignID: &campaignID, AnonymizedID: &pseudonym, EventID: &eventID,
		})
	}
	for i := 0; i < 12; i++ {
		rid := p.recipient(uuid.NewString() + "@example.com")
		e := p.enroll(programID, rid, 1)
		pseudonym := uuid.New()
		f.pseudonyms = append(f.pseudonyms, pseudonym)
		created := f.months[0].Add(time.Duration(i) * 7 * time.Hour)
		fields := map[string]any{
			"anonymized_id": pseudonym, "position": 2, "created_at": created.UTC(),
		}
		if i == 11 {
			fields["status"] = model.ProgramEnrollmentExited
			fields["exited_at"] = f.months[2].Add(50 * time.Hour).UTC()
		}
		p.setEnrollment(e.ID, fields)
		f.enrolled = append(f.enrolled, e)
		for m, c := range campaigns {
			sentAt := f.months[m].AddDate(0, 0, 9).Add(time.Duration(i) * time.Hour)
			id := p.send(c, rid, false)
			p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(map[string]any{
				"sent_at": utils.RFC3339UTC(sentAt), "send_at": utils.RFC3339UTC(sentAt),
				"anonymized_id": pseudonym, "department": "Sales", "position": "Lead",
			})
			if (i+m)%2 == 0 {
				event(c, pseudonym, visit, sentAt.Add(time.Hour))
			}
			if i >= 6 && m%2 == 1 {
				event(c, pseudonym, reported, sentAt.Add(2*time.Hour))
			}
		}
		if i < 2 {
			id := p.send(open, rid, false)
			p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(map[string]any{
				"send_at": utils.RFC3339UTC(now.AddDate(0, 0, 3)), "anonymized_id": pseudonym,
			})
			f.unsent = append(f.unsent, id)
			event(open, pseudonym, scheduled, time.Now().UTC())
		}
	}
	return f
}

// results computes the results of every time range and of every month from
// a new read of the database, without the cache. The enrollment counts of
// the tiles are left out, a close stops the active people.
func (f *anonCloseFixture) results() string {
	f.t.Helper()
	program := f.loadProgram(f.programID)
	now := time.Now()
	st, err := f.svc.loadProgramFactsStore(f.ctx, program, now)
	if err != nil {
		f.t.Fatalf("load facts: %v", err)
	}
	out := map[string]any{}
	for _, key := range []string{ProgramTimeRangeAll, ProgramTimeRange12Months, ProgramTimeRange90Days, ProgramTimeRange30Days} {
		in, _, err := f.svc.programResultsInputs(f.ctx, program, now, nil, nil)
		if err != nil {
			f.t.Fatalf("inputs: %v", err)
		}
		r := st.programResults(in, &programTilesPeriod{key: key, from: programTimeRangeFrom(key, now, f.loc)}, false)
		r.Tiles.Recipients, r.Tiles.Completed, r.Tiles.Removed = 0, 0, 0
		out[key] = map[string]any{"summary": r.Summary, "months": r.Months, "templates": r.Templates, "tiles": r.Tiles}
	}
	for _, m := range f.months {
		to := m.AddDate(0, 1, 0)
		in, _, err := f.svc.programResultsInputs(f.ctx, program, now, &m, &to)
		if err != nil {
			f.t.Fatalf("inputs: %v", err)
		}
		out[m.Format("2006-01")] = st.programResults(in, nil, false).Summary
	}
	b, err := json.Marshal(out)
	if err != nil {
		f.t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// checkUnlinked checks no pseudonym holds more than one month of sends, no
// old pseudonym is left, every event with a pseudonym has its send in its
// campaign, the never sent rows and their events are gone, the department
// and title are cleared and the enrollments are rounded
func (f *anonCloseFixture) checkUnlinked() {
	f.t.Helper()
	var sends []database.CampaignRecipient
	f.db.Where("campaign_id IN (?)", f.db.Model(&database.Campaign{}).Select("id").Where("program_id = ?", f.programID)).Find(&sends)
	old := map[uuid.UUID]bool{}
	for _, p := range f.pseudonyms {
		old[p] = true
	}
	monthsOf := map[uuid.UUID]map[string]bool{}
	sendOf := map[[2]uuid.UUID]bool{}
	for _, s := range sends {
		if s.SentAt == nil {
			f.t.Fatalf("a never sent row is left: %v", s.ID)
		}
		if s.AnonymizedID == nil || old[*s.AnonymizedID] {
			f.t.Fatalf("a send keeps no pseudonym or its old one: %v", s.AnonymizedID)
		}
		if s.Department != "" || s.Position != "" {
			f.t.Fatalf("department and title must be cleared: %q %q", s.Department, s.Position)
		}
		if s.RecipientID != nil {
			f.t.Fatalf("a closed anonymous send keeps no person")
		}
		month := s.SentAt.In(f.loc).Format("2006-01")
		if monthsOf[*s.AnonymizedID] == nil {
			monthsOf[*s.AnonymizedID] = map[string]bool{}
		}
		monthsOf[*s.AnonymizedID][month] = true
		sendOf[[2]uuid.UUID{*s.CampaignID, *s.AnonymizedID}] = true
	}
	if len(sends) != 12*len(f.months) {
		f.t.Fatalf("expected %d sends, got %d", 12*len(f.months), len(sends))
	}
	for p, months := range monthsOf {
		if len(months) != 1 {
			f.t.Fatalf("pseudonym %v holds %d months", p, len(months))
		}
	}
	// two sends per person per month would share a pseudonym, here each
	// person has one send a month
	if len(monthsOf) != 12*len(f.months) {
		f.t.Fatalf("expected a pseudonym per person and month, got %d", len(monthsOf))
	}
	var events []database.CampaignEvent
	f.db.Where("campaign_id IN (?)", f.db.Model(&database.Campaign{}).Select("id").Where("program_id = ?", f.programID)).Find(&events)
	for _, e := range events {
		if e.AnonymizedID == nil {
			continue
		}
		if old[*e.AnonymizedID] || !sendOf[[2]uuid.UUID{*e.CampaignID, *e.AnonymizedID}] {
			f.t.Fatalf("an event is not on a send of its campaign: %+v", e)
		}
	}
	var unsentEvents int64
	f.db.Model(&database.CampaignEvent{}).Where("event_id = ?", timelineEventID(data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED)).
		Where("anonymized_id IN ?", f.pseudonyms).Count(&unsentEvents)
	if unsentEvents != 0 {
		f.t.Fatalf("the events of never sent rows must go, %d left", unsentEvents)
	}
	for i, e := range f.enrolled {
		row := f.enrollment(e.ID)
		local := row.CreatedAt.In(f.loc)
		if local.Day() != 1 || local.Hour() != 0 || local.Minute() != 0 {
			f.t.Fatalf("enrollment time must be the start of its month: %v", local)
		}
		if row.ExitedAt != nil || row.Position != 0 || row.LoopCount != 0 || row.AnonymizedID != nil {
			f.t.Fatalf("enrollment keeps a link: %+v", row)
		}
		if i == 11 && row.Status != model.ProgramEnrollmentExited {
			f.t.Fatalf("the status is kept, got %s", row.Status)
		}
	}
}

// TestAnonymousCloseUnlinksAndKeepsResults checks the close of an anonymous
// program gives a new pseudonym per person and month, clears the department
// and title, deletes the never sent rows, rounds the enrollments, and leaves
// every result count of every range and month as it was. Running the unlink
// again, as a close that stopped half way does, changes no count either.
func TestAnonymousCloseUnlinksAndKeepsResults(t *testing.T) {
	f := newAnonCloseFixture(t)
	before := f.results()
	var check struct {
		All struct {
			Summary *ProgramSummary `json:"summary"`
		} `json:"all"`
	}
	if err := json.Unmarshal([]byte(before), &check); err != nil || check.All.Summary.Sent == nil ||
		(check.All.Summary.ClickedLink == nil && check.All.Summary.Reported == nil) {
		t.Fatalf("the fixture must show counts to compare: %s", before)
	}
	if err := f.svc.closeWait(f.ctx, authorizedSession(t), &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	f.checkUnlinked()
	if after := f.results(); after != before {
		t.Fatalf("results changed by the close:\nbefore %s\nafter  %s", before, after)
	}
	// a close that stopped half way runs the unlink again
	if err := f.svc.unlinkAnonymousClose(f.ctx, &f.programID, f.loc, time.Now()); err != nil {
		t.Fatalf("unlink again: %v", err)
	}
	f.checkUnlinked()
	if after := f.results(); after != before {
		t.Fatalf("results changed by a second unlink:\nbefore %s\nafter  %s", before, after)
	}
}

// TestAnonymousCloseResumesHalfWay checks a rotation that stopped after part
// of the pseudonyms leaves counts the same once the close runs again
func TestAnonymousCloseResumesHalfWay(t *testing.T) {
	f := newAnonCloseFixture(t)
	before := f.results()
	if err := f.svc.ProgramRepository.RotateSendPseudonyms(f.ctx, &f.programID, f.pseudonyms[:5], f.loc, time.Now()); err != nil {
		t.Fatalf("partial rotate: %v", err)
	}
	if after := f.results(); after != before {
		t.Fatalf("results changed by a partial rotation:\nbefore %s\nafter  %s", before, after)
	}
	if err := f.svc.closeWait(f.ctx, authorizedSession(t), &f.programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	f.checkUnlinked()
	if after := f.results(); after != before {
		t.Fatalf("results changed after the resumed close:\nbefore %s\nafter  %s", before, after)
	}
}

// TestNamedCloseKeepsSends checks a program that is not anonymous keeps its
// never sent rows, pseudonyms and enrollment dates at close
func TestNamedCloseKeepsSends(t *testing.T) {
	p := newProgramLifecycleTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step := p.step(programID, 0, true)
	campaignID := p.stepCampaign(programID, step, 0)
	alice := p.recipient("alice@example.com")
	e := p.enroll(programID, alice, 1)
	p.setEnrollment(e.ID, map[string]any{"position": 1})
	held := p.sendAt(campaignID, alice, time.Now().Add(time.Hour), false)
	if err := p.svc.closeWait(p.ctx, authorizedSession(t), &programID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if row := p.campaignRecipient(held); row.CancelledAt == nil {
		t.Fatal("the never sent row of a named program is cancelled and kept")
	}
	if row := p.enrollment(e.ID); row.Position != 1 || row.LoopCount != 1 {
		t.Fatalf("a named program keeps its enrollment: %+v", row)
	}
}

// TestAnonymousEnrollmentStatus checks the recipient page shows an anonymous
// program as enrolled, left or closed only
func TestAnonymousEnrollmentStatus(t *testing.T) {
	cases := []struct {
		status string
		closed bool
		want   string
	}{
		{model.ProgramEnrollmentActive, false, model.ProgramEnrollmentActive},
		{model.ProgramEnrollmentCompleted, false, model.ProgramEnrollmentActive},
		{model.ProgramEnrollmentExited, false, model.ProgramEnrollmentExited},
		{model.ProgramEnrollmentCompleted, true, model.ProgramEnrollmentStopped},
		{model.ProgramEnrollmentStopped, true, model.ProgramEnrollmentStopped},
		{model.ProgramEnrollmentExited, true, model.ProgramEnrollmentExited},
	}
	for _, c := range cases {
		if got := anonymousEnrollmentStatus(c.status, c.closed); got != c.want {
			t.Errorf("%s closed=%v: got %s, want %s", c.status, c.closed, got, c.want)
		}
	}
}

// TestIdentityRemovalDeletesDeviceCodes checks the removal of old person links
// of an anonymous program also deletes the device code entries of the sends
// it unlinked, and keeps the entry of a send that still names its person
func TestIdentityRemovalDeletesDeviceCodes(t *testing.T) {
	p := newProgramServiceTest(t)
	session := authorizedSession(t)
	now := time.Now().UTC()
	programID := p.program(model.ProgramLoopComplete)
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	campaignID := p.stepCampaign(programID, p.step(programID, 0, true), 0)
	old, recent := p.recipient("old@example.com"), p.recipient("recent@example.com")
	code := func(recipientID uuid.UUID) uuid.UUID {
		id := uuid.New()
		p.create(&database.MicrosoftDeviceCode{
			ID: &id, CreatedAt: &now, UpdatedAt: &now, DeviceCode: "d", UserCode: "u", VerificationURI: "v",
			ExpiresAt: &now, ClientID: "c", CampaignID: &campaignID, RecipientID: &recipientID, Captured: true,
		})
		return id
	}
	for recipientID, sentAt := range map[uuid.UUID]time.Time{old: now.Add(-20 * 24 * time.Hour), recent: now.Add(-time.Hour)} {
		id := p.send(campaignID, recipientID, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(map[string]any{
			"anonymized_id": uuid.New(), "sent_at": utils.RFC3339UTC(sentAt),
		})
	}
	oldCode, recentCode := code(old), code(recent)
	p.svc.removeOldIdentityLinks(p.ctx, session, now)
	var n int64
	p.db.Model(&database.MicrosoftDeviceCode{}).Where("id = ?", oldCode).Count(&n)
	if n != 0 {
		t.Fatal("the device code of an unlinked send must be deleted")
	}
	p.db.Model(&database.MicrosoftDeviceCode{}).Where("id = ?", recentCode).Count(&n)
	if n != 1 {
		t.Fatal("the device code of a send that still names its person must be kept")
	}
}
