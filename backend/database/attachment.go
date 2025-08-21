package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	ATTACHMENT_TABLE = "attachments"
)

// Attachment is gorm data model
type Attachment struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	// can has one
	CompanyID *uuid.UUID `gorm:"index;type:uuid;"`

	// many to many
	Mails []Email `gorm:"many2many:message_attachments;"`

	Name            string `gorm:";index"`
	Description     string `gorm:";"`
	Filename        string `gorm:"not null;index"`
	EmbeddedContent bool   `gorm:"not null;default:false;index"`
}

func (Attachment) TableName() string {
	return ATTACHMENT_TABLE
}
