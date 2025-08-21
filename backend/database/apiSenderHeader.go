package database

import (
	"time"

	"github.com/google/uuid"
)

type APISenderHeader struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`

	Key   string `gorm:"not null;"`
	Value string `gorm:"not null;"`
	// IsRequestHeader is true if the header is a request header
	// and false if it is a expected response header
	IsRequestHeader bool `gorm:"not null;"`

	// belongs to
	APISenderID *uuid.UUID `gorm:"index;not null;type:uuid"`
}

func (APISenderHeader) TableName() string {
	return "api_sender_headers"
}
