package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/validate"
)

// automated program report intervals
const (
	ProgramReportMonthly   = "monthly"
	ProgramReportQuarterly = "quarterly"
	ProgramReportYearly    = "yearly"
)

// automated program report periods. Last covers the interval that just ended,
// start covers everything from the month the program started in.
const (
	ProgramReportPeriodLast  = "last"
	ProgramReportPeriodStart = "start"
)

// ProgramReportIntervals are the intervals an automated report may use
var ProgramReportIntervals = []string{ProgramReportMonthly, ProgramReportQuarterly, ProgramReportYearly}

// ProgramReportPeriods are the periods an automated report may cover
var ProgramReportPeriods = []string{ProgramReportPeriodLast, ProgramReportPeriodStart}

// programReportMinGap is the least time between the ends of two reported
// periods. Every interval is far longer, so a period end that moved by a time
// zone change is not taken for a new period.
const programReportMinGap = 48 * time.Hour

// validateReportSchedule checks the automated report settings
func (p *Program) validateReportSchedule() error {
	if p.ReportInterval.IsSpecified() && !p.ReportInterval.IsNull() {
		if err := validate.ErrorIfNotContains(ProgramReportIntervals, p.ReportInterval.MustGet()); err != nil {
			return validate.WrapErrorWithField(errors.New("Pick monthly, quarterly or yearly"), "reportInterval")
		}
	}
	if p.ReportPeriod.IsSpecified() && !p.ReportPeriod.IsNull() {
		if err := validate.ErrorIfNotContains(ProgramReportPeriods, p.ReportPeriod.MustGet()); err != nil {
			return validate.WrapErrorWithField(errors.New("Pick what the report covers"), "reportPeriod")
		}
	}
	return nil
}

// reportScheduleToDBMap adds the automated report settings to a column map
func (p *Program) reportScheduleToDBMap(m map[string]any) {
	if p.ReportEnabled.IsSpecified() && !p.ReportEnabled.IsNull() {
		m["report_enabled"] = p.ReportEnabled.MustGet()
	}
	if p.ReportInterval.IsSpecified() && !p.ReportInterval.IsNull() {
		m["report_interval"] = p.ReportInterval.MustGet()
	}
	if p.ReportPeriod.IsSpecified() && !p.ReportPeriod.IsNull() {
		m["report_period"] = p.ReportPeriod.MustGet()
	}
	if p.ReportBaseline != nil {
		m["report_last_period_end"] = p.ReportBaseline.UTC()
		// a new baseline replaces a claim that was not sent
		m["report_claimed_at"] = nil
		m["report_claim_prev_end"] = nil
	}
}

// ReportEnabledOn reports whether automated reports are on
func (p *Program) ReportEnabledOn() bool {
	v, err := p.ReportEnabled.Get()
	return err == nil && v
}

// ReportIntervalOrDefault returns the report interval, monthly when unset
func (p *Program) ReportIntervalOrDefault() string {
	if v, err := p.ReportInterval.Get(); err == nil && v != "" {
		return v
	}
	return ProgramReportMonthly
}

// ReportPeriodOrDefault returns what the report covers, last when unset
func (p *Program) ReportPeriodOrDefault() string {
	if v, err := p.ReportPeriod.Get(); err == nil && v != "" {
		return v
	}
	return ProgramReportPeriodLast
}

// ProgramReportIntervalStart returns the first instant, read in loc, of the
// calendar month, quarter or year that t falls in.
func ProgramReportIntervalStart(t time.Time, interval string, loc *time.Location) time.Time {
	l := t.In(loc)
	switch interval {
	case ProgramReportQuarterly:
		month := time.Month((int(l.Month())-1)/3*3 + 1)
		return time.Date(l.Year(), month, 1, 0, 0, 0, 0, loc)
	case ProgramReportYearly:
		return time.Date(l.Year(), time.January, 1, 0, 0, 0, 0, loc)
	}
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, loc)
}

// ProgramReportIntervalShift moves an interval start, as returned by
// ProgramReportIntervalStart, n whole intervals forward or back.
func ProgramReportIntervalShift(start time.Time, interval string, n int, loc *time.Location) time.Time {
	l := start.In(loc)
	switch interval {
	case ProgramReportQuarterly:
		return time.Date(l.Year(), l.Month()+time.Month(3*n), 1, 0, 0, 0, 0, loc)
	case ProgramReportYearly:
		return time.Date(l.Year()+n, time.January, 1, 0, 0, 0, 0, loc)
	}
	return time.Date(l.Year(), l.Month()+time.Month(n), 1, 0, 0, 0, 0, loc)
}

