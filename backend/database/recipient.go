package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	RECIPIENT_TABLE = "recipients"
)

// Recipient is a gorm data model
type Recipient struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	DeletedAt *time.Time `gorm:"index;"`

	Email           *string `gorm:";uniqueIndex"`
	Phone           *string `gorm:";index"`
	ExtraIdentifier *string `gorm:";index"`

	FirstName    string `gorm:";"`
	LastName     string `gorm:";"`
	Position     string `gorm:";"`
	Department   string `gorm:";"`
	City         string `gorm:";"`
	Country      string `gorm:";"`
	Misc         string `gorm:";"`
	ScimUserName string `gorm:";"`

	// ScimSoftDeletedAt marks a recipient that the IdP has deprovisioned via SCIM
	// but is kept during the retention grace period before being pruned. Null
	// means the recipient is active.
	ScimSoftDeletedAt *time.Time `gorm:"index;"`

	// can belong to
	CompanyID *uuid.UUID `gorm:"type:uuid;index;"`
	Company   *Company

	// many-to-many
	Groups []RecipientGroup `gorm:"many2many:recipient_group_recipients;"`
}

func (Recipient) TableName() string {
	return RECIPIENT_TABLE
}

// Migrate adds an index on LOWER(email). The case insensitive lookup used by
// import and upsert (WHERE LOWER(email) = LOWER(?)) cannot use the plain email
// index because the LOWER() call wraps the column, so without this it scans the
// whole table on every recipient and a large import becomes quadratic.
func (Recipient) Migrate(db *gorm.DB) error {
	return db.Exec(`CREATE INDEX IF NOT EXISTS idx_recipients_email_lower ON recipients (LOWER(email))`).Error
}
