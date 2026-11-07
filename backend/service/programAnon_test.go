package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
)

func TestWithholdCount(t *testing.T) {
	cases := []struct {
		group, count int
		want         bool
	}{
		{10, 0, false},
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

func TestProgramResultRowCountsPeople(t *testing.T) {
	// three people with four sends each are 12 sends but only 3 people
	c := &resultCounts{}
	for _, p := range people(3) {
		for i := 0; i < 4; i++ {
			c.add(send(p, 10+i, "clicked"))
		}
	}
	if r := c.row("m", "May", true); !r.Suppressed || r.Sent != nil {
		t.Fatalf("a row of 3 people must be withheld: %+v", r)
	}
	if r := c.row("m", "May", false); r.Suppressed || *r.Sent != 12 {
		t.Fatalf("a normal row shows every count: %+v", r)
	}

	// one person clicked six times among ten people: six sends but one person
	c = &resultCounts{}
	ps := people(10)
	for i := 0; i < 6; i++ {
		c.add(send(ps[0], 10+i, "clicked"))
	}
	for _, p := range ps[1:] {
		c.add(send(p, 10, "ignored"))
	}
	r := c.row("m", "May", true)
	if r.Suppressed || *r.Sent != 15 || r.Clicked != nil || *r.Reported != 0 {
		t.Fatalf("one clicker must be withheld: %+v", r)
	}
}

func TestProgramResultRowComplement(t *testing.T) {
	build := func(clickers int) *ProgramResultRow {
		c := &resultCounts{}
		for i, p := range people(10) {
			outcome := "ignored"
			if i < clickers {
				outcome = "clicked"
			}
			c.add(send(p, 10, outcome))
		}
		return c.row("m", "May", true)
	}
	for clickers, shown := range map[int]bool{0: true, 2: false, 5: true, 8: false, 10: false} {
		r := build(clickers)
		if (r.Clicked != nil) != shown {
			t.Fatalf("%d of 10 clicked: shown %v, want %v", clickers, r.Clicked != nil, shown)
		}
		if *r.Sent != 10 || r.Suppressed {
			t.Fatalf("the row itself must show: %+v", r)
		}
	}
}

func TestProgramTemplateDifficultyWithheld(t *testing.T) {
	// 30 sends to 30 people, 2 clicked: the label would tell the band of a
	// withheld count
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Invoice"}}
	for i, p := range people(30) {
		outcome := "ignored"
		if i < 2 {
			outcome = "clicked"
		}
		facts.sends = append(facts.sends, send(p, 10, outcome))
	}
	r := buildProgramResults(facts, nil, time.UTC, riskNow, nil, nil)
	if r.Templates[0].Clicked != nil || r.Templates[0].Difficulty != "" {
		t.Fatalf("withheld clicks must drop the label: %+v", r.Templates[0])
	}
	facts.anonymous = false
	r = buildProgramResults(facts, nil, time.UTC, riskNow, nil, nil)
	if r.Templates[0].Difficulty != ProgramDifficultyEasy {
		t.Fatalf("a normal program keeps the label: %+v", r.Templates[0])
	}
}

func TestProgramExposureLaterBucketCountsPeople(t *testing.T) {
	// two people with 15 sends each fill the 12th or later row with 8 sends
	byPerson := map[uuid.UUID][]*programSend{}
	for _, p := range people(2) {
		for i := 0; i < 15; i++ {
			byPerson[p] = append(byPerson[p], send(p, 200-i*10, "ignored"))
		}
	}
	// five more people make the early rows big enough
	for _, p := range people(5) {
		byPerson[p] = append(byPerson[p], send(p, 100, "clicked"))
	}
	rows := programExposure(byPerson, riskNow, true)
	last := rows[len(rows)-1]
	if last.N != programExposureMaxRows || !last.Suppressed || last.Sent != nil {
		t.Fatalf("12th or later row of 2 people must be withheld: %+v", last)
	}
	// the first row has 7 people, 5 clicked leaves 2 out and is withheld
	if rows[0].Suppressed || *rows[0].Sent != 7 || rows[0].Clicked != nil || rows[0].ClickRate != nil {
		t.Fatalf("1st row clicks must be withheld: %+v", rows[0])
	}
	if *rows[0].Reported != 0 || *rows[0].ReportRate != 0 {
		t.Fatalf("1st row zero reports are shown: %+v", rows[0])
	}
}

func TestProgramResilienceAnonymous(t *testing.T) {
	build := func(reporters int) []*programSend {
		out := []*programSend{}
		for i, p := range people(12) {
			outcome := "ignored"
			if i < reporters {
				outcome = "reported"
			}
			out = append(out, send(p, 10, outcome))
		}
		return out
	}
	r := programResilience(build(3), true)
	if r.Suppressed || r.ReportRate != nil || r.MedianMinutesToReport != nil || r.ReportedWithoutClickRate != nil {
		t.Fatalf("three reports must be withheld: %+v", r)
	}
	for _, name := range []string{"reportRate", "reportedWithoutClickRate", "medianMinutesToReport", "resilienceRatio"} {
		found := false
		for _, w := range r.Withheld {
			found = found || w == name
		}
		if !found {
			t.Fatalf("%s must be listed as withheld: %v", name, r.Withheld)
		}
	}
	r = programResilience(build(5), true)
	if r.ReportRate == nil || r.MedianMinutesToReport == nil || r.ReportedWithoutClickRate == nil {
		t.Fatalf("five reports among twelve are shown: %+v", r)
	}
	// nobody clicked: no ratio, and not withheld either
	if r.ResilienceRatio != nil || len(r.Withheld) != 0 {
		t.Fatalf("no clicks gives no ratio and nothing withheld: %+v", r)
	}
	// a normal program shows a median of a single report
	if r := programResilience(build(1), false); r.MedianMinutesToReport == nil || len(r.Withheld) != 0 {
		t.Fatalf("a normal program shows every value: %+v", r)
	}
	// six people with 3 sends each make 18 sends, 6 reported sends all by
	// one person is withheld
	sends := []*programSend{}
	ps := people(6)
	for i := 0; i < 6; i++ {
		sends = append(sends, send(ps[0], 10+i, "reported"))
	}
	for _, p := range ps[1:] {
		sends = append(sends, send(p, 10, "ignored"), send(p, 11, "ignored"))
	}
	if r := programResilience(sends, true); r.ReportRate != nil || r.MedianMinutesToReport != nil {
		t.Fatalf("one reporter must be withheld: %+v", r)
	}
}

func TestProgramResultsAnonymousDaySnap(t *testing.T) {
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
	// at any time of a later day the sends of one day stay together, and they
	// are not counted at all until they have settled
	for _, hour := range []int{1, 12, 23} {
		now := day.AddDate(0, 0, 3).Add(time.Duration(hour) * time.Hour)
		r := buildProgramResults(facts, nil, loc, now, nil, nil)
		if !r.Last30.Suppressed || r.Last30.Open != nil || len(r.Months) != 0 || r.LastSentAt != nil {
			t.Fatalf("hour %d: sends that have not settled must not count: %+v", hour, r.Last30)
		}
		now = day.AddDate(0, 0, 4).Add(time.Duration(hour) * time.Hour)
		r = buildProgramResults(facts, nil, loc, now, nil, nil)
		if r.Last30.Open != nil || *r.Last30.Sent != 10 {
			t.Fatalf("hour %d: the whole day must be counted: sent=%v open=%v", hour, r.Last30.Sent, r.Last30.Open)
		}
		if !r.LastSentAt.Equal(day) {
			t.Fatalf("last sent must be the start of the day, got %v", r.LastSentAt)
		}
	}
	// a normal program keeps exact times and counts young sends as open
	facts.anonymous = false
	now := day.AddDate(0, 0, 3).Add(12 * time.Hour)
	r := buildProgramResults(facts, nil, loc, now, nil, nil)
	if *r.Last30.Open != 5 || !r.LastSentAt.Equal(day.Add(22*time.Hour)) {
		t.Fatalf("normal program wrong: open=%v last=%v", r.Last30.Open, r.LastSentAt)
	}
}

func TestProgramAnonymousSettledOutcomes(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no time zone data")
	}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	now := day.AddDate(0, 0, 20).Add(15 * time.Hour)
	cutoff := programSettleCutoff(now, loc)
	if !cutoff.Equal(day.AddDate(0, 0, 17)) {
		t.Fatalf("cutoff = %v", cutoff)
	}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	// twelve old sends, five clicked long ago and one clicked just now
	for i, p := range people(12) {
		sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: day.Add(9 * time.Hour), Template: "t1"}
		switch {
		case i < 5:
			sd.ClickedAt = tp(day.Add(10 * time.Hour))
		case i == 5:
			sd.ClickedAt = tp(now.Add(-time.Minute))
		}
		facts.sends = append(facts.sends, sd)
	}
	r := buildProgramResults(facts, nil, loc, now, nil, nil)
	if *r.Months[0].Sent != 12 || *r.Months[0].Clicked != 5 {
		t.Fatalf("a fresh click must not count yet: %+v", r.Months[0])
	}
	stats := programResultStats(programFunnel(facts.countable(now, loc)), true)
	if stats.EmailsSent != 12 || stats.WebsiteLoaded == nil || *stats.WebsiteLoaded != 5 {
		t.Fatalf("funnel must count settled outcomes only: %+v", stats)
	}
	// three days later the click has settled
	later := now.AddDate(0, 0, 4)
	r = buildProgramResults(facts, nil, loc, later, nil, nil)
	if r.Months[0].Clicked == nil || *r.Months[0].Clicked != 6 {
		t.Fatalf("a settled click must count: %+v", r.Months[0])
	}
	// a send made now is not counted, so the sent count does not move
	young := &programSend{Person: uuid.New(), Campaign: uuid.New(), SentAt: now.Add(-time.Hour), Template: "t1"}
	facts.sends = append(facts.sends, young)
	r = buildProgramResults(facts, nil, loc, now, nil, nil)
	if *r.Months[0].Sent != 12 || len(r.Months) != 1 {
		t.Fatalf("a young send must not count: %+v", r.Months)
	}
	if got := programFunnel(facts.countable(now, loc)); got.EmailsSent != 12 {
		t.Fatalf("funnel counts a young send: %+v", got)
	}
	// a named program counts everything right away
	facts.anonymous = false
	r = buildProgramResults(facts, nil, loc, now, nil, nil)
	if *r.Months[0].Sent != 13 || *r.Months[0].Clicked != 6 {
		t.Fatalf("named program wrong: %+v", r.Months)
	}
}

