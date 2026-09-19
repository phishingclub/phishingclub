package database

import (
	"github.com/google/uuid"
)

const (
	CAMPAIGN_SCRIPT_TABLE = "campaign_scripts"
)

// CampaignScript is a gorm data model.
// It is a junction table for the campaign to script many to many relationship
// and stores the per script configuration (events and data level).
// It mirrors CampaignWebhook so scripts are configured on a campaign the
// same way webhooks are.
type CampaignScript struct {
	CampaignID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_script;primaryKey;"`
	Campaign   *Campaign

	ScriptID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_script;primaryKey;"`
	Script   *Script

	// scriptincludedata is the data level handed to the script.
	// values: "none", "basic", "full"
	ScriptIncludeData string `gorm:"not null;default:'full'"`

	// scriptevents is a binary format storing selected events as bits.
	// 0 = all events (default). Uses the same bit map as webhooks
	// (data.WebhookEventToBit) so the two features subscribe identically.
	ScriptEvents int `gorm:"not null;default:0"`
}

func (CampaignScript) TableName() string {
	return CAMPAIGN_SCRIPT_TABLE
}
