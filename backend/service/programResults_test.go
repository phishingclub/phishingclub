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

var resultsNow = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

// anonymousNow is a time the months of the sends near resultsNow have settled,
// so an anonymous program counts them
var anonymousNow = time.Date(2025, 7, 10, 12, 0, 0, 0, time.UTC)

func at(daysAgo int) time.Time {
	return resultsNow.Add(-time.Duration(daysAgo) * 24 * time.Hour)
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

// results builds the results of facts at now in loc over [from, to)
func results(facts *programFacts, loc *time.Location, now time.Time, from, to *time.Time) *ProgramResults {
	return buildProgramResults(facts, programResultsInput{loc: loc, now: now, from: from, to: to, repeatMonths: 12})
}

// TestProgramSummaryCountsRecipients checks the result tiles count each
// recipient once, and repeat offenders follow the repeat offender window
func TestProgramSummaryCountsRecipients(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{}}
	ps := people(4)
	// p0 clicked twice within a year: a repeat offender
	facts.sends = append(facts.sends, send(ps[0], 100, "clicked"), send(ps[0], 50, "submitted"), send(ps[0], 10, "reported"))
	// p1 clicked twice, once over a year ago: not a repeat offender
	facts.sends = append(facts.sends, send(ps[1], 400, "clicked"), send(ps[1], 20, "clicked"))
	// p2 clicked once
	facts.sends = append(facts.sends, send(ps[2], 20, "clickedReported"))
	// p3 only got training, which is not a phishing send
	facts.sends = append(facts.sends, &programSend{Person: ps[3], SentAt: at(5), Training: true,
		TrainingStartedAt: tp(at(4)), TrainingCompletedAt: tp(at(4))})
	r := buildProgramResults(facts, programResultsInput{loc: time.UTC, now: resultsNow, recipients: 9, repeatMonths: 12})
	s := r.Summary
	want := map[string]int{"sent": 3, "clicked": 3, "submitted": 1, "reported": 2, "offenders": 1, "trainingStarted": 1, "trainingCompleted": 1}
	got := map[string]*int{"sent": s.Sent, "clicked": s.ClickedLink, "submitted": s.SubmittedData, "reported": s.Reported,
		"offenders": s.RepeatOffenders, "trainingStarted": s.TrainingStarted, "trainingCompleted": s.TrainingCompleted}
	for k, v := range want {
		if got[k] == nil || *got[k] != v {
			t.Fatalf("%s = %v, want %d", k, got[k], v)
		}
	}
	if s.Recipients != 9 || s.SentTotal != 7 || !s.HasTraining || s.Suppressed || s.RepeatOffenderMonths != 12 {
		t.Fatalf("summary wrong: %+v", s)
	}
	// a shorter window leaves out p0's click 100 days ago
	r = buildProgramResults(facts, programResultsInput{loc: time.UTC, now: resultsNow, repeatMonths: 2})
	if *r.Summary.RepeatOffenders != 0 {
		t.Fatalf("2 months window: offenders = %d", *r.Summary.RepeatOffenders)
	}
	// without training the training counts stay empty
	facts.sends = facts.sends[:6]
	r = results(facts, time.UTC, resultsNow, nil, nil)
	if r.Summary.HasTraining || r.Summary.TrainingStarted != nil {
		t.Fatalf("no training: %+v", r.Summary)
	}
}

// TestProgramSummaryAnonymous checks an anonymous program counts sends and
// withholds small and nearly total counts
func TestProgramSummaryAnonymous(t *testing.T) {
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	for i, p := range people(12) {
		outcome := "ignored"
		switch {
		case i < 2:
			outcome = "submitted"
		case i < 8:
			outcome = "reported"
		}
		facts.sends = append(facts.sends, send(p, 10, outcome), send(p, 20, outcome))
	}
	s := results(facts, time.UTC, anonymousNow, nil, nil).Summary
	if !s.Suppressed || s.ClickedLink != nil || s.SubmittedData != nil || s.RepeatOffenders != nil {
		t.Fatalf("2 of 12 must be withheld: %+v", s)
	}
	// 6 of 12 people reported, shown as the 12 sends they reported
	if s.Sent == nil || *s.Sent != 24 || s.Reported == nil || *s.Reported != 12 {
		t.Fatalf("24 sent and 12 reported are shown: %+v", s)
	}
	// fewer than 5 people withhold every outcome and the send total too, so a
	// one month range cannot single the month out
	facts.sends = facts.sends[:8]
	s = results(facts, time.UTC, anonymousNow, nil, nil).Summary
	if s.Reported != nil || s.ClickedLink != nil || s.Sent != nil {
		t.Fatalf("4 people withhold every outcome and the send total: %+v", s)
	}
}

