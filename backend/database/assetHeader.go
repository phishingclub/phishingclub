package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	ASSET_HEADER_TABLE = "asset_headers"
)

// AssetHeader is a response header sent when a specific asset is served
type AssetHeader struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	Key       string     `gorm:"not null;"`
	Value     string     `gorm:"not null;"`

	// belongs to
	AssetID *uuid.UUID `gorm:"index;not null;type:uuid"`
}

func (AssetHeader) TableName() string {
	return ASSET_HEADER_TABLE
}
