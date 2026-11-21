package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

// results cache tuning
const (
	// programResultsCacheSize is how many results are kept, over every program
	// and period
	programResultsCacheSize = 64
	// programResultsNamedMaxAge is how long the results of a named program are
	// kept at most. The event check catches new sends and outcomes, the age
	// bounds what it can not see, such as a removed recipient.
	programResultsNamedMaxAge = 5 * time.Minute
	// programResultsNamedMinAge is how long the results of a named program are
	// kept even when new events or sends arrive, when computing them was slow,
	// so a large busy program is computed at most once in this time however
	// often the page is read. Results that were quick to compute are computed
	// again as soon as something new arrives.
	programResultsNamedMinAge = 2 * time.Minute
	// programResultsSlowCompute is how long a computation may take before the
	// minimum age applies to its results
	programResultsSlowCompute = time.Second
	// programFactsCacheSize is how many programs keep their facts in memory
	programFactsCacheSize = 8
	// programFactsMaxSends bounds the sends the kept facts hold over every
	// program, about 100 bytes each with the index, so about 100 MB. The least
	// recently used program makes room, a program larger than this alone is
	// read for each computation and not kept.
	programFactsMaxSends = 1_000_000
)

// programResultsKey names one cached result: a program and a period, and the
// time range of the result tiles for the program page
type programResultsKey struct {
	program  uuid.UUID
	from, to string
	// tiles is the time range key and tilesFrom its start, empty for results
	// without tiles
	tiles, tilesFrom string
}

// withTiles returns the key of the results with the tiles of a time range
func (k programResultsKey) withTiles(p *programTilesPeriod) programResultsKey {
	k.tiles = p.key
	if p.from != nil {
		k.tilesFrom = p.from.UTC().Format(time.RFC3339Nano)
	}
	return k
}

func newProgramResultsKey(programID uuid.UUID, from, to *time.Time) programResultsKey {
	k := programResultsKey{program: programID}
	if from != nil {
		k.from = from.UTC().Format(time.RFC3339Nano)
	}
	if to != nil {
		k.to = to.UTC().Format(time.RFC3339Nano)
	}
	return k
}

// programResultsStamp is everything besides the sends and events that the
// results are computed from. A cached result is used only while the stamp is
// the same.
type programResultsStamp struct {
	updatedAt    string
	closed       bool
	anonymous    bool
	byPseudonym  bool
	timeZone     string
	recipients   int
	enrollment   programEnrollmentCounts
	repeatMonths int
	stepTraining bool
	stepPhishing bool
	// day is the day an anonymous program counts up to, the results change
	// only when it moves
	day string
}

// programResultsEntry is one cached result
type programResultsEntry struct {
	stamp   programResultsStamp
	results *ProgramResults
	// factsLoad and factsVersion name the facts the results were computed
	// from, a named program is computed again once its facts change. The
	// entry names the facts rather than holding them, so facts the cache no
	// longer keeps are freed.
	factsLoad    uint64
	factsVersion uint64
	// took is how long the computation took, reading the facts included
	took time.Duration
	// computedAt is the now the results were computed for
	computedAt time.Time
	expires    time.Time
	used       time.Time
}

// programResultsCache keeps the facts of programs and the results computed
// from them. The zero value is ready to use.
type programResultsCache struct {
	mu      sync.Mutex
	entries map[programResultsKey]*programResultsEntry
	// generation counts the invalidations of the results of a program, a
	// result computed across an invalidation is not stored
	generation map[uuid.UUID]uint64
	// factsGeneration counts the times the facts of a program were dropped,
	// facts read across a drop are not kept
	factsGeneration map[uuid.UUID]uint64
	group           singleflight.Group
	// facts holds the facts of the programs read most recently
	facts       map[uuid.UUID]*programFactsStore
	factsFlight singleflight.Group
	// loads counts the facts read, each read gets its own number
	loads uint64
}

// nextLoad returns the number of a new read of facts
func (c *programResultsCache) nextLoad() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loads++
	return c.loads
}

// factsGenerationOf returns the count of the times the facts of a program
// were dropped
func (c *programResultsCache) factsGenerationOf(programID uuid.UUID) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.factsGeneration[programID]
}

