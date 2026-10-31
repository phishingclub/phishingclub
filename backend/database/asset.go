package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	ASSET_TABLE = "assets"
)

// Asset is gorm data model
type Asset struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	// has one
	DomainID   *uuid.UUID `gorm:"index;index:idx_assets_domain_id_path,priority:1;type:uuid;"`
	DomainName string

	// can has one
	CompanyID *uuid.UUID `gorm:"index;type:uuid;"`

	Name        string `gorm:";index"`
	Description string `gorm:";"`
	Path        string `gorm:"not null;index;index:idx_assets_domain_id_path,priority:2"`
	// ContentType is the media type sent when the asset is served. Empty means
	// the type is found from the file extension.
	ContentType string `gorm:"not null;default:''"`

	// has many
	Headers []*AssetHeader `gorm:"foreignKey:AssetID"`
}

func (Asset) TableName() string {
	return ASSET_TABLE
}
