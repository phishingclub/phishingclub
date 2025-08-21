package database

import (
	"github.com/google/uuid"
)

// CampaignRecipientGroup is gorm data model
// is a table of those recipient groups that belong to a campaign
type CampaignRecipientGroup struct {
	CampaignID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_recipient_group;"`
	Campaign   *Campaign

	RecipientGroupID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_recipient_group;"`
	RecipientGroup   *RecipientGroup
}

func (CampaignRecipientGroup) TableName() string {
	return "campaign_recipient_groups"
}
