package service

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seededProgramFacts builds a named program of n people over about two years
// with phishing and training sends and random outcomes
func seededProgramFacts(seed int64, n int, now time.Time) *programFacts {
	rng := rand.New(rand.NewSource(seed))
	facts := &programFacts{templateNames: map[string]string{}}
	templates := []string{"", "t-deleted"}
	for i := 0; i < 6; i++ {
		key := uuid.NewString()
		templates = append(templates, key)
		facts.templateNames[key] = fmt.Sprintf("Template %d", i)
	}
	ps := people(n)
	start := now.AddDate(-2, 0, 0)
	span := int64(now.Add(5*24*time.Hour).Sub(start) / time.Second)
	for _, p := range ps {
		sends := 4 + rng.Intn(20)
		for i := 0; i < sends; i++ {
			sd := &programSend{
				Person:   p,
				Campaign: uuid.New(),
				Template: templates[rng.Intn(len(templates))],
				SentAt:   start.Add(time.Duration(rng.Int63n(span))*time.Second + time.Duration(rng.Intn(1e9))),
				Training: rng.Intn(6) == 0,
			}
			after := func() *time.Time {
				t := sd.SentAt.Add(time.Duration(rng.Int63n(int64(40 * 24 * time.Hour))))
				return &t
			}
			if sd.Training {
				if rng.Intn(2) == 0 {
					sd.TrainingStartedAt = after()
					if rng.Intn(2) == 0 {
						sd.TrainingCompletedAt = after()
					}
				}
			} else {
				if rng.Intn(3) == 0 {
					sd.ClickedAt = after()
				}
				if rng.Intn(6) == 0 {
					sd.SubmittedAt = after()
				}
				if rng.Intn(5) == 0 {
					sd.ReportedAt = after()
				}
			}
			facts.sends = append(facts.sends, sd)
		}
	}
	return facts
}

// sameResults reports the first difference between two results, the last
// send compared as an instant
func sameResults(t *testing.T, label string, want, got *ProgramResults) {
	t.Helper()
	w, g := *want, *got
	if (w.LastSentAt == nil) != (g.LastSentAt == nil) || (w.LastSentAt != nil && !w.LastSentAt.Equal(*g.LastSentAt)) {
		t.Fatalf("%s: last sent %v, want %v", label, g.LastSentAt, w.LastSentAt)
	}
	w.LastSentAt, g.LastSentAt = nil, nil
	if !reflect.DeepEqual(w.Summary, g.Summary) {
		t.Fatalf("%s: summary\n got %+v\nwant %+v", label, g.Summary, w.Summary)
	}
	if !reflect.DeepEqual(w.Months, g.Months) {
		t.Fatalf("%s: months differ", label)
	}
	if !reflect.DeepEqual(w.Templates, g.Templates) {
		for i := range w.Templates {
			t.Logf("want %+v got %+v", w.Templates[i], g.Templates[i])
		}
		t.Fatalf("%s: templates differ", label)
	}
	if !reflect.DeepEqual(w, g) {
		t.Fatalf("%s: results differ\n got %+v\nwant %+v", label, g, w)
	}
}

