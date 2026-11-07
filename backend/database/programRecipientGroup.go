package database

import (
	"github.com/google/uuid"
)

const (
	PROGRAM_RECIPIENT_GROUP_TABLE = "program_recipient_groups"
)

// ProgramRecipientGroup is the join table of recipient groups assigned to a
// program. Membership of these groups drives enrollment.
type ProgramRecipientGroup struct {
	ProgramID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_recipient_group;"`
	Program   *Program

	RecipientGroupID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_recipient_group;"`
	RecipientGroup   *RecipientGroup
}

func (ProgramRecipientGroup) TableName() string {
	return PROGRAM_RECIPIENT_GROUP_TABLE
}
