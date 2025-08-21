package database

import (
	"github.com/google/uuid"
)

// Option is a database option (options stored in the database)
type Option struct {
	ID    *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	Key   string     `gorm:"not null;unique;index"`
	Value string     `gorm:"not null;"`
}

func (Option) TableName() string {
	return "options"
}
