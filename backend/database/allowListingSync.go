package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ALLOW_LISTING_TARGET_TABLE = "allow_listing_targets"
	ALLOW_LISTING_ENTRY_TABLE  = "allow_listing_entries"
	ALLOW_LISTING_PIN_TABLE    = "allow_listing_pins"
)

// AllowListingTarget is one Microsoft 365 tenant that a runner in the tenant
// keeps allow listed. A row with a NULL company_id is the global target and
// covers global campaigns and programs only. A company target covers only the
// campaigns and programs of that company. Targets never share anything.
type AllowListingTarget struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex;type:uuid"` // NULL is the global target

	Enabled bool `gorm:"not null;default:true"`
	// TokenHash is the SHA-256 hash of the runner token, TokenPrefix its first
	// characters so an admin can tell tokens apart
	TokenHash   string `gorm:"not null;"`
	TokenPrefix string `gorm:"not null;"`

	// LeadMinutes is how long before its start a campaign counts
	LeadMinutes int `gorm:"not null;default:1440"`
	// GraceMinutes is how long after its close a campaign counts, and how long
	// an owned entry stays after nothing needs it
	GraceMinutes int `gorm:"not null;default:1440"`
	// IntervalMinutes is how often the schedule in the tenant runs the
	// runner. How old a report may be follows from it.
	IntervalMinutes int `gorm:"not null;default:60"`
	// ReportGapMinutes is the time between the last two reports
	ReportGapMinutes int `gorm:"not null;default:0"`
	// RunnerDomain is the domain the runbook was set up to call
	RunnerDomain string `gorm:"not null;default:''"`

	// the last plan handed to the runner
	PlanID       *uuid.UUID `gorm:"type:uuid"`
	PlanIssuedAt *time.Time
	PlanJSON     string `gorm:"not null;default:'';type:text"`
	// RecentPlansJSON holds the adds and removes of the plans handed out in
	// the last hours, so a late report or one of runs that overlap still
	// records which values Phishing Club added. It holds at most 20 plans.
	RecentPlansJSON string `gorm:"not null;default:'';type:text"`

	LastPlanAt   *time.Time
	LastReportAt *time.Time
	LastError    string `gorm:"not null;default:'';type:text"`
	// LastNote says how the last report was read, such as tenant values
	// that could not be read. It is not a failure.
	LastNote     string `gorm:"not null;default:'';type:text"`
	FailureCount int    `gorm:"not null;default:0"`
	DriftCount   int    `gorm:"not null;default:0"`

	// DeleteRequestedAt is set when the target was deleted with its values
	// removed first. Nothing counts as needed any more, owned entries are
	// removed without the grace time and the target is deleted once they are
	// gone.
	DeleteRequestedAt *time.Time

	Company *Company `gorm:"foreignKey:CompanyID"`
}

func (e *AllowListingTarget) Migrate(db *gorm.DB) error {
	// at most one global target (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_allow_listing_targets_null_company_id ON allow_listing_targets ((company_id IS NULL)) WHERE (company_id IS NULL)`
	return db.Exec(idx).Error
}

func (AllowListingTarget) TableName() string {
	return ALLOW_LISTING_TARGET_TABLE
}

// AllowListingEntry is one value the runner last saw in the tenant. A row
// exists only while the value is in the tenant, so the table never holds more
// than the Microsoft limits allow per target.
type AllowListingEntry struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	TargetID  *uuid.UUID `gorm:"not null;type:uuid;index;uniqueIndex:idx_allow_listing_entries_target_kind_value"`
	// Kind is domain, ip or url
	Kind  string `gorm:"not null;type:varchar(16);uniqueIndex:idx_allow_listing_entries_target_kind_value"`
	Value string `gorm:"not null;type:varchar(300);uniqueIndex:idx_allow_listing_entries_target_kind_value"`
	// State is owned when Phishing Club added the value, tenant when it was
	// there already. A tenant entry is never removed.
	State      string     `gorm:"not null;type:varchar(16)"`
	AddedAt    *time.Time `gorm:"not null"`
	LastSeenAt *time.Time `gorm:"not null"`
	// UndesiredSince is when nothing needed an owned value any more. It is
	// cleared as soon as something needs it again.
	UndesiredSince *time.Time

	Target *AllowListingTarget `gorm:"foreignKey:TargetID"`
}

func (AllowListingEntry) TableName() string {
	return ALLOW_LISTING_ENTRY_TABLE
}

// AllowListingPin keeps a template or a single value allow listed without a
// campaign, such as for test sends
type AllowListingPin struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	TargetID  *uuid.UUID `gorm:"not null;type:uuid;index"`
	// either a template or a kind and value
	CampaignTemplateID *uuid.UUID `gorm:"type:uuid;index"`
	// CampaignTemplateName is kept so the pin still reads well once the
	// template is gone
	CampaignTemplateName string `gorm:"not null;default:'';type:varchar(255)"`
	Kind                 string `gorm:"not null;default:'';type:varchar(16)"`
	Value                string `gorm:"not null;default:'';type:varchar(300)"`
	Note                 string `gorm:"not null;default:'';type:varchar(255)"`
	// ExpiresAt is when the pin stops counting, nil never
	ExpiresAt *time.Time `gorm:"index"`

	// the template has no foreign key on purpose, a pin of a deleted template
	// simply stops adding values
	Target *AllowListingTarget `gorm:"foreignKey:TargetID"`
}

func (AllowListingPin) TableName() string {
	return ALLOW_LISTING_PIN_TABLE
}
