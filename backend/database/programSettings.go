package database

import (
	"github.com/google/uuid"
)

const (
	PROGRAM_ALLOW_DENY_TABLE = "program_allow_denies"
	PROGRAM_WEBHOOK_TABLE    = "program_webhooks"
)

// ProgramAllowDeny is the join table of allow or deny lists set on a program.
// They are copied to every hidden campaign the program runs.
type ProgramAllowDeny struct {
	ProgramID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_allow_deny;primaryKey;"`
	Program   *Program

	AllowDenyID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_allow_deny;primaryKey;"`
	AllowDeny   *AllowDeny
}

func (ProgramAllowDeny) TableName() string {
	return PROGRAM_ALLOW_DENY_TABLE
}

// ProgramWebhook is the join table of webhooks set on a program with the same
// per webhook settings a campaign has. They are copied to every hidden campaign.
type ProgramWebhook struct {
	ProgramID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_webhook;primaryKey;"`
	Program   *Program

	WebhookID *uuid.UUID `gorm:"not null;index;type:uuid;uniqueIndex:idx_program_webhook;primaryKey;"`
	Webhook   *Webhook

	// none, basic or full
	WebhookIncludeData string `gorm:"not null;default:'full'"`
	// event bit mask, 0 means all events
	WebhookEvents int `gorm:"not null;default:0"`
}

func (ProgramWebhook) TableName() string {
	return PROGRAM_WEBHOOK_TABLE
}
