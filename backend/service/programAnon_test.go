package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
)

func TestWithholdCount(t *testing.T) {
	cases := []struct {
		group, count int
		want         bool
	}{
		{10, 0, true}, // nobody
		{10, 1, true},
		{10, 4, true},
		{10, 5, false},
		{10, 6, true},  // 4 left out
		{10, 10, true}, // everyone
		{20, 15, false},
		{20, 16, true},
	}
	for _, c := range cases {
		if got := withholdCount(c.group, c.count); got != c.want {
			t.Fatalf("withholdCount(%d, %d) = %v, want %v", c.group, c.count, got, c.want)
		}
	}
}

// people returns n new person keys
func people(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

// TestProgramAnonymousRowsSizesOnly checks an anonymous row shows its size
// only and withholds it for fewer than the minimum people, while a named row
// shows every count
func TestProgramAnonymousRowsSizesOnly(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	may := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template", "t2": "Small"}}
	// three people with four sends each are 12 sends but only 3 people
	for _, p := range people(3) {
		for i := 0; i < 4; i++ {
			facts.sends = append(facts.sends, &programSend{Person: p, SentAt: may.Add(time.Duration(i) * time.Hour), Template: "t2",
				ClickedAt: tp(may.Add(time.Duration(i)*time.Hour + time.Minute))})
		}
	}
	// ten people, one clicked six times
	ps := people(10)
	for i := 0; i < 6; i++ {
		facts.sends = append(facts.sends, &programSend{Person: ps[0], SentAt: may, Template: "t1", ClickedAt: tp(may.Add(time.Hour))})
	}
	for _, p := range ps[1:] {
		facts.sends = append(facts.sends, &programSend{Person: p, SentAt: may, Template: "t1"})
	}
	r := results(facts, time.UTC, now, nil, nil)
	if len(r.Templates) != 2 {
		t.Fatalf("templates: %+v", r.Templates)
	}
	for _, row := range r.Templates {
		if row.Clicked != nil || row.Submitted != nil || row.Reported != nil || !row.OutcomesHidden {
			t.Fatalf("an anonymous row must show sends only: %+v", row)
		}
		switch row.Key {
		case "t1":
			if row.Suppressed || row.Sent == nil || *row.Sent != 15 {
				t.Fatalf("a row of 10 people shows its size: %+v", row)
			}
		case "t2":
			if !row.Suppressed || row.Sent != nil {
				t.Fatalf("a row of 3 people must be withheld: %+v", row)
			}
		}
	}
	facts.anonymous = false
	r = results(facts, time.UTC, now, nil, nil)
	for _, row := range r.Templates {
		if row.OutcomesHidden || row.Suppressed || row.Clicked == nil {
			t.Fatalf("a named row shows every count: %+v", row)
		}
		if row.Key == "t1" && *row.Clicked != 6 {
			t.Fatalf("named clicks: %+v", row)
		}
	}
}

// TestProgramAnonymousResultsNoBackSolve checks that no month or template
// outcome of an anonymous program can be learned by subtracting shown rows
// from each other or from a total. One small month is hidden; the template
// total minus the shown months would give its clicks if outcomes were shown
// per row.
func TestProgramAnonymousResultsNoBackSolve(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	add := func(month time.Month, persons []uuid.UUID, clickers int) {
		for i, p := range persons {
			sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: time.Date(2026, month, 10, 9, 0, 0, 0, time.UTC), Template: "t1"}
			if i < clickers {
				sd.ClickedAt = tp(sd.SentAt.Add(time.Hour))
			}
			facts.sends = append(facts.sends, sd)
		}
	}
	all := people(12)
	// March has 10 people, 5 clicked. April has 2 people, one clicked, too
	// few to show
	add(time.March, all[:10], 5)
	add(time.April, all[10:], 1)
	r := results(facts, time.UTC, now, nil, nil)
	if len(r.Months) != 2 || len(r.Templates) != 1 {
		t.Fatalf("rows: %d months, %d templates", len(r.Months), len(r.Templates))
	}
	for _, row := range append(append([]*ProgramResultRow{}, r.Months...), r.Templates...) {
		if row.Clicked != nil || row.Submitted != nil || row.Reported != nil || !row.OutcomesHidden {
			t.Fatalf("row %s shows an outcome: %+v", row.Name, row)
		}
	}
	if !r.Months[1].Suppressed || r.Months[1].Sent != nil {
		t.Fatalf("the small month must be withheld: %+v", r.Months[1])
	}
	// the small month withholds the outcomes of any period that holds it
	if r.Summary.ClickedLink != nil || !r.Summary.Suppressed {
		t.Fatalf("summary clicks with a small month: %+v", r.Summary)
	}
	// the outcomes are in the summary only, here of March alone
	march := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	april := march.AddDate(0, 1, 0)
	if m := results(facts, time.UTC, now, &march, &april); m.Summary.ClickedLink == nil || *m.Summary.ClickedLink != 5 {
		t.Fatalf("March clicks: %+v", m.Summary)
	}
	// the CSV export carries the same rows, with no outcome per row
	for _, line := range programAggregateCSV(r) {
		if line[0] == "Month" || line[0] == "Template" {
			if line[5] != "" || line[6] != "" || line[7] != "" {
				t.Fatalf("csv row shows an outcome: %v", line)
			}
		}
	}
	// the report table and trend show no outcome either
	d := programReportFromResults(&model.Program{IsAnonymous: nullable.NewNullableWithValue(true)}, &programReportSetup{}, r, now, nil, nil)
	for _, row := range append(append([]programReportRow{}, d.Months...), d.Templates...) {
		for _, cell := range row.Cells[2:] {
			if cell != programWithheldText {
				t.Fatalf("report row shows an outcome: %v", row.Cells)
			}
		}
	}
	for _, m := range d.Trend {
		if m.Clicked.Height != 0 || m.Reported.Height != 0 {
			t.Fatalf("trend shows an outcome: %+v", m)
		}
	}
}

