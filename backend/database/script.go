package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	SCRIPT_TABLE = "scripts"
)

// Script is a gorm data model for scripts.
// An script holds a JavaScript program that runs when a subscribed
// campaign event fires. It is the scripting counterpart to a webhook.
type Script struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex:idx_scripts_unique_name_and_company_id;type:uuid"`
	Name      string     `gorm:"not null;uniqueIndex:idx_scripts_unique_name_and_company_id;"`
	Script    string     `gorm:"not null;"`
}

func (e *Script) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, SCRIPT_TABLE)
}

func (Script) TableName() string {
	return SCRIPT_TABLE
}
