package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	COMPANY_TABLE = "companies"
)

type Company struct {
	ID        uuid.UUID  `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	Name      string     `gorm:"not null;unique;index"`
	Comment   *string    `gorm:"type:text"`
	// Color is an optional #RGB or #RRGGBB used to tint the company view banner and frame
	Color *string `gorm:"type:text"`
	// AssetsKey is a random slug naming the company asset folder stored under the
	// shared asset directory, so company assets resolve on any domain via {{.BaseURL}}/<AssetsKey>/file
	AssetsKey string `gorm:"unique;index"`

	// backref: many-to-one
	Users           []*User           //`gorm:"foreignKey:CompanyID;"`
	RecipientGroups []*RecipientGroup //`gorm:"foreignKey:CompanyID;"`
}

func (Company) TableName() string {
	return COMPANY_TABLE
}
