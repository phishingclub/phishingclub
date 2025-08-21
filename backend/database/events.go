package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	EVENT_TABLE = "events"
)

type Event struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	Name      string     `gorm:"not null;index;"`
}

func (Event) TableName() string {
	return EVENT_TABLE
}