// TestProgramResultsMatchReference checks the facts store gives a named
// program the same results and tiles as the computation it replaced, for the
// whole program, report periods and every time range
func TestProgramResultsMatchReference(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no time zone data")
	}
	now := time.Date(2026, 3, 29, 14, 30, 0, 0, loc)
	facts := seededProgramFacts(7, 80, now)
	started := now.AddDate(-2, 0, 3)
	closedAt := now.AddDate(0, 0, -2)
	day := func(y int, m time.Month, d int) *time.Time {
		t := time.Date(y, m, d, 0, 0, 0, 0, loc)
		return &t
	}
	periods := []struct{ from, to *time.Time }{
		{nil, nil},
		{day(2025, 1, 1), day(2025, 7, 1)},
		{day(2025, 3, 15), nil},
		{nil, day(2025, 10, 31)},
		{day(2026, 3, 1), day(2026, 5, 1)},
	}
	for _, closed := range []*time.Time{nil, &closedAt} {
		for _, months := range []int{12, 3} {
			for i, period := range periods {
				in := programResultsInput{
					loc: loc, now: now, from: period.from, to: period.to,
					recipients: 80, enrollment: programEnrollmentCounts{total: 80, active: 70, completed: 6, exited: 4},
					hasTraining: i%2 == 0, hasPhishing: i%3 == 0, repeatMonths: months, startedAt: &started, closedAt: closed,
				}
				label := fmt.Sprintf("period %d months %d closed %v", i, months, closed != nil)
				want := refBuildProgramResults(facts, in)
				got := buildProgramResults(facts, in)
				sameResults(t, label, want, got)
				if period.from != nil || period.to != nil {
					continue
				}
				for _, key := range []string{ProgramTimeRange30Days, ProgramTimeRange90Days, ProgramTimeRange12Months, ProgramTimeRangeAll} {
					tin := in
					tin.hasTraining = want.Summary.HasTraining
					tin.hasPhishing = want.Summary.HasPhishing
					tp := programTilesPeriod{key: key, from: programTimeRangeFrom(key, now, loc)}
					wantTiles := refBuildProgramTiles(facts, tin, tp, closed != nil, want.Summary)
					gotTiles := buildProgramTiles(facts, tin, tp, closed != nil, got.Summary)
					if !reflect.DeepEqual(wantTiles, gotTiles) {
						t.Fatalf("%s tiles %s\n got %+v\nwant %+v", label, key, gotTiles, wantTiles)
					}
				}
			}
		}
	}
}

// anonMonthSends builds sends in a month of 2026, n people of which the first
// clicked clicked
func anonMonthSends(ps []uuid.UUID, month time.Month, clicked int) []*programSend {
	out := []*programSend{}
	for i, p := range ps {
		sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: time.Date(2026, month, 12, 10, 0, 0, 0, time.UTC), Template: "t1"}
		if i < clicked {
			sd.ClickedAt = tp(sd.SentAt.Add(time.Hour))
		}
		out = append(out, sd)
	}
	return out
}

