package service

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

var riskNow = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func at(daysAgo int) time.Time {
	return riskNow.Add(-time.Duration(daysAgo) * 24 * time.Hour)
}

func tp(t time.Time) *time.Time {
	return &t
}

// send builds a phishing send for a person, sent daysAgo
func send(person uuid.UUID, daysAgo int, outcome string) *programSend {
	s := &programSend{Person: person, Campaign: uuid.New(), SentAt: at(daysAgo), Template: "t1"}
	after := s.SentAt.Add(time.Hour)
	switch outcome {
	case "submitted":
		s.ClickedAt, s.SubmittedAt = tp(after), tp(after)
	case "clicked":
		s.ClickedAt = tp(after)
	case "clickedReported":
		s.ClickedAt, s.ReportedAt = tp(after), tp(after.Add(time.Minute))
	case "reported":
		s.ReportedAt = tp(after)
	}
	return s
}

func TestProgramSendScore(t *testing.T) {
	p := uuid.New()
	cases := map[string]float64{
		"submitted":       100,
		"clicked":         70,
		"clickedReported": 50,
		"ignored":         20,
		"reported":        0,
	}
	for outcome, want := range cases {
		if got := programSendScore(send(p, 10, outcome)); got != want {
			t.Fatalf("%s scored %v, want %v", outcome, got, want)
		}
	}
}

func TestProgramPersonRiskNeedsThreeSends(t *testing.T) {
	p := uuid.New()
	sends := []*programSend{send(p, 10, "clicked"), send(p, 20, "clicked")}
	if got := programPersonRisk(sends, nil, riskNow); got != nil {
		t.Fatalf("two sends should give no risk, got %v", *got)
	}
	// a send older than a year does not count
	sends = append(sends, send(p, 400, "clicked"))
	if got := programPersonRisk(sends, nil, riskNow); got != nil {
		t.Fatalf("a send older than a year should not count, got %v", *got)
	}
	sends = append(sends, send(p, 30, "ignored"))
	got := programPersonRisk(sends, nil, riskNow)
	if got == nil || *got != 160.0/3 {
		t.Fatalf("expected mean of 70 70 20, got %v", got)
	}
	if programRiskLevel(got) != ProgramRiskMedium {
		t.Fatalf("expected medium, got %s", programRiskLevel(got))
	}
}

func TestProgramPersonRiskTrainingCredit(t *testing.T) {
	p := uuid.New()
	sends := []*programSend{
		send(p, 30, "clicked"),
		send(p, 20, "submitted"),
		send(p, 10, "reported"),
	}
	// one training after the submit lowers that send only
	done := []time.Time{at(19)}
	got := programPersonRisk(sends, done, riskNow)
	if got == nil || *got != (70.0+90.0+0.0)/3 {
		t.Fatalf("expected training to lower the latest failed send, got %v", got)
	}
	// a second training lowers the earlier click, a third has nothing to lower
	done = []time.Time{at(19), at(18), at(5)}
	got = programPersonRisk(sends, done, riskNow)
	if got == nil || *got != (60.0+90.0+0.0)/3 {
		t.Fatalf("expected two credits, got %v", got)
	}
	// a reported send never goes below zero and is never credited
	sends = []*programSend{send(p, 30, "reported"), send(p, 20, "reported"), send(p, 10, "reported")}
	got = programPersonRisk(sends, []time.Time{at(1)}, riskNow)
	if got == nil || *got != 0 || programRiskLevel(got) != ProgramRiskLow {
		t.Fatalf("expected zero risk, got %v", got)
	}
	sends = []*programSend{send(p, 30, "submitted"), send(p, 20, "submitted"), send(p, 10, "clicked")}
	if lvl := programRiskLevel(programPersonRisk(sends, nil, riskNow)); lvl != ProgramRiskHigh {
		t.Fatalf("expected high, got %s", lvl)
	}
}

