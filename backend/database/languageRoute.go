package database

import (
	"github.com/google/uuid"
)

const (
	CAMPAIGN_LANGUAGE_ROUTE_TABLE = "campaign_language_routes"
	PROGRAM_LANGUAGE_ROUTE_TABLE  = "program_language_routes"
)

// CampaignLanguageRoute maps a recipient field value to a language for a
// campaign. MatchKey is the value after model.NormalizeLanguageKey, the raw
// value is not stored.
type CampaignLanguageRoute struct {
	CampaignID *uuid.UUID `gorm:"not null;type:uuid;primaryKey"`
	MatchKey   string     `gorm:"not null;type:varchar(127);primaryKey"`
	Language   string     `gorm:"not null;type:varchar(16)"`
}

func (CampaignLanguageRoute) TableName() string {
	return CAMPAIGN_LANGUAGE_ROUTE_TABLE
}

// ProgramLanguageRoute maps a recipient field value to a language for a
// program, see CampaignLanguageRoute
type ProgramLanguageRoute struct {
	ProgramID *uuid.UUID `gorm:"not null;type:uuid;primaryKey"`
	MatchKey  string     `gorm:"not null;type:varchar(127);primaryKey"`
	Language  string     `gorm:"not null;type:varchar(16)"`
}

func (ProgramLanguageRoute) TableName() string {
	return PROGRAM_LANGUAGE_ROUTE_TABLE
}