func TestProgramReportFunnelWithholds(t *testing.T) {
	program := &model.Program{Status: "active"}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Invoice"}}
	ps := people(10)
	for i, p := range ps {
		s := send(p, 10, "ignored")
		if i < 2 {
			s = send(p, 10, "clicked")
		}
		if i < 5 {
			s.ReadAt = tp(s.SentAt.Add(time.Minute))
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
	results := buildProgramResults(facts, ps, time.UTC, riskNow, nil, nil)
	d := programReportFromResults(program, &programReportSetup{}, results, results, facts, riskNow, nil, nil)
	got := map[string]string{}
	for _, l := range d.Funnel {
		got[l.Label] = l.Value
	}
	want := map[string]string{
		"Emails sent":        "10",
		"Clicked":            programWithheldText,
		"Submitted data":     "0 (0%)",
		"Reported":           "0 (0%)",
		"Training sent":      "6",
		"Training completed": programWithheldText,
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	// a funnel of 4 people is withheld whole
	facts.sends = facts.sends[6:10]
	results = buildProgramResults(facts, nil, time.UTC, riskNow, nil, nil)
	d = programReportFromResults(program, &programReportSetup{}, results, results, facts, riskNow, nil, nil)
	if d.Funnel[0].Value != programWithheldText {
		t.Fatalf("a funnel of 4 people must be withheld: %+v", d.Funnel)
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
