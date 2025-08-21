package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	WEBHOOK_TABLE = "webhooks"
)

// Webhook is a gorm data model for webhooks
type Webhook struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex:idx_webhooks_unique_name_and_company_id;type:uuid"`
	Name      string     `gorm:"not null;uniqueIndex:idx_webhooks_unique_name_and_company_id;"`
	URL       string     `gorm:"not null;"`
	Secret    string
}

func (e *Webhook) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "webhooks")
}

func (Webhook) TableName() string {
	return WEBHOOK_TABLE
}
