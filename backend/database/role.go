package database

import (
	"github.com/google/uuid"
)

// Role is a role
type Role struct {
	ID   *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	Name string     `gorm:"not null;index;unique;"`

	// one-to-many
	Users []*User
}

func (Role) TableName() string {
	return "roles"
}
