package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	OAUTH_PROVIDER_TABLE = "oauth_providers"
)

// OAuthProvider is the gorm data model for oauth providers
type OAuthProvider struct {
	ID        uuid.UUID  `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Name string `gorm:"not null;uniqueIndex:idx_oauth_providers_unique_name_and_company_id;"`

	// oauth endpoints (user configurable)
	AuthURL  string `gorm:"not null;type:varchar(512);"`
	TokenURL string `gorm:"not null;type:varchar(512);"`
	Scopes   string `gorm:"not null;type:varchar(2048);"`

	// user's oauth app credentials (stored as plain text like smtp passwords)
	ClientID     string `gorm:"not null;type:varchar(255);"`
	ClientSecret string `gorm:"not null;type:varchar(255);"`

	// current token state (stored as plain text)
	AccessToken    string     `gorm:"type:varchar(4096);"`
	RefreshToken   string     `gorm:"type:varchar(4096);"`
	TokenExpiresAt *time.Time `gorm:"index;"`

	// authorization metadata
	AuthorizedEmail string     `gorm:"type:varchar(255);"`
	AuthorizedAt    *time.Time `gorm:"index;"`

	// status
	IsAuthorized bool `gorm:"not null;default:false;"`

	// indicates if this provider was created via import (with pre-authorized tokens)
	// imported providers cannot be authorized/reauthorized via oauth flow
	IsImported bool `gorm:"not null;default:false;"`

	// can belong-to
	CompanyID *uuid.UUID `gorm:"uniqueIndex:idx_oauth_providers_unique_name_and_company_id;"`
	Company   *Company   `gorm:"foreignkey:CompanyID;"`
}

func (o *OAuthProvider) Migrate(db *gorm.DB) error {
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "oauth_providers")
}

func (OAuthProvider) TableName() string {
	return OAUTH_PROVIDER_TABLE
}
