package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	REMOTE_BROWSER_TABLE = "remote_browsers"
)

// RemoteBrowser is a gorm data model for saved remote browser scripts.
type RemoteBrowser struct {
	ID          *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt   *time.Time `gorm:"not null;index;"`
	UpdatedAt   *time.Time `gorm:"not null;index"`
	CompanyID   *uuid.UUID `gorm:"index;uniqueIndex:idx_remote_browsers_unique_name_and_company_id;type:uuid"`
	Name        string     `gorm:"not null;index;uniqueIndex:idx_remote_browsers_unique_name_and_company_id;"`
	Description string     `gorm:"type:text"`
	Script      string     `gorm:"type:text;not null;"`
	Config      string     `gorm:"type:text;not null;default:'{}'"`

	Company *Company
}

func (e *RemoteBrowser) Migrate(db *gorm.DB) error {
	return UniqueIndexNameAndNullCompanyID(db, "remote_browsers")
}

func (RemoteBrowser) TableName() string {
	return REMOTE_BROWSER_TABLE
}
