package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	ALLOW_DENY_TABLE = "allow_denies"
)

// AllowDeny is a gorm data model for allow deny listing
type AllowDeny struct {
	ID              *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt       *time.Time `gorm:"not null;index;"`
	UpdatedAt       *time.Time `gorm:"not null;index"`
	CompanyID       *uuid.UUID `gorm:"uniqueIndex:idx_allow_denies_unique_name_and_company_id;type:uuid"`
	Name            string     `gorm:"not null;uniqueIndex:idx_allow_denies_unique_name_and_company_id;"`
	Cidrs           string     `gorm:"not null;default:''"`
	JA4Fingerprints string     `gorm:"not null;default:''"`
	Allowed         bool       `gorm:"not null;"`
}

func (AllowDeny) TableName() string {
	return ALLOW_DENY_TABLE
}

func (e *AllowDeny) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "allow_denies")
}
