package database

import (
	"github.com/google/uuid"
)

// CampaignWebhook is gorm data model
// is a junction table for campaign-webhook many-to-many relationship
// stores per-webhook configuration (events and data level)
type CampaignWebhook struct {
	CampaignID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_webhook;primaryKey;"`
	Campaign   *Campaign

	WebhookID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_campaign_webhook;primaryKey;"`
	Webhook   *Webhook

	// webhookincludedata is the data level to include in webhook payload
	// values: "none", "basic", "full"
	WebhookIncludeData string `gorm:"not null;default:'full'"`

	// webhookevents is a binary format storing selected events as bits (10 events)
	// 0 = all events (default, backward compatible)
	// non-zero = only selected events trigger webhooks
	// bit 0 (1): campaign_closed
	// bit 1 (2): campaign_recipient_message_sent
	// bit 2 (4): campaign_recipient_message_failed
	// bit 3 (8): campaign_recipient_message_read
	// bit 4 (16): campaign_recipient_submitted_data
	// bit 5 (32): campaign_recipient_evasion_page_visited
	// bit 6 (64): campaign_recipient_before_page_visited
	// bit 7 (128): campaign_recipient_page_visited
	// bit 8 (256): campaign_recipient_after_page_visited
	// bit 9 (512): campaign_recipient_deny_page_visited
	WebhookEvents int `gorm:"not null;default:0"`
}

func (CampaignWebhook) TableName() string {
	return "campaign_webhooks"
}
