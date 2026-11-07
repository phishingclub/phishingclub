package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PROGRAM_CAMPAIGN_TABLE = "program_campaigns"
)

// ProgramCampaign links a program step and loop iteration to the hidden campaign
// that carries its sends. It is created lazily the first time any recipient
// reaches that iteration. When that campaign is closed, for example because its
// template could not be sent, a new one is linked for the same step and loop,
// and the newest one that is not closed is used. Reporting unions the campaigns
// of a program (or a step) through this table.
type ProgramCampaign struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`

	ProgramID     *uuid.UUID `gorm:"type:uuid;not null;index;index:idx_program_campaigns_step_loop_lookup"`
	ProgramStepID *uuid.UUID `gorm:"type:uuid;not null;index;index:idx_program_campaigns_step_loop_lookup"`
	LoopIndex     int        `gorm:"not null;index:idx_program_campaigns_step_loop_lookup"`

	CampaignID *uuid.UUID `gorm:"type:uuid;not null;index"`
	Campaign   *Campaign
}

func (ProgramCampaign) TableName() string {
	return PROGRAM_CAMPAIGN_TABLE
}

// Migrate drops the old unique index on (program, step, loop), since a closed
// step campaign can now be followed by a new one for the same step and loop.
func (p *ProgramCampaign) Migrate(db *gorm.DB) error {
	const oldIndex = "idx_program_campaigns_step_loop"
	if db.Migrator().HasIndex(&ProgramCampaign{}, oldIndex) {
		return db.Migrator().DropIndex(&ProgramCampaign{}, oldIndex)
	}
	return nil
}
