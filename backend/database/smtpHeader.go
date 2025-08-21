package database

import (
	"time"

	"github.com/google/uuid"
)

// SMTPHeader is headers sent with specific SMTP configurations
type SMTPHeader struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	Key       string     `gorm:"not null;"`
	Value     string     `gorm:"not null;"`

	// belongs to
	SMTPConfigurationID *uuid.UUID         `gorm:"index;not null;type:uuid"`
	SMTP                *SMTPConfiguration `gorm:"foreignKey:SMTPConfigurationID"`
}

func (SMTPHeader) TableName() string {
	return "smtp_headers"
}