// factsOf returns the kept facts of a program when they were read at the
// facts generation gen for the same anonymous setting and the same way of
// naming people
func (c *programResultsCache) factsOf(programID uuid.UUID, anonymous, byPseudonym bool, gen uint64) *programFactsStore {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.facts[programID]
	if !ok {
		return nil
	}
	if st.gen != gen || st.anonymous != anonymous || st.byPseudonym != byPseudonym {
		delete(c.facts, programID)
		return nil
	}
	return st
}

// keepFacts keeps the facts of a program unless they were dropped since they
// were read. The least recently used facts make room when too many programs
// or sends are kept, facts too large to keep are dropped. Facts read for
// background work are kept only when they already were, and do not count as
// used, so a run over many programs does not push out what people read.
func (c *programResultsCache) keepFacts(st *programFactsStore, now time.Time, background bool) {
	// the size is read before the cache lock, so a store being refreshed
	// does not hold up every other results lookup
	size := st.size()
	c.mu.Lock()
	defer c.mu.Unlock()
	// the results were invalidated after the store was read, the next use
	// checks it against the database
	if g := c.generation[st.program]; st.resultsGen != g {
		st.recheck.Store(true)
		st.resultsGen = g
	}
	if c.factsGeneration[st.program] != st.gen || size > programFactsMaxSends {
		if c.facts[st.program] == st {
			delete(c.facts, st.program)
		}
		return
	}
	kept := c.facts[st.program] == st
	if background && !kept {
		return
	}
	if c.facts == nil {
		c.facts = map[uuid.UUID]*programFactsStore{}
	}
	st.keptSize = size
	if !background {
		st.used = now
	}
	c.facts[st.program] = st
	for {
		total := 0
		var oldest *programFactsStore
		for _, v := range c.facts {
			if v != st {
				total += v.keptSize
				if oldest == nil || v.used.Before(oldest.used) {
					oldest = v
				}
			}
		}
		if oldest == nil || (len(c.facts) <= programFactsCacheSize && total+size <= programFactsMaxSends) {
			return
		}
		delete(c.facts, oldest.program)
	}
}

// lookup returns the cached entry of a key when its stamp matches and it has
// not expired. A background lookup does not count as a use.
func (c *programResultsCache) lookup(key programResultsKey, stamp programResultsStamp, now time.Time, background bool) *programResultsEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	if e.stamp != stamp || !now.Before(e.expires) {
		delete(c.entries, key)
		return nil
	}
	if !background {
		e.used = now
	}
	return e
}

// drop removes the entry of a key when it is still the given entry
func (c *programResultsCache) drop(key programResultsKey, e *programResultsEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries[key] == e {
		delete(c.entries, key)
	}
}

// generationOf returns the invalidation count of a program
func (c *programResultsCache) generationOf(programID uuid.UUID) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation[programID]
}

