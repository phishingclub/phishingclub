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

	// backref: many-to-one
	Users           []*User           //`gorm:"foreignKey:CompanyID;"`
	RecipientGroups []*RecipientGroup //`gorm:"foreignKey:CompanyID;"`
}

func (Company) TableName() string {
	return COMPANY_TABLE
}
