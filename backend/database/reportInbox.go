package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	REPORT_INBOX_TABLE         = "report_inboxes"
	REPORT_INBOX_MESSAGE_TABLE = "report_inbox_messages"
)

// ReportInbox is a mailbox that users report phishing to. Phishing Club reads it
// on a schedule and records a reported event for every campaign recipient it can
// match. A row with a NULL company_id covers global campaigns only.
type ReportInbox struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex;type:uuid"` // NULL is the global scope

	Enabled bool `gorm:"not null;default:false"`
	// AuthMode is "app" (client credentials) or "delegated" (an authorized oauth provider)
	AuthMode string `gorm:"not null;default:'app';type:varchar(16)"`
	// Mailbox is the address of the mailbox that is read
	Mailbox string `gorm:"not null;default:'';type:varchar(255)"`

	// app mode, stored as plain text like smtp passwords and oauth secrets
	TenantID     string `gorm:"not null;default:'';type:varchar(255)"`
	ClientID     string `gorm:"not null;default:'';type:varchar(255)"`
	ClientSecret string `gorm:"not null;default:'';type:varchar(1024)"`

	// delegated mode
	OAuthProviderID *uuid.UUID `gorm:"type:uuid;index"`

	// ReadSince is when the inbox was turned on or its connection changed. Mail
	// that arrived before it is never read.
	ReadSince *time.Time

	// poll state
	Watermark *time.Time // receivedDateTime of the newest processed message
	// ResumeAt is set when a poll stopped with more messages waiting. The next
	// poll continues from it, so a long run of handled messages is not read again.
	ResumeAt       *time.Time
	NextPollAt     *time.Time `gorm:"index"`
	LastPolledAt   *time.Time
	LastSuccessAt  *time.Time
	LastError      string `gorm:"not null;default:'';type:text"`
	FailureCount   int    `gorm:"not null;default:0"`
	ProcessedCount int    `gorm:"not null;default:0"`
	MatchedCount   int    `gorm:"not null;default:0"`
	// ConfigVersion goes up on every settings change. A poll that started before
	// the change does not write its state over the new settings.
	ConfigVersion int `gorm:"not null;default:0"`

	Company       *Company       `gorm:"foreignKey:CompanyID"`
	OAuthProvider *OAuthProvider `gorm:"foreignKey:OAuthProviderID"`
}

func (e *ReportInbox) Migrate(db *gorm.DB) error {
	// at most one global inbox (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_report_inboxes_null_company_id ON report_inboxes ((company_id IS NULL)) WHERE (company_id IS NULL)`
	return db.Exec(idx).Error
}

func (ReportInbox) TableName() string {
	return REPORT_INBOX_TABLE
}

// ReportInboxMessage remembers a mailbox message that was already processed, so
// a message is never counted twice. The access is read only, so messages can not
// be moved or flagged in the mailbox itself. Only ids are stored, never content.
type ReportInboxMessage struct {
	ID            *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt     *time.Time `gorm:"not null;index;"`
	ReportInboxID *uuid.UUID `gorm:"not null;type:uuid;uniqueIndex:idx_report_inbox_messages_inbox_message"`
	// MessageID is the provider id of the message in the mailbox
	MessageID  string     `gorm:"not null;type:varchar(512);uniqueIndex:idx_report_inbox_messages_inbox_message"`
	ReceivedAt *time.Time `gorm:"index"`
	Matched    bool       `gorm:"not null;default:false"`
	// Pending marks a message that failed to read and is tried again by its id
	// on the next polls, until it is read or passed over
	Pending       bool `gorm:"not null;default:false;index"`
	Failures      int  `gorm:"not null;default:0"`
	FirstFailedAt *time.Time
	LastFailedAt  *time.Time `gorm:"index"`
}

func (e *ReportInboxMessage) Migrate(db *gorm.DB) error {
	return nil
}

func (ReportInboxMessage) TableName() string {
	return REPORT_INBOX_MESSAGE_TABLE
}
