package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	MICROSOFT_DEVICE_CODE_TABLE = "device_codes"
)

// MicrosoftDeviceCode stores a microsoft device code flow entry per campaign-recipient
type MicrosoftDeviceCode struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;"`

	// device code flow fields returned by microsoft
	DeviceCode      string     `gorm:"not null;"`
	UserCode        string     `gorm:"not null;"`
	VerificationURI string     `gorm:"not null;"`
	ExpiresAt       *time.Time `gorm:"not null;index;"`

	// last_polled_at is nil when the entry has never been polled
	LastPolledAt *time.Time `gorm:"index;"`

	// options used to create the device code
	Resource string `gorm:"not null;default:'https://graph.microsoft.com'"`
	ClientID string `gorm:"not null;"`
	TenantID string `gorm:"not null;default:'organizations'"`
	Scope    string `gorm:"not null;default:'https://graph.microsoft.com/.default openid profile offline_access'"`

	// captured access token / refresh token, set after successful poll
	AccessToken  string `gorm:"not null;default:''"`
	RefreshToken string `gorm:"not null;default:''"`
	IDToken      string `gorm:"not null;default:''"`

	// captured is true after successfully polling and getting a token
	Captured bool `gorm:"not null;default:false"`

	// foreign keys
	CampaignID  *uuid.UUID `gorm:"not null;type:uuid;index;"`
	RecipientID *uuid.UUID `gorm:"type:uuid;index;"`
}

func (MicrosoftDeviceCode) TableName() string {
	return MICROSOFT_DEVICE_CODE_TABLE
}

// Migrate runs extra migrations for device_codes
func (MicrosoftDeviceCode) Migrate(db *gorm.DB) error {
	// composite unique index: one active device code per campaign+recipient
	err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_microsoft_device_codes_campaign_recipient ON device_codes(campaign_id, recipient_id)`).Error
	if err != nil {
		return err
	}
	return nil
}
