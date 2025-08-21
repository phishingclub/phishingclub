package database

import (
	"github.com/google/uuid"
)

const (
	IDENTIFIER_TABLE = "identifiers"
)

type Identifier struct {
	ID   *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	Name string     `gorm:"not null;uniqueIndex"`
}

func (Identifier) TableName() string {
	return IDENTIFIER_TABLE
}
