package database

import (
	"reflect"
	"time"

	"github.com/google/uuid"
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
	CampaignID *uuid.UUID `gorm:"not null;index;type:uuid;"`
	EventID    *uuid.UUID `gorm:"not null;index;type:uuid;"`

	// can has one
	UserAgent string `gorm:";"`
	IPAddress string `gorm:";"`

	// AnonymizedID is set when the recipient has been anonymized
	AnonymizedID *uuid.UUID `gorm:"type:uuid;index;"`
	// if null either the event has no recipient or the recipient has been anonymized
	RecipientID *uuid.UUID `gorm:"index;type:uuid;"`
	Recipient   *Recipient

	CompanyID *uuid.UUID `gorm:"index;type:uuid;index;"`
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