// TestProgramPartialMonths checks the first month of a program started mid
// month and the running month are marked partial
func TestProgramPartialMonths(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{}}
	p := uuid.New()
	for _, d := range []time.Time{
		time.Date(2025, 3, 20, 9, 0, 0, 0, time.UTC),
		time.Date(2025, 4, 10, 9, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC),
	} {
		facts.sends = append(facts.sends, &programSend{Person: p, SentAt: d, Template: "t1"})
	}
	started := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
	r := buildProgramResults(facts, programResultsInput{loc: time.UTC, now: resultsNow, startedAt: &started})
	partial := []bool{}
	for _, m := range r.Months {
		partial = append(partial, m.Partial)
	}
	if len(partial) != 3 || !partial[0] || partial[1] || !partial[2] {
		t.Fatalf("partial months = %v, want first and last", partial)
	}
	// a program closed at the end of June has no partial last month
	closed := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	r = buildProgramResults(facts, programResultsInput{loc: time.UTC, now: closed.AddDate(0, 1, 0), startedAt: &started, closedAt: &closed})
	if r.Months[2].Partial {
		t.Fatalf("a whole month is not partial")
	}
}

func TestProgramResultsSuppression(t *testing.T) {
	ps := people(6)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Invoice", "t2": "Parcel"}}
	for _, p := range ps {
		facts.sends = append(facts.sends, send(p, 10, "clicked"))
	}
	// two sends of another template, too few to show
	t2a, t2b := send(ps[0], 5, "reported"), send(ps[1], 50, "ignored")
	t2a.Template, t2b.Template = "t2", "t2"
	facts.sends = append(facts.sends, t2a, t2b)
	// a training send is not a phishing result
	facts.sends = append(facts.sends, &programSend{Person: ps[0], SentAt: at(9), Training: true, Template: "t3"})

	r := results(facts, time.UTC, anonymousNow, nil, nil)
	if !r.Anonymous {
		t.Fatalf("expected anonymous results")
	}
	byKey := map[string]*ProgramResultRow{}
	for _, row := range r.Templates {
		byKey[row.Key] = row
	}
	// the row shows its size, never an outcome
	if byKey["t1"].Suppressed || *byKey["t1"].Sent != 6 || byKey["t1"].Clicked != nil || byKey["t1"].Reported != nil {
		t.Fatalf("t1 row wrong: %+v", byKey["t1"])
	}
	if !byKey["t2"].Suppressed || byKey["t2"].Sent != nil || byKey["t2"].Clicked != nil {
		t.Fatalf("t2 row should be suppressed: %+v", byKey["t2"])
	}
	if _, ok := byKey["t3"]; ok {
		t.Fatalf("training send must not be a template result")
	}
	// the same data for a normal program shows every count
	facts.anonymous = false
	r = results(facts, time.UTC, resultsNow, nil, nil)
	// a training template row holds the training counts, not the phishing ones
	for _, row := range r.Templates {
		if row.Key == "t3" {
			if row.Suppressed || row.Sent != nil || row.TrainingSent == nil || *row.TrainingSent != 1 {
				t.Fatalf("training row wrong: %+v", row)
			}
			continue
		}
		if row.Suppressed || row.Sent == nil || row.Clicked == nil {
			t.Fatalf("normal program rows must not be suppressed: %+v", row)
		}
	}
	// a period keeps only the sends inside it
	from, to := at(20), at(6)
	r = results(facts, time.UTC, resultsNow, &from, &to)
	// the 6 phishing sends of t1 and the training send of t3
	if len(r.Templates) != 2 || *r.Templates[0].Sent != 6 || r.Templates[1].Key != "t3" || *r.Summary.Sent != 6 {
		t.Fatalf("period filter wrong: %+v", r.Templates)
	}
}

