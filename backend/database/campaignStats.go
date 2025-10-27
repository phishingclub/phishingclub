package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	CAMPAIGN_STATS_TABLE = "campaign_stats"
)

// CampaignStats is gorm data model for aggregated campaign statistics
type CampaignStats struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid" json:"id"`
	CreatedAt *time.Time `gorm:"not null;index;" json:"createdAt"`
	UpdatedAt *time.Time `gorm:"not null;" json:"updatedAt"`

	// Campaign reference
	CampaignID   *uuid.UUID `gorm:"index;type:uuid;" json:"campaignId"`
	CampaignName string     `gorm:"not null;" json:"campaignName"`
	CompanyID    *uuid.UUID `gorm:"index;type:uuid;" json:"companyId"` // nullable for global campaigns

	// Time metrics
	CampaignStartDate *time.Time `gorm:"index;" json:"campaignStartDate"`
	CampaignEndDate   *time.Time `gorm:"index;" json:"campaignEndDate"`
	CampaignClosedAt  *time.Time `gorm:"index;" json:"campaignClosedAt"`

	// Volume metrics
	TotalRecipients int `gorm:"not null;default:0" json:"totalRecipients"`
	TotalEvents     int `gorm:"not null;default:0" json:"totalEvents"`

	// Event type breakdowns
	EmailsSent          int `gorm:"not null;default:0" json:"emailsSent"`
	TrackingPixelLoaded int `gorm:"not null;default:0" json:"trackingPixelLoaded"` // Email opens
	WebsiteVisits       int `gorm:"not null;default:0" json:"websiteVisits"`       // Link clicks
	DataSubmissions     int `gorm:"not null;default:0" json:"dataSubmissions"`     // Form submissions
	Reported            int `gorm:"not null;default:0" json:"reported"`            // Reported phishing

	// Campaign metadata
	TemplateName string `gorm:"" json:"templateName"`
	CampaignType string `gorm:"" json:"campaignType"` // 'scheduled', 'self-managed'
}

func (CampaignStats) TableName() string {
	return CAMPAIGN_STATS_TABLE
}