func TestProgramRepeatClickers(t *testing.T) {
	byPerson := map[uuid.UUID][]*programSend{}
	add := func(outcomes ...string) {
		p := uuid.New()
		for i, o := range outcomes {
			// oldest first
			byPerson[p] = append(byPerson[p], send(p, 100-i*10, o))
		}
	}
	add("clicked", "clicked", "clicked") // 3 of 3
	add("clicked", "ignored", "clicked") // 2 of 3
	add("clicked", "ignored")            // 1 of 2
	add("clicked")                       // one send, not eligible
	add("clicked", "clicked", "ignored", "ignored", "ignored", "ignored", "ignored", "ignored")
	// the last person clicked twice but outside their last 6 sends
	got := programRepeatClickers(byPerson, riskNow, false)
	if *got.Eligible2 != 4 || *got.TwoPlus != 2 {
		t.Fatalf("expected 2 of 4 with two clicks, got %d of %d", *got.TwoPlus, *got.Eligible2)
	}
	if *got.Eligible3 != 3 || *got.ThreePlus != 1 {
		t.Fatalf("expected 1 of 3 with three clicks, got %d of %d", *got.ThreePlus, *got.Eligible3)
	}
	// anonymous with fewer than 5 eligible people withholds the counts
	anon := programRepeatClickers(byPerson, riskNow, true)
	if !anon.Suppressed || anon.TwoPlus != nil || anon.ThreePlus != nil {
		t.Fatalf("expected suppressed counts, got %+v", anon)
	}
}

func TestProgramExposure(t *testing.T) {
	byPerson := map[uuid.UUID][]*programSend{}
	for i := 0; i < 5; i++ {
		p := uuid.New()
		first := "clicked"
		if i >= 2 {
			first = "ignored"
		}
		second := "reported"
		if i == 0 {
			second = "clicked"
		}
		byPerson[p] = []*programSend{send(p, 40, first), send(p, 20, second), send(p, 1, "clicked")}
	}
	rows := programExposure(byPerson, riskNow, false)
	// the third send is younger than three days and is not judged
	if len(rows) != 2 {
		t.Fatalf("expected 2 exposure rows, got %d", len(rows))
	}
	if *rows[0].Sent != 5 || *rows[0].Clicked != 2 || *rows[0].ClickRate != 0.4 {
		t.Fatalf("1st row wrong: %+v", rows[0])
	}
	if *rows[1].Clicked != 1 || *rows[1].Reported != 4 || rows[1].Label != "2nd" {
		t.Fatalf("2nd row wrong: %+v", rows[1])
	}
	// anonymous keeps rows with 5 sends and withholds smaller ones
	delete(byPerson, func() uuid.UUID {
		for k := range byPerson {
			return k
		}
		return uuid.Nil
	}())
	anon := programExposure(byPerson, riskNow, true)
	if !anon[0].Suppressed || anon[0].Sent != nil || anon[0].ClickRate != nil {
		t.Fatalf("expected suppressed exposure row, got %+v", anon[0])
	}
}