// TestProgramResultsAnonymousMonthSnap checks an anonymous program counts a
// month only once it has ended and settled, at any time of the day
func TestProgramResultsAnonymousMonthSnap(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no time zone data")
	}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	day := time.Date(2025, 5, 20, 0, 0, 0, 0, loc)
	// five sends early in the day and five late, all one day
	for i, p := range people(10) {
		sentAt := day.Add(time.Hour)
		if i >= 5 {
			sentAt = day.Add(22 * time.Hour)
		}
		facts.sends = append(facts.sends, &programSend{Person: p, Campaign: uuid.New(), SentAt: sentAt, Template: "t1"})
	}
	settled := time.Date(2025, 6, 4, 0, 0, 0, 0, loc)
	for _, hour := range []int{1, 12, 23} {
		// the month ended on 1 June and settles 3 days later
		for _, now := range []time.Time{day.AddDate(0, 0, 4), settled.AddDate(0, 0, -1)} {
			now = now.Add(time.Duration(hour) * time.Hour)
			r := results(facts, loc, now, nil, nil)
			if *r.Summary.Sent != 0 || len(r.Months) != 0 || r.LastSentAt != nil {
				t.Fatalf("%v: a month that has not settled must not count: %+v", now, r.Summary)
			}
		}
		now := settled.Add(time.Duration(hour) * time.Hour)
		r := results(facts, loc, now, nil, nil)
		if *r.Summary.Sent != 10 || *r.Months[0].Sent != 10 {
			t.Fatalf("hour %d: the whole month must be counted: %+v", hour, r.Summary)
		}
		if month := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, loc); !r.LastSentAt.Equal(month) {
			t.Fatalf("last sent must be the start of the month, got %v", r.LastSentAt)
		}
		if !r.CountedTo.Equal(time.Date(2025, 6, 1, 0, 0, 0, 0, loc)) {
			t.Fatalf("counted to %v", r.CountedTo)
		}
	}
	// a normal program keeps exact times and counts young sends right away
	facts.anonymous = false
	now := day.AddDate(0, 0, 1).Add(12 * time.Hour)
	r := results(facts, loc, now, nil, nil)
	if *r.Summary.Sent != 10 || !r.LastSentAt.Equal(day.Add(22*time.Hour)) || r.CountedTo != nil {
		t.Fatalf("normal program wrong: sent=%v last=%v", r.Summary.Sent, r.LastSentAt)
	}
}

