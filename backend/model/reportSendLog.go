package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

// ReportSendLog is one record of a report delivery attempt
type ReportSendLog struct {
	ID             nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt      *time.Time                   `json:"createdAt"`
	CompanyID      nullable.Nullable[uuid.UUID] `json:"companyID"`
	CampaignID     nullable.Nullable[uuid.UUID] `json:"campaignID"`
	CampaignName   string                       `json:"campaignName"`
	GroupName      string                       `json:"groupName"`
	Trigger        string                       `json:"trigger"`
	Status         string                       `json:"status"`
	RecipientCount int                          `json:"recipientCount"`
	Recipients     string                       `json:"recipients"`
	SenderEmail    string                       `json:"senderEmail"`
	ErrorMessage   string                       `json:"errorMessage"`
}

// ToDBMap converts the fields for persistence
func (r *ReportSendLog) ToDBMap() map[string]any {
	m := map[string]any{}
	m["campaign_name"] = r.CampaignName
	m["group_name"] = r.GroupName
	m["trigger"] = r.Trigger
	m["status"] = r.Status
	m["recipient_count"] = r.RecipientCount
	m["recipients"] = r.Recipients
	m["sender_email"] = r.SenderEmail
	m["error_message"] = r.ErrorMessage

	m["company_id"] = nil
	if r.CompanyID.IsSpecified() && !r.CompanyID.IsNull() {
		m["company_id"] = r.CompanyID.MustGet()
	}
	m["campaign_id"] = nil
	if r.CampaignID.IsSpecified() && !r.CampaignID.IsNull() {
		m["campaign_id"] = r.CampaignID.MustGet()
	}
	return m
}
