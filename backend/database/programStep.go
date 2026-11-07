package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	PROGRAM_STEP_TABLE = "program_steps"
)

// ProgramStep is one item in a program's ordered content list. It references a
// campaign template, which itself bundles the email, domain, sender and landing
// pages. A training step is simply a step whose template has IsTraining set.
type ProgramStep struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	ProgramID *uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_program_steps_program_position"`
	Program   *Program

	CampaignTemplateID *uuid.UUID `gorm:"type:uuid;not null;index"`
	CampaignTemplate   *CampaignTemplate

	// Position is the order of the steps in sequential mode.
	Position int `gorm:"not null;uniqueIndex:idx_program_steps_program_position"`
}

func (ProgramStep) TableName() string {
	return PROGRAM_STEP_TABLE
}