// TestProgramAnonymousNoDifferencing is the case of a report period that
// would give away one person: 20 people sent to in January, 6 clicked, and
// one new person in March who clicked. January to February shows 6 clicks;
// January to March must not show 7, as the difference names the March
// recipient. Neither may March alone or the program page ranges that hold it.
func TestProgramAnonymousNoDifferencing(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	facts.sends = append(facts.sends, anonMonthSends(people(20), time.January, 6)...)
	facts.sends = append(facts.sends, anonMonthSends(people(1), time.March, 1)...)
	month := func(m time.Month) *time.Time {
		t := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	period := func(from, to time.Month) *ProgramSummary {
		return results(facts, time.UTC, now, month(from), month(to)).Summary
	}
	if s := period(time.January, time.March); s.ClickedLink == nil || *s.ClickedLink != 6 || *s.Sent != 20 {
		t.Fatalf("January to February: %+v", s)
	}
	for _, s := range []*ProgramSummary{
		period(time.January, time.April),
		period(time.March, time.April),
		period(time.February, time.June),
		results(facts, time.UTC, now, nil, nil).Summary,
	} {
		if s.ClickedLink != nil || s.SubmittedData != nil || s.Reported != nil || !s.Suppressed {
			t.Fatalf("a period that holds March must withhold its outcomes: %+v", s)
		}
	}
	// the send total of a period that holds March is withheld too, as 21
	// against the 20 of January to February would show March sent to 1 person
	for _, s := range []*ProgramSummary{period(time.January, time.April), period(time.March, time.April)} {
		if s.Sent != nil {
			t.Fatalf("a period that holds March must withhold its send total: %+v", s)
		}
	}
	in := programResultsInput{now: now, enrollment: programEnrollmentCounts{total: 21, active: 21}}
	for _, key := range []string{ProgramTimeRange90Days, ProgramTimeRange12Months, ProgramTimeRangeAll} {
		if tl := tilesFor(facts, in, key, false); tl.ClickedLink != nil || tl.Sent != nil || !tl.Suppressed {
			t.Fatalf("%s holds March and must withhold clicks: %+v", key, tl)
		}
	}
}

// TestProgramAnonymousViewsAreSumsOfPassingMonths checks the rule behind the
// anonymous results on random data: every view an operator can read, any run
// of whole months and every time range on any day, shows an outcome only when
// each of its months shows it alone, and then shows their sum. Two views then
// differ by whole months that pass on their own.
func TestProgramAnonymousViewsAreSumsOfPassingMonths(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	ps := people(40)
	for m := time.January; m <= time.September; m++ {
		n := []int{3, 6, 12, 25, 40}[rng.Intn(5)]
		group := append([]uuid.UUID{}, ps...)
		rng.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
		for _, sd := range anonMonthSends(group[:n], m, rng.Intn(n+1)) {
			sd.SentAt = sd.SentAt.Add(time.Duration(rng.Intn(15*24)) * time.Hour)
			if rng.Intn(4) == 0 {
				sd.ReportedAt = tp(sd.SentAt.Add(2 * time.Hour))
			}
			facts.sends = append(facts.sends, sd)
		}
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	month := func(m time.Month) *time.Time {
		t := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	alone := map[time.Month]*ProgramSummary{}
	for m := time.January; m <= time.September; m++ {
		alone[m] = results(facts, time.UTC, now, month(m), month(m+1)).Summary
	}
	check := func(label string, from, to time.Month, sent, clicked, reported *int) {
		t.Helper()
		wantSent, wantClicked, wantReported := 0, 0, 0
		sentOK, clickedOK, reportedOK := true, true, true
		for m := from; m < to; m++ {
			if alone[m].Sent == nil {
				sentOK = false
			} else {
				wantSent += *alone[m].Sent
			}
			if alone[m].ClickedLink == nil {
				clickedOK = false
			} else {
				wantClicked += *alone[m].ClickedLink
			}
			if alone[m].Reported == nil {
				reportedOK = false
			} else {
				wantReported += *alone[m].Reported
			}
		}
		if sentOK != (sent != nil) || (sent != nil && *sent != wantSent) {
			t.Fatalf("%s: sent %v, want %d shown %v", label, sent, wantSent, sentOK)
		}
		if clickedOK != (clicked != nil) || (clicked != nil && *clicked != wantClicked) {
			t.Fatalf("%s: clicked %v, want %d shown %v", label, clicked, wantClicked, clickedOK)
		}
		if reportedOK != (reported != nil) || (reported != nil && *reported != wantReported) {
			t.Fatalf("%s: reported %v, want %d shown %v", label, reported, wantReported, reportedOK)
		}
	}
	for from := time.January; from <= time.September; from++ {
		for to := from + 1; to <= time.October; to++ {
			s := results(facts, time.UTC, now, month(from), month(to)).Summary
			check(fmt.Sprintf("%v to %v", from, to), from, to, s.Sent, s.ClickedLink, s.Reported)
		}
	}
	// the time ranges on every day of a month read the same whole months
	in := programResultsInput{enrollment: programEnrollmentCounts{total: 40, active: 40}}
	for d := 0; d < 31; d++ {
		in.now = time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC).AddDate(0, 0, d)
		for key, n := range map[string]int{ProgramTimeRange30Days: 1, ProgramTimeRange90Days: 3, ProgramTimeRange12Months: 9} {
			tl := tilesFor(facts, in, key, false)
			check(fmt.Sprintf("day %d %s", d, key), time.October-time.Month(n), time.October, tl.Sent, tl.ClickedLink, tl.Reported)
		}
	}
}

// TestProgramFactsStoreKeepsFirstOutcome checks an event applied twice or out
// of order keeps the first time of an outcome
func TestProgramFactsStoreKeepsFirstOutcome(t *testing.T) {
	var cur = programFactNone
	setEarliest(&cur, 20)
	setEarliest(&cur, 30)
	setEarliest(&cur, 10)
	setEarliest(&cur, 10)
	if cur != 10 {
		t.Fatalf("earliest %d, want 10", cur)
	}
}

// TestProgramFactsCacheBounds checks the facts of too many programs or too
// many sends make room by the least recently used, and facts read across an
// invalidation are not kept
func TestProgramFactsCacheBounds(t *testing.T) {
	c := &programResultsCache{}
	now := time.Now()
	stores := []*programFactsStore{}
	for i := 0; i < programFactsCacheSize+3; i++ {
		st := newProgramFactsStore(uuid.New(), false)
		st.sends = make([]programFactSend, 10)
		c.keepFacts(st, now.Add(time.Duration(i)*time.Second), false)
		stores = append(stores, st)
	}
	if len(c.facts) != programFactsCacheSize {
		t.Fatalf("kept %d, want %d", len(c.facts), programFactsCacheSize)
	}
	if _, ok := c.facts[stores[0].program]; ok {
		t.Fatal("the least recently used facts must make room")
	}
	big := newProgramFactsStore(uuid.New(), false)
	big.sends = make([]programFactSend, programFactsMaxSends-5)
	c.keepFacts(big, now.Add(time.Hour), false)
	if c.facts[big.program] != big || len(c.facts) != 1 {
		t.Fatalf("a large program must push out the others, kept %d", len(c.facts))
	}
	huge := newProgramFactsStore(uuid.New(), false)
	huge.sends = make([]programFactSend, programFactsMaxSends+1)
	c.keepFacts(huge, now.Add(2*time.Hour), false)
	if _, ok := c.facts[huge.program]; ok {
		t.Fatal("facts over the bound must not be kept")
	}
	st := newProgramFactsStore(uuid.New(), false)
	c.invalidate(st.program)
	c.keepFacts(st, now, false)
	if _, ok := c.facts[st.program]; ok {
		t.Fatal("facts read across an invalidation were kept")
	}
}

// TestSentRereadFrom checks the bound a refresh reads sends after is written
// in the form the mark is stored in
func TestSentRereadFrom(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"2026-11-13T10:43:50.023172473Z":      "2026-11-13T10:41:50Z",
		"2026-11-13 10:43:50.5+00:00":         "2026-11-13 10:41:50+00:00",
		"2026-11-13 10:43:50+00:00":           "2026-11-13 10:41:50+00:00",
		"not a time":                          "not a time",
		"2026-01-01T00:01:00Z":                "2025-12-31T23:59:00Z",
		"2026-11-13T10:43:50.023172473+02:00": "2026-11-13T08:41:50Z",
	}
	for in, want := range cases {
		if got := sentRereadFrom(in); got != want {
			t.Fatalf("sentRereadFrom(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAnonymousTemplateRowsHiddenLast checks a withheld template row of an
// anonymous program comes after every shown row, sorted by name and not by
// its size, while the shown rows and the rows of a named program keep the
// most sent first
func TestAnonymousTemplateRowsHiddenLast(t *testing.T) {
	counts := []programRowSends{
		{used: true, sent: 3},
		{used: true, sent: 90},
		{used: true, sent: 40},
		{used: true, sent: 50},
		{used: true, sent: 1},
	}
	names := []string{"d hidden", "a shown", "c shown", "b hidden", "e hidden"}
	hidden := map[int]bool{0: true, 3: true, 4: true}
	row := func(i int, c *programRowSends) *ProgramResultRow {
		return &ProgramResultRow{Key: names[i], Name: names[i], Suppressed: hidden[i]}
	}
	order := func(rows []*ProgramResultRow) string {
		out := []string{}
		for _, r := range rows {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}
	anon := newProgramFactsStore(uuid.New(), true)
	if got := order(anon.sortedTemplateRows(counts, row)); got != "a shown,c shown,b hidden,d hidden,e hidden" {
		t.Fatalf("anonymous order: %s", got)
	}
	named := newProgramFactsStore(uuid.New(), false)
	if got := order(named.sortedTemplateRows(counts, row)); got != "a shown,b hidden,c shown,d hidden,e hidden" {
		t.Fatalf("named order: %s", got)
	}
}
