package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// resultsEntries returns the cached result entries of the fixture program
func (f *resultsFixture) resultsEntries() []*programResultsEntry {
	f.svc.resultsCache.mu.Lock()
	defer f.svc.resultsCache.mu.Unlock()
	out := []*programResultsEntry{}
	for k, e := range f.svc.resultsCache.entries {
		if k.program == f.programID {
			out = append(out, e)
		}
	}
	return out
}

// TestProgramResultsEntryNamesFacts checks a cached result names the facts it
// was computed from instead of holding them, so facts the cache drops are
// freed, and that a result computed from facts read again is not reused
func TestProgramResultsEntryNamesFacts(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	f.sentTo(now.Add(-time.Hour))
	f.results(now)
	kept := f.keptFacts()
	entries := f.resultsEntries()
	if kept == nil || len(entries) != 1 || entries[0].factsLoad != kept.load || kept.load == 0 {
		t.Fatalf("the entry must name the kept facts, entries %d", len(entries))
	}
	// the facts are pushed out of the cache, as by other programs
	f.svc.resultsCache.mu.Lock()
	delete(f.svc.resultsCache.facts, f.programID)
	f.svc.resultsCache.mu.Unlock()
	if got := f.results(now.Add(time.Second)).Summary.SentTotal; got != 1 {
		t.Fatalf("sent total %d, want 1", got)
	}
	again := f.keptFacts()
	if again == nil || again.load == kept.load {
		t.Fatal("the facts must be read again with a new load number")
	}
	entries = f.resultsEntries()
	if len(entries) != 1 || entries[0].factsLoad != again.load {
		t.Fatal("the result must be computed again from the facts read again")
	}
}

