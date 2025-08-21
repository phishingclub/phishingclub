package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	CAMPAIGN_RECIPIENT_TABLE_NAME = "campaign_recipients"
)

// CampaigReciever is gorm data model
// this model/table is primarily used to keep track of who and when should recieve a campaign
type CampaignRecipient struct {
	ID *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`

	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Campaign   *Campaign
	CampaignID *uuid.UUID `gorm:"not null;type:uuid;uniqueIndex:idx_campaign_recipients_campaign_id_recipient_id;"`

	// CancelledAt *time.Time `gorm:"index;"`
	CancelledAt *time.Time `gorm:"index;"`

	// when it should be send
	SendAt *time.Time `gorm:"index;"`

	// when it was last attempted send
	LastAttemptAt *time.Time `gorm:"index;"`

	// when it was sent
	SentAt *time.Time `gorm:"index;"`

	// self-managed
	SelfManaged bool `gorm:"not null;default:false;"`

	// AnonymizedID is set when the recipient has been anonymized
	AnonymizedID *uuid.UUID `gorm:"type:uuid;"`
	Recipient    *Recipient
	// A null recipientID means that the data has been anonymized
	RecipientID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_campaign_recipients_campaign_id_recipient_id;"`

	// NotableEventID is the most notable event for this recipient
	NotableEvent   *Event     `gorm:"foreignKey:NotableEventID;references:ID"`
	NotableEventID *uuid.UUID `gorm:"type:uuid;index"`
}

func (CampaignRecipient) TableName() string {
	return CAMPAIGN_RECIPIENT_TABLE_NAME
}
