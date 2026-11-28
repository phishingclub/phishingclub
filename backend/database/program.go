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
	// ClosingAt marks a close that has stopped all sending but has not closed
	// every hidden campaign yet. The scheduler or a new close finishes it.
	ClosingAt *time.Time `gorm:"index;"`
	// DeletingAt marks a delete that has stopped all sending but has not
	// removed every hidden campaign yet. A new delete finishes it.
	DeletingAt *time.Time `gorm:"index;"`
	// AnonymizeAt is when a closed program has its people replaced with
	// pseudonyms in every result. AnonymizedAt is set once that is done for
	// every hidden campaign.
	AnonymizeAt  *time.Time `gorm:"index;"`
	AnonymizedAt *time.Time
	// AnonymizingAt marks an Anonymize All that has begun and not finished.
	// The anonymize task finishes it.
	AnonymizingAt *time.Time `gorm:"index;"`
	// the people counts by status taken when Anonymize All began. The
	// enrollments are removed by it, so the counts of the program are read
	// from here once it began.
	AnonymizedPeople    int `gorm:"not null;default:0"`
	AnonymizedCompleted int `gorm:"not null;default:0"`
	AnonymizedExited    int `gorm:"not null;default:0"`
	AnonymizedStopped   int `gorm:"not null;default:0"`
	// DataAnonymizeAt is when the submitted data, IP addresses, user agents
	// and browser metadata of the program are removed. DataAnonymizedAt is
	// set once that is done for every hidden campaign.
	DataAnonymizeAt  *time.Time `gorm:"index;"`
	DataAnonymizedAt *time.Time
	// DataAnonymizingAt marks an Anonymize Data run by hand that has not
	// finished, so the background pass runs it after a failure or a restart
	DataAnonymizingAt *time.Time
	// ReplanPendingAt marks an edit whose people are not all planned again
	// yet. It is set in the write of the edit and cleared once they are. The
	// membership pass plans them again while it is set, so an edit cut short
	// by a restart or an error is still finished.
	ReplanPendingAt *time.Time
	// ReplanOldEndAt is the end before the edit of ReplanPendingAt when the
	// edit moved the end later. The people waiting at it are drawn again.
	ReplanOldEndAt *time.Time

	// OrderMode is 'shuffle' or 'sequential'. Shuffle picks a random step the
	// recipient has not had in the current loop. Sequential takes the steps in
	// order for everyone.
	OrderMode string `gorm:"not null;default:'shuffle'"`
	// LoopMode is 'complete' or 'loop'. Loop starts a recipient over after the
	// last step, complete marks them done.
	LoopMode string `gorm:"not null;default:'complete'"`
	// TemplateMixBy is the recipient field templates are mixed by in shuffle
	// order: department, position or city. Empty is off. Sequential order
	// keeps the value but does not use it.
	TemplateMixBy string `gorm:"not null;default:''"`
	// LanguageRouteBy and LanguageDefault pick each person's language, see
	// Campaign. The values are mapped in program_language_routes.
	LanguageRouteBy string `gorm:"not null;type:varchar(32);default:''"`
	LanguageDefault string `gorm:"not null;type:varchar(16);default:''"`

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
	// SaveSubmittedData is copied to every hidden campaign the program creates.
	// It is never on for an anonymous program.
	SaveSubmittedData bool `gorm:"not null;default:false"`

	// ReportEnabled turns on the automated report email. ReportInterval is
	// monthly, quarterly or yearly and ReportPeriod is last (the interval that
	// just ended) or start (everything since the program started). The period
	// boundaries are read in TimeZone.
	ReportEnabled  bool   `gorm:"not null;default:false"`
	ReportInterval string `gorm:"not null;default:'monthly'"`
	ReportPeriod   string `gorm:"not null;default:'last'"`
	// ReportLastPeriodEnd is the end of the last period an automated report
	// was sent for, so a period is never sent twice
	ReportLastPeriodEnd *time.Time
	// ReportClaimedAt is set while a claimed report period has not been sent,
	// with ReportClaimPrevEnd the period end before the claim. A claim left
	// by a crash is given back after a while, so the period is not lost.
	ReportClaimedAt    *time.Time
	ReportClaimPrevEnd *time.Time

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