// TestProgramFactsKeepUsesKeptSizes checks keepFacts does not read the size
// of the other kept stores, so a store under its write lock for a long
// refresh does not hold up keeping another
func TestProgramFactsKeepUsesKeptSizes(t *testing.T) {
	c := &programResultsCache{}
	now := time.Now()
	busy := newProgramFactsStore(uuid.New(), false)
	busy.sends = make([]programFactSend, 10)
	c.keepFacts(busy, now, false)
	if busy.keptSize != 10 {
		t.Fatalf("kept size %d, want 10", busy.keptSize)
	}
	busy.mu.Lock()
	defer busy.mu.Unlock()
	done := make(chan struct{})
	go func() {
		other := newProgramFactsStore(uuid.New(), false)
		other.sends = make([]programFactSend, 5)
		c.keepFacts(other, now.Add(time.Second), false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keeping facts waited for the lock of another store")
	}
	if len(c.facts) != 2 {
		t.Fatalf("kept %d, want 2", len(c.facts))
	}
}

// TestProgramFactsBackgroundNotKept checks facts read for background work
// are not kept, and that using kept facts from background work does not
// count as a use
func TestProgramFactsBackgroundNotKept(t *testing.T) {
	c := &programResultsCache{}
	now := time.Now()
	st := newProgramFactsStore(uuid.New(), false)
	c.keepFacts(st, now, true)
	if _, ok := c.facts[st.program]; ok {
		t.Fatal("facts read for background work must not be kept")
	}
	c.keepFacts(st, now, false)
	c.keepFacts(st, now.Add(time.Hour), true)
	if c.facts[st.program] != st || !st.used.Equal(now) {
		t.Fatal("background work must keep kept facts without counting a use")
	}

	f := newResultsFixture(newProgramServiceTest(t))
	f.sentTo(now.Add(-time.Hour))
	ctx := withProgramFactsBackground(f.ctx)
	r, err := f.svc.programResults(ctx, nil, f.loadProgram(f.programID), now, nil, nil)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if r.Summary.SentTotal != 1 {
		t.Fatalf("sent total %d, want 1", r.Summary.SentTotal)
	}
	if f.keptFacts() != nil {
		t.Fatal("the background report run must not keep the facts it read")
	}
}

// TestProgramFactsInvalidateKeepsFacts checks a change to the program keeps
// the facts when the sends still match, and reads them again at once when a
// send was removed, without waiting for the count interval
func TestProgramFactsInvalidateKeepsFacts(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	f.sentTo(now.Add(-2 * time.Hour))
	gone := f.sentTo(now.Add(-time.Hour))
	f.results(now)
	kept := f.keptFacts()
	if kept == nil {
		t.Fatal("the facts must be kept")
	}
	f.svc.invalidateResults(&f.programID)
	if f.keptFacts() != kept {
		t.Fatal("an invalidation must keep the facts")
	}
	if got := f.results(now.Add(time.Second)).Summary.SentTotal; got != 2 {
		t.Fatalf("sent total %d, want 2", got)
	}
	if f.keptFacts() != kept {
		t.Fatal("facts that still match must not be read again")
	}
	if err := f.db.Where("campaign_id = ? AND recipient_id = ?", f.campaignID, gone).
		Delete(&database.CampaignRecipient{}).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.svc.invalidateResults(&f.programID)
	if got := f.results(now.Add(2 * time.Second)).Summary.SentTotal; got != 1 {
		t.Fatalf("after a delete and an invalidation sent total %d, want 1", got)
	}
	if f.keptFacts() == kept {
		t.Fatal("facts that no longer match must be read again")
	}
}

// TestProgramFactsRereadAfterRecipientDelete checks a send read again after
// its recipient was deleted, now with a new pseudonym and no recipient id, is
// found by its row id and not counted twice, and that an event with the new
// pseudonym lands on it
func TestProgramFactsRereadAfterRecipientDelete(t *testing.T) {
	campaignID, rowID, recipientID, pseudonym := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	st := newProgramFactsStore(uuid.New(), false)
	st.addCampaign(repository.ProgramCampaignInfo{ID: campaignID})
	sentAt := time.Now()
	st.addRow(&repository.ProgramSendRow{ID: rowID, CampaignID: campaignID, RecipientID: &recipientID, SentAt: sentAt}, math.MinInt64)
	st.addRow(&repository.ProgramSendRow{ID: rowID, CampaignID: campaignID, AnonymizedID: &pseudonym, SentAt: sentAt}, math.MinInt64)
	if st.size() != 1 {
		t.Fatalf("sends %d, want 1", st.size())
	}
	ids := programResultEventIDs()
	visit := eventIDFor(data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	ids.visit = &visit
	st.applyEvent(&repository.ProgramEventRow{CampaignID: campaignID, AnonymizedID: &pseudonym, EventID: visit, CreatedAt: sentAt}, ids)
	if !st.sends[0].has(factClicked) || len(st.pending) != 0 {
		t.Fatal("the event with the new pseudonym must land on the send")
	}
	// a row id older than the recent bound is forgotten
	st.pruneRecent(unixNanos(sentAt.Add(time.Minute)))
	if len(st.recent) != 0 {
		t.Fatalf("recent %d, want 0", len(st.recent))
	}
}

// TestProgramFactsPendingEvent checks an event read before its send is known
// is kept and lands on the send once a refresh reads it
func TestProgramFactsPendingEvent(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	f.sentTo(now.Add(-time.Hour))
	f.results(now)
	// a send whose sent time is stored after its event was read
	late := f.recipient(uuid.NewString() + "@example.com")
	sendID := f.send(f.campaignID, late, false)
	f.event(late, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	r := f.results(now.Add(time.Second))
	if r.Summary.SentTotal != 1 || len(f.keptFacts().pending) != 1 {
		t.Fatalf("sent total %d pending %d, want 1 and 1", r.Summary.SentTotal, len(f.keptFacts().pending))
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).
		Update("sent_at", now.Add(time.Minute).UTC()).Error; err != nil {
		t.Fatalf("set sent_at: %v", err)
	}
	r = f.results(now.Add(2 * time.Second))
	if r.Summary.SentTotal != 2 || r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 1 {
		t.Fatalf("after the send: sent %d clicked %v", r.Summary.SentTotal, r.Summary.ClickedLink)
	}
	if len(f.keptFacts().pending) != 0 {
		t.Fatal("the applied event must leave pending")
	}
	sameResults(t, "pending", f.coldResults(now.Add(2*time.Second)), r)
}

// TestProgramFactsNewTemplateNamed checks a send on a new campaign with a new
// template is named in the same refresh that adds it
func TestProgramFactsNewTemplateNamed(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	f.sentTo(now.Add(-time.Hour))
	f.results(now)
	stepID := f.step(f.programID, 1, true)
	campaignID := f.stepCampaign(f.programID, stepID, 0)
	f.linkTemplate(campaignID, stepID)
	rid := f.recipient(uuid.NewString() + "@example.com")
	id := f.send(campaignID, rid, true)
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", now.Add(time.Minute).UTC()).Error; err != nil {
		t.Fatalf("set sent_at: %v", err)
	}
	r := f.results(now.Add(time.Second))
	want := f.templateNameOf(stepID)
	found := false
	key := f.stepTemplate(stepID).String()
	for _, row := range r.Templates {
		if row.Key == key {
			found = row.Name == want
		}
	}
	if !found {
		t.Fatalf("the new template must be named %q when it is added", want)
	}
}

// TestProgramEventsSortedByTimeOnly checks the events of a program are sorted
// by time whatever column is asked for
func TestProgramEventsSortedByTimeOnly(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopLoop)
	stepID := p.step(programID, 0, true)
	zed, abe := p.recipient("zed@example.com"), p.recipient("abe@example.com")
	p.enroll(programID, zed, 0)
	p.enroll(programID, abe, 0)
	now := time.Now().UTC().Truncate(time.Second)
	p.sentWithEvent(programID, stepID, zed, 0, now, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, now.Add(-time.Minute))
	p.sentWithEvent(programID, stepID, abe, 1, now, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED, now)
	for _, column := range []string{
		repository.TableColumn(database.RECIPIENT_TABLE, "email"),
		repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "ip_address"),
		"",
	} {
		q := &vo.QueryArgs{OrderBy: column, Limit: 10}
		view, err := p.svc.GetEvents(p.ctx, authorizedSession(t), &programID, q, nil)
		if err != nil {
			t.Fatalf("events: %v", err)
		}
		if len(view.Rows) != 2 || *view.Rows[0].RecipientID != zed || *view.Rows[1].RecipientID != abe {
			t.Fatalf("sorted by %q: want the events oldest first", column)
		}
		if q.OrderBy != column {
			t.Fatal("the caller's query args must not be changed")
		}
	}
}

