package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_TABLE = "campaigns"
)

// Campaign is gorm data model
type Campaign struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	CloseAt      *time.Time `gorm:"index;"`
	ClosedAt     *time.Time `gorm:"index;"`
	AnonymizeAt  *time.Time `gorm:"index;"`
	AnonymizedAt *time.Time `gorm:"index;"`
	SortField    string     `gorm:";"`
	SortOrder    string     `gorm:";"` // 'asc,desc,random'
	SendStartAt  *time.Time `gorm:"index;"`
	SendEndAt    *time.Time `gorm:"index;"`

	// ConstraintWeekDays is a binary format.
	// 0b00000001 = 1 = sunday
	// 0b00000010 = 2 = monday
	// 0b00000100 = 4 = tuesday
	// 0b00001000 = 8 = ...
	// 0b00010000 = 16 =
	// 0b00100000 = 32 =
	// 0b01000000 = 64 =
	ConstraintWeekDays *int `gorm:";"`
	// hh:mm
	ConstraintStartTime *string `gorm:"index;"`
	// hh:mm
	ConstraintEndTime *string `gorm:"index;"`
	SaveSubmittedData bool    `gorm:"not null;default:false"`
	IsAnonymous       bool    `gorm:"not null;default:false"`
	IsTest            bool    `gorm:"not null;default:false"`

	// has one
	CampaignTemplateID *uuid.UUID `gorm:"index;type:uuid;"`
	CampaignTemplate   *CampaignTemplate

	// can has one
	CompanyID     *uuid.UUID `gorm:"index;type:uuid;index;uniqueIndex:idx_campaigns_unique_name_and_company_id;"`
	Company       *Company
	DenyPageID    *uuid.UUID `gorm:"type:uuid;index;"`
	DenyPage      *Page      `gorm:"foreignKey:DenyPageID;references:ID"`
	EvasionPageID *uuid.UUID `gorm:"type:uuid;index;"`
	EvasionPage   *Page      `gorm:"foreignKey:EvasionPageID;references:ID"`
	// NotableEventID notable event for this campaign
	NotableEvent   *Event     `gorm:"foreignKey:NotableEventID;references:ID"`
	NotableEventID *uuid.UUID `gorm:"type:uuid;index"`

	WebhookID *uuid.UUID `gorm:"type:uuid;index;"`

	// has many-to-many
	RecipientGroups []*RecipientGroup `gorm:"many2many:campaign_recipient_groups"`
	AllowDeny       []*AllowDeny      `gorm:"many2many:campaign_allow_denies"`

	Name string `gorm:"not null;uniqueIndex:idx_campaigns_unique_name_and_company_id"`
}

func (c *Campaign) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "campaigns")
}

func (Campaign) TableName() string {
	return CAMPAIGN_TABLE
}