func TestProgramResultsSuppressionAndOpen(t *testing.T) {
	people := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Invoice", "t2": "Parcel"}}
	for _, p := range people {
		s := send(p, 10, "clicked")
		facts.sends = append(facts.sends, s)
	}
	// two sends of another template, too few to show
	t2a, t2b := send(people[0], 1, "reported"), send(people[1], 50, "ignored")
	t2a.Template, t2b.Template = "t2", "t2"
	facts.sends = append(facts.sends, t2a, t2b)
	// a training send is not a phishing result
	facts.sends = append(facts.sends, &programSend{Person: people[0], SentAt: at(9), Training: true, Template: "t3"})

	r := buildProgramResults(facts, people, time.UTC, riskNow, nil, nil)
	if !r.Anonymous {
		t.Fatalf("expected anonymous results")
	}
	byKey := map[string]*ProgramResultRow{}
	for _, row := range r.Templates {
		byKey[row.Key] = row
	}
	// everyone clicked, so the click count is withheld while the row shows
	if byKey["t1"].Suppressed || *byKey["t1"].Sent != 6 || byKey["t1"].Clicked != nil || *byKey["t1"].Reported != 0 {
		t.Fatalf("t1 row wrong: %+v", byKey["t1"])
	}
	if !byKey["t2"].Suppressed || byKey["t2"].Sent != nil || byKey["t2"].Clicked != nil {
		t.Fatalf("t2 row should be suppressed: %+v", byKey["t2"])
	}
	if _, ok := byKey["t3"]; ok {
		t.Fatalf("training send must not be a template result")
	}
	// last 30 days: six sends ten days old, one send one day old is open and
	// a single open send is withheld
	if *r.Last30.Sent != 6 || r.Last30.Open != nil {
		t.Fatalf("last30 wrong: sent=%v open=%v", r.Last30.Sent, r.Last30.Open)
	}
	// previous 30 days has one send, withheld
	if !r.Previous30.Suppressed || r.Previous30.Sent != nil {
		t.Fatalf("previous30 should be suppressed: %+v", r.Previous30)
	}
	// all six clicked, which is nearly everyone and withheld
	if !r.PhishProne.Suppressed || r.PhishProne.Prone != nil {
		t.Fatalf("phish prone wrong: %+v", r.PhishProne)
	}
	if r.Coverage.Active != 6 || r.Coverage.Covered != 6 {
		t.Fatalf("coverage wrong: %+v", r.Coverage)
	}
	// the same data for a normal program shows every count
	facts.anonymous = false
	r = buildProgramResults(facts, people, time.UTC, riskNow, nil, nil)
	for _, row := range r.Templates {
		if row.Suppressed || row.Sent == nil || row.Clicked == nil {
			t.Fatalf("normal program rows must not be suppressed: %+v", row)
		}
	}
	if *r.Last30.Open != 1 || *r.PhishProne.Prone != 6 {
		t.Fatalf("normal program must show every count: open=%v prone=%v", r.Last30.Open, r.PhishProne.Prone)
	}
	// a period keeps only the sends inside it
	from, to := at(20), at(5)
	r = buildProgramResults(facts, people, time.UTC, riskNow, &from, &to)
	if len(r.Templates) != 1 || *r.Templates[0].Sent != 6 {
		t.Fatalf("period filter wrong: %+v", r.Templates)
	}
}

func TestProgramResilienceAndDifficulty(t *testing.T) {
	p := uuid.New()
	sends := []*programSend{
		send(p, 10, "reported"),
		send(p, 10, "clickedReported"),
		send(p, 10, "clicked"),
		send(p, 10, "ignored"),
	}
	r := programResilience(sends, false)
	if *r.ReportRate != 0.5 || *r.ReportedWithoutClickRate != 0.25 || *r.ResilienceRatio != 1 {
		t.Fatalf("resilience wrong: %+v", r)
	}
	// reports one hour and one hour one minute after the send
	if *r.MedianMinutesToReport != 60.5 {
		t.Fatalf("median wrong: %v", *r.MedianMinutesToReport)
	}
	if !programResilience(sends, true).Suppressed {
		t.Fatalf("four anonymous sends must be withheld")
	}
	if programDifficulty(29, 29) != "" {
		t.Fatalf("no label below 30 sends")
	}
	if programDifficulty(100, 20) != ProgramDifficultyHard ||
		programDifficulty(100, 8) != ProgramDifficultyMedium ||
		programDifficulty(100, 7) != ProgramDifficultyEasy {
		t.Fatalf("difficulty labels wrong")
	}
}

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"=SUM(A1)":    "'=SUM(A1)",
		"+1":          "'+1",
		"-1":          "'-1",
		"@cmd":        "'@cmd",
		"\tx":         "'\tx",
		"\rx":         "'\rx",
		"\nx":         "'\nx",
		" =cmd":       "' =cmd",
		"  +1":        "'  +1",
		"\n=1+1":      "'\n=1+1",
		" \t@x":       "' \t@x",
		" alice":      " alice",
		"   ":         "   ",
		"alice@x.com": "alice@x.com",
		"":            "",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Fatalf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProgramAggregateCSVWithholds(t *testing.T) {
	results := &ProgramResults{
		Months: []*ProgramResultRow{{Key: "2025-05", Name: "May 2025", Suppressed: true}},
		Templates: []*ProgramResultRow{
			{Key: "t", Name: "=HYPERLINK(\"x\")", Sent: intPtr(9), Clicked: intPtr(1), Submitted: intPtr(0), Reported: intPtr(2)},
		},
	}
	rows := programAggregateCSV(results)
	if len(rows) != 3 {
		t.Fatalf("expected header and 2 rows, got %d", len(rows))
	}
	if rows[1][3] != "" || rows[1][7] != "yes" {
		t.Fatalf("suppressed month must have no counts: %v", rows[1])
	}
	if rows[2][2] != "'=HYPERLINK(\"x\")" || rows[2][3] != "9" {
		t.Fatalf("template row wrong: %v", rows[2])
	}
}

