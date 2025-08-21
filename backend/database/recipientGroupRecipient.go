package database

import (
	"github.com/google/uuid"
)

const (
	RECIPIENT_GROUP_RECIPIENT_TABLE = "recipient_group_recipients"
)

// RecipientGroupRecipient is a grouping of recipients and recipient groups
type RecipientGroupRecipient struct {
	Recipient   *Recipient
	RecipientID *uuid.UUID `gorm:"not null;uniqueIndex:idx_recipient_group"`

	RecipientGroup   *RecipientGroup
	RecipientGroupID *uuid.UUID `gorm:"not null;uniqueIndex:idx_recipient_group"`
}

func (RecipientGroupRecipient) TableName() string {
	return RECIPIENT_GROUP_RECIPIENT_TABLE
}