// ReportDuePeriod returns the period [from, to) an automated report is due
// for at now, ok false when none is due. The period ends at the start of the
// interval now falls in, so only a finished month, quarter or year is
// reported, and every bound is the first day of a month in the program zone.
// A period is due once: when its end is not after the last reported end. A
// program that never started has nothing to report. A closed program reports
// the period it closed in once that period ends, then nothing more.
func (p *Program) ReportDuePeriod(now time.Time) (from, to time.Time, ok bool) {
	if !p.ReportEnabledOn() || p.StartedAt == nil {
		return from, to, false
	}
	loc := p.Location()
	interval := p.ReportIntervalOrDefault()
	to = ProgramReportIntervalStart(now, interval, loc)
	last := ProgramReportIntervalShift(to, interval, -1, loc)
	if !p.StartedAt.Before(to) {
		return from, to, false
	}
	if closed, err := p.ClosedAt.Get(); err == nil && !closed.After(last) {
		return from, to, false
	}
	if p.ReportLastPeriodEnd != nil && to.Sub(*p.ReportLastPeriodEnd) < programReportMinGap {
		return from, to, false
	}
	from = last
	if p.ReportPeriodOrDefault() == ProgramReportPeriodStart {
		from = ProgramReportIntervalStart(*p.StartedAt, ProgramReportMonthly, loc)
	}
	return from, to, true
}

// ReportNextAtTime returns when the next automated report is sent, nil when
// reports are off, the program is a draft or a closed program has nothing
// more to send. A report that is due already returns now.
func (p *Program) ReportNextAtTime(now time.Time) *time.Time {
	if !p.ReportEnabledOn() {
		return nil
	}
	loc := p.Location()
	// an anonymous program reports a period once it has settled, so its
	// clock runs ProgramAnonymousSettleDays behind
	clock := now
	settleDays := 0
	if p.IsAnonymousOn() {
		settleDays = ProgramAnonymousSettleDays
		clock = ProgramAnonymousSettleCutoff(now, loc)
	}
	if _, _, ok := p.ReportDuePeriod(clock); ok {
		return &now
	}
	interval := p.ReportIntervalOrDefault()
	from := clock
	if p.StartedAt == nil {
		start := p.StartAtTime()
		if start == nil {
			return nil
		}
		if start.After(from) {
			from = *start
		}
	}
	next := ProgramReportIntervalShift(ProgramReportIntervalStart(from, interval, loc), interval, 1, loc)
	if p.ReportLastPeriodEnd != nil && next.Sub(*p.ReportLastPeriodEnd) < programReportMinGap {
		next = ProgramReportIntervalShift(next, interval, 1, loc)
	}
	if closed, err := p.ClosedAt.Get(); err == nil {
		// the period the program closed in is the last one reported
		if !closed.After(ProgramReportIntervalShift(next, interval, -1, loc)) {
			return nil
		}
	}
	next = next.AddDate(0, 0, settleDays)
	return &next
}

// ProgramAnonymousSettleDays is how many days an anonymous program waits after
// a day ends before it counts that day, so a count never changes once shown
const ProgramAnonymousSettleDays = 3

// ProgramAnonymousSettleCutoff returns the end of the last settled day of an
// anonymous program at now, read in loc
func ProgramAnonymousSettleCutoff(now time.Time, loc *time.Location) time.Time {
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day()-ProgramAnonymousSettleDays, 0, 0, 0, 0, loc)
}

// ApplyReportBaseline sets the report baseline of a create or update that
// turns automated reports on or changes their interval, so the first report
// covers the first interval that ends after the change and no older period is
// sent. existing is nil on create.
func (p *Program) ApplyReportBaseline(existing *Program, now time.Time) {
	if !p.ReportEnabled.IsSpecified() || !p.ReportEnabledOn() {
		return
	}
	interval := ProgramReportMonthly
	if existing != nil {
		interval = existing.ReportIntervalOrDefault()
	}
	if p.ReportInterval.IsSpecified() && !p.ReportInterval.IsNull() {
		interval = p.ReportInterval.MustGet()
	}
	if existing != nil && existing.ReportEnabledOn() && existing.ReportIntervalOrDefault() == interval {
		return
	}
	loc := time.UTC
	if existing != nil {
		loc = existing.Location()
	}
	if p.TimeZone.IsSpecified() && !p.TimeZone.IsNull() {
		loc = p.Location()
	}
	baseline := ProgramReportIntervalStart(now, interval, loc)
	p.ReportBaseline = &baseline
}
