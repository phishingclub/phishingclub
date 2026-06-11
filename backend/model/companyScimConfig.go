package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

// CompanyScimConfig is the SCIM configuration for a company
type CompanyScimConfig struct {
	ID          nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt   *time.Time                   `json:"createdAt"`
	UpdatedAt   *time.Time                   `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID] `json:"companyID"`
	TokenPrefix nullable.Nullable[string]    `json:"tokenPrefix"` // shown to user for identification
	Enabled     bool                         `json:"enabled"`
	LastSyncAt  *time.Time                   `json:"lastSyncAt"`
	// Token is only set when a new token has just been generated (never persisted directly)
	Token string `json:"token,omitempty"`
}

// Validate checks if the config is valid
func (c *CompanyScimConfig) Validate() error {
	return nil
}

// ToDBMap converts the fields for persistence
func (c *CompanyScimConfig) ToDBMap() map[string]any {
	m := map[string]any{}
	m["enabled"] = c.Enabled
	return m
}
