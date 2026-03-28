package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	RECIPIENT_GROUP_TABLE = "recipient_groups"
)

// RecipientGroup is a grouping of recipients
type RecipientGroup struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Name string `gorm:"not null;index;uniqueIndex:idx_recipient_groups_unique_name_and_company_id;"`

	// IsDynamic indicates that members are resolved at query time via FilterField/FilterValue
	IsDynamic bool `gorm:"not null;default:false;"`
	// FilterField is the recipient attribute to filter on (position, department, city, country, misc)
	FilterField string `gorm:"not null;default:'';"`
	// FilterValue is the value to match against the filter field
	FilterValue string `gorm:"not null;default:'';"`

	// can belong-to
	CompanyID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_recipient_groups_unique_name_and_company_id"`
	Company   *Company

	// many-to-many (only used when IsDynamic is false)
	Recipients []Recipient `gorm:"many2many:recipient_group_recipients;"`
}

func (e *RecipientGroup) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "recipient_groups")
}

func (RecipientGroup) TableName() string {
	return RECIPIENT_GROUP_TABLE
}
