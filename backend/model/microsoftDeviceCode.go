package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

// MicrosoftDeviceCode is a microsoft device code flow entry
type MicrosoftDeviceCode struct {
	ID        nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt *time.Time                   `json:"createdAt"`
	UpdatedAt *time.Time                   `json:"updatedAt"`

	DeviceCode      string     `json:"deviceCode"`
	UserCode        string     `json:"userCode"`
	VerificationURI string     `json:"verificationUri"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	LastPolledAt    *time.Time `json:"lastPolledAt"`

	Resource string `json:"resource"`
	ClientID string `json:"clientId"`
	TenantID string `json:"tenantId"`
	Scope    string `json:"scope"`

	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	IDToken      string `json:"idToken"`

	Captured bool `json:"captured"`

	CampaignID  nullable.Nullable[uuid.UUID] `json:"campaignId"`
	RecipientID nullable.Nullable[uuid.UUID] `json:"recipientId"`
}

// IsExpired returns true if the device code has expired or has no expiry set
func (d *MicrosoftDeviceCode) IsExpired() bool {
	if d.ExpiresAt == nil {
		return true
	}
	return time.Now().UTC().After(*d.ExpiresAt)
}

// ExpiresWithin returns true if the device code expires within the given duration
func (d *MicrosoftDeviceCode) ExpiresWithin(duration time.Duration) bool {
	if d.ExpiresAt == nil {
		return true
	}
	return time.Until(*d.ExpiresAt) < duration
}