func TestProgramAggregateCSVWithholds(t *testing.T) {
	results := &ProgramResults{
		Anonymous: true,
		Summary:   &ProgramSummary{Recipients: 20, HasPhishing: true, Sent: intPtr(18), ClickedLink: nil, SubmittedData: intPtr(0), Reported: intPtr(7)},
		Months:    []*ProgramResultRow{{Key: "2025-05", Name: "May 2025", Suppressed: true}},
		Templates: []*ProgramResultRow{
			{Key: "t", Name: "=HYPERLINK(\"x\")", Sent: intPtr(9), Clicked: intPtr(1), Submitted: intPtr(0), Reported: intPtr(2)},
		},
	}
	rows := programAggregateCSV(results)
	// header, 5 result rows without training and without repeat offenders,
	// which an anonymous program never counts, a month and a template
	if len(rows) != 8 {
		t.Fatalf("expected 8 rows, got %d", len(rows))
	}
	labels := []string{}
	for _, r := range rows[1:6] {
		labels = append(labels, r[2])
	}
	if strings.Join(labels, ",") != "Recipients,Sent,Clicked link,Data submitted,Reported" {
		t.Fatalf("result rows: %v", labels)
	}
	if rows[3][3] != "" || rows[3][9] != "yes" || rows[5][3] != "7" {
		t.Fatalf("a withheld result has no value: %v %v", rows[3], rows[5])
	}
	if rows[6][4] != "" || rows[6][9] != "yes" {
		t.Fatalf("suppressed month must have no counts: %v", rows[6])
	}
	if rows[7][2] != "'=HYPERLINK(\"x\")" || rows[7][4] != "9" {
		t.Fatalf("template row wrong: %v", rows[7])
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
	d := programReportFromResults(program, &programReportSetup{}, results(facts, time.UTC, resultsNow, nil, nil), resultsNow, nil, nil)
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

// TestProgramSummaryAnonymousSmallMonthHidesSent checks the send total of an
// anonymous program is withheld for a range that holds a month with fewer than
// the minimum people, so a one month range or report cannot single it out,
// while a range of only large months shows the send total
func TestProgramSummaryAnonymousSmallMonthHidesSent(t *testing.T) {
	now := time.Date(2025, 7, 10, 12, 0, 0, 0, time.UTC)
	monthStart := func(m time.Month) time.Time { return time.Date(2025, m, 1, 0, 0, 0, 0, time.UTC) }
	sentIn := func(p uuid.UUID, m time.Month) *programSend {
		return &programSend{Person: p, SentAt: monthStart(m).Add(9 * 24 * time.Hour), Template: "t1"}
	}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	// May and June each have 10 people, a settled month
	for _, m := range []time.Month{time.May, time.June} {
		for _, p := range people(10) {
			facts.sends = append(facts.sends, sentIn(p, m))
		}
	}
	// a small April month of 3 people
	for _, p := range people(3) {
		facts.sends = append(facts.sends, sentIn(p, time.April))
	}
	// the whole range holds the small April month, so the send total is withheld
	s := results(facts, time.UTC, now, nil, nil).Summary
	if s.Sent != nil {
		t.Fatalf("a range with a small month must withhold the send total: %+v", s)
	}
	// the total of every template follows, so subtracting the shown months
	// from it can not give the small month, while HasSent still says sends went out
	if s.SentTotal != 0 || !s.SentTotalHidden || !s.HasSent {
		t.Fatalf("a range with a small month must withhold the total too: %+v", s)
	}
	// a range of May and June only shows the summed send total
	from := monthStart(time.May)
	s = results(facts, time.UTC, now, &from, nil).Summary
	if s.Sent == nil || *s.Sent != 20 {
		t.Fatalf("a range of large months shows the send total: %+v", s)
	}
	if s.SentTotal != 20 || s.SentTotalHidden || !s.HasSent {
		t.Fatalf("a range of large months shows the total: %+v", s)
	}
}

// TestProgramSummaryAnonymousSmallTrainingHidesTotal checks the total of every
// template is withheld when the training sends of a month are too few, so
// subtracting Sent from it can not give them
func TestProgramSummaryAnonymousSmallTrainingHidesTotal(t *testing.T) {
	now := time.Date(2025, 7, 10, 12, 0, 0, 0, time.UTC)
	sentAt := time.Date(2025, time.May, 10, 0, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	ps := people(20)
	for _, p := range ps {
		facts.sends = append(facts.sends, &programSend{Person: p, SentAt: sentAt, Template: "t1"})
	}
	// one person got a training send in the same month
	facts.sends = append(facts.sends, &programSend{Person: ps[0], SentAt: sentAt, Template: "t2", Training: true})
	s := results(facts, time.UTC, now, nil, nil).Summary
	if s.Sent == nil || *s.Sent != 20 {
		t.Fatalf("20 phishing sends show: %+v", s)
	}
	if s.SentTotal != 0 || !s.SentTotalHidden || !s.HasSent {
		t.Fatalf("a small training month must withhold the total: %+v", s)
	}
}

// TestProgramAnonymousTemplateRowHoldsSmallMonth checks a template row is
// withheld when a month of the view sent that template to too few people, so
// its total can not be compared with the month rows or another view
func TestProgramAnonymousTemplateRowHoldsSmallMonth(t *testing.T) {
	now := time.Date(2025, 7, 10, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2025, time.January, 10, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, time.February, 10, 0, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	for _, p := range people(10) {
		facts.sends = append(facts.sends, &programSend{Person: p, SentAt: jan, Template: "t1"})
	}
	for _, p := range people(2) {
		facts.sends = append(facts.sends, &programSend{Person: p, SentAt: feb, Template: "t1"})
	}
	both := results(facts, time.UTC, now, nil, nil)
	if len(both.Templates) != 1 || both.Templates[0].Sent != nil || !both.Templates[0].Suppressed {
		t.Fatalf("a view holding the small February must withhold the template row: %+v", both.Templates)
	}
	to := time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)
	janOnly := results(facts, time.UTC, now, nil, &to)
	if len(janOnly.Templates) != 1 || janOnly.Templates[0].Sent == nil || *janOnly.Templates[0].Sent != 10 {
		t.Fatalf("a view of January only shows the template row: %+v", janOnly.Templates)
	}
}