// TestProcessReportedCSVClosedProgram checks a report after the close of a
// program is recorded at the close, so it is listed with the program events
func TestProcessReportedCSVClosedProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	step := p.step(programID, 0, true)
	campaign := p.stepCampaign(programID, step, 0)
	alice := p.recipient("alice@example.com")
	bob := p.recipient("bob@example.com")
	p.enroll(programID, alice, 0)
	p.enroll(programID, bob, 0)
	sentAt := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	closedAt := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	for _, r := range []uuid.UUID{alice, bob} {
		id := p.send(campaign, r, true)
		p.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", utils.RFC3339UTC(sentAt))
	}
	p.setProgram(programID, map[string]any{"closed_at": closedAt})
	header, rows := reportedRows([][]string{
		{"email", "reported"},
		{"alice@example.com", "2026-03-01 10:00:00"}, // after the close
		{"bob@example.com", "2026-01-20 10:00:00"},   // before the close
	})
	out, err := p.svc.ProcessReportedCSV(p.ctx, authorizedSession(t), &programID, header, rows, 0, 1)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out.Processed != 2 {
		t.Fatalf("got %+v", out)
	}
	reported := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	for r, want := range map[uuid.UUID]time.Time{alice: closedAt, bob: time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)} {
		var e database.CampaignEvent
		if err := p.db.Where("event_id = ? AND recipient_id = ?", reported, r).First(&e).Error; err != nil {
			t.Fatalf("report: %v", err)
		}
		if !e.CreatedAt.Equal(want) {
			t.Fatalf("report at %v, want %v", e.CreatedAt, want)
		}
	}
}

