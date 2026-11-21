package service

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/errs"
)

// tilesFor builds the tiles of facts for a time range at now, as the program
// page reads them: the results since the start, then the tiles of the range
func tilesFor(facts *programFacts, in programResultsInput, key string, closed bool) *ProgramTiles {
	if in.loc == nil {
		in.loc = time.UTC
	}
	if in.repeatMonths == 0 {
		in.repeatMonths = 12
	}
	all := buildProgramResults(facts, in)
	in.hasTraining = all.Summary.HasTraining
	period := programTilesPeriod{key: key, from: programTimeRangeFrom(key, in.now, in.loc)}
	return buildProgramTiles(facts, in, period, closed, all.Summary)
}

func intOr(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

// TestProgramTilesCountSendsInRange checks the tiles count sends, each once
// per outcome, sent in the time range, and that repeat offenders and the
// recipients do not change with the range
func TestProgramTilesCountSendsInRange(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{}}
	ps := people(3)
	facts.sends = append(facts.sends,
		send(ps[0], 5, "clicked"),
		send(ps[0], 40, "clicked"),
		send(ps[0], 100, "submitted"),
		send(ps[1], 10, "reported"),
		send(ps[1], 200, "clicked"),
		send(ps[1], 400, ""),
		&programSend{Person: ps[2], SentAt: at(3), Training: true,
			TrainingStartedAt: tp(at(2)), TrainingCompletedAt: tp(at(2))},
		&programSend{Person: ps[2], SentAt: at(60), Training: true, TrainingStartedAt: tp(at(59))},
	)
	in := programResultsInput{now: resultsNow, enrollment: programEnrollmentCounts{
		total: 9, active: 5, completed: 3, exited: 1,
	}}
	cases := []struct {
		key                                              string
		sent, clicked, submitted, reported               int
		trainingSent, trainingStarted, trainingCompleted int
	}{
		{ProgramTimeRange30Days, 2, 1, 0, 1, 1, 1, 1},
		{ProgramTimeRange90Days, 3, 2, 0, 1, 2, 2, 1},
		{ProgramTimeRange12Months, 5, 4, 1, 1, 2, 2, 1},
		{ProgramTimeRangeAll, 6, 4, 1, 1, 2, 2, 1},
	}
	for _, c := range cases {
		tl := tilesFor(facts, in, c.key, false)
		if tl.Range != c.key || intOr(tl.Sent) != c.sent || intOr(tl.ClickedLink) != c.clicked ||
			intOr(tl.SubmittedData) != c.submitted || intOr(tl.Reported) != c.reported {
			t.Fatalf("%s: phishing tiles %+v", c.key, tl)
		}
		if !tl.HasTraining || intOr(tl.TrainingSent) != c.trainingSent ||
			intOr(tl.TrainingStarted) != c.trainingStarted || intOr(tl.TrainingCompleted) != c.trainingCompleted {
			t.Fatalf("%s: training tiles %+v", c.key, tl)
		}
		// p0 clicked on 3 sends within 12 months, p1 on 1
		if intOr(tl.RepeatOffenders) != 1 || tl.RepeatOffenderMonths != 12 {
			t.Fatalf("%s: repeat offenders %+v", c.key, tl)
		}
		if tl.Recipients != 5 || tl.Completed != 3 || tl.Removed != 1 || tl.Suppressed {
			t.Fatalf("%s: recipients %+v", c.key, tl)
		}
	}
	// a closed program shows who was still active when it closed
	in.enrollment = programEnrollmentCounts{total: 9, stopped: 4, completed: 5}
	if tl := tilesFor(facts, in, ProgramTimeRange30Days, true); tl.Recipients != 4 {
		t.Fatalf("closed recipients %+v", tl)
	}
}

// TestProgramTilesRangeStartsAtDay checks a time range starts at the start of
// a day in the program time zone, today included
func TestProgramTilesRangeStartsAtDay(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no time zone data")
	}
	now := time.Date(2025, 6, 1, 23, 30, 0, 0, time.UTC) // 1:30 on June 2 in Copenhagen
	from := programTimeRangeFrom(ProgramTimeRange30Days, now, loc)
	if want := time.Date(2025, 5, 4, 0, 0, 0, 0, loc); !from.Equal(want) {
		t.Fatalf("30 days from %v, want %v", from, want)
	}
	if want := time.Date(2025, 3, 5, 0, 0, 0, 0, loc); !programTimeRangeFrom(ProgramTimeRange90Days, now, loc).Equal(want) {
		t.Fatalf("90 days from %v", programTimeRangeFrom(ProgramTimeRange90Days, now, loc))
	}
	if want := time.Date(2024, 6, 3, 0, 0, 0, 0, loc); !programTimeRangeFrom(ProgramTimeRange12Months, now, loc).Equal(want) {
		t.Fatalf("12 months from %v", programTimeRangeFrom(ProgramTimeRange12Months, now, loc))
	}
	if programTimeRangeFrom(ProgramTimeRangeAll, now, loc) != nil {
		t.Fatal("since start must have no start")
	}
}

