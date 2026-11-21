package model

import (
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("zone %s: %v", name, err)
	}
	return loc
}

func reportProgram(zone, interval, period string, started time.Time) *Program {
	return &Program{
		ReportEnabled:  nullable.NewNullableWithValue(true),
		ReportInterval: nullable.NewNullableWithValue(interval),
		ReportPeriod:   nullable.NewNullableWithValue(period),
		TimeZone:       nullable.NewNullableWithValue(zone),
		StartedAt:      &started,
		ClosedAt:       nullable.NewNullNullable[time.Time](),
	}
}

func TestProgramReportIntervalStart(t *testing.T) {
	cph := mustZone(t, "Europe/Copenhagen")
	at := time.Date(2026, time.August, 17, 13, 0, 0, 0, cph)
	cases := []struct {
		interval string
		want     time.Time
	}{
		{ProgramReportMonthly, time.Date(2026, time.August, 1, 0, 0, 0, 0, cph)},
		{ProgramReportQuarterly, time.Date(2026, time.July, 1, 0, 0, 0, 0, cph)},
		{ProgramReportYearly, time.Date(2026, time.January, 1, 0, 0, 0, 0, cph)},
	}
	for _, c := range cases {
		got := ProgramReportIntervalStart(at, c.interval, cph)
		if !got.Equal(c.want) {
			t.Fatalf("%s: got %v want %v", c.interval, got, c.want)
		}
	}
	// quarter edges
	for month, want := range map[time.Month]time.Month{
		time.January: time.January, time.March: time.January, time.April: time.April,
		time.June: time.April, time.September: time.July, time.October: time.October, time.December: time.October,
	} {
		got := ProgramReportIntervalStart(time.Date(2026, month, 15, 0, 0, 0, 0, cph), ProgramReportQuarterly, cph)
		if got.Month() != want || got.Day() != 1 {
			t.Fatalf("quarter of %v: got %v", month, got)
		}
	}
	// shifting across a year end
	got := ProgramReportIntervalShift(time.Date(2026, time.October, 1, 0, 0, 0, 0, cph), ProgramReportQuarterly, 1, cph)
	if !got.Equal(time.Date(2027, time.January, 1, 0, 0, 0, 0, cph)) {
		t.Fatalf("next quarter: %v", got)
	}
	got = ProgramReportIntervalShift(time.Date(2026, time.January, 1, 0, 0, 0, 0, cph), ProgramReportMonthly, -1, cph)
	if !got.Equal(time.Date(2025, time.December, 1, 0, 0, 0, 0, cph)) {
		t.Fatalf("previous month: %v", got)
	}
}

func TestProgramReportDuePeriodZoneBoundary(t *testing.T) {
	// 23:30 UTC on 31 Oct is already 1 Nov in Copenhagen, the month is over
	// there but not in New York
	now := time.Date(2026, time.October, 31, 23, 30, 0, 0, time.UTC)
	started := time.Date(2026, time.March, 3, 9, 0, 0, 0, time.UTC)
	p := reportProgram("Europe/Copenhagen", ProgramReportMonthly, ProgramReportPeriodLast, started)
	from, to, ok := p.ReportDuePeriod(now)
	cph := mustZone(t, "Europe/Copenhagen")
	if !ok || !from.Equal(time.Date(2026, time.October, 1, 0, 0, 0, 0, cph)) || !to.Equal(time.Date(2026, time.November, 1, 0, 0, 0, 0, cph)) {
		t.Fatalf("copenhagen: %v %v %v", from, to, ok)
	}
	ny := reportProgram("America/New_York", ProgramReportMonthly, ProgramReportPeriodLast, started)
	nyLoc := mustZone(t, "America/New_York")
	from, to, ok = ny.ReportDuePeriod(now)
	if !ok || !to.Equal(time.Date(2026, time.October, 1, 0, 0, 0, 0, nyLoc)) || !from.Equal(time.Date(2026, time.September, 1, 0, 0, 0, 0, nyLoc)) {
		t.Fatalf("new york still reports september: %v %v %v", from, to, ok)
	}
}

