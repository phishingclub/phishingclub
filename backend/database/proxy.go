package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PROXY_TABLE = "proxies"
)

// Proxy is a gorm data model
type Proxy struct {
	ID          *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt   *time.Time `gorm:"not null;index;"`
	UpdatedAt   *time.Time `gorm:"not null;index"`
	CompanyID   *uuid.UUID `gorm:"index;uniqueIndex:idx_proxies_unique_name_and_company_id;type:uuid"`
	Name        string     `gorm:"not null;index;uniqueIndex:idx_proxies_unique_name_and_company_id;"`
	Description string     `gorm:"type:text"`
	StartURL    string     `gorm:"not null;"`
	ProxyConfig string     `gorm:"type:text;not null;"`

	// could has-one
	Company *Company
}

func (e *Proxy) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "proxies")
}

func (Proxy) TableName() string {
	return PROXY_TABLE
}
