package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	CAMPAIGN_TEMPLATE_LANGUAGE_TABLE = "campaign_template_languages"
)

// CampaignTemplateLanguage is one more language of a campaign template. It
// holds the email and the pages of that language. Every other setting, the
// proxy stages included, stays on the template. A row only fills the stages
// the template has as pages.
type CampaignTemplateLanguage struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;"`
	UpdatedAt *time.Time `gorm:"not null;"`

	CampaignTemplateID *uuid.UUID `gorm:"not null;type:uuid;index;uniqueIndex:idx_campaign_template_languages_template_language"`
	// Language is a code of data.Languages
	Language string `gorm:"not null;type:varchar(16);uniqueIndex:idx_campaign_template_languages_template_language"`

	EmailID             *uuid.UUID `gorm:"type:uuid;index"`
	BeforeLandingPageID *uuid.UUID `gorm:"type:uuid;index"`
	LandingPageID       *uuid.UUID `gorm:"type:uuid;index"`
	AfterLandingPageID  *uuid.UUID `gorm:"type:uuid;index"`

	// IsUsable is true when the row has an email and every stage the template
	// has as a page
	IsUsable bool `gorm:"not null;default:false"`
}

func (CampaignTemplateLanguage) TableName() string {
	return CAMPAIGN_TEMPLATE_LANGUAGE_TABLE
}
