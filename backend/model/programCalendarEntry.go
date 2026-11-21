package model

import (
	"time"

	"github.com/google/uuid"
)

// ProgramCalendarEntry is a program shown on the dashboard calendar, from its
// start to its close or end, ongoing when it has neither. StartAt is when the
// program started, or its planned start while it has not.
type ProgramCalendarEntry struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	CompanyName string     `json:"companyName"`
	Status      string     `json:"status"`
	StartAt     time.Time  `json:"startAt"`
	EndAt       *time.Time `json:"endAt"`
	ClosedAt    *time.Time `json:"closedAt"`
	PausedAt    *time.Time `json:"pausedAt"`
	// PlannedStartAt is the start the program was given, the status reads it
	PlannedStartAt time.Time `json:"-"`
}

// SetStatus sets the status of the entry at the given time
func (e *ProgramCalendarEntry) SetStatus(now time.Time) {
	start := e.PlannedStartAt
	e.Status = ProgramStatusOf(e.ClosedAt != nil, e.PausedAt != nil, &start, now)
}
