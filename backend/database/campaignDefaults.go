package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_DEFAULTS_TABLE = "campaign_defaults"
)

// CampaignDefaults holds the values the new campaign form starts with.
// a row with a NULL company_id is the global default, a company row overrides
// it field by field. a NULL field is not set and is taken from the level above.
type CampaignDefaults struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex;type:uuid"` // NULL is the global default

	// days after the last send that the campaign closes
	CloseAfterDays *int
	// days after close that all data is anonymized
	AnonymizeAfterDays *int
	// days after close that the submitted data is anonymized
	DataAnonymizeAfterDays *int
	IsAnonymous            *bool
	SaveSubmittedData      *bool

	Company *Company `gorm:"foreignKey:CompanyID"`
}

func (e *CampaignDefaults) Migrate(db *gorm.DB) error {
	// at most one global row (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_campaign_defaults_null_company_id ON campaign_defaults ((company_id IS NULL)) WHERE (company_id IS NULL)`
	return db.Exec(idx).Error
}

func (CampaignDefaults) TableName() string {
	return CAMPAIGN_DEFAULTS_TABLE
}
