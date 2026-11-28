package service

import (
	"context"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// programFactNone marks an outcome that did not happen
const programFactNone int64 = math.MinInt64

// the outcome slots of a fact send. A phishing send keeps clicked, submitted
// and reported, a training send keeps started and completed.
const (
	factClicked           = 0
	factSubmitted         = 1
	factReported          = 2
	factTrainingStarted   = 0
	factTrainingCompleted = 1
)

// programSentRereadSlack is how far back from the newest sent time a refresh
// reads sends again, so a send written a little after a newer one is not
// missed. A send read twice is found by its row id or in the index and
// skipped.
const programSentRereadSlack = 2 * time.Minute

// programRecentMargin widens the time the row ids of recent sends are kept
// for, past the reread slack, as the reread bound is cut to the second
const programRecentMargin = 5 * time.Second

// programPendingEventsMax bounds the events kept while their send is not
// known yet, see programFactsStore.pending
const programPendingEventsMax = 10_000

// programFactSend is one sent message in a small fixed size form. Times are
// unix nanoseconds, an outcome that did not happen is programFactNone.
type programFactSend struct {
	sentAt   int64
	out      [3]int64
	person   int32
	template int32
	training bool
	// language is the index of the language stamp in languages, 0 for a send
	// with no stamp, which is in its template language
	language uint16
}

func (f *programFactSend) has(slot int) bool {
	return f.out[slot] != programFactNone
}

// clicked reports whether the person clicked, a submit counts as a click
func (f *programFactSend) clicked() bool {
	return f.has(factClicked) || f.has(factSubmitted)
}

// earliestOf returns the earlier of two outcome times, or programFactNone
// when neither happened
func (f *programFactSend) earliestOf(a, b int) int64 {
	switch {
	case !f.has(a):
		return f.out[b]
	case !f.has(b):
		return f.out[a]
	}
	return min(f.out[a], f.out[b])
}

// clickedAt returns when the person clicked. A submit with no page visit
// counts as a click at the time of the submit.
func (f *programFactSend) clickedAt() int64 {
	return f.earliestOf(factClicked, factSubmitted)
}

// trainingStarted reports whether the training was started, a completed
// training counts as started
func (f *programFactSend) trainingStarted() bool {
	return f.has(factTrainingStarted) || f.has(factTrainingCompleted)
}

// trainingStartedAt returns when the training was started. A completion with
// no start event counts as a start at the time of the completion.
func (f *programFactSend) trainingStartedAt() int64 {
	return f.earliestOf(factTrainingStarted, factTrainingCompleted)
}

// unixNanos returns t in unix nanoseconds, clamped to the range an int64 holds
func unixNanos(t time.Time) int64 {
	if t.Year() < 1700 {
		return programFactNone + 1
	}
	if t.Year() > 2200 {
		return math.MaxInt64
	}
	return t.UnixNano()
}

// setEarliest keeps the first time of an outcome
func setEarliest(cur *int64, at int64) {
	if *cur == programFactNone || at < *cur {
		*cur = at
	}
}

// programFactKey finds a send by its campaign and a recipient id or pseudonym
type programFactKey struct {
	campaign int32
	id       uuid.UUID
}

// programFactCampaign is a campaign of the program as the facts know it
type programFactCampaign struct {
	id       uuid.UUID
	training bool
	template int32
	// templateID is the template the campaign sends, nil for none
	templateID *uuid.UUID
}

// programFactsStore holds every send of one program with its outcomes, read
// once and then brought up to date with the sends and events added since.
// Every time range, report period and the program page are computed from it
// in memory. People and templates are held once and named by a small index.
// The store is safe for concurrent reads; a refresh changes it under the
// write lock.
type programFactsStore struct {
	mu        sync.RWMutex
	program   uuid.UUID
	anonymous bool
	// byPseudonym names people by pseudonym first, as an anonymous program
	// does. A named program does so once its Anonymize All began, see
	// programPersonByPseudonym.
	byPseudonym bool
	// gen is the invalidation count of the program the store was read at
	gen uint64

	campaignOf map[uuid.UUID]int32
	campaigns  []programFactCampaign
	templateOf map[string]int32
	// templates is the template key of each template index, the template id
	// as text, empty for none. names is its name.
	templates []string
	names     []string
	persons   map[uuid.UUID]int32
	// languages is the language stamp of each language index, the first one
	// empty for no stamp
	languages  []string
	languageOf map[string]uint16
	sends      []programFactSend
	index      map[programFactKey]int32
	// hasTraining is set when any training template was sent
	hasTraining bool
	// hasPhishing is set when any phishing template was sent
	hasPhishing bool
	// recent finds the sends a refresh may read again by their row id, the
	// sends from programSentRereadSlack before the sent mark on. A send read
	// again after its recipient was deleted carries a new pseudonym and no
	// recipient id, which the index does not know.
	recent map[uuid.UUID]int32
	// pending holds the events read before their send was known, as when a
	// send is stored after its event was read. They are tried again on each
	// refresh.
	pending []repository.ProgramEventRow

	// eventMark is the newest event row id when the last read started, the
	// next refresh reads the events after it. It never moves back.
	eventMark int64
	// eventsUpToMark counts the events read with a row id at or below
	// eventMark. The recheck compares it with the database, which finds an
	// event stored with a row id given out again, see
	// repository.LatestCampaignEventRowID, that the mark can not see.
	eventsUpToMark int64
	// sentMark is the newest stored sent time when the last read started
	sentMark string
	// checkedAt is when the sends were last counted against the database,
	// which finds removed sends the marks can not see
	checkedAt time.Time
	// version counts the changes, a computed result names the version it read
	version uint64
	// load names this read of the facts, a computed result names it
	load uint64
	// recheck is set when the program changed in a way the marks can not
	// see, the next refresh then counts the sends and compares the campaigns
	recheck atomic.Bool
	// used and keptSize are guarded by the cache lock. keptSize is the size
	// when the store was last kept.
	used     time.Time
	keptSize int
	// resultsGen is the results invalidation count of the program the store
	// has seen, guarded by the cache lock once the store is shared. A store
	// kept after an invalidation it did not see, such as one made while it
	// was read, is checked again on its next use.
	resultsGen uint64
}

func newProgramFactsStore(program uuid.UUID, anonymous bool) *programFactsStore {
	return &programFactsStore{
		program:    program,
		anonymous:  anonymous,
		campaignOf: map[uuid.UUID]int32{},
		templateOf: map[string]int32{},
		persons:    map[uuid.UUID]int32{},
		languages:  []string{""},
		languageOf: map[string]uint16{"": 0},
		index:      map[programFactKey]int32{},
		recent:     map[uuid.UUID]int32{},
	}
}

// language returns the index of a language stamp, adding it when new. The
// stamps are codes of the fixed list, a code past the index range counts as
// no stamp.
func (st *programFactsStore) language(code string) uint16 {
	if i, ok := st.languageOf[code]; ok {
		return i
	}
	if len(st.languages) > math.MaxUint16 {
		return 0
	}
	i := uint16(len(st.languages))
	st.languageOf[code] = i
	st.languages = append(st.languages, code)
	return i
}

// size is how many sends the store holds
func (st *programFactsStore) size() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.sends)
}

