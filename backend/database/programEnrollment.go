package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PROGRAM_ENROLLMENT_TABLE = "program_enrollments"
)

// ProgramEnrollment holds one recipient's journey through a program. There is at
// most one enrollment per recipient per program, which dedupes a recipient who
// is a member of several assigned groups.
type ProgramEnrollment struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	ProgramID   *uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_program_enrollments_program_recipient"`
	Program     *Program
	RecipientID *uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_program_enrollments_program_recipient"`
	Recipient   *Recipient

	// Position is how many steps the recipient has been sent in the current loop.
	Position int `gorm:"not null;default:0"`
	// LoopCount counts how many times the recipient has completed the sequence.
	// It also keys which hidden campaign a step send lands in, so a later loop
	// uses a fresh campaign and never collides with an earlier send.
	LoopCount int `gorm:"not null;default:0"`
	// RoundStartAt is the start of the recipient's current round. The next round
	// starts one step interval later, whatever time the send landed on.
	RoundStartAt *time.Time `gorm:"index"`

	// NextDueAt is when the scheduler should schedule this recipient's next step.
	// Null means nothing is due, for example a completed recipient.
	NextDueAt *time.Time `gorm:"index"`

	// Status is 'active', 'exited', 'completed' or 'stopped'. Stopped is a
	// person who was still active when the program closed.
	Status      string     `gorm:"not null;default:'active';index"`
	ExitedAt    *time.Time `gorm:"index"`
	CompletedAt *time.Time `gorm:"index"`

	// MissingSince is set when the recipient is no longer found in the program
	// groups. The enrollment is exited only after a grace period, so a short
	// group glitch does not cancel sends.
	MissingSince *time.Time `gorm:"index"`

	// LastSendAt is when the recipient's last scheduled step was planned to go
	// out, used to keep a minimum gap to the next one.
	LastSendAt *time.Time
	// LastStepID is the scheduled step sent last, so a new loop in shuffle mode
	// does not start with the same template.
	LastStepID *uuid.UUID `gorm:"type:uuid;"`

	// AnonymizedID is the pseudonym used for this person in every hidden
	// campaign of an anonymous program, so results count each person once. It is
	// cleared when the program closes, so closed results cannot be linked back.
	AnonymizedID *uuid.UUID `gorm:"type:uuid;"`

	// AdvanceFailures counts the scheduler attempts in a row that failed for
	// this person. It sets how long the next attempt waits and is reset to zero
	// after an attempt that works.
	AdvanceFailures int `gorm:"not null;default:0"`
}

func (ProgramEnrollment) TableName() string {
	return PROGRAM_ENROLLMENT_TABLE
}

// Migrate adds the composite index the scheduler due scan reads every tick, so
// it can seek the active rows and range over next_due_at already ordered.
func (ProgramEnrollment) Migrate(db *gorm.DB) error {
	return ensureIndex(db, "idx_program_enrollments_status_due",
		`CREATE INDEX IF NOT EXISTS idx_program_enrollments_status_due
			ON program_enrollments (status, next_due_at)`)
}
