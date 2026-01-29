package database

import (
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_EVENT_TABLE = "campaign_events"
)

// Campaign is gorm data model
type CampaignEvent struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;"`

	// arbitrary data
	Data string `gorm:"not null;"`

	// metadata stores browser fingerprinting data (ja4, platform, accept-language) as json
	Metadata string `gorm:"not null;default:''"`

	// has one
	CampaignID *uuid.UUID `gorm:"not null;type:uuid;"`
	EventID    *uuid.UUID `gorm:"not null;type:uuid;"`

	// can has one
	UserAgent string `gorm:";"`
	IPAddress string `gorm:";"`

	// AnonymizedID is set when the recipient has been anonymized
	AnonymizedID *uuid.UUID `gorm:"type:uuid;index;"`
	// if null either the event has no recipient or the recipient has been anonymized
	RecipientID *uuid.UUID `gorm:"index;type:uuid;"`
	Recipient   *Recipient

	CompanyID *uuid.UUID `gorm:"type:uuid;"`
}

// Migrate creates composite index and removes redundant single-column indexes
func (CampaignEvent) Migrate(db *gorm.DB) error {
	// create composite index for campaign_id + event_id (used heavily in GetResultStats)
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_campaign_events_campaign_event ON campaign_events(campaign_id, event_id)`).Error; err != nil {
		return err
	}

	// remove redundant single-column indexes that are covered by the composite index
	// ignore errors as indexes may not exist on fresh installs
	db.Exec(`DROP INDEX IF EXISTS idx_campaign_events_campaign_id`)
	db.Exec(`DROP INDEX IF EXISTS idx_campaign_events_event_id`)

	// remove unused company_id index (column is never populated)
	db.Exec(`DROP INDEX IF EXISTS idx_campaign_events_company_id`)

	return nil
}

// RecipientCampaignEvent is a aggregated read-only model
type RecipientCampaignEvent struct {
	CampaignEvent

	Name         string // event name
	CampaignName string
}

func (CampaignEvent) TableName() string {
	return CAMPAIGN_EVENT_TABLE
}

var _ = reflect.TypeOf(RecipientCampaignEvent{})
