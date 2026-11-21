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

// TestProgramReportPeriodRepeatOffenders checks a report period counts its own
// sends and reads the repeat offender window back from the end of the period
func TestProgramReportPeriodRepeatOffenders(t *testing.T) {
	to := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	facts := &programFacts{templateNames: map[string]string{}}
	for _, p := range people(10) {
		// clicks nine months and ten days before the end
		for _, sentAt := range []time.Time{to.AddDate(0, -9, 0), to.AddDate(0, 0, -10)} {
			sd := &programSend{Person: p, Campaign: uuid.New(), SentAt: sentAt, Template: "t1"}
			sd.ClickedAt = tp(sentAt.Add(time.Hour))
			facts.sends = append(facts.sends, sd)
		}
		// a send after the end is not read
		after := &programSend{Person: p, Campaign: uuid.New(), SentAt: to.AddDate(0, 0, 5), Template: "t1"}
		after.ClickedAt = tp(after.SentAt.Add(time.Hour))
		facts.sends = append(facts.sends, after)
	}
	in := programResultsInput{loc: time.UTC, now: now, to: &to, repeatMonths: 12}
	r := buildProgramResults(facts, in)
	if *r.Summary.RepeatOffenders != 10 || *r.Summary.Sent != 10 || r.Summary.SentTotal != 20 {
		t.Fatalf("period summary: %+v", r.Summary)
	}
	// a 6 month window from the end of the period leaves one click each
	in.repeatMonths = 6
	if r := buildProgramResults(facts, in); *r.Summary.RepeatOffenders != 0 {
		t.Fatalf("6 months: %+v", r.Summary)
	}
	// a period of the last week before the end holds no sends
	from := to.AddDate(0, 0, -7)
	in = programResultsInput{loc: time.UTC, now: now, from: &from, to: &to, repeatMonths: 12}
	d := programReportFromResults(&model.Program{Status: "active"}, &programReportSetup{}, buildProgramResults(facts, in), now, &from, &to)
	if d.HasSends || d.Period != "24 Apr 2025 to 30 Apr 2025" {
		t.Fatalf("period = %q, sends %v", d.Period, d.HasSends)
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

// TestProgramWindowEndOfDay checks an end of 23:59 runs to midnight, so an all
// day window has no closed minute between days
func TestProgramWindowEndOfDay(t *testing.T) {
	p, loc := copenhagenWindow(t)
	p.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("00:00"))
	p.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("23:59"))
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, loc)
	spans := programWindowSpans(p, day, day.AddDate(0, 0, 2))
	if len(spans) != 2 {
		t.Fatalf("expected two day spans, got %d", len(spans))
	}
	if !spans[0].end.Equal(day.AddDate(0, 0, 1)) || !spans[1].start.Equal(spans[0].end) {
		t.Fatalf("days must meet at midnight: %v to %v, then %v", spans[0].start, spans[0].end, spans[1].start)
	}
	lastMinute := time.Date(2026, 6, 10, 23, 59, 30, 0, loc)
	if got := programNextWindowSlot(p, lastMinute); !got.Equal(lastMinute) {
		t.Fatalf("23:59:30 must be inside the window, next slot %v", got)
	}
}
