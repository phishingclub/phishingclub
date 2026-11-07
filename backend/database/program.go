package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PROGRAM_TABLE = "programs"
)

// Program is a continuous, self running sequence of phishing and training
// content. It enrolls recipients from its assigned groups on its own and walks
// each recipient through the ordered steps at their own pace. The actual
// sending is done by hidden, program managed campaigns created per step (see
// ProgramCampaign). A Program never sends mail directly.
type Program struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Name string `gorm:"not null;uniqueIndex:idx_programs_unique_name_and_company_id"`

	// can belong to a company
	CompanyID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_programs_unique_name_and_company_id"`
	Company   *Company

	// ClosedAt marks a program that has been archived and no longer runs.
	ClosedAt *time.Time `gorm:"index;"`
	// PausedAt marks a program that is temporarily halted. While paused the
	// scheduler adds no enrollments and advances no recipients, and step edits
	// are allowed.
	PausedAt *time.Time `gorm:"index;"`

	// OrderMode is 'shuffle' or 'sequential'. Shuffle picks a random step the
	// recipient has not had in the current loop. Sequential takes the steps in
	// order for everyone.
	OrderMode string `gorm:"not null;default:'shuffle'"`
	// LoopMode is 'complete' or 'loop'. Loop starts a recipient over after the
	// last step, complete marks them done.
	LoopMode string `gorm:"not null;default:'complete'"`

	// StepIntervalMinutes is the time between the start of one recipient's
	// rounds, one step per round. The default is model.ProgramDefaultIntervalMinutes,
	// written out as a tag can not refer to a constant.
	StepIntervalMinutes int `gorm:"not null;default:20160"`
	// SpreadMinutes is how far into a round a recipient's send may land. Each
	// round draws a new random time in it, so colleagues are not sent at once.
	SpreadMinutes int `gorm:"not null;default:0"`
	// NewMemberDelayMinutes holds back the first send to someone who joins a
	// group after the program has started.
	NewMemberDelayMinutes int `gorm:"not null;default:0"`
	// StartedAt is set on the first enrollment pass. Members found then are the
	// starting audience and are not delayed.
	StartedAt *time.Time `gorm:"index;"`
	// StartAt is when the program may start. Null is a draft: nobody is
	// enrolled and nothing is sent. The scheduler runs a program only once
	// this time has passed.
	StartAt *time.Time `gorm:"index;"`
	// EndAt is when the program closes on its own. Null runs until it is
	// closed. No send is planned at or after this time.
	EndAt *time.Time `gorm:"index;"`
	// BlackoutDates is a JSON list of {from, to} dates (YYYY-MM-DD) read in the
	// zone the window is read in. No send lands on these days.
	BlackoutDates *string `gorm:";"`
	// TimeZone is the IANA zone the delivery window is read in.
	TimeZone string `gorm:"not null;default:'UTC'"`
	// ConstraintWeekDays is the same weekday bitmask used by campaigns.
	// 0b00000001 = 1 = sunday
	// 0b00000010 = 2 = monday
	// 0b00000100 = 4 = tuesday
	ConstraintWeekDays *int `gorm:";"`
	// hh:mm
	ConstraintStartTime *string `gorm:"index;"`
	// hh:mm
	ConstraintEndTime *string `gorm:"index;"`

	// IsAnonymous is propagated to every hidden campaign the program creates.
	IsAnonymous bool `gorm:"not null;default:false"`

	// DenyPageID and EvasionPageID are copied to every hidden campaign, the
	// allow or deny lists and webhooks are in program_allow_denies and
	// program_webhooks
	DenyPageID    *uuid.UUID `gorm:"type:uuid;index;"`
	EvasionPageID *uuid.UUID `gorm:"type:uuid;index;"`

	// has many to many
	RecipientGroups []*RecipientGroup `gorm:"many2many:program_recipient_groups"`
}

func (p *Program) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + null company id is unique
	return UniqueIndexNameAndNullCompanyID(db, PROGRAM_TABLE)
}

func (Program) TableName() string {
	return PROGRAM_TABLE
}