func TestProgramReportDuePeriodNoDoubleSend(t *testing.T) {
	utc := time.UTC
	started := time.Date(2026, time.January, 10, 0, 0, 0, 0, utc)
	p := reportProgram("UTC", ProgramReportQuarterly, ProgramReportPeriodLast, started)
	now := time.Date(2026, time.July, 2, 8, 0, 0, 0, utc)
	from, to, ok := p.ReportDuePeriod(now)
	if !ok || !from.Equal(time.Date(2026, time.April, 1, 0, 0, 0, 0, utc)) || !to.Equal(time.Date(2026, time.July, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("second quarter due: %v %v %v", from, to, ok)
	}
	// once sent, the same period is not due again for the rest of the quarter
	p.ReportLastPeriodEnd = &to
	for _, later := range []time.Time{now.Add(time.Minute), time.Date(2026, time.September, 30, 23, 59, 0, 0, utc)} {
		if _, _, ok := p.ReportDuePeriod(later); ok {
			t.Fatalf("period sent twice at %v", later)
		}
	}
	// the next quarter is due when it ends
	_, to2, ok := p.ReportDuePeriod(time.Date(2026, time.October, 1, 0, 0, 1, 0, utc))
	if !ok || !to2.Equal(time.Date(2026, time.October, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("third quarter due: %v %v", to2, ok)
	}
	// a zone change moves the period end by hours, that is not a new period
	p.TimeZone = nullable.NewNullableWithValue("America/Los_Angeles")
	if _, _, ok := p.ReportDuePeriod(time.Date(2026, time.July, 1, 12, 0, 0, 0, utc)); ok {
		t.Fatalf("a zone change must not send the period again")
	}
}

func TestProgramReportDuePeriodYearAndStart(t *testing.T) {
	utc := time.UTC
	started := time.Date(2025, time.March, 14, 10, 0, 0, 0, utc)
	p := reportProgram("UTC", ProgramReportYearly, ProgramReportPeriodStart, started)
	from, to, ok := p.ReportDuePeriod(time.Date(2026, time.January, 1, 0, 5, 0, 0, utc))
	if !ok || !to.Equal(time.Date(2026, time.January, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("year due: %v %v", to, ok)
	}
	// since start covers whole months from the month the program started in
	if !from.Equal(time.Date(2025, time.March, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("since start: %v", from)
	}
	// a program that started after the last year ended has no year to report
	late := reportProgram("UTC", ProgramReportYearly, ProgramReportPeriodLast, time.Date(2026, time.February, 1, 0, 0, 0, 0, utc))
	if _, _, ok := late.ReportDuePeriod(time.Date(2026, time.June, 1, 0, 0, 0, 0, utc)); ok {
		t.Fatalf("nothing is due before the first year ends")
	}
}

func TestProgramReportDuePeriodSkips(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, time.May, 3, 0, 0, 0, 0, utc)
	started := time.Date(2026, time.January, 5, 0, 0, 0, 0, utc)
	off := reportProgram("UTC", ProgramReportMonthly, ProgramReportPeriodLast, started)
	off.ReportEnabled = nullable.NewNullableWithValue(false)
	if _, _, ok := off.ReportDuePeriod(now); ok {
		t.Fatalf("reports off")
	}
	draft := reportProgram("UTC", ProgramReportMonthly, ProgramReportPeriodLast, started)
	draft.StartedAt = nil
	if _, _, ok := draft.ReportDuePeriod(now); ok {
		t.Fatalf("a program that never started has nothing to report")
	}
	// a program closed in April reports April once, then nothing
	closed := reportProgram("UTC", ProgramReportMonthly, ProgramReportPeriodLast, started)
	closed.ClosedAt = nullable.NewNullableWithValue(time.Date(2026, time.April, 20, 0, 0, 0, 0, utc))
	from, to, ok := closed.ReportDuePeriod(now)
	if !ok || !from.Equal(time.Date(2026, time.April, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("final period of a closed program: %v %v %v", from, to, ok)
	}
	closed.ReportLastPeriodEnd = &to
	if _, _, ok := closed.ReportDuePeriod(time.Date(2026, time.June, 2, 0, 0, 0, 0, utc)); ok {
		t.Fatalf("a closed program reports its final period only")
	}
	if next := closed.ReportNextAtTime(time.Date(2026, time.May, 10, 0, 0, 0, 0, utc)); next != nil {
		t.Fatalf("a closed program that sent its final report has no next: %v", next)
	}
}

func TestApplyReportBaseline(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, time.November, 12, 10, 0, 0, 0, utc)
	incoming := &Program{
		ReportEnabled:  nullable.NewNullableWithValue(true),
		ReportInterval: nullable.NewNullableWithValue(ProgramReportQuarterly),
		TimeZone:       nullable.NewNullableWithValue("UTC"),
	}
	incoming.ApplyReportBaseline(nil, now)
	if incoming.ReportBaseline == nil || !incoming.ReportBaseline.Equal(time.Date(2026, time.October, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("baseline on create: %v", incoming.ReportBaseline)
	}
	// the stored baseline holds back the quarter that ended before reports were on
	started := time.Date(2026, time.January, 5, 0, 0, 0, 0, utc)
	p := reportProgram("UTC", ProgramReportQuarterly, ProgramReportPeriodLast, started)
	p.ReportLastPeriodEnd = incoming.ReportBaseline
	if _, _, ok := p.ReportDuePeriod(now); ok {
		t.Fatalf("no report for the quarter before reports were on")
	}
	if next := p.ReportNextAtTime(now); next == nil || !next.Equal(time.Date(2027, time.January, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("next report: %v", next)
	}
	// an update that keeps reports on with the same interval keeps the stored end
	same := &Program{ReportEnabled: nullable.NewNullableWithValue(true)}
	same.ApplyReportBaseline(p, now)
	if same.ReportBaseline != nil {
		t.Fatalf("unchanged settings must keep the last reported end")
	}
	// a new interval starts over from now
	other := &Program{ReportEnabled: nullable.NewNullableWithValue(true), ReportInterval: nullable.NewNullableWithValue(ProgramReportMonthly)}
	other.ApplyReportBaseline(p, now)
	if other.ReportBaseline == nil || !other.ReportBaseline.Equal(time.Date(2026, time.November, 1, 0, 0, 0, 0, utc)) {
		t.Fatalf("baseline on interval change: %v", other.ReportBaseline)
	}
	// turning reports off sets no baseline
	offUpdate := &Program{ReportEnabled: nullable.NewNullableWithValue(false)}
	offUpdate.ApplyReportBaseline(p, now)
	if offUpdate.ReportBaseline != nil {
		t.Fatalf("turning off sets no baseline")
	}
}

func TestProgramReportValidate(t *testing.T) {
	p := &Program{
		Name:           nullable.NewNullableWithValue(*vo.NewString64Must("p")),
		ReportInterval: nullable.NewNullableWithValue("weekly"),
	}
	if err := p.Validate(); err == nil {
		t.Fatalf("weekly is not an interval")
	}
	p.ReportInterval = nullable.NewNullableWithValue(ProgramReportYearly)
	p.ReportPeriod = nullable.NewNullableWithValue("all")
	if err := p.Validate(); err == nil {
		t.Fatalf("unknown period")
	}
	p.ReportPeriod = nullable.NewNullableWithValue(ProgramReportPeriodStart)
	if err := p.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	m := p.ToDBMap()
	if m["report_interval"] != ProgramReportYearly || m["report_period"] != ProgramReportPeriodStart {
		t.Fatalf("db map: %v", m)
	}
	if _, has := m["report_last_period_end"]; has {
		t.Fatalf("the last reported end is never written from a request")
	}
}

func TestProgramReportNextAtAnonymousSettles(t *testing.T) {
	utc := time.UTC
	started := time.Date(2026, time.January, 5, 0, 0, 0, 0, utc)
	p := reportProgram("UTC", ProgramReportMonthly, ProgramReportPeriodLast, started)
	p.IsAnonymous = nullable.NewNullableWithValue(true)
	sept := time.Date(2026, time.October, 1, 0, 0, 0, 0, utc)
	p.ReportLastPeriodEnd = &sept
	// October has ended but is not settled on 2 November, so its report is
	// sent on 4 November, the day the service sends it
	now := time.Date(2026, time.November, 2, 9, 0, 0, 0, utc)
	if next := p.ReportNextAtTime(now); next == nil || !next.Equal(time.Date(2026, time.November, 4, 0, 0, 0, 0, utc)) {
		t.Fatalf("anonymous next report: %v", next)
	}
	// once settled the report is due now
	settled := time.Date(2026, time.November, 4, 9, 0, 0, 0, utc)
	if next := p.ReportNextAtTime(settled); next == nil || !next.Equal(settled) {
		t.Fatalf("anonymous report due once settled: %v", next)
	}
	// a named program reports October on 1 November
	p.IsAnonymous = nullable.NewNullableWithValue(false)
	if next := p.ReportNextAtTime(now); next == nil || !next.Equal(now) {
		t.Fatalf("named report due at month end: %v", next)
	}
}
