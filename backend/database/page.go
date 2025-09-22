package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PAGE_TABLE = "pages"
)

// Page is a gorm data model
type Page struct {
	ID         *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt  *time.Time `gorm:"not null;index;"`
	UpdatedAt  *time.Time `gorm:"not null;index"`
	CompanyID  *uuid.UUID `gorm:"index;uniqueIndex:idx_pages_unique_name_and_company_id;type:uuid"`
	Name       string     `gorm:"not null;index;uniqueIndex:idx_pages_unique_name_and_company_id;"`
	Content    string     `gorm:"not null;"`
	Type       string     `gorm:"not null;default:'regular';"`
	TargetURL  string
	ProxyConfig string

	// could has-one
	Company *Company
}

func (e *Page) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "pages")
}

func (Page) TableName() string {
	return PAGE_TABLE
}
