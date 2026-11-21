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

	// the program_id lookups are served by the program and recipient index and
	// the program, status and due index, which both lead with program_id
	ProgramID *uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_program_enrollments_program_recipient"`
	Program   *Program
	// RecipientID is null once the recipient is deleted, the enrollment then
	// only counts, see UnlinkEnrollmentsOfRecipient. The unique index allows
	// many null rows.
	RecipientID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_program_enrollments_program_recipient"`
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
	// the status lookups are served by the status and due index
	Status      string     `gorm:"not null;default:'active'"`
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
	if err := ensureIndex(db, "idx_program_enrollments_status_due",
		`CREATE INDEX idx_program_enrollments_status_due
			ON program_enrollments (status, next_due_at)`); err != nil {
		return err
	}
	if err := migrateProgramEnrollmentReadIndex(db); err != nil {
		return err
	}
	return migrateProgramEnrollmentPseudonymIndex(db)
}

// migrateProgramEnrollmentPseudonymIndex adds the index the link of an
// unlinked anonymous program send reads: the enrollment of a program that
// carries a pseudonym is found without reading the other enrollments.
func migrateProgramEnrollmentPseudonymIndex(db *gorm.DB) error {
	return ensureIndex(db, "idx_program_enrollments_program_pseudonym",
		`CREATE INDEX idx_program_enrollments_program_pseudonym
			ON program_enrollments (program_id, anonymized_id)`)
}

// migrateProgramEnrollmentReadIndex adds the index the program page counts and
// the program list read: the enrollments of a program by status and the
// earliest next send are answered from the index alone, without the table.
func migrateProgramEnrollmentReadIndex(db *gorm.DB) error {
	return ensureIndex(db, "idx_program_enrollments_program_status_due",
		`CREATE INDEX idx_program_enrollments_program_status_due
			ON program_enrollments (program_id, status, next_due_at)`)
}
