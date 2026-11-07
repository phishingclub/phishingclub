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
)

// ProgramBlackoutMax is the most blackout ranges a program may have
const ProgramBlackoutMax = 50

// ProgramDateLayout is the date only format of program blackout ranges and
// report periods
const ProgramDateLayout = "2006-01-02"

// ProgramBlackout is a range of days, both ends included, on which a program
// sends nothing. The dates are read in the zone the delivery window uses.
type ProgramBlackout struct {
	From string `json:"from"`
	To   string `json:"to"`
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
	}
	return nil
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
		// the layout sorts the same as the dates, so a text compare is enough
		if date >= r.From && date <= r.To {
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

// StatusAt returns the status of the program at the given time
func (p *Program) StatusAt(now time.Time) string {
	switch {
	case p.IsClosed():
		return ProgramStatusClosed
	case p.IsPaused():
		return ProgramStatusPaused
	}
	start := p.StartAtTime()
	if start == nil {
		return ProgramStatusDraft
	}
	if start.After(now) {
		return ProgramStatusScheduled
	}
	return ProgramStatusActive
}
