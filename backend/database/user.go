package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	USER_TABLE = "users"
)

// User is a database user
type User struct {
	ID        *uuid.UUID     `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time     `gorm:"not null;index;"`
	UpdatedAt *time.Time     `gorm:"not null;index"`
	DeletedAt gorm.DeletedAt `gorm:"index;"`

	Name                 string `gorm:"not null;"`
	Username             string `gorm:"not null;unique;"`
	Email                string `gorm:"unique;"`
	PasswordHash         string `gorm:"type:varchar(255);"`
	RequirePasswordRenew bool   `gorm:"default:false;"`

	// MFA
	TOTPEnabled bool `gorm:"default:false;"`
	TOTPSecret  string
	TOTPAuthURL string
	// TODO rename to MFARecoveryCode
	TOTPRecoveryCode string `gorm:"type:varchar(64);"`

	// SSO id
	SSOID string

	// maybe has one
	CompanyID *uuid.UUID `gorm:"type:uuid;index;"`
	Company   *Company
	// has one
	RoleID *uuid.UUID `gorm:"not null;type:uuid;index"`
	Role   *Role
	// APIKey
	APIKey string `gorm:"index"`
}

func (User) TableName() string {
	return USER_TABLE
}