// TestProgramAnonymousSettledOutcomes checks an anonymous month counts the
// outcomes that happened before it settled, and keeps the same counts however
// late it is read
func TestProgramAnonymousSettledOutcomes(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no time zone data")
	}
	day := time.Date(2026, 10, 20, 0, 0, 0, 0, loc)
	settles := time.Date(2026, 11, 4, 0, 0, 0, 0, loc)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	// twelve October sends: five clicked that day, one clicked on 3 November
	// before the month settled, one on 4 November after it settled
	for i, p := range people(12) {
		sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: day.Add(9 * time.Hour), Template: "t1"}
		switch {
		case i < 5:
			sd.ClickedAt = tp(day.Add(10 * time.Hour))
		case i == 5:
			sd.ClickedAt = tp(settles.Add(-time.Hour))
		case i == 6:
			sd.ClickedAt = tp(settles.Add(time.Hour))
		}
		facts.sends = append(facts.sends, sd)
	}
	// a November send is never in the October counts
	facts.sends = append(facts.sends, &programSend{Person: uuid.New(), Campaign: uuid.New(), SentAt: settles.Add(-2 * time.Hour), Template: "t1"})
	if r := results(facts, loc, settles.Add(-time.Minute), nil, nil); len(r.Months) != 0 || *r.Summary.Sent != 0 {
		t.Fatalf("October must not count before it settles: %+v", r.Summary)
	}
	for _, now := range []time.Time{settles.Add(time.Minute), settles.AddDate(0, 0, 20), settles.AddDate(0, 0, 40)} {
		r := results(facts, loc, now, nil, nil)
		if len(r.Months) == 0 || *r.Months[0].Sent != 12 || r.Months[0].Clicked != nil {
			t.Fatalf("%v: an anonymous month shows its size only: %+v", now, r.Months)
		}
		october := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
		november := october.AddDate(0, 1, 0)
		r = results(facts, loc, now, &october, &november)
		if r.Summary.ClickedLink == nil || *r.Summary.ClickedLink != 6 || *r.Summary.Sent != 12 {
			t.Fatalf("%v: October counts the clicks before it settled: %+v", now, r.Summary)
		}
	}
	// a named program counts everything right away
	facts.anonymous = false
	r := results(facts, loc, settles, nil, nil)
	if *r.Months[0].Sent != 12 || *r.Months[0].Clicked != 7 {
		t.Fatalf("named program wrong: %+v", r.Months)
	}
}

func TestProgramReportResultsWithhold(t *testing.T) {
	program := &model.Program{Status: "active"}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Invoice"}}
	ps := people(10)
	for i, p := range ps {
		s := send(p, 10, "ignored")
		if i < 2 {
			s = send(p, 10, "clicked")
		}
		facts.sends = append(facts.sends, s)
	}
	// six training sends, two completed
	for i, p := range ps[:6] {
		tr := &programSend{Person: p, Campaign: uuid.New(), SentAt: at(9), Training: true}
		if i < 2 {
			tr.TrainingCompletedAt = tp(at(8))
		}
		facts.sends = append(facts.sends, tr)
	}
	// the sends are in May, which has settled on 4 July
	later := time.Date(2025, 7, 4, 0, 0, 0, 0, time.UTC)
	r := buildProgramResults(facts, programResultsInput{loc: time.UTC, now: later, recipients: 10, repeatMonths: 12})
	d := programReportFromResults(program, &programReportSetup{}, r, later, nil, nil)
	got := map[string]string{}
	for _, l := range d.Results {
		got[l.Label] = l.Value
	}
	want := map[string]string{
		"Recipients":   "10",
		"Sent":         "10",
		"Clicked link": programWithheldText,
		// a zero is withheld, it tells what everyone did
		"Data submitted":     programWithheldText,
		"Reported":           programWithheldText,
		"Training started":   programWithheldText,
		"Training completed": programWithheldText,
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	// an anonymous program never counts repeat offenders, so the report has
	// no such line
	if _, ok := got["Repeat offenders"]; ok {
		t.Fatalf("the report of an anonymous program must leave out repeat offenders: %v", got)
	}
	// 4 people withhold every outcome
	facts.sends = facts.sends[6:10]
	r = results(facts, time.UTC, later, nil, nil)
	d = programReportFromResults(program, &programReportSetup{}, r, later, nil, nil)
	for _, l := range d.Results {
		if (l.Label == "Clicked link" || l.Label == "Reported") && !l.Withheld {
			t.Fatalf("an outcome of 4 people must be withheld: %+v", l)
		}
	}
}

func TestParseProgramReportedDate(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no time zone data")
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
	}{
		// a date is the end of that day in the program zone
		{"2026-02-01", time.Date(2026, 2, 1, 23, 59, 59, 999999999, loc)},
		{"02/01/2026", time.Date(2026, 2, 1, 23, 59, 59, 999999999, loc)},
		// a time without a zone is read in the program zone
		{"2026-02-01 09:30:00", time.Date(2026, 2, 1, 9, 30, 0, 0, loc)},
		{"2026-02-01T09:30:00", time.Date(2026, 2, 1, 9, 30, 0, 0, loc)},
		// a zone in the value is kept
		{"2026-02-01T09:30:00Z", time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)},
		{"2026-02-01T09:30:00+02:00", time.Date(2026, 2, 1, 7, 30, 0, 0, time.UTC)},
		// the end of today is never later than now
		{"2026-03-01", now},
		// a future time is never later than now
		{"2026-03-02 09:30:00", now},
		{"2027-01-01T09:30:00Z", now},
	}
	for _, c := range cases {
		got, ok := parseProgramReportedDate(c.in, loc, now)
		if !ok || !got.Equal(c.want) {
			t.Fatalf("parse %q = %v %v, want %v", c.in, got, ok, c.want)
		}
	}
	if _, ok := parseProgramReportedDate("not a date", loc, now); ok {
		t.Fatal("expected a bad value to fail")
	}
}

