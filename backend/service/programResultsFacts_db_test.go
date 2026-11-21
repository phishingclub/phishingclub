package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
)

// coldResults computes the results of the fixture program with an empty
// cache, reading every send and event again
func (f *resultsFixture) coldResults(now time.Time) *ProgramResults {
	f.t.Helper()
	program := f.loadProgram(f.programID)
	st, err := f.svc.loadProgramFactsStore(f.ctx, program, now)
	if err != nil {
		f.t.Fatalf("load facts: %v", err)
	}
	in, kinds, err := f.svc.programResultsInputs(f.ctx, program, now, nil, nil)
	if err != nil {
		f.t.Fatalf("inputs: %v", err)
	}
	in.hasTraining = kinds.training || st.hasTraining
	in.hasPhishing = kinds.phishing || st.hasPhishing
	return st.programResults(in, nil, program.IsClosed())
}

// keptFacts returns the kept facts of the fixture program
func (f *resultsFixture) keptFacts() *programFactsStore {
	f.svc.resultsCache.mu.Lock()
	defer f.svc.resultsCache.mu.Unlock()
	return f.svc.resultsCache.facts[f.programID]
}

// TestProgramFactsRefreshMatchesFullRead checks facts brought up to date with
// new sends and events give the same results as reading everything again,
// and that a removed send is found once the facts are counted again
func TestProgramFactsRefreshMatchesFullRead(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	first := f.sentTo(now.Add(-48 * time.Hour))
	second := f.sentTo(now.Add(-24 * time.Hour))
	f.event(first, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	f.results(now)
	kept := f.keptFacts()
	if kept == nil {
		t.Fatal("the facts must be kept")
	}
	// new outcomes on old sends and a new send with its own outcomes
	f.event(second, data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA)
	f.event(first, data.EVENT_CAMPAIGN_RECIPIENT_REPORTED)
	third := f.sentTo(now.Add(time.Second))
	f.event(third, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	at := now.Add(2 * time.Second)
	warm := f.results(at)
	if f.keptFacts() != kept {
		t.Fatal("new sends and events must be added to the kept facts, not read again")
	}
	sameResults(t, "after refresh", f.coldResults(at), warm)
	if warm.Summary.SentTotal != 3 || *warm.Summary.ClickedLink != 3 || *warm.Summary.SubmittedData != 1 || *warm.Summary.Reported != 1 {
		t.Fatalf("refreshed summary: %+v", warm.Summary)
	}
	// a removed send is not seen by the marks, the count finds it
	if err := f.db.Where("campaign_id = ? AND recipient_id = ?", f.campaignID, third).
		Delete(&database.CampaignRecipient{}).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	later := now.Add(programResultsNamedMaxAge + 3*time.Second)
	r := f.results(later)
	if r.Summary.SentTotal != 2 {
		t.Fatalf("after a delete sent total %d, want 2", r.Summary.SentTotal)
	}
	if f.keptFacts() == kept {
		t.Fatal("the facts must be read again after a delete")
	}
	sameResults(t, "after delete", f.coldResults(later), r)
}

// TestProgramFactsInvalidateDropsFacts checks a full invalidation of the
// cache drops the kept facts of the program
func TestProgramFactsInvalidateDropsFacts(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	f.sentTo(now.Add(-time.Hour))
	f.results(now)
	if f.keptFacts() == nil {
		t.Fatal("the facts must be kept")
	}
	f.svc.resultsCache.invalidate(f.programID)
	if f.keptFacts() != nil {
		t.Fatal("an invalidation must drop the facts")
	}
}

// TestProgramFactsFindEventOnReusedRowID proves an event stored with a row id
// given out again is found. The newest campaign event belongs to another
// campaign; once it is deleted SQLite gives its row id to the next event, a
// click on the program at or below the mark the kept facts read up to. The
// mark can not see it, the recheck counts the events up to the mark and
// reads the facts again.
func TestProgramFactsFindEventOnReusedRowID(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	rid := f.sentTo(now.Add(-time.Hour))
	f.sentTo(now.Add(-time.Hour))
	visit := eventIDFor(data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	other, otherEvent, created := uuid.New(), uuid.New(), now.UTC()
	f.create(&database.CampaignEvent{
		ID: &otherEvent, CreatedAt: &created, UpdatedAt: &created,
		CampaignID: &other, EventID: &visit,
	})
	var topRowID int64
	if err := f.db.Raw("SELECT MAX(rowid) FROM campaign_events").Scan(&topRowID).Error; err != nil {
		t.Fatalf("max rowid: %v", err)
	}
	if r := f.results(now); r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 0 {
		t.Fatalf("no click yet: %+v", r.Summary)
	}
	kept := f.keptFacts()
	if kept == nil || kept.eventMark != topRowID {
		t.Fatalf("the facts must be kept up to row id %d", topRowID)
	}
	if err := f.db.Where("id = ?", otherEvent).Delete(&database.CampaignEvent{}).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	f.event(rid, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	var clickRowID int64
	if err := f.db.Raw("SELECT MAX(rowid) FROM campaign_events").Scan(&clickRowID).Error; err != nil {
		t.Fatalf("max rowid: %v", err)
	}
	if clickRowID > topRowID {
		t.Fatalf("the click must reuse a row id: %d > %d", clickRowID, topRowID)
	}
	// the recheck runs once the facts are older than the named max age
	later := now.Add(programResultsNamedMaxAge + 3*time.Second)
	r := f.results(later)
	if r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 1 {
		t.Fatalf("the click on a reused row id must be counted: %+v", r.Summary)
	}
	sameResults(t, "after a reused row id", f.coldResults(later), r)
	// facts that match the database are kept by the recheck
	again := f.keptFacts()
	f.results(later.Add(programResultsNamedMaxAge + 3*time.Second))
	if f.keptFacts() != again {
		t.Fatal("facts that match the database must be kept")
	}
}