// currentVersion returns the version under the read lock
func (st *programFactsStore) currentVersion() uint64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.version
}

// template returns the index of a template key, adding it when new
func (st *programFactsStore) template(key string) int32 {
	if i, ok := st.templateOf[key]; ok {
		return i
	}
	i := int32(len(st.templates))
	st.templateOf[key] = i
	st.templates = append(st.templates, key)
	st.names = append(st.names, programDeletedTemplateName)
	return i
}

// addCampaign adds a campaign of the program
func (st *programFactsStore) addCampaign(info repository.ProgramCampaignInfo) {
	if _, ok := st.campaignOf[info.ID]; ok {
		return
	}
	key := ""
	if info.CampaignTemplateID != nil {
		key = info.CampaignTemplateID.String()
	}
	c := programFactCampaign{id: info.ID, training: info.IsTraining, template: st.template(key)}
	if info.CampaignTemplateID != nil {
		id := *info.CampaignTemplateID
		c.templateID = &id
	}
	st.campaignOf[info.ID] = int32(len(st.campaigns))
	st.campaigns = append(st.campaigns, c)
}

// sameCampaigns reports whether every campaign the store knows is in infos
// with the same kind and template
func (st *programFactsStore) sameCampaigns(infos []repository.ProgramCampaignInfo) bool {
	byID := make(map[uuid.UUID]repository.ProgramCampaignInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	for _, c := range st.campaigns {
		info, ok := byID[c.id]
		if !ok || info.IsTraining != c.training {
			return false
		}
		if (info.CampaignTemplateID == nil) != (c.templateID == nil) ||
			(c.templateID != nil && *info.CampaignTemplateID != *c.templateID) {
			return false
		}
	}
	return true
}

// person returns the index of a person, adding it when new
func (st *programFactsStore) person(id uuid.UUID) int32 {
	if i, ok := st.persons[id]; ok {
		return i
	}
	i := int32(len(st.persons))
	st.persons[id] = i
	return i
}

// addRow adds a sent message. A row that names no person or a campaign the
// store does not know is left out, and so is a row already held: found by its
// row id among the recent sends, or by its recipient id or pseudonym. The
// keys of a held row are added to the index, so an event that carries a new
// pseudonym finds the send. recentFrom is the sent time, in unix nanoseconds,
// from which the row id is kept for a later reread.
func (st *programFactsStore) addRow(r *repository.ProgramSendRow, recentFrom int64) {
	person, ok := programPerson(st.anonymous || st.byPseudonym, r.RecipientID, r.AnonymizedID)
	if !ok {
		return
	}
	ci, ok := st.campaignOf[r.CampaignID]
	if !ok {
		return
	}
	held, found := st.recent[r.ID]
	for _, id := range []*uuid.UUID{r.RecipientID, r.AnonymizedID} {
		if found || id == nil {
			continue
		}
		held, found = st.index[programFactKey{ci, *id}]
	}
	if found {
		for _, id := range []*uuid.UUID{r.RecipientID, r.AnonymizedID} {
			if id != nil {
				st.index[programFactKey{ci, *id}] = held
			}
		}
		return
	}
	c := st.campaigns[ci]
	sd := programFactSend{
		sentAt:   unixNanos(r.SentAt),
		out:      [3]int64{programFactNone, programFactNone, programFactNone},
		person:   st.person(person),
		template: c.template,
		training: c.training,
		language: st.language(r.Language),
	}
	if c.training {
		st.hasTraining = true
	} else {
		st.hasPhishing = true
	}
	i := int32(len(st.sends))
	st.sends = append(st.sends, sd)
	if sd.sentAt >= recentFrom {
		st.recent[r.ID] = i
	}
	// a send is found by its recipient id and by its pseudonym, so an event
	// matches whichever of the two it carries
	if r.RecipientID != nil {
		st.index[programFactKey{ci, *r.RecipientID}] = i
	}
	if r.AnonymizedID != nil {
		st.index[programFactKey{ci, *r.AnonymizedID}] = i
	}
}

// applyEvent records an event on its send, keeping the first time of each
// outcome. An event on a campaign the store does not know is left out, one
// without a held send is kept in pending, up to programPendingEventsMax, and
// tried again by retryPending.
func (st *programFactsStore) applyEvent(e *repository.ProgramEventRow, ids programResultEvents) {
	if st.applyHeld(e, ids) == programEventNoSend && len(st.pending) < programPendingEventsMax {
		st.pending = append(st.pending, *e)
	}
}

// what applyHeld did with an event
const (
	programEventApplied = iota
	programEventNoCampaign
	programEventNoSend
)

// retryPending applies the pending events whose send is now held
func (st *programFactsStore) retryPending(ids programResultEvents) {
	if len(st.pending) == 0 {
		return
	}
	left := st.pending[:0]
	for i := range st.pending {
		if st.applyHeld(&st.pending[i], ids) == programEventNoSend {
			left = append(left, st.pending[i])
		}
	}
	clear(st.pending[len(left):])
	st.pending = left
}

// applyHeld records an event on its send when the store holds the send
func (st *programFactsStore) applyHeld(e *repository.ProgramEventRow, ids programResultEvents) int {
	ci, ok := st.campaignOf[e.CampaignID]
	if !ok {
		return programEventNoCampaign
	}
	i := int32(-1)
	if e.RecipientID != nil {
		if v, ok := st.index[programFactKey{ci, *e.RecipientID}]; ok {
			i = v
		}
	}
	if i < 0 && e.AnonymizedID != nil {
		if v, ok := st.index[programFactKey{ci, *e.AnonymizedID}]; ok {
			i = v
		}
	}
	if i < 0 {
		return programEventNoSend
	}
	sd := &st.sends[i]
	at := unixNanos(e.CreatedAt)
	is := func(id *uuid.UUID) bool { return id != nil && *id == e.EventID }
	if sd.training {
		// opening a training page is not a click on a phish
		switch {
		case is(ids.trainingStarted):
			setEarliest(&sd.out[factTrainingStarted], at)
		case is(ids.trainingCompleted):
			setEarliest(&sd.out[factTrainingCompleted], at)
		}
		return programEventApplied
	}
	switch {
	case is(ids.beforeVisit), is(ids.visit), is(ids.afterVisit):
		setEarliest(&sd.out[factClicked], at)
	case is(ids.submitted):
		setEarliest(&sd.out[factSubmitted], at)
	case is(ids.reported):
		setEarliest(&sd.out[factReported], at)
	}
	return programEventApplied
}

// pruneRecent forgets the row ids of the sends sent before recentFrom, in
// unix nanoseconds, which no refresh reads again
func (st *programFactsStore) pruneRecent(recentFrom int64) {
	for id, i := range st.recent {
		if st.sends[i].sentAt < recentFrom {
			delete(st.recent, id)
		}
	}
}

// recentFromMark returns the sent time, in unix nanoseconds, from which a
// refresh after the stored sent mark may read sends again. Every send is
// kept when the mark is empty or not known.
func recentFromMark(mark string) int64 {
	for _, form := range programSentAtForms {
		if t, err := time.Parse(form.read, mark); err == nil {
			return unixNanos(t.Add(-programSentRereadSlack - programRecentMargin))
		}
	}
	return math.MinInt64
}

// templateIDs returns the ids of the templates the campaigns send
func (st *programFactsStore) templateIDs() []*uuid.UUID {
	out := make([]*uuid.UUID, 0, len(st.campaigns))
	for _, c := range st.campaigns {
		if c.templateID != nil {
			out = append(out, c.templateID)
		}
	}
	return out
}

// setNames names every template, one that no longer exists as deleted
func (st *programFactsStore) setNames(names map[uuid.UUID]string) {
	for i, key := range st.templates {
		st.names[i] = programDeletedTemplateName
		if id, err := uuid.Parse(key); err == nil {
			if n, ok := names[id]; ok {
				st.names[i] = n
			}
		}
	}
}

// nameTemplates names the templates found in names and leaves the others as
// they are
func (st *programFactsStore) nameTemplates(names map[uuid.UUID]string) {
	for id, n := range names {
		if i, ok := st.templateOf[id.String()]; ok {
			st.names[i] = n
		}
	}
}

// newTemplateIDs returns the templates of infos the store does not know yet.
// The caller holds a lock.
func (st *programFactsStore) newTemplateIDs(infos []repository.ProgramCampaignInfo) []*uuid.UUID {
	out := []*uuid.UUID{}
	for i := range infos {
		id := infos[i].CampaignTemplateID
		if id == nil {
			continue
		}
		if _, ok := st.templateOf[id.String()]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// programFactsStoreOf builds a store from loaded facts
func programFactsStoreOf(facts *programFacts) *programFactsStore {
	st := newProgramFactsStore(uuid.UUID{}, facts.anonymous)
	for _, sd := range facts.sends {
		ti := st.template(sd.Template)
		f := programFactSend{
			sentAt:   unixNanos(sd.SentAt),
			out:      [3]int64{programFactNone, programFactNone, programFactNone},
			person:   st.person(sd.Person),
			template: ti,
			training: sd.Training,
		}
		set := func(slot int, t *time.Time) {
			if t != nil {
				f.out[slot] = unixNanos(*t)
			}
		}
		if sd.Training {
			st.hasTraining = true
			set(factTrainingStarted, sd.TrainingStartedAt)
			set(factTrainingCompleted, sd.TrainingCompletedAt)
		} else {
			st.hasPhishing = true
			set(factClicked, sd.ClickedAt)
			set(factSubmitted, sd.SubmittedAt)
			set(factReported, sd.ReportedAt)
		}
		st.sends = append(st.sends, f)
	}
	for i, key := range st.templates {
		if n, ok := facts.templateNames[key]; ok {
			st.names[i] = n
		}
	}
	return st
}

// loadProgramFactsStore reads every send and outcome of a program into a new
// store. The newest event and send are read first, so one added while
// loading is read again by the next refresh.
func (s *Program) loadProgramFactsStore(ctx context.Context, program *model.Program, now time.Time) (*programFactsStore, error) {
	programID := program.ID.MustGet()
	st := newProgramFactsStore(programID, program.IsAnonymousOn())
	st.byPseudonym = programPersonByPseudonym(program)
	st.load = s.resultsCache.nextLoad()
	mark, err := s.ProgramRepository.LatestCampaignEventRowID(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	sentMark, err := s.ProgramRepository.LatestSentAt(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, &programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, info := range infos {
		st.addCampaign(info)
	}
	recentFrom := recentFromMark(sentMark)
	err = s.ProgramRepository.EachSentRow(ctx, &programID, nil, func(r *repository.ProgramSendRow) error {
		st.addRow(r, recentFrom)
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ids := programResultEventIDs()
	err = s.ProgramRepository.EachEventRow(ctx, &programID, nil, ids.all(), func(e *repository.ProgramEventRow) error {
		st.applyEvent(e, ids)
		if e.RowID <= mark {
			st.eventsUpToMark++
		}
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	names, err := s.programTemplateNames(ctx, st.templateIDs())
	if err != nil {
		return nil, err
	}
	st.setNames(names)
	st.eventMark, st.sentMark, st.checkedAt = mark, sentMark, now
	return st, nil
}

// programSentAtForms are the forms a sent time is stored in, each with the
// form a bound is written in: RFC 3339 as the application writes it, and the
// form of the SQLite driver for a time value
var programSentAtForms = []struct{ read, write string }{
	{time.RFC3339Nano, time.RFC3339},
	{"2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00"},
}

// sentRereadFrom returns the stored sent time a refresh reads sends after:
// the mark less programSentRereadSlack in the form the mark is stored in, or
// the mark itself when its form is not known
func sentRereadFrom(mark string) string {
	if mark == "" {
		return ""
	}
	for _, form := range programSentAtForms {
		if t, err := time.Parse(form.read, mark); err == nil {
			return t.Add(-programSentRereadSlack).UTC().Truncate(time.Second).Format(form.write)
		}
	}
	return mark
}

// refreshProgramFacts brings a store up to date with the sends and events
// added since it was read. It reports false when the store can not be brought
// up to date and must be read again: a campaign was removed or changed, or
// the sends in the database no longer match the store, as after a delete.
func (s *Program) refreshProgramFacts(ctx context.Context, st *programFactsStore, now time.Time) (bool, error) {
	programID := st.program
	st.mu.RLock()
	eventMark, sentMark, checkedAt := st.eventMark, st.sentMark, st.checkedAt
	st.mu.RUnlock()
	newEvents, err := s.ProgramRepository.HasProgramEventsAfter(ctx, &programID, eventMark)
	if err != nil {
		return false, errs.Wrap(err)
	}
	// a send is followed by its event in a second write, the sent time shows
	// it also when that event is late or was not stored
	newSends, err := s.ProgramRepository.HasProgramSendsAfter(ctx, &programID, sentMark)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if newEvents || newSends {
		ok, err := s.readNewProgramFacts(ctx, st, eventMark, sentMark)
		if err != nil || !ok {
			return ok, err
		}
	}
	// a check asked for is taken here, and asked for again when it fails
	recheck := st.recheck.Swap(false)
	if !recheck && now.Sub(checkedAt) < programResultsNamedMaxAge {
		return true, nil
	}
	checked := false
	defer func() {
		if recheck && !checked {
			st.recheck.Store(true)
		}
	}()
	// the marks only see new rows, a removed or changed send is found by
	// counting, a removed or changed campaign by comparing, and a renamed
	// template by reading the names again
	count, err := s.ProgramRepository.CountSentRows(ctx, &programID)
	if err != nil {
		return false, errs.Wrap(err)
	}
	if count != int64(st.size()) {
		return false, nil
	}
	// an event stored with a row id given out again is below the mark, the
	// count of the events up to the mark finds it
	st.mu.RLock()
	markNow, eventsUpToMark := st.eventMark, st.eventsUpToMark
	st.mu.RUnlock()
	events, err := s.ProgramRepository.CountProgramEventsUpTo(ctx, &programID, markNow, programResultEventIDs().all())
	if err != nil {
		return false, errs.Wrap(err)
	}
	if events != eventsUpToMark {
		return false, nil
	}
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, &programID)
	if err != nil {
		return false, errs.Wrap(err)
	}
	st.mu.RLock()
	same := st.sameCampaigns(infos)
	templateIDs := st.templateIDs()
	st.mu.RUnlock()
	if !same {
		return false, nil
	}
	names, err := s.programTemplateNames(ctx, templateIDs)
	if err != nil {
		return false, err
	}
	st.mu.Lock()
	st.setNames(names)
	st.checkedAt = now
	st.version++
	st.mu.Unlock()
	checked = true
	return true, nil
}

// readNewProgramFacts reads the sends and events added after the marks and
// adds them to the store. The new rows and the names of new templates are
// read without the lock and added under it, with the version moved once, so a
// computation never reads a new template before it is named.
func (s *Program) readNewProgramFacts(ctx context.Context, st *programFactsStore, eventMark int64, sentMark string) (bool, error) {
	programID := st.program
	newEventMark, err := s.ProgramRepository.LatestCampaignEventRowID(ctx)
	if err != nil {
		return false, errs.Wrap(err)
	}
	newSentMark, err := s.ProgramRepository.LatestSentAt(ctx)
	if err != nil {
		return false, errs.Wrap(err)
	}
	infos, err := s.ProgramRepository.GetCampaignInfos(ctx, &programID)
	if err != nil {
		return false, errs.Wrap(err)
	}
	st.mu.RLock()
	same := st.sameCampaigns(infos)
	newTemplates := st.newTemplateIDs(infos)
	st.mu.RUnlock()
	if !same {
		return false, nil
	}
	names := map[uuid.UUID]string{}
	if len(newTemplates) > 0 {
		names, err = s.programTemplateNames(ctx, newTemplates)
		if err != nil {
			return false, err
		}
	}
	rows := []repository.ProgramSendRow{}
	err = s.ProgramRepository.EachSentRowAfter(ctx, &programID, sentRereadFrom(sentMark), func(r *repository.ProgramSendRow) error {
		rows = append(rows, *r)
		return nil
	})
	if err != nil {
		return false, errs.Wrap(err)
	}
	ids := programResultEventIDs()
	events := []repository.ProgramEventRow{}
	err = s.ProgramRepository.EachEventRowAfter(ctx, &programID, eventMark, ids.all(), func(e *repository.ProgramEventRow) error {
		events = append(events, *e)
		return nil
	})
	if err != nil {
		return false, errs.Wrap(err)
	}
	recentFrom := recentFromMark(newSentMark)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, info := range infos {
		st.addCampaign(info)
	}
	st.nameTemplates(names)
	for i := range rows {
		st.addRow(&rows[i], recentFrom)
	}
	// the events read earlier whose send is now held
	st.retryPending(ids)
	for i := range events {
		st.applyEvent(&events[i], ids)
	}
	st.pruneRecent(recentFrom)
	// the mark never moves back: after the newest events are deleted the
	// latest row id is lower, and the events up to the old mark are counted
	if newEventMark < st.eventMark {
		newEventMark = st.eventMark
	}
	for i := range events {
		// an event past the new mark is read again by the next refresh, it is
		// counted then
		if events[i].RowID <= newEventMark {
			st.eventsUpToMark++
		}
	}
	st.eventMark, st.sentMark = newEventMark, newSentMark
	st.version++
	return true, nil
}

// programFactsBackgroundKey marks a context of background work, such as the
// automated report run, see withProgramFactsBackground
type programFactsBackgroundKey struct{}

// withProgramFactsBackground marks ctx as background work. Facts read for it
// are not kept unless they already were, and using kept facts does not count
// as a use, so a run over every program leaves the kept facts to the programs
// people read.
func withProgramFactsBackground(ctx context.Context) context.Context {
	return context.WithValue(ctx, programFactsBackgroundKey{}, true)
}

func programFactsBackground(ctx context.Context) bool {
	v, _ := ctx.Value(programFactsBackgroundKey{}).(bool)
	return v
}

// programFactsFor returns the facts of a program, read once and brought up to
// date on each call. Concurrent calls for a program share one read.
func (s *Program) programFactsFor(ctx context.Context, program *model.Program, now time.Time) (*programFactsStore, error) {
	programID := program.ID.MustGet()
	anonymous := program.IsAnonymousOn()
	byPseudonym := programPersonByPseudonym(program)
	background := programFactsBackground(ctx)
	v, err, _ := s.resultsCache.factsFlight.Do(programID.String(), func() (any, error) {
		gen := s.resultsCache.factsGenerationOf(programID)
		if st := s.resultsCache.factsOf(programID, anonymous, byPseudonym, gen); st != nil {
			ok, err := s.refreshProgramFacts(ctx, st, now)
			if err != nil {
				return nil, err
			}
			if ok {
				s.resultsCache.keepFacts(st, now, background)
				return st, nil
			}
		}
		// read before the load, so an invalidation during the load is seen
		// when the store is kept
		resultsGen := s.resultsCache.generationOf(programID)
		st, err := s.loadProgramFactsStore(ctx, program, now)
		if err != nil {
			return nil, err
		}
		st.gen = gen
		st.resultsGen = resultsGen
		s.resultsCache.keepFacts(st, now, background)
		return st, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*programFactsStore), nil
}

// programMonth is one calendar month of the sends in the program zone
type programMonth struct {
	start, end time.Time
	startN     int64
	endN       int64
}

// programMonths finds the calendar month of a time in a zone. Sends come in
// runs of near times, so the last month found is tried first.
type programMonths struct {
	loc    *time.Location
	months []*programMonth
	last   int
}

func newProgramMonths(loc *time.Location) *programMonths {
	return &programMonths{loc: loc, last: -1}
}

// of returns the index of the month of t in unix nanoseconds, in the order the
// months were first found
func (m *programMonths) of(t int64) int {
	if m.last >= 0 {
		if mo := m.months[m.last]; t >= mo.startN && t < mo.endN {
			return m.last
		}
	}
	for i, mo := range m.months {
		if t >= mo.startN && t < mo.endN {
			m.last = i
			return i
		}
	}
	local := time.Unix(0, t).In(m.loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, m.loc)
	end := start.AddDate(0, 1, 0)
	m.months = append(m.months, &programMonth{start: start, end: end, startN: start.UnixNano(), endN: end.UnixNano()})
	m.last = len(m.months) - 1
	return m.last
}

// programRowSends counts the phishing and the training sends of a month or
// template row
type programRowSends struct {
	used                               bool
	sent, clicked, submitted, reported int
	// trainingSent counts the training sends, trainingStarted counts a
	// completed training too
	trainingSent, trainingStarted, trainingCompleted int
}

func (r *programRowSends) add(sd *programFactSend) {
	r.used = true
	if sd.training {
		r.trainingSent++
		if sd.trainingStarted() {
			r.trainingStarted++
		}
		if sd.has(factTrainingCompleted) {
			r.trainingCompleted++
		}
		return
	}
	r.sent++
	if sd.clicked() {
		r.clicked++
	}
	if sd.has(factSubmitted) {
		r.submitted++
	}
	if sd.has(factReported) {
		r.reported++
	}
}

// person flags of a result count
const (
	personSent uint8 = 1 << iota
	personClicked
	personSubmitted
	personReported
	personTrainingSent
	personTrainingStarted
	personTrainingCompleted
)

// personFlags returns the flags a send sets on its person, with outcomes
// counted only when keep says so
func personFlags(sd *programFactSend, keep func(int64) bool) uint8 {
	var f uint8
	if sd.training {
		f = personTrainingSent
		// a completed training counts as started, so completed never passes
		// started
		started := (sd.has(factTrainingStarted) && keep(sd.out[factTrainingStarted])) ||
			(sd.has(factTrainingCompleted) && keep(sd.out[factTrainingCompleted]))
		if started {
			f |= personTrainingStarted
		}
		if sd.has(factTrainingCompleted) && keep(sd.out[factTrainingCompleted]) {
			f |= personTrainingCompleted
		}
		return f
	}
	f = personSent
	clicked := (sd.has(factClicked) && keep(sd.out[factClicked])) ||
		(sd.has(factSubmitted) && keep(sd.out[factSubmitted]))
	if clicked {
		f |= personClicked
	}
	if sd.has(factSubmitted) && keep(sd.out[factSubmitted]) {
		f |= personSubmitted
	}
	if sd.has(factReported) && keep(sd.out[factReported]) {
		f |= personReported
	}
	return f
}

// boundNanos returns a period bound in unix nanoseconds, or open when nil
func boundNanos(t *time.Time, open int64) int64 {
	if t == nil {
		return open
	}
	return unixNanos(*t)
}

// sortedTemplateRows turns the template counts into rows, most sent first
// then by name and key. In an anonymous program a withheld row comes last and
// is sorted by name and key only, as its place among the sizes would tell its
// size.
func (st *programFactsStore) sortedTemplateRows(counts []programRowSends, row func(i int, c *programRowSends) *ProgramResultRow) []*ProgramResultRow {
	// sent counts the phishing and the training sends of the template
	type templateRow struct {
		row  *ProgramResultRow
		sent int
	}
	trows := []templateRow{}
	for i := range counts {
		if !counts[i].used {
			continue
		}
		trows = append(trows, templateRow{row(i, &counts[i]), counts[i].sent + counts[i].trainingSent})
	}
	sort.Slice(trows, func(i, j int) bool {
		if st.anonymous {
			hiddenI, hiddenJ := trows[i].row.Suppressed, trows[j].row.Suppressed
			if hiddenI != hiddenJ {
				return hiddenJ
			}
			if hiddenI && trows[i].row.Name != trows[j].row.Name {
				return trows[i].row.Name < trows[j].row.Name
			}
			if hiddenI {
				return trows[i].row.Key < trows[j].row.Key
			}
		}
		if trows[i].sent != trows[j].sent {
			return trows[i].sent > trows[j].sent
		}
		if trows[i].row.Name != trows[j].row.Name {
			return trows[i].row.Name < trows[j].row.Name
		}
		return trows[i].row.Key < trows[j].row.Key
	})
	out := make([]*ProgramResultRow, 0, len(trows))
	for _, t := range trows {
		out = append(out, t.row)
	}
	return out
}

// partialMonth reports whether the program or the period [from, to) covers a
// month only in part. runTo is the last instant counted.
func partialMonth(in programResultsInput, runTo time.Time, start, end time.Time) bool {
	runFrom := in.startedAt
	if in.from != nil && (runFrom == nil || in.from.After(*runFrom)) {
		runFrom = in.from
	}
	if in.closedAt != nil && in.closedAt.Before(runTo) {
		runTo = *in.closedAt
	}
	if in.to != nil && in.to.Before(runTo) {
		runTo = *in.to
	}
	return (runFrom != nil && runFrom.After(start)) || runTo.Before(end)
}

// namedResults computes the results of a named program: only sends in
// [from, to) count when a bound is set, the summary counts each recipient
// once and the rows count sends
func (st *programFactsStore) namedResults(in programResultsInput) *ProgramResults {
	loc, now := in.loc, in.now
	out := &ProgramResults{Months: []*ProgramResultRow{}, Templates: []*ProgramResultRow{}}
	repeatMonths := in.repeatMonths
	if repeatMonths <= 0 {
		repeatMonths = programRepeatOffenderMonthsDefault
	}
	sum := &ProgramSummary{
		Recipients:           in.recipients,
		RepeatOffenderMonths: repeatMonths,
		HasTraining:          in.hasTraining,
		HasPhishing:          in.hasPhishing,
	}
	end := now
	if in.to != nil && in.to.Before(end) {
		end = *in.to
	}
	since := unixNanos(end.AddDate(0, -repeatMonths, 0))
	from := boundNanos(in.from, math.MinInt64)
	to := boundNanos(in.to, math.MaxInt64)
	flags := make([]uint8, len(st.persons))
	offences := make([]uint8, len(st.persons))
	months := newProgramMonths(loc)
	monthCounts := []programRowSends{}
	templateCounts := make([]programRowSends, len(st.templates))
	last := programFactNone
	keepAll := func(int64) bool { return true }
	for i := range st.sends {
		sd := &st.sends[i]
		if sd.sentAt < from || sd.sentAt >= to {
			continue
		}
		sum.SentTotal++
		sum.HasSent = true
		if sd.sentAt > last {
			last = sd.sentAt
		}
		flags[sd.person] |= personFlags(sd, keepAll)
		if sd.training {
			sum.HasTraining = true
		} else {
			sum.HasPhishing = true
			if (sd.has(factClicked) && sd.out[factClicked] >= since) ||
				(sd.has(factSubmitted) && sd.out[factSubmitted] >= since) {
				if offences[sd.person] < 2 {
					offences[sd.person]++
				}
			}
		}
		m := months.of(sd.sentAt)
		for len(monthCounts) <= m {
			monthCounts = append(monthCounts, programRowSends{})
		}
		monthCounts[m].add(sd)
		templateCounts[sd.template].add(sd)
	}
	var people [8]int
	offenders := 0
	for p, f := range flags {
		for bit := 0; bit < 7; bit++ {
			if f&(1<<bit) != 0 {
				people[bit]++
			}
		}
		if offences[p] >= 2 {
			offenders++
		}
	}
	sum.Sent = intPtr(people[0])
	sum.ClickedLink = intPtr(people[1])
	sum.SubmittedData = intPtr(people[2])
	sum.Reported = intPtr(people[3])
	sum.RepeatOffenders = intPtr(offenders)
	if sum.HasTraining {
		sum.TrainingSent = intPtr(people[4])
		sum.TrainingStarted = intPtr(people[5])
		sum.TrainingCompleted = intPtr(people[6])
	}
	out.Summary = sum
	if last != programFactNone {
		t := time.Unix(0, last).UTC()
		out.LastSentAt = &t
	}
	// row builds a month or template row. phishing and training say whether
	// the row shows the phishing and the training counts.
	row := func(key, name string, c *programRowSends, phishing, training bool) *ProgramResultRow {
		r := &ProgramResultRow{Key: key, Name: name}
		if phishing {
			r.Sent, r.Clicked, r.Submitted, r.Reported = intPtr(c.sent), intPtr(c.clicked), intPtr(c.submitted), intPtr(c.reported)
		}
		if training {
			r.TrainingSent = intPtr(c.trainingSent)
			r.TrainingStarted = intPtr(c.trainingStarted)
			r.TrainingCompleted = intPtr(c.trainingCompleted)
		}
		return r
	}
	for i, mo := range months.months {
		r := row(mo.start.Format("2006-01"), mo.start.Format("Jan 2006"), &monthCounts[i], sum.HasPhishing, sum.HasTraining)
		r.Partial = partialMonth(in, now, mo.start, mo.end)
		out.Months = append(out.Months, r)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].Key < out.Months[j].Key })
	out.Templates = st.sortedTemplateRows(templateCounts, func(i int, c *programRowSends) *ProgramResultRow {
		// a template sends either phishing or training, a template whose kind
		// changed shows both
		return row(st.templates[i], st.names[i], c, c.sent > 0, sum.HasTraining && c.trainingSent > 0)
	})
	return out
}

// namedTiles counts the sends of a named program from the start of the time
// range, each send once per outcome
func (st *programFactsStore) namedTiles(period programTilesPeriod, out *ProgramTiles) {
	from := boundNanos(period.from, math.MinInt64)
	var sent, clicked, submitted, reported, trainingSent, trainingStarted, trainingCompleted int
	for i := range st.sends {
		sd := &st.sends[i]
		if sd.sentAt < from {
			continue
		}
		if sd.training {
			trainingSent++
			if sd.trainingStarted() {
				trainingStarted++
			}
			if sd.has(factTrainingCompleted) {
				trainingCompleted++
			}
			continue
		}
		sent++
		if sd.clicked() {
			clicked++
		}
		if sd.has(factSubmitted) {
			submitted++
		}
		if sd.has(factReported) {
			reported++
		}
	}
	out.Sent = intPtr(sent)
	out.ClickedLink = intPtr(clicked)
	out.SubmittedData = intPtr(submitted)
	out.Reported = intPtr(reported)
	if out.HasTraining {
		out.TrainingSent = intPtr(trainingSent)
		out.TrainingStarted = intPtr(trainingStarted)
		out.TrainingCompleted = intPtr(trainingCompleted)
	}
}
