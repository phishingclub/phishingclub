package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWithholdGap(t *testing.T) {
	cases := []struct {
		a, b int
		want bool
	}{
		{10, 10, true},
		{10, 9, true},
		{10, 6, true},
		{10, 5, false},
		{6, 10, true},
		{0, 5, false},
		{20, 0, false},
	}
	for _, c := range cases {
		if got := withholdGap(c.a, c.b); got != c.want {
			t.Fatalf("withholdGap(%d, %d) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// anonGapMonth returns the January sends of 20 people where the first clicked
// people clicked, the first submitted of them also submitted and the first
// reported reported
func anonGapMonth(ps []uuid.UUID, clicked, submitted, reported int) []*programSend {
	out := anonMonthSends(ps, time.January, clicked)
	for i, sd := range out {
		if i < submitted {
			sd.SubmittedAt = tp(sd.SentAt.Add(2 * time.Hour))
		}
		if i < reported {
			sd.ReportedAt = tp(sd.SentAt.Add(3 * time.Hour))
		}
	}
	return out
}

// TestProgramAnonymousWithholdsZero checks an anonymous program withholds an
// outcome nobody had in a month, in the summary, the tiles and every view
// that holds the month, while the other outcomes still show
func TestProgramAnonymousWithholdsZero(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	facts.sends = anonGapMonth(people(20), 8, 0, 6)
	jan := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb := jan.AddDate(0, 1, 0)
	for _, s := range []*ProgramSummary{
		results(facts, time.UTC, now, &jan, &feb).Summary,
		results(facts, time.UTC, now, nil, nil).Summary,
	} {
		if s.SubmittedData != nil || !s.Suppressed {
			t.Fatalf("a submitted count of zero must be withheld: %+v", s)
		}
		if intOr(s.ClickedLink) != 8 || intOr(s.Reported) != 6 || intOr(s.Sent) != 20 {
			t.Fatalf("the other outcomes must show: %+v", s)
		}
	}
	in := programResultsInput{now: now, enrollment: programEnrollmentCounts{total: 20, active: 20}}
	tl := tilesFor(facts, in, ProgramTimeRangeAll, false)
	if tl.SubmittedData != nil || intOr(tl.ClickedLink) != 8 || !tl.Suppressed {
		t.Fatalf("tiles must withhold the zero: %+v", tl)
	}
	// the PDF and the CSV read the same summary
	csv := programAggregateCSV(results(facts, time.UTC, now, nil, nil))
	for _, row := range csv {
		if len(row) > 9 && row[2] == "Data submitted" && (row[3] != "" || row[9] != "yes") {
			t.Fatalf("the CSV must withhold the zero: %v", row)
		}
	}
}

// TestProgramAnonymousRelatedGap checks clicked and submitted, and training
// started and completed, are both withheld when they differ by fewer than
// programMinGroup people in a month, as the difference would name them, and
// that a view holding that month withholds them too
func TestProgramAnonymousRelatedGap(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	feb := jan.AddDate(0, 1, 0)
	summary := func(facts *programFacts) *ProgramSummary {
		return results(facts, time.UTC, now, &jan, &feb).Summary
	}
	// 8 clicked and 6 of them submitted: the 2 who only clicked would show
	gap := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	gap.sends = anonGapMonth(people(20), 8, 6, 6)
	s := summary(gap)
	if s.ClickedLink != nil || s.SubmittedData != nil || !s.Suppressed {
		t.Fatalf("a clicked and submitted gap of 2 must withhold both: %+v", s)
	}
	if intOr(s.Reported) != 6 {
		t.Fatalf("reported is not related and must show: %+v", s)
	}
	in := programResultsInput{now: now, enrollment: programEnrollmentCounts{total: 20, active: 20}}
	if tl := tilesFor(gap, in, ProgramTimeRangeAll, false); tl.ClickedLink != nil || tl.SubmittedData != nil || !tl.Suppressed {
		t.Fatalf("tiles must withhold the gap: %+v", tl)
	}
	// a later month that passes on its own does not bring them back, as the
	// view would differ from February alone by January
	ps := people(20)
	later := anonMonthSends(ps, time.February, 12)
	for i, sd := range later {
		if i < 6 {
			sd.SubmittedAt = tp(sd.SentAt.Add(2 * time.Hour))
		}
	}
	gap.sends = append(gap.sends, later...)
	mar := feb.AddDate(0, 1, 0)
	if s := results(gap, time.UTC, now, &jan, &mar).Summary; s.ClickedLink != nil || s.SubmittedData != nil {
		t.Fatalf("a view holding January must withhold both: %+v", s)
	}
	if s := results(gap, time.UTC, now, &feb, &mar).Summary; intOr(s.ClickedLink) != 12 || intOr(s.SubmittedData) != 6 {
		t.Fatalf("February alone passes: %+v", s)
	}

	// a gap of 5 is shown
	wide := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	wide.sends = anonGapMonth(people(20), 11, 6, 6)
	if s := summary(wide); intOr(s.ClickedLink) != 11 || intOr(s.SubmittedData) != 6 {
		t.Fatalf("a gap of 5 must show: %+v", s)
	}

	// training started and completed differ by 2
	training := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Template"}}
	for i, p := range people(20) {
		sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: time.Date(2026, time.January, 12, 10, 0, 0, 0, time.UTC), Template: "t1", Training: true}
		if i < 10 {
			sd.TrainingStartedAt = tp(sd.SentAt.Add(time.Hour))
		}
		if i < 8 {
			sd.TrainingCompletedAt = tp(sd.SentAt.Add(2 * time.Hour))
		}
		training.sends = append(training.sends, sd)
	}
	if s := summary(training); s.TrainingStarted != nil || s.TrainingCompleted != nil {
		t.Fatalf("a training gap of 2 must withhold both: %+v", s)
	}
}
