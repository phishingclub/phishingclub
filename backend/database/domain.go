package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	DOMAIN_TABLE = "domains"
)

// Domain is gorm data model
type Domain struct {
	ID               uuid.UUID  `gorm:"primary_key;not null;unique;type:uuid;"`
	CreatedAt        *time.Time `gorm:"not null;index;"`
	UpdatedAt        *time.Time `gorm:"not null;index;"`
	CompanyID        *uuid.UUID `gorm:"index;type:uuid;"`
	ProxyID           *uuid.UUID `gorm:"index;type:uuid;"`
	Name             string     `gorm:"not null;unique;"`
	Type             string     `gorm:"not null;default:'regular';"`
	ProxyTargetDomain string

	ManagedTLSCerts     bool `gorm:"not null;index;default:false"`
	OwnManagedTLS       bool `gorm:"not null;index;default:false"`
	HostWebsite         bool `gorm:"not null;"`
	PageContent         string
	PageNotFoundContent string
	RedirectURL         string
	// could has-one
	Company *Company
}

func (Domain) TableName() string {
	return DOMAIN_TABLE
}
