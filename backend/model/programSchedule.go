package model

import (
	"encoding/json"
	"time"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/validate"
)

// program statuses shown to the operator
const (
	ProgramStatusDraft     = "draft"
	ProgramStatusScheduled = "scheduled"
	ProgramStatusActive    = "active"
	ProgramStatusPaused    = "paused"
	ProgramStatusClosed    = "closed"
	// ProgramStatusNeedsAttention is an active program with a template that
	// cannot be sent, or a started one with nobody enrolled. It is a list
	// filter, the status of such a program reads active.
	ProgramStatusNeedsAttention = "needs_attention"
)

// ProgramListStatuses are the statuses the program list can be filtered by
var ProgramListStatuses = []string{
	ProgramStatusDraft,
	ProgramStatusScheduled,
	ProgramStatusActive,
	ProgramStatusPaused,
	ProgramStatusNeedsAttention,
	ProgramStatusClosed,
}

// ProgramBlackoutMax is the most blackout ranges a program may have
const ProgramBlackoutMax = 50

// ProgramDateLayout is the date only format of program blackout ranges and
// report periods
const ProgramDateLayout = "2006-01-02"

// ProgramBlackout is a range of days, both ends included, on which a program
// sends nothing. The dates are read in the zone the delivery window uses. A
// yearly range repeats every year: only the month and day of From and To
// count, and when To is in the year after From the range runs over the new
// year, for example Dec 24 to Jan 1.
type ProgramBlackout struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Yearly bool   `json:"yearly,omitempty"`
}

// Contains reports whether a date (YYYY-MM-DD) falls in the range
func (r ProgramBlackout) Contains(date string) bool {
	// the layout sorts the same as the dates, so a text compare is enough
	if !r.Yearly {
		return date >= r.From && date <= r.To
	}
	if len(date) != len(ProgramDateLayout) || len(r.From) != len(ProgramDateLayout) || len(r.To) != len(ProgramDateLayout) {
		return false
	}
	// MM-DD sorts the same as the days of a year
	day, from, to := date[5:], r.From[5:], r.To[5:]
	if r.From[:4] == r.To[:4] {
		return day >= from && day <= to
	}
	// the range runs over the new year
	return day >= from || day <= to
}

// leavesADay reports whether a yearly range leaves a day free in a year
// without Feb 29. A range from Mar 1 to Feb 28 of the next year is shorter
// than a year when that year is a leap year, but covers every day of the
// other years. A year with Feb 29 has every day of the other years too, so a
// free day there is free in every year.
func (r ProgramBlackout) leavesADay() bool {
	day := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	for day.Year() == 2023 {
		if !r.Contains(day.Format(ProgramDateLayout)) {
			return true
		}
		day = day.AddDate(0, 0, 1)
	}
	return false
}

// ProgramSendDayLookAheadDays is how far ahead the scheduler looks for a day
// to send on, the look ahead of the next window opening in the service
const ProgramSendDayLookAheadDays = 400 * (ProgramBlackoutMax + 5)

// errNoSendDay refuses skip ranges that, with the sending days, leave no day
// to send on
var errNoSendDay = errors.New("The skip dates and sending days leave no day to send on. Remove a skip range or add a sending day.")

// hasSendDay reports whether a day from the date of now in the program zone
// on, within ProgramSendDayLookAheadDays, is a sending day of the window and
// outside every skip range. Without a window every day is a sending day.
func (p *Program) hasSendDay(now time.Time) bool {
	if !p.HasBlackouts() {
		return !p.HasWindow() || len(p.ConstraintWeekDays.MustGet().AsSlice()) > 0
	}
	allowed := map[time.Weekday]bool{}
	if p.HasWindow() {
		for _, d := range p.ConstraintWeekDays.MustGet().AsSlice() {
			allowed[time.Weekday(d)] = true
		}
	} else {
		for d := time.Sunday; d <= time.Saturday; d++ {
			allowed[d] = true
		}
	}
	y, mo, d := now.In(p.Location()).Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	for i := 0; i < ProgramSendDayLookAheadDays; i++ {
		if allowed[day.Weekday()] && !p.IsBlackoutDay(day.Format(ProgramDateLayout)) {
			return true
		}
		day = day.AddDate(0, 0, 1)
	}
	return false
}

// validateSendDay refuses skip ranges and sending days that together leave no
// day to send on, see hasSendDay
func (p *Program) validateSendDay(now time.Time) error {
	if !p.hasSendDay(now) {
		return validate.WrapErrorWithField(errNoSendDay, "blackoutDates")
	}
	return nil
}

