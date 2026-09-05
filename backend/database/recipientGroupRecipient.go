package database

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// Migrate adds an index leading with recipient_group_id. The existing unique index
// leads with recipient_id, so counting or listing a group's members
// (WHERE recipient_group_id = ?) could not use it and scanned the recipients per
// group, making the group list quadratic in recipients times groups.
func (RecipientGroupRecipient) Migrate(db *gorm.DB) error {
	return db.Exec(`CREATE INDEX IF NOT EXISTS idx_rgr_group ON recipient_group_recipients(recipient_group_id, recipient_id)`).Error
}
