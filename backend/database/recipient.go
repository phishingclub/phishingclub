package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	RECIPIENT_TABLE = "recipients"
)

// Recipient is a gorm data model
type Recipient struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	DeletedAt *time.Time `gorm:"index;"`

	Email           *string `gorm:";uniqueIndex"`
	Phone           *string `gorm:";index"`
	ExtraIdentifier *string `gorm:";index"`

	FirstName  string `gorm:";"`
	LastName   string `gorm:";"`
	Position   string `gorm:";"`
	Department string `gorm:";"`
	City       string `gorm:";"`
	Country    string `gorm:";"`
	Misc       string `gorm:";"`

	// can belong to
	CompanyID *uuid.UUID `gorm:"type:uuid;index;"`
	Company   *Company

	// many-to-many
	Groups []RecipientGroup `gorm:"many2many:recipient_group_recipients;"`
}

func (Recipient) TableName() string {
	return RECIPIENT_TABLE
}
