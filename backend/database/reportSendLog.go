package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	REPORT_SEND_LOG_TABLE = "report_send_logs"
)

// ReportSendLog is one record of a report delivery attempt for a campaign.
// values are denormalized so the row stays meaningful after the campaign,
// group or smtp config is changed or removed.
type ReportSendLog struct {
	ID             *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt      *time.Time `gorm:"not null;index;"`
	CompanyID      *uuid.UUID `gorm:"type:uuid;index"` // the campaign's company, NULL for a global campaign
	CampaignID     *uuid.UUID `gorm:"type:uuid;index"`
	CampaignName   string     `gorm:"not null;default:''"`
	GroupName      string     `gorm:"not null;default:''"`           // recipient group name at send time
	Trigger        string     `gorm:"not null;default:''"`           // on_demand or on_finish
	Status         string     `gorm:"not null;default:''"`           // sent or failed
	RecipientCount int        `gorm:"not null;default:0"`
	Recipients     string     `gorm:"not null;default:'';type:text"` // comma separated recipient addresses
	SenderEmail    string     `gorm:"not null;default:''"`
	ErrorMessage   string     `gorm:"not null;default:'';type:text"` // empty on success

	Company *Company `gorm:"foreignKey:CompanyID"`
}

func (e *ReportSendLog) Migrate(db *gorm.DB) error {
	return nil
}

func (ReportSendLog) TableName() string {
	return REPORT_SEND_LOG_TABLE
}