// validateSchedule checks the start and blackout fields
func (p *Program) validateSchedule() error {
	if !p.BlackoutDates.IsSpecified() || p.BlackoutDates.IsNull() {
		return nil
	}
	ranges := p.BlackoutDates.MustGet()
	if len(ranges) > ProgramBlackoutMax {
		return validate.WrapErrorWithField(errors.Errorf("At most %d skip date ranges are allowed", ProgramBlackoutMax), "blackoutDates")
	}
	for _, r := range ranges {
		from, err := time.Parse(ProgramDateLayout, r.From)
		if err != nil {
			return validate.WrapErrorWithField(errors.New("Skip dates must be YYYY-MM-DD"), "blackoutDates")
		}
		to, err := time.Parse(ProgramDateLayout, r.To)
		if err != nil {
			return validate.WrapErrorWithField(errors.New("Skip dates must be YYYY-MM-DD"), "blackoutDates")
		}
		if to.Before(from) {
			return validate.WrapErrorWithField(errors.New("The last skip day must be on or after the first"), "blackoutDates")
		}
		// a yearly range must leave at least one day of the year to send on
		if r.Yearly && (!to.Before(from.AddDate(1, 0, -1)) || !r.leavesADay()) {
			return validate.WrapErrorWithField(errors.New("A yearly skip range must be shorter than a year"), "blackoutDates")
		}
		// a one off range covers at most a year, so the schedule can always
		// find the end of the ranges
		if !r.Yearly && !to.Before(from.AddDate(1, 0, 0)) {
			return validate.WrapErrorWithField(errors.New("A skip range can be at most a year"), "blackoutDates")
		}
	}
	return p.validateSendDay(time.Now())
}

// scheduleToDBMap adds the start and blackout fields to a column map
func (p *Program) scheduleToDBMap(m map[string]any) {
	if p.StartAt.IsSpecified() {
		if p.StartAt.IsNull() {
			m["start_at"] = nil
		} else {
			m["start_at"] = p.StartAt.MustGet().UTC()
		}
	}
	if p.EndAt.IsSpecified() {
		if p.EndAt.IsNull() {
			m["end_at"] = nil
		} else {
			m["end_at"] = p.EndAt.MustGet().UTC()
		}
	}
	if p.BlackoutDates.IsSpecified() {
		if p.BlackoutDates.IsNull() || len(p.BlackoutDates.MustGet()) == 0 {
			m["blackout_dates"] = nil
		} else if b, err := json.Marshal(p.BlackoutDates.MustGet()); err == nil {
			m["blackout_dates"] = string(b)
		}
	}
}

// ParseProgramBlackouts reads the stored blackout list, an empty list when the
// value is empty or broken
func ParseProgramBlackouts(raw *string) []ProgramBlackout {
	out := []ProgramBlackout{}
	if raw == nil || *raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		return []ProgramBlackout{}
	}
	return out
}

// IsBlackoutDay reports whether a date (YYYY-MM-DD) falls in a blackout range
func (p *Program) IsBlackoutDay(date string) bool {
	if !p.BlackoutDates.IsSpecified() || p.BlackoutDates.IsNull() {
		return false
	}
	for _, r := range p.BlackoutDates.MustGet() {
		if r.Contains(date) {
			return true
		}
	}
	return false
}

// HasBlackouts reports whether the program has any blackout range
func (p *Program) HasBlackouts() bool {
	return p.BlackoutDates.IsSpecified() && !p.BlackoutDates.IsNull() && len(p.BlackoutDates.MustGet()) > 0
}

// IsPaused returns true if the program is paused
func (p *Program) IsPaused() bool {
	return p.PausedAt.IsSpecified() && !p.PausedAt.IsNull()
}

// IsStopping reports whether a close or a delete has begun and not finished.
// Such a program sends nothing and only a close or a delete may change it.
func (p *Program) IsStopping() bool {
	return p.ClosingAt != nil || p.DeletingAt != nil
}

// StartAtTime returns the start time, nil for a draft
func (p *Program) StartAtTime() *time.Time {
	if p.StartAt.IsSpecified() && !p.StartAt.IsNull() {
		t := p.StartAt.MustGet()
		return &t
	}
	return nil
}

// EndAtTime returns the end time, nil when the program runs until closed
func (p *Program) EndAtTime() *time.Time {
	if p.EndAt.IsSpecified() && !p.EndAt.IsNull() {
		t := p.EndAt.MustGet()
		return &t
	}
	return nil
}

// IsEndedBy reports whether the program has reached its end date at the given
// time
func (p *Program) IsEndedBy(now time.Time) bool {
	end := p.EndAtTime()
	return end != nil && !now.Before(*end)
}

// ValidateEndAt checks the end date of a create or update request. start is
// the start the program will have, nil for a draft. changed is true when the
// end is new or differs from the stored one, only then must it be in the
// future.
func (p *Program) ValidateEndAt(start *time.Time, changed bool, now time.Time) error {
	end := p.EndAtTime()
	if end == nil {
		return nil
	}
	if changed && !end.After(now) {
		return validate.WrapErrorWithField(errors.New("The end must be in the future"), "endAt")
	}
	if start != nil && !end.After(*start) {
		return validate.WrapErrorWithField(errors.New("The end must be after the start"), "endAt")
	}
	return nil
}

// StatusAt returns the status of the program at the given time. A program
// that has not reached its start is a draft or scheduled also while paused,
// as the program list shows it.
func (p *Program) StatusAt(now time.Time) string {
	return ProgramStatusOf(p.IsClosed(), p.IsPaused(), p.StartAtTime(), now)
}

// ProgramStatusOf returns the status of a program from its stored state
func ProgramStatusOf(closed bool, paused bool, startAt *time.Time, now time.Time) string {
	switch {
	case closed:
		return ProgramStatusClosed
	case startAt == nil:
		return ProgramStatusDraft
	case startAt.After(now):
		return ProgramStatusScheduled
	case paused:
		return ProgramStatusPaused
	}
	return ProgramStatusActive
}
