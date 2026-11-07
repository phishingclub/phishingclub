package service

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// copenhagenWindow is a program sending every day 09:00 to 17:00 in Copenhagen
func copenhagenWindow(t *testing.T) (*model.Program, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	return &model.Program{
		TimeZone:            nullable.NewNullableWithValue("Europe/Copenhagen"),
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(127)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
	}, loc
}

// TestProgramWindowAcrossDaylightSaving checks the window keeps its wall clock
// hours on the days the clocks change in spring and in autumn
func TestProgramWindowAcrossDaylightSaving(t *testing.T) {
	p, loc := copenhagenWindow(t)
	for _, day := range []time.Time{
		// spring forward, 02:00 becomes 03:00
		time.Date(2026, 3, 29, 0, 0, 0, 0, loc),
		// fall back, 03:00 becomes 02:00
		time.Date(2026, 10, 25, 0, 0, 0, 0, loc),
	} {
		spans := programWindowSpans(p, day, day.AddDate(0, 0, 1))
		if len(spans) != 1 {
			t.Fatalf("%v: expected one window, got %d", day, len(spans))
		}
		start, end := spans[0].start.In(loc), spans[0].end.In(loc)
		if start.Hour() != 9 || start.Minute() != 0 || end.Hour() != 17 || end.Minute() != 0 {
			t.Fatalf("%v: window %v to %v, want 09:00 to 17:00", day, start, end)
		}
		if spans[0].end.Sub(spans[0].start) != 8*time.Hour {
			t.Fatalf("%v: the window must last 8 hours", day)
		}
		// a time before the window opens on that day moves to 09:00 local
		early := time.Date(day.Year(), day.Month(), day.Day(), 4, 0, 0, 0, loc)
		if got := programNextWindowSlot(p, early).In(loc); got.Hour() != 9 || got.Day() != day.Day() {
			t.Fatalf("%v: next slot %v, want 09:00 that day", day, got)
		}
		// every drawn time over a spread across the change stays in the window
		rng := rand.New(rand.NewSource(1))
		p.SpreadMinutes = nullable.NewNullableWithValue(3 * 24 * 60)
		roundStart := day.AddDate(0, 0, -1)
		for i := 0; i < 200; i++ {
			got := programPickSendTime(p, roundStart, roundStart, rng).In(loc)
			if got.Hour() < 9 || got.Hour() >= 17 {
				t.Fatalf("%v: drawn time %v is outside the window", day, got)
			}
		}
	}
}

// TestProgramSettleCutoffAcrossDaylightSaving checks the settle cutoff is three
// whole calendar days back, also across a clock change
func TestProgramSettleCutoffAcrossDaylightSaving(t *testing.T) {
	_, loc := copenhagenWindow(t)
	cases := []struct{ now, want time.Time }{
		{time.Date(2026, 3, 30, 10, 0, 0, 0, loc), time.Date(2026, 3, 27, 0, 0, 0, 0, loc)},
		{time.Date(2026, 10, 27, 23, 30, 0, 0, loc), time.Date(2026, 10, 24, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := programSettleCutoff(c.now, loc); !got.Equal(c.want) {
			t.Fatalf("cutoff at %v = %v, want %v", c.now, got, c.want)
		}
	}
}

// TestProgramReportHeadlineWindows checks the headline reads its own windows
// back from the end of the report, not the selected period
func TestProgramReportHeadlineWindows(t *testing.T) {
	to := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{templateNames: map[string]string{}}
	ps := people(10)
	for i, p := range ps {
		// a send nine months before the end, clicked by half of the people
		old := &programSend{Person: p, Campaign: uuid.New(), SentAt: to.AddDate(0, -9, 0), Template: "t1"}
		if i < 5 {
			old.ClickedAt = tp(old.SentAt.Add(time.Hour))
		}
		// a send ten days before the end, reported by four people
		recent := &programSend{Person: p, Campaign: uuid.New(), SentAt: to.AddDate(0, 0, -10), Template: "t1"}
		if i < 4 {
			recent.ReportedAt = tp(recent.SentAt.Add(time.Hour))
		}
		// a send after the end is not read
		after := &programSend{Person: p, Campaign: uuid.New(), SentAt: to.AddDate(0, 0, 5), Template: "t1"}
		after.ClickedAt = tp(after.SentAt.Add(time.Hour))
		facts.sends = append(facts.sends, old, recent, after)
	}
	headline := programReportHeadlineResults(facts, time.UTC, now, &to)
	if headline.PhishProne.People == nil || *headline.PhishProne.People != 10 || *headline.PhishProne.Prone != 5 {
		t.Fatalf("phish prone reads 12 months back from the end: %+v", headline.PhishProne)
	}
	if headline.Last30.Sent == nil || *headline.Last30.Sent != 10 || *headline.Last30.Reported != 4 {
		t.Fatalf("report rate reads 30 days back from the end: %+v", headline.Last30)
	}
	if headline.RepeatClickers.TwoPlus == nil || *headline.RepeatClickers.TwoPlus != 0 {
		t.Fatalf("a click after the end does not count: %+v", headline.RepeatClickers)
	}
	// a period of the last week before the end holds no sends, the headline
	// still shows the windows
	from := to.AddDate(0, 0, -7)
	results := buildProgramResults(facts, nil, time.UTC, now, &from, &to)
	program := &model.Program{Status: "active"}
	d := programReportFromResults(program, &programReportSetup{}, results, headline, facts, now, &from, &to)
	got := map[string]string{}
	for _, l := range d.Headline {
		got[l.Label] = l.Value
	}
	if got["Phish prone (12 months)"] != "50% (5 of 10 people)" {
		t.Fatalf("headline phish prone = %q", got["Phish prone (12 months)"])
	}
	if got["Report rate (30 days)"] != "40%" {
		t.Fatalf("headline report rate = %q", got["Report rate (30 days)"])
	}
	if d.Period != "24 Apr 2025 to 30 Apr 2025" {
		t.Fatalf("period = %q", d.Period)
	}
	// without an end the windows read back from now
	headline = programReportHeadlineResults(facts, time.UTC, now, nil)
	if *headline.Last30.Sent != 10 || *headline.Last30.Clicked != 10 {
		t.Fatalf("without an end the last 30 days are read back from now: %+v", headline.Last30)
	}
}

// TestProgramTimeZoneAllowlist checks a program takes the same zones as a
// campaign, so the server zone is refused
func TestProgramTimeZoneAllowlist(t *testing.T) {
	for zone, ok := range map[string]bool{
		"Europe/Copenhagen": true,
		"UTC":               true,
		"Local":             false,
		"localtime":         false,
		"":                  false,
		"Factory":           false,
	} {
		p := &model.Program{}
		p.Name.Set(*vo.NewString64Must("zone"))
		p.TimeZone = nullable.NewNullableWithValue(zone)
		if err := p.Validate(); (err == nil) != ok {
			t.Fatalf("zone %q: accepted %v, want %v (%v)", zone, err == nil, ok, err)
		}
	}
}
