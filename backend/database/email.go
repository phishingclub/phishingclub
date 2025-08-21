package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	EMAIL_TABLE = "emails"
)

// Email is a gorm data model
type Email struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	Name      string     `gorm:"not null;index;uniqueIndex:idx_emails_name_company_id;"`
	Content   string     `gorm:"not null;"`

	AddTrackingPixel bool `gorm:"not null;"`

	// mail fields
	// Envelope header - Bounce / Return-Path
	MailFrom string `gorm:"not null;"`
	// Mail header
	Subject string `gorm:"not null;"`
	From    string `gorm:"not null;"`

	// many to many
	Attachments []*Attachment `gorm:"many2many:email_attachments;"`

	// can belong to
	CompanyID *uuid.UUID `gorm:"index;type:uuid;uniqueIndex:idx_emails_name_company_id;"`
	Company   *Company
}

func (e *Email) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + null company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "emails")
}

func (Email) TableName() string {
	return EMAIL_TABLE
}
