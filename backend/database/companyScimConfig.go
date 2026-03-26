package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	COMPANY_SCIM_CONFIG_TABLE = "company_scim_configs"
)

// CompanyScimConfig holds the SCIM configuration for a company
type CompanyScimConfig struct {
	ID          *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt   *time.Time `gorm:"not null;index;"`
	UpdatedAt   *time.Time `gorm:"not null;index;"`
	CompanyID   *uuid.UUID `gorm:"not null;unique;type:uuid;index;"` // one-to-one with company
	TokenHash   string     `gorm:"not null;"`                        // bcrypt hash of the bearer token
	TokenPrefix string     `gorm:"not null;"`                        // first 8 chars of token for identification
	Enabled     bool       `gorm:"not null;default:true"`
	LastSyncAt  *time.Time // nullable: last time SCIM pushed an update

	Company *Company `gorm:"foreignKey:CompanyID"`
}

func (e *CompanyScimConfig) Migrate(db *gorm.DB) error {
	return nil
}

func (CompanyScimConfig) TableName() string {
	return COMPANY_SCIM_CONFIG_TABLE
}
