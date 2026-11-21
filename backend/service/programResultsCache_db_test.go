package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// resultsFixture is a started program with one step campaign
type resultsFixture struct {
	*programTest
	programID  uuid.UUID
	campaignID uuid.UUID
}

func newResultsFixture(p *programTest) *resultsFixture {
	programID := p.program(model.ProgramLoopComplete)
	stepID := p.step(programID, 0, true)
	return &resultsFixture{programTest: p, programID: programID, campaignID: p.stepCampaign(programID, stepID, 0)}
}

// sentTo adds a sent message to a new recipient, sent at the given time
func (f *resultsFixture) sentTo(at time.Time) uuid.UUID {
	f.t.Helper()
	rid := f.recipient(uuid.NewString() + "@example.com")
	id := f.send(f.campaignID, rid, true)
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Update("sent_at", at.UTC()).Error; err != nil {
		f.t.Fatalf("set sent_at: %v", err)
	}
	return rid
}

func (f *resultsFixture) event(recipientID uuid.UUID, name string) {
	f.t.Helper()
	if cache.EventIDByName[name] == nil {
		id := uuid.New()
		cache.EventIDByName[name] = &id
	}
	now := time.Now().UTC()
	id := uuid.New()
	f.create(&database.CampaignEvent{
		ID: &id, CreatedAt: &now, UpdatedAt: &now,
		CampaignID: &f.campaignID, RecipientID: &recipientID, EventID: cache.EventIDByName[name],
	})
}

func (f *resultsFixture) results(now time.Time) *ProgramResults {
	f.t.Helper()
	r, err := f.svc.programResults(f.ctx, nil, f.loadProgram(f.programID), now, nil, nil)
	if err != nil {
		f.t.Fatalf("results: %v", err)
	}
	return r
}