// TestProgramAnonymousRepeatOffendersFollowClicks checks an anonymous program
// hides repeat offenders when clicked or submitted is hidden, as the repeat
// count would show how many clicked
func TestProgramAnonymousRepeatOffendersFollowClicks(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	all := people(12)
	for round, day := range []int{3, 10} {
		for i, p := range all {
			sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: time.Date(2026, time.May, day, 9, 0, 0, 0, time.UTC), Template: "t1"}
			// 11 of 12 click the first send, 6 of them click the second too
			if (round == 0 && i < 11) || (round == 1 && i < 6) {
				sd.ClickedAt = tp(sd.SentAt.Add(time.Hour))
			}
			facts.sends = append(facts.sends, sd)
		}
	}
	r := results(facts, time.UTC, now, nil, nil)
	if r.Summary.ClickedLink != nil {
		t.Fatalf("11 of 12 clicked must be withheld: %+v", r.Summary)
	}
	if r.Summary.RepeatOffenders != nil {
		t.Fatalf("repeat offenders must be withheld when clicks are: %d", *r.Summary.RepeatOffenders)
	}
}

// TestProgramAnonymousTemplateNotSubtractedFromMonth checks a template that
// went to too few people in a month can not be found by subtracting the shown
// template rows from the month row or the summary. January sent template A to
// 6 people and B to 2: showing January as 8 and A as 6 would give B as 2. The
// month row, the summary and the tiles of a range holding January withhold
// their send count, while February alone shows it.
func TestProgramAnonymousTemplateNotSubtractedFromMonth(t *testing.T) {
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"a": "A", "b": "B"}}
	add := func(month time.Month, persons []uuid.UUID, template string) {
		for _, p := range persons {
			facts.sends = append(facts.sends, &programSend{Person: p, Campaign: uuid.New(), SentAt: time.Date(2026, month, 10, 9, 0, 0, 0, time.UTC), Template: template})
		}
	}
	ps := people(8)
	add(time.January, ps[:6], "a")
	add(time.January, ps[6:], "b")
	add(time.February, ps[:6], "a")
	r := results(facts, time.UTC, now, nil, nil)
	byKey := map[string]*ProgramResultRow{}
	for _, row := range append(append([]*ProgramResultRow{}, r.Months...), r.Templates...) {
		byKey[row.Key] = row
	}
	if row := byKey["2026-01"]; row == nil || row.Sent != nil || !row.Suppressed {
		t.Fatalf("January holds a withheld template and must withhold its sends: %+v", row)
	}
	if row := byKey["2026-02"]; row == nil || row.Sent == nil || *row.Sent != 6 {
		t.Fatalf("February shows its sends: %+v", row)
	}
	if row := byKey["b"]; row == nil || row.Sent != nil || !row.Suppressed {
		t.Fatalf("template B went to 2 people and must be withheld: %+v", row)
	}
	if row := byKey["a"]; row == nil || row.Sent == nil || *row.Sent != 12 {
		t.Fatalf("template A shows its sends: %+v", row)
	}
	if s := r.Summary; s.Sent != nil || s.SentTotal != 0 || !s.SentTotalHidden || !s.HasSent {
		t.Fatalf("a view holding January must withhold its send total: %+v", s)
	}
	month := func(m time.Month) *time.Time {
		t := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	if s := results(facts, time.UTC, now, month(time.February), month(time.March)).Summary; s.Sent == nil || *s.Sent != 6 || s.SentTotalHidden {
		t.Fatalf("February alone shows its send total: %+v", s)
	}
	in := programResultsInput{now: now, enrollment: programEnrollmentCounts{total: 8, active: 8}}
	if tl := tilesFor(facts, in, ProgramTimeRangeAll, false); tl.Sent != nil {
		t.Fatalf("the tiles of a range holding January must withhold the sends: %+v", tl)
	}
}