// TestProgramTilesAnonymous checks an anonymous program counts the last
// whole settled months of a range per send, and withholds an outcome when a
// month of the range withholds it on its own
func TestProgramTilesAnonymous(t *testing.T) {
	now := time.Date(2025, 6, 10, 12, 0, 0, 0, time.UTC)
	month := func(m time.Month) time.Time { return time.Date(2025, m, 1, 0, 0, 0, 0, time.UTC) }
	sentIn := func(p uuid.UUID, m time.Month, clicked bool) *programSend {
		sd := &programSend{Person: p, SentAt: month(m).Add(9 * 24 * time.Hour), Template: "t1"}
		if clicked {
			sd.ClickedAt = tp(sd.SentAt.Add(time.Hour))
		}
		return sd
	}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	ps := people(12)
	// a send each in March and April, the first 6 people clicked both
	for i, p := range ps {
		facts.sends = append(facts.sends, sentIn(p, time.March, i < 6), sentIn(p, time.April, i < 6))
	}
	// 3 people in May, one clicked
	short := people(3)
	facts.sends = append(facts.sends, sentIn(short[0], time.May, true), sentIn(short[1], time.May, false), sentIn(short[2], time.May, false))
	// a June send has not settled and is never counted
	facts.sends = append(facts.sends, sentIn(ps[0], time.June, true))
	in := programResultsInput{now: now, enrollment: programEnrollmentCounts{total: 15, active: 15}}

	// the last 30 days are May, 3 people: every outcome is withheld
	tl := tilesFor(facts, in, ProgramTimeRange30Days, false)
	// May has 3 people, so the send total is withheld with every outcome
	if tl.Sent != nil || tl.ClickedLink != nil || tl.SubmittedData != nil || tl.Reported != nil || !tl.Suppressed {
		t.Fatalf("30 days must be withheld: %+v", tl)
	}
	if !tl.RangeFrom.Equal(month(time.May)) || !tl.RangeTo.Equal(month(time.June)) || tl.RangeText != "May 2025" {
		t.Fatalf("30 days covers %v to %v", tl.RangeFrom, tl.RangeTo)
	}
	// 90 days hold May, so the outcomes stay withheld though the total is large
	tl = tilesFor(facts, in, ProgramTimeRange90Days, false)
	// the small May month is in the range, so the send total is withheld too
	if tl.Sent != nil || tl.ClickedLink != nil || !tl.RangeFrom.Equal(month(time.March)) {
		t.Fatalf("90 days with a small month: %+v", tl)
	}
	// without May every month of the range passes, the sends are summed
	facts.sends = facts.sends[:24]
	tl = tilesFor(facts, in, ProgramTimeRange90Days, false)
	// nobody submitted, a zero is withheld as it tells what everyone did
	if intOr(tl.Sent) != 24 || intOr(tl.ClickedLink) != 12 || tl.SubmittedData != nil {
		t.Fatalf("90 days: %+v", tl)
	}
	// repeat offenders count people across months and are never shown
	if tl.RepeatOffenders != nil || !tl.Suppressed {
		t.Fatalf("repeat offenders must be withheld: %+v", tl)
	}
	tl = tilesFor(facts, in, ProgramTimeRangeAll, false)
	if intOr(tl.Sent) != 24 || !tl.RangeFrom.Equal(month(time.March)) || !tl.RangeTo.Equal(month(time.June)) || tl.RangeText != "Mar 2025 to May 2025" {
		t.Fatalf("since the start: %+v", tl)
	}
	// 9 of 12 clicked in April, a count that leaves fewer than the minimum out
	// is withheld
	for i := 6; i < 9; i++ {
		facts.sends[i*2+1].ClickedAt = tp(facts.sends[i*2+1].SentAt.Add(time.Hour))
	}
	tl = tilesFor(facts, in, ProgramTimeRangeAll, false)
	if intOr(tl.Sent) != 24 || tl.ClickedLink != nil || !tl.Suppressed {
		t.Fatalf("9 of 12 clicked must be withheld: %+v", tl)
	}
}

// TestProgramTimeRangeValidation checks only the time range keys are taken
func TestProgramTimeRangeValidation(t *testing.T) {
	for _, v := range []string{"30d", "90d", "12m", "all"} {
		if !IsValidProgramTimeRange(v) {
			t.Fatalf("%q must be valid", v)
		}
	}
	for _, v := range []string{"", "7d", "30D", "all ", "1=1", "365d"} {
		if IsValidProgramTimeRange(v) {
			t.Fatalf("%q must be refused", v)
		}
	}
	s := &Program{}
	if _, err := s.GetResults(context.Background(), nil, nil, "7d"); !errors.As(err, &errs.ValidationError{}) {
		t.Fatalf("an unknown range must fail validation, got %v", err)
	}
	if defaultProgramTimeRange(true) != ProgramTimeRange90Days || defaultProgramTimeRange(false) != ProgramTimeRange30Days {
		t.Fatal("default time ranges")
	}
}

// TestProgramTilesAnonymousCompleted checks an anonymous program counts the
// people who had every template with the people still in it, open or closed,
// so the tiles never show when a person finished
func TestProgramTilesAnonymousCompleted(t *testing.T) {
	facts := &programFacts{anonymous: true, templateNames: map[string]string{}}
	in := programResultsInput{now: resultsNow, enrollment: programEnrollmentCounts{
		total: 9, active: 5, completed: 3, exited: 1,
	}}
	if tl := tilesFor(facts, in, ProgramTimeRangeAll, false); tl.Recipients != 8 || tl.Completed != 0 || tl.Removed != 1 {
		t.Fatalf("open anonymous tiles %+v", tl)
	}
	in.enrollment = programEnrollmentCounts{total: 9, stopped: 4, completed: 4, exited: 1}
	if tl := tilesFor(facts, in, ProgramTimeRangeAll, true); tl.Recipients != 8 || tl.Completed != 0 || tl.Removed != 1 {
		t.Fatalf("closed anonymous tiles %+v", tl)
	}
}