// TestInboxReportTimeAnonymousProgram checks an inbox report on a send of an
// anonymous program is recorded no earlier than when the last shown month
// settled, and that other reports keep their time
func TestInboxReportTimeAnonymousProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	programID := p.program(model.ProgramLoopComplete)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	old := time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)
	campaign := &model.Campaign{}
	campaign.ProgramID.Set(programID)
	cs := p.svc.CampaignService

	at, err := cs.inboxReportTime(p.ctx, campaign, old, now)
	if err != nil || !at.Equal(old) {
		t.Fatalf("a named program report must keep its time, got %v %v", at, err)
	}
	p.setProgram(programID, map[string]any{"is_anonymous": true})
	at, err = cs.inboxReportTime(p.ctx, campaign, old, now)
	if err != nil {
		t.Fatalf("time: %v", err)
	}
	fixed := programAnonymousFixedFrom(now, time.UTC)
	if !at.Equal(fixed) || at.After(now) {
		t.Fatalf("report at %v, want %v", at, fixed)
	}
	// the last shown month at now is October, it settled on November 4
	if want := time.Date(2026, 11, 1+model.ProgramAnonymousSettleDays, 0, 0, 0, 0, time.UTC); !fixed.Equal(want) {
		t.Fatalf("fixed from %v, want %v", fixed, want)
	}
	recent := now.Add(-time.Hour)
	if at, _ := cs.inboxReportTime(p.ctx, campaign, recent, now); !at.Equal(recent) {
		t.Fatalf("a report after the fixed time must keep its time, got %v", at)
	}
	// a campaign of no program keeps its time
	if at, _ := cs.inboxReportTime(p.ctx, &model.Campaign{}, old, now); !at.Equal(old) {
		t.Fatalf("a campaign report must keep its time, got %v", at)
	}
}

// TestInboxReportTimeKeepsShownMonths checks a report clamped by
// inboxReportTime never changes the months an anonymous program shows
func TestInboxReportTimeKeepsShownMonths(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Copenhagen")
	for _, now := range []time.Time{
		time.Date(2026, 11, 3, 23, 0, 0, 0, loc),
		time.Date(2026, 11, 4, 0, 30, 0, 0, loc),
		time.Date(2026, 11, 30, 12, 0, 0, 0, loc),
		time.Date(2026, 3, 29, 2, 30, 0, 0, time.UTC),
	} {
		fixed := programAnonymousFixedFrom(now, loc)
		if fixed.After(now) {
			t.Fatalf("now %v: fixed from %v is after now", now, fixed)
		}
		countedTo := programAnonymousCountedTo(now, loc)
		// every shown month ends at or before countedTo and settles at or
		// before fixed
		last := countedTo.AddDate(0, -1, 0)
		if settle := programMonthSettle(countedTo, loc); settle.After(fixed) {
			t.Fatalf("now %v: month from %v settles %v after %v", now, last, settle, fixed)
		}
	}
}

