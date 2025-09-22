package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_TEMPLATE_TABLE = "campaign_templates"
)

// CampaignTemplate is gorm data model
type CampaignTemplate struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Name string `gorm:"not null;index;uniqueIndex:idx_campaign_templates_unique_name_and_company_id;"`

	URLPath string `gorm:"not null;default:'';index"`

	// IsUsable indicates if a template is usable based on if it has all the required
	// data such as domainID, landingPage and etc to be used in a campaign
	IsUsable bool `gorm:"not null;default:false;index"`

	// has-a
	LandingPageID *uuid.UUID `gorm:"type:uuid;index;"`
	LandingPage   *Page      `gorm:"references:LandingPage;foreignKey:LandingPageID;references:ID;"`

	// landing page can also be a proxy
	LandingProxyID *uuid.UUID `gorm:"type:uuid;index;"`
	LandingProxy   *Proxy      `gorm:"foreignKey:LandingProxyID;references:ID;"`

	DomainID *uuid.UUID `gorm:"type:uuid;index;"`
	Domain   *Domain    `gorm:"foreignKey:DomainID"`

	URLIdentifierID *uuid.UUID  `gorm:"not null;type:uuid;index"`
	URLIdentifier   *Identifier `gorm:"references:foreignKey:URLIdentifierID;references:ID"`

	StateIdentifierID *uuid.UUID  `gorm:"type:uuid;index"`
	StateIdentifier   *Identifier `gorm:"references:foreignKey:StateIdentifierID;references:ID"`

	// has-a optional
	BeforeLandingPageID *uuid.UUID `gorm:"type:uuid;index"`
	BeforeLandingPage   *Page      `gorm:"foreignkey:BeforeLandingPageID;references:ID"`

	// before landing page can also be a proxy
	BeforeLandingProxyID *uuid.UUID `gorm:"type:uuid;index"`
	BeforeLandingProxy   *Proxy      `gorm:"foreignKey:BeforeLandingProxyID;references:ID"`

	AfterLandingPageID *uuid.UUID `gorm:"type:uuid;index"`
	AfterLandingPage   *Page      `gorm:"foreignKey:AfterLandingPageID;references:ID"`

	// after landing page can also be a proxy
	AfterLandingProxyID *uuid.UUID `gorm:"type:uuid;index"`
	AfterLandingProxy   *Proxy      `gorm:"foreignKey:AfterLandingProxyID;references:ID"`

	AfterLandingPageRedirectURL string `gorm:"not null;"`

	EmailID *uuid.UUID `gorm:"type:uuid;index;"`
	Email   *Email     `gorm:"foreignKey:EmailID;references:ID;"`

	SMTPConfigurationID *uuid.UUID         `gorm:"type:uuid;index;"`
	SMTPConfiguration   *SMTPConfiguration `gorm:"foreignKey:SMTPConfigurationID"`

	APISenderID *uuid.UUID `gorm:"type:uuid;index;"`
	APISender   *APISender `gorm:"foreignKey:APISenderID"`

	// can belong-to
	CompanyID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_campaign_templates_unique_name_and_company_id"`
	Company   *Company   `gorm:"foreignKey:CompanyID"`
}

func (e *CampaignTemplate) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "campaign_templates")
}

func (CampaignTemplate) TableName() string {
	return CAMPAIGN_TEMPLATE_TABLE
}
