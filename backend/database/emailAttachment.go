package database

import (
	"github.com/google/uuid"
)

// EmailAttachment is a gorm data model
// it is a many to many relationship between messages and attachments
type EmailAttachment struct {
	EmailID      *uuid.UUID `gorm:"primary_key;not null;index;type:uuid;unique_index:idx_message_attachment;"`
	AttachmentID *uuid.UUID `gorm:"primary_key;not null;index;type:uuid;unique_index:idx_message_attachment;"`
	IsInline     bool       `gorm:"not null;default:false;"`
}

func (EmailAttachment) TableName() string {
	return "email_attachments"
}