// TestProgramSendCSVFromFacts checks the per send export gives one row per
// send oldest first, with the outcomes, kind and template of each
func TestProgramSendCSVFromFacts(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now().UTC().Truncate(time.Second)
	second := f.sentTo(now.Add(-time.Hour))
	first := f.sentTo(now.Add(-2 * time.Hour))
	f.event(first, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	f.event(first, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED)
	rows := [][]string{}
	err := f.svc.programSendCSV(context.Background(), f.loadProgram(f.programID), func(row []string) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(rows) != 3 || strings.Join(rows[0], ",") != strings.Join(programSendCSVHeader, ",") {
		t.Fatalf("rows %d: %v", len(rows), rows)
	}
	emailOf := func(id uuid.UUID) string {
		var r database.Recipient
		f.db.Where("id = ?", id).First(&r)
		return *r.Email
	}
	if rows[1][0] != emailOf(first) || rows[2][0] != emailOf(second) {
		t.Fatal("the sends must be oldest first")
	}
	if rows[1][3] != ProgramSendKindPhishing || rows[1][4] != now.Add(-2*time.Hour).Format(time.RFC3339) {
		t.Fatalf("first row %v", rows[1])
	}
	if rows[1][5] == "" || rows[1][7] == "" || rows[1][6] != "" || rows[2][5] != "" {
		t.Fatalf("outcomes %v %v", rows[1], rows[2])
	}
	if rows[1][2] != programDeletedTemplateName {
		t.Fatalf("template %q", rows[1][2])
	}
}

// TestProgramSendCSVTrainingColumns checks the per send export of a named
// program with phishing and training: a training send has the training kind,
// its started and completed times and no phishing outcome, and a phishing
// send has no training times
func TestProgramSendCSVTrainingColumns(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now().UTC().Truncate(time.Second)
	phished := f.sentTo(now.Add(-3 * time.Hour))
	f.event(phished, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)

	// a training step with its hidden training campaign
	templateID := f.trainingTemplate()
	stepID := uuid.New()
	f.create(&database.ProgramStep{
		ID: &stepID, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &f.programID, CampaignTemplateID: &templateID, Position: 1,
	})
	trainingID := uuid.New()
	f.create(&database.Campaign{
		ID: &trainingID, CreatedAt: &now, UpdatedAt: &now, Name: "training " + trainingID.String(),
		ProgramID: &f.programID, IsTraining: true, CampaignTemplateID: &templateID,
	})
	linkID := uuid.New()
	f.create(&database.ProgramCampaign{
		ID: &linkID, CreatedAt: &now,
		ProgramID: &f.programID, ProgramStepID: &stepID, CampaignID: &trainingID,
	})
	trained := f.recipient(uuid.NewString() + "@example.com")
	sendID := f.send(trainingID, trained, true)
	sentAt := now.Add(-2 * time.Hour)
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", sendID).Update("sent_at", sentAt).Error; err != nil {
		t.Fatalf("set sent_at: %v", err)
	}
	startedAt, completedAt := now.Add(-90*time.Minute), now.Add(-time.Hour)
	for name, at := range map[string]time.Time{
		data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED:   startedAt,
		data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED: completedAt,
		// a visit on a training send is no click on a phish
		data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED: startedAt,
	} {
		if cache.EventIDByName[name] == nil {
			id := uuid.New()
			cache.EventIDByName[name] = &id
		}
		id := uuid.New()
		f.create(&database.CampaignEvent{
			ID: &id, CreatedAt: &at, UpdatedAt: &at,
			CampaignID: &trainingID, RecipientID: &trained, EventID: cache.EventIDByName[name],
		})
	}

	rows := [][]string{}
	err := f.svc.programSendCSV(context.Background(), f.loadProgram(f.programID), func(row []string) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want the header and 2 sends, got %d: %v", len(rows), rows)
	}
	phish, training := rows[1], rows[2]
	if phish[3] != ProgramSendKindPhishing || phish[5] == "" || phish[8] != "" || phish[9] != "" {
		t.Fatalf("phishing row %v", phish)
	}
	if training[3] != ProgramSendKindTraining || training[4] != sentAt.Format(time.RFC3339) {
		t.Fatalf("training row %v", training)
	}
	if training[5] != "" || training[6] != "" || training[7] != "" {
		t.Fatalf("a training row has no phishing outcome: %v", training)
	}
	if training[8] != startedAt.Format(time.RFC3339) || training[9] != completedAt.Format(time.RFC3339) {
		t.Fatalf("training times %q %q, want %q %q", training[8], training[9],
			startedAt.Format(time.RFC3339), completedAt.Format(time.RFC3339))
	}
	if !strings.HasPrefix(training[2], "training ") {
		t.Fatalf("training template name %q", training[2])
	}
}
