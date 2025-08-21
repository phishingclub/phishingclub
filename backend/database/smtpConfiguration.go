package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	SMTP_CONFIGURATION_TABLE = "smtp_configurations"
)

// SMTPConfiguration is a page gorm data model
// Simple Mail Transfer Protocol
type SMTPConfiguration struct {
	ID               uuid.UUID  `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt        *time.Time `gorm:"not null;index;"`
	UpdatedAt        *time.Time `gorm:"not null;index;"`
	Name             string     `gorm:"not null;uniqueIndex:idx_smtp_configurations_unique_name_and_company_id;"`
	Host             string     `gorm:"not null;"`
	Port             uint16     `gorm:"not null;"`
	Username         string     `gorm:"not null;"`
	Password         string     `gorm:"not null;"`
	IgnoreCertErrors bool       `gorm:"not null;"`

	// back-reference
	Headers []*SMTPHeader

	// can belong-to
	CompanyID *uuid.UUID `gorm:"uniqueIndex:idx_smtp_configurations_unique_name_and_company_id;"`
	Company   *Company   `gorm:"foreignkey:CompanyID;"`
}

func (e *SMTPConfiguration) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "smtp_configurations")
}

func (SMTPConfiguration) TableName() string {
	return SMTP_CONFIGURATION_TABLE
}
