package model

import (
	"fmt"
	"time"

	"github.com/phishingclub/phishingclub/validate"
)

// ProgramLastMinuteOfDay is 23:59, the latest end a window can be given. A
// window that ends then runs to midnight.
const ProgramLastMinuteOfDay = 23*60 + 59

// programClockCheckDays is how many days ahead the window bounds are checked
// against clock changes. Zone rules repeat every year, two years cover them.
const programClockCheckDays = 2 * 366

// ProgramWallTime returns the instant of a wall clock time on a date in loc,
// and false when that wall time does not exist or exists twice there because
// of a clock change. Minute 0 is the start of the day and minute 1440 the end
// of the day, the first instant of the next day. Both always exist, also where
// a clock change skips midnight, so a window from midnight and a window to
// midnight are built the same way.
func ProgramWallTime(y int, mo time.Month, d int, minutes int, loc *time.Location) (time.Time, bool) {
	if minutes <= 0 {
		return programStartOfDay(y, mo, d, loc), true
	}
	if minutes >= 24*60 {
		return programStartOfDay(y, mo, d+1, loc), true
	}
	return exactWallTime(y, mo, d, minutes/60, minutes%60, loc)
}

// programStartOfDay returns the first instant of a date in loc. Where a clock
// change skips midnight that is the first wall time after the gap.
func programStartOfDay(y int, mo time.Month, d int, loc *time.Location) time.Time {
	date := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	y, mo, d = date.Date()
	if t, ok := exactWallTime(y, mo, d, 0, 0, loc); ok {
		return t
	}
	// midnight is in a gap or occurs twice: walk from 12 hours before by the
	// minute to the first instant whose local date is the day
	t := time.Date(y, mo, d, 0, 0, 0, 0, loc).Add(-12 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 36*60; i++ {
		ly, lmo, ld := t.In(loc).Date()
		if ly == y && lmo == mo && ld == d {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Date(y, mo, d, 0, 0, 0, 0, loc)
}

// HasWindow reports whether the program has a full delivery window
func (p *Program) HasWindow() bool {
	return p.ConstraintWeekDays.IsSpecified() && !p.ConstraintWeekDays.IsNull() &&
		p.ConstraintStartTime.IsSpecified() && !p.ConstraintStartTime.IsNull() &&
		p.ConstraintEndTime.IsSpecified() && !p.ConstraintEndTime.IsNull()
}

// WindowMinutes returns the start and end of the delivery window in minutes
// after midnight. An end of 23:59 is 1440, the end of the day.
func (p *Program) WindowMinutes() (int, int) {
	start := int(p.ConstraintStartTime.MustGet().Minutes() / time.Minute)
	end := int(p.ConstraintEndTime.MustGet().Minutes() / time.Minute)
	if end == ProgramLastMinuteOfDay {
		end = 24 * 60
	}
	return start, end
}

// validateWindowClock rejects a delivery window whose start or end does not
// exist or exists twice on a sending day in the program zone, the same rule
// Daily Slots has, as the window on that day would be a guess. The days from
// now and programClockCheckDays on are checked.
func (p *Program) validateWindowClock(now time.Time) error {
	if !p.HasWindow() {
		return nil
	}
	loc := p.Location()
	startMinutes, endMinutes := p.WindowMinutes()
	allowed := map[time.Weekday]bool{}
	for _, d := range p.ConstraintWeekDays.MustGet().AsSlice() {
		allowed[time.Weekday(d)] = true
	}
	y, mo, d := now.In(loc).Date()
	// dates are walked in UTC, which has no clock changes
	first := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	for i := 0; i < programClockCheckDays; i++ {
		day := first.AddDate(0, 0, i)
		if !allowed[day.Weekday()] {
			continue
		}
		dy, dmo, dd := day.Date()
		_, okStart := ProgramWallTime(dy, dmo, dd, startMinutes, loc)
		_, okEnd := ProgramWallTime(dy, dmo, dd, endMinutes, loc)
		if !okStart || !okEnd {
			return validate.WrapErrorWithField(
				fmt.Errorf(
					"the daily hours %s to %s do not exist or occur twice on %s in %s because of a clock change",
					p.ConstraintStartTime.MustGet().String(), p.ConstraintEndTime.MustGet().String(),
					day.Format(ProgramDateLayout), loc.String(),
				),
				"constraintStartTime",
			)
		}
	}
	return nil
}
