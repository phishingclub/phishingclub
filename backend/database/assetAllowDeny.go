package database

import (
	"github.com/google/uuid"
)

const (
	ASSET_ALLOW_DENY_TABLE = "asset_allow_denies"
)

// AssetAllowDeny links an asset to an allow deny list that decides who can
// get the asset file
type AssetAllowDeny struct {
	AssetID     *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_asset_allow_denies;"`
	Asset       *Asset
	AllowDenyID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_asset_allow_denies;"`
	AllowDeny   *AllowDeny
}

func (AssetAllowDeny) TableName() string {
	return ASSET_ALLOW_DENY_TABLE
}