// store keeps an entry unless the program was invalidated since gen was read.
// The least recently used entry makes room when the cache is full. Results
// computed for background work only replace an entry of the same key, keeping
// its last use, so a run over many programs does not push out what people
// read.
func (c *programResultsCache) store(key programResultsKey, gen uint64, e *programResultsEntry, background bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation[key.program] != gen {
		return
	}
	if background {
		old, ok := c.entries[key]
		if !ok {
			return
		}
		e.used = old.used
	}
	if c.entries == nil {
		c.entries = map[programResultsKey]*programResultsEntry{}
	}
	if _, ok := c.entries[key]; !ok && len(c.entries) >= programResultsCacheSize {
		var oldest programResultsKey
		var oldestAt time.Time
		first := true
		for k, v := range c.entries {
			if first || v.used.Before(oldestAt) {
				oldest, oldestAt, first = k, v.used, false
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = e
}

// invalidate forgets the facts and every cached result of a program
func (c *programResultsCache) invalidate(programID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.factsGeneration == nil {
		c.factsGeneration = map[uuid.UUID]uint64{}
	}
	c.factsGeneration[programID]++
	delete(c.facts, programID)
	c.dropResultsLocked(programID)
}

// invalidateResults forgets every cached result of a program and keeps its
// facts, which are checked against the database on their next use: the sends
// are counted and the campaigns compared, and the facts are read again only
// when they differ. A change to the program, such as a pause or an edit,
// then costs a count instead of reading every send again.
func (c *programResultsCache) invalidateResults(programID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropResultsLocked(programID)
	// a store being read now is not kept yet, it sees the new count when it
	// is kept, see keepFacts
	if st, ok := c.facts[programID]; ok {
		st.recheck.Store(true)
		st.resultsGen = c.generation[programID]
	}
}

// programsWithTemplate returns the programs with cached results or kept
// facts that may name the template: the programs whose kept facts hold a
// campaign of it, and the programs with results whose facts are not kept, as
// those can not be checked
func (c *programResultsCache) programsWithTemplate(templateID uuid.UUID) []uuid.UUID {
	c.mu.Lock()
	stores := make([]*programFactsStore, 0, len(c.facts))
	for _, st := range c.facts {
		stores = append(stores, st)
	}
	unchecked := map[uuid.UUID]bool{}
	for k := range c.entries {
		if _, ok := c.facts[k.program]; !ok {
			unchecked[k.program] = true
		}
	}
	c.mu.Unlock()
	out := make([]uuid.UUID, 0, len(unchecked))
	for id := range unchecked {
		out = append(out, id)
	}
	// the store lock is taken without the cache lock held
	for _, st := range stores {
		st.mu.RLock()
		for _, id := range st.templateIDs() {
			if *id == templateID {
				out = append(out, st.program)
				break
			}
		}
		st.mu.RUnlock()
	}
	return out
}

// dropResultsLocked forgets every cached result of a program. The caller
// holds the lock.
func (c *programResultsCache) dropResultsLocked(programID uuid.UUID) {
	if c.generation == nil {
		c.generation = map[uuid.UUID]uint64{}
	}
	c.generation[programID]++
	for k := range c.entries {
		if k.program == programID {
			delete(c.entries, k)
		}
	}
}

// get returns the cached result of a key, or computes and stores it. fresh
// reports whether a cached entry still holds, it is asked only for an entry
// whose stamp matches. Concurrent misses of the same key and stamp compute
// once. The caller gets its own copy. background marks background work, see
// store.
func (c *programResultsCache) get(
	key programResultsKey,
	stamp programResultsStamp,
	now time.Time,
	background bool,
	fresh func(*programResultsEntry) (bool, error),
	compute func() (*programResultsEntry, error),
) (*ProgramResults, error) {
	if e := c.lookup(key, stamp, now, background); e != nil {
		ok, err := fresh(e)
		if err != nil {
			return nil, err
		}
		if ok {
			return e.results.clone(), nil
		}
		c.drop(key, e)
	}
	flight := fmt.Sprintf("%s|%s|%s|%s|%s|%+v", key.program, key.from, key.to, key.tiles, key.tilesFrom, stamp)
	v, err, _ := c.group.Do(flight, func() (any, error) {
		gen := c.generationOf(key.program)
		e, err := compute()
		if err != nil {
			return nil, err
		}
		e.stamp = stamp
		e.used = now
		e.computedAt = now
		c.store(key, gen, e, background)
		return e.results, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*ProgramResults).clone(), nil
}

// clone copies the results, so a caller may change its copy
func (r *ProgramResults) clone() *ProgramResults {
	if r == nil {
		return nil
	}
	out := *r
	if r.Summary != nil {
		sum := *r.Summary
		out.Summary = &sum
	}
	copyRows := func(rows []*ProgramResultRow) []*ProgramResultRow {
		cp := make([]*ProgramResultRow, 0, len(rows))
		for _, row := range rows {
			c := *row
			cp = append(cp, &c)
		}
		return cp
	}
	if r.Tiles != nil {
		tiles := *r.Tiles
		out.Tiles = &tiles
	}
	out.Months = copyRows(r.Months)
	out.Templates = copyRows(r.Templates)
	if r.LastSentAt != nil {
		t := *r.LastSentAt
		out.LastSentAt = &t
	}
	return &out
}