// TestProgramResultsCacheNamed checks that a named program result is reused
// until an event or a newer send is added on its campaigns or the program is
// invalidated, and that a caller can not change the cached copy
func TestProgramResultsCacheNamed(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	rid := f.sentTo(now.Add(-time.Hour))
	if got := f.results(now).Summary.SentTotal; got != 1 {
		t.Fatalf("sent total %d, want 1", got)
	}
	// a send that is not newer than the newest send, without an event, is not
	// seen while the result is cached
	f.sentTo(now.Add(-time.Hour))
	first := f.results(now)
	if first.Summary.SentTotal != 1 {
		t.Fatalf("cached sent total %d, want 1", first.Summary.SentTotal)
	}
	first.Summary.SentTotal = 99
	first.Months[0].Name = "changed"
	if again := f.results(now); again.Summary.SentTotal != 1 || again.Months[0].Name == "changed" {
		t.Fatal("a caller changed the cached result")
	}
	// a quick result is computed again right after a new event
	f.event(rid, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	r := f.results(now.Add(time.Second))
	if r.Summary.SentTotal != 2 || r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 1 {
		t.Fatalf("after event: sent %d clicked %v", r.Summary.SentTotal, r.Summary.ClickedLink)
	}
	// a newer send is seen without any event
	f.sentTo(now)
	if got := f.results(now.Add(2 * time.Second)).Summary.SentTotal; got != 3 {
		t.Fatalf("after a new send sent total %d, want 3", got)
	}
	// an invalidation drops the result
	f.sentTo(now.Add(-time.Hour))
	f.svc.invalidateResults(&f.programID)
	if got := f.results(now).Summary.SentTotal; got != 4 {
		t.Fatalf("after invalidate sent total %d, want 4", got)
	}
	// the result is computed again once it is too old
	f.sentTo(now.Add(-time.Hour))
	if got := f.results(now.Add(programResultsNamedMaxAge + time.Second)).Summary.SentTotal; got != 5 {
		t.Fatalf("after max age sent total %d, want 5", got)
	}
}

// TestProgramResultsCacheSlowMinAge checks that a result that was slow to
// compute is kept for the minimum age even when new events and sends arrive
func TestProgramResultsCacheSlowMinAge(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	now := time.Now()
	rid := f.sentTo(now.Add(-time.Hour))
	if got := f.results(now).Summary.SentTotal; got != 1 {
		t.Fatalf("sent total %d, want 1", got)
	}
	// mark the cached computation as slow
	f.svc.resultsCache.mu.Lock()
	for _, e := range f.svc.resultsCache.entries {
		e.took = programResultsSlowCompute + time.Second
	}
	f.svc.resultsCache.mu.Unlock()
	f.event(rid, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	f.sentTo(now)
	if got := f.results(now.Add(time.Second)).Summary.SentTotal; got != 1 {
		t.Fatalf("within the minimum age sent total %d, want 1", got)
	}
	r := f.results(now.Add(programResultsNamedMinAge))
	if r.Summary.SentTotal != 2 || r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 1 {
		t.Fatalf("after the minimum age: sent %d clicked %v", r.Summary.SentTotal, r.Summary.ClickedLink)
	}
}

// TestProgramResultsCacheAnonymousDay checks that an anonymous program result
// holds for the day and is computed again on the next day
func TestProgramResultsCacheAnonymousDay(t *testing.T) {
	f := newResultsFixture(newProgramServiceTest(t))
	if err := f.db.Model(&database.Program{}).Where("id = ?", f.programID).Update("is_anonymous", true).Error; err != nil {
		t.Fatalf("set anonymous: %v", err)
	}
	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	old := day.AddDate(0, 0, -10)
	for i := 0; i < 6; i++ {
		f.sentTo(old)
	}
	if got := f.results(day).Summary.SentTotal; got != 6 {
		t.Fatalf("sent total %d, want 6", got)
	}
	// a send and an event later the same day do not change the day's result
	rid := f.sentTo(old)
	f.event(rid, data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED)
	if got := f.results(day.Add(5 * time.Hour)).Summary.SentTotal; got != 6 {
		t.Fatalf("same day sent total %d, want 6", got)
	}
	// the next day counts again
	if got := f.results(day.AddDate(0, 0, 1)).Summary.SentTotal; got != 7 {
		t.Fatalf("next day sent total %d, want 7", got)
	}
}

// TestProgramResultsCacheIsolation checks that programs and periods never
// share a cached result
func TestProgramResultsCacheIsolation(t *testing.T) {
	p := newProgramServiceTest(t)
	a := newResultsFixture(p)
	b := newResultsFixture(p)
	now := time.Now()
	a.sentTo(now.Add(-time.Hour))
	for i := 0; i < 3; i++ {
		b.sentTo(now.Add(-48 * time.Hour))
	}
	if got := a.results(now).Summary.SentTotal; got != 1 {
		t.Fatalf("program a sent %d, want 1", got)
	}
	if got := b.results(now).Summary.SentTotal; got != 3 {
		t.Fatalf("program b sent %d, want 3", got)
	}
	from := now.Add(-24 * time.Hour)
	r, err := p.svc.programResults(p.ctx, nil, p.loadProgram(b.programID), now, &from, nil)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if r.Summary.SentTotal != 0 {
		t.Fatalf("period sent %d, want 0", r.Summary.SentTotal)
	}
	if got := b.results(now).Summary.SentTotal; got != 3 {
		t.Fatalf("program b after period sent %d, want 3", got)
	}
}

// TestProgramResultsCacheBounds checks the size bound and that a result
// computed across an invalidation is not kept
func TestProgramResultsCacheBounds(t *testing.T) {
	c := &programResultsCache{}
	now := time.Now()
	for i := 0; i < programResultsCacheSize+10; i++ {
		key := newProgramResultsKey(uuid.New(), nil, nil)
		c.store(key, 0, &programResultsEntry{results: &ProgramResults{}, expires: now.Add(time.Hour), used: now.Add(time.Duration(i))}, false)
	}
	if len(c.entries) != programResultsCacheSize {
		t.Fatalf("entries %d, want %d", len(c.entries), programResultsCacheSize)
	}
	id := uuid.New()
	key := newProgramResultsKey(id, nil, nil)
	gen := c.generationOf(id)
	c.invalidate(id)
	c.store(key, gen, &programResultsEntry{results: &ProgramResults{}, expires: now.Add(time.Hour)}, false)
	if _, ok := c.entries[key]; ok {
		t.Fatal("a result computed across an invalidation was kept")
	}
}

// TestProgramResultsCacheBackground checks results of background work never
// push out an entry and only replace one of the same key, keeping its use
func TestProgramResultsCacheBackground(t *testing.T) {
	c := &programResultsCache{}
	now := time.Now()
	keys := []programResultsKey{}
	for i := 0; i < programResultsCacheSize; i++ {
		key := newProgramResultsKey(uuid.New(), nil, nil)
		keys = append(keys, key)
		c.store(key, 0, &programResultsEntry{results: &ProgramResults{}, expires: now.Add(time.Hour), used: now.Add(time.Duration(i))}, false)
	}
	c.store(newProgramResultsKey(uuid.New(), nil, nil), 0, &programResultsEntry{results: &ProgramResults{}, expires: now.Add(time.Hour), used: now.Add(time.Hour)}, true)
	for _, k := range keys {
		if _, ok := c.entries[k]; !ok {
			t.Fatal("background results pushed out an entry")
		}
	}
	if len(c.entries) != programResultsCacheSize {
		t.Fatalf("entries %d, want %d", len(c.entries), programResultsCacheSize)
	}
	fresh := &programResultsEntry{results: &ProgramResults{}, expires: now.Add(time.Hour), used: now.Add(time.Hour)}
	c.store(keys[0], 0, fresh, true)
	if c.entries[keys[0]] != fresh || !fresh.used.Equal(now) {
		t.Fatalf("background results must replace the entry and keep its use: %v", c.entries[keys[0]].used)
	}
}

// TestProgramResultsCacheInvalidateDuringLoad checks a facts store read
// across a results invalidation is checked again when it is kept
func TestProgramResultsCacheInvalidateDuringLoad(t *testing.T) {
	c := &programResultsCache{}
	id := uuid.New()
	st := newProgramFactsStore(id, false)
	st.resultsGen = c.generationOf(id)
	// the program changes while the store is read
	c.invalidateResults(id)
	c.keepFacts(st, time.Now(), false)
	if !st.recheck.Load() {
		t.Fatal("a store read across an invalidation must be checked again")
	}
	st.recheck.Store(false)
	c.keepFacts(st, time.Now(), false)
	if st.recheck.Load() {
		t.Fatal("a store that saw the invalidation must not be checked again")
	}
	c.invalidateResults(id)
	if !st.recheck.Load() {
		t.Fatal("a kept store must be checked again after an invalidation")
	}
}
