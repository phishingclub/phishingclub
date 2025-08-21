package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	SESSION_TABLE = "sessions"
)

type Session struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	// IP address of the user when the session was created
	IPAddress string `gorm:"not null;index;default:''"`
	// the expiresAt is the time when the session will expire, nomatter the maxAgeAt
	ExpiresAt *time.Time `gorm:"not null;index"`
	// the maxAgeAt is the time when the session will expire, nomatter the expiresAt
	MaxAgeAt *time.Time `gorm:"not null;index"`
	// has-one
	//
	// belongs to
	UserID string `gorm:";type:uuid;"`
	User   *User
}

func (Session) TableName() string {
	return SESSION_TABLE
}
