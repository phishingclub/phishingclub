package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// ReportTemplate is a report template
type ReportTemplate struct {
	ID        nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt *time.Time                              `json:"createdAt"`
	UpdatedAt *time.Time                              `json:"updatedAt"`
	CompanyID nullable.Nullable[uuid.UUID]            `json:"companyID"`
	Content   nullable.Nullable[vo.OptionalString1MB] `json:"content"`

	Company *Company `json:"-"`
}

// Validate checks if the report template has a valid state
func (r *ReportTemplate) Validate() error {
	if err := validate.NullableFieldRequired("content", r.Content); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts updatable fields to a map
func (r *ReportTemplate) ToDBMap() map[string]any {
	m := map[string]any{}
	if r.Content.IsSpecified() {
		m["content"] = nil
		if content, err := r.Content.Get(); err == nil {
			m["content"] = content.String()
		}
	}
	if r.CompanyID.IsSpecified() {
		if r.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = r.CompanyID.MustGet()
		}
	}
	return m
}

// ReportData is the data context passed to a report HTML template for rendering.
// Date fields are pre-formatted as "YYYY-MM-DD" strings (empty when not set).
type ReportData struct {
	// Campaign identity
	CampaignName      string
	CompanyName       string
	ReportDate        string
	CampaignStartDate string
	CampaignEndDate   string
	CampaignClosedAt  string

	// Totals
	TotalTargets int64
	EmailsSent   int64
	EmailsOpened int64

	// Core outcome counts
	ResultClicked   int64
	ResultSubmitted int64
	ResultReported  int64

	// Formatted percentages (e.g. "45.2") — ready to use directly in templates
	ResultClickedPercent   string
	ResultSubmittedPercent string
	ResultReportedPercent  string

	// Float percentages for custom formatting with {{printf "%.1f" .ClickRate}}
	SentRate    float64
	OpenRate    float64
	ClickRate   float64
	SubmitRate  float64
	ReportRate  float64

	// Relative conversion rates — funnel step-to-step (formatted strings like "45.2")
	OpenedOfSent       string // EmailsOpened / EmailsSent
	ClickedOfOpened    string // ResultClicked / EmailsOpened
	SubmittedOfClicked string // ResultSubmitted / ResultClicked

	// Per-recipient detail — empty for anonymous or anonymized campaigns
	Recipients []ReportRecipient
}

// ReportRecipient holds per-recipient result data for the recipient detail table
type ReportRecipient struct {
	FirstName     string
	LastName      string
	Email         string
	Department    string
	Position      string
	ClickedLink   bool
	SubmittedData bool
	Reported      bool
}