func TestSortAndPageByRisk(t *testing.T) {
	r := func(email string, v *float64) ProgramEnrollmentRow {
		return ProgramEnrollmentRow{Email: email, RiskScore: v}
	}
	f := func(v float64) *float64 { return &v }
	rows := []ProgramEnrollmentRow{r("a", f(10)), r("b", nil), r("c", f(80)), r("d", f(40))}
	got := sortAndPageByRisk(rows, true, 0, 2)
	if len(got) != 2 || got[0].Email != "c" || got[1].Email != "d" {
		t.Fatalf("desc page wrong: %+v", got)
	}
	got = sortAndPageByRisk(rows, false, 2, 5)
	if len(got) != 2 || got[0].Email != "c" || got[1].Email != "b" {
		t.Fatalf("asc page with no risk last wrong: %+v", got)
	}
}

func TestProgramReportEscapes(t *testing.T) {
	program := &model.Program{
		Name:   nullable.NewNullableWithValue(*vo.NewString64Must("<script>alert(1)</script>")),
		Status: "active",
	}
	p := uuid.New()
	facts := &programFacts{templateNames: map[string]string{"t1": "<img src=x onerror=alert(1)>"}}
	facts.sends = []*programSend{send(p, 10, "clicked")}
	results := buildProgramResults(facts, []uuid.UUID{p}, time.UTC, riskNow, nil, nil)
	d := programReportFromResults(program, &programReportSetup{}, results, results, facts, riskNow, nil, nil)
	var buf bytes.Buffer
	if err := programReportTemplate.Execute(&buf, d); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>alert") || strings.Contains(out, "<img src=x") {
		t.Fatalf("report must escape user text")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected escaped name in report")
	}
}

func TestProgramPhishProneWindowAndSmallCounts(t *testing.T) {
	byPerson := map[uuid.UUID][]*programSend{}
	add := func(daysAgo int, outcome string) {
		p := uuid.New()
		byPerson[p] = append(byPerson[p], send(p, daysAgo, outcome))
	}
	// a click older than a year no longer counts, nor does that person
	add(400, "clicked")
	add(10, "clicked")
	for i := 0; i < 5; i++ {
		add(10, "ignored")
	}
	got := programPhishProne(byPerson, riskNow, false)
	if *got.People != 6 || *got.Prone != 1 {
		t.Fatalf("expected 1 of 6, got %d of %d", *got.Prone, *got.People)
	}
	// anonymous: 6 people is enough, but 1 clicker is a handful and is hidden
	anon := programPhishProne(byPerson, riskNow, true)
	if !anon.Suppressed || anon.Prone != nil || anon.Rate != nil {
		t.Fatalf("expected a hidden count, got %+v", anon)
	}
	// nobody clicked is safe to show
	clean := map[uuid.UUID][]*programSend{}
	for i := 0; i < 5; i++ {
		p := uuid.New()
		clean[p] = append(clean[p], send(p, 10, "ignored"))
	}
	if r := programPhishProne(clean, riskNow, true); r.Suppressed || *r.Prone != 0 {
		t.Fatalf("expected 0 shown, got %+v", r)
	}
}
