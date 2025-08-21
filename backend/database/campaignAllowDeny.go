package database

import (
	"github.com/google/uuid"
)

const (
	CAMPAIGN_ALLOW_DENY_TABLE = "campaign_allow_denies"
)

// CampaignAllowDeny is a gorm data model
// is a table of those allow deny lists that belong to a campaign
type CampaignAllowDeny struct {
	CampaignID  *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_allow_denies;"`
	Campaign    *Campaign
	AllowDenyID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_allow_denies;"`
	AllowDeny   *AllowDeny
}

func (CampaignAllowDeny) TableName() string {
	return CAMPAIGN_ALLOW_DENY_TABLE
}
