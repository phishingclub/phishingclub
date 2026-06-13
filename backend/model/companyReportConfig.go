package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// CompanyReportConfig is the automatic report delivery configuration for a company
type CompanyReportConfig struct {
	ID                  nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt           *time.Time                   `json:"createdAt"`
	UpdatedAt           *time.Time                   `json:"updatedAt"`
	CompanyID           nullable.Nullable[uuid.UUID] `json:"companyID"`
	Enabled             bool                         `json:"enabled"`
	SendOnFinish        bool                         `json:"sendOnFinish"`
	RecipientGroupID    nullable.Nullable[uuid.UUID] `json:"recipientGroupID"`
	SMTPConfigurationID nullable.Nullable[uuid.UUID] `json:"smtpConfigurationID"`
	SenderEmail         nullable.Nullable[vo.Email]  `json:"senderEmail"`
	EmailSubject        nullable.Nullable[string]    `json:"emailSubject"`
	EmailBody           nullable.Nullable[string]    `json:"emailBody"`
	LastSentAt          *time.Time                   `json:"lastSentAt"`
}

// Validate checks if the config is in a valid state.
// when enabled the delivery target fields are required, otherwise the config may
// be saved partially while the user is still setting it up.
func (c *CompanyReportConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if err := validate.NullableFieldRequired("recipientGroupID", c.RecipientGroupID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("smtpConfigurationID", c.SMTPConfigurationID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("senderEmail", c.SenderEmail); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts updatable fields to a map for persistence
func (c *CompanyReportConfig) ToDBMap() map[string]any {
	m := map[string]any{}
	m["enabled"] = c.Enabled
	m["send_on_finish"] = c.SendOnFinish

	m["recipient_group_id"] = nil
	if c.RecipientGroupID.IsSpecified() && !c.RecipientGroupID.IsNull() {
		m["recipient_group_id"] = c.RecipientGroupID.MustGet()
	}

	m["smtp_configuration_id"] = nil
	if c.SMTPConfigurationID.IsSpecified() && !c.SMTPConfigurationID.IsNull() {
		m["smtp_configuration_id"] = c.SMTPConfigurationID.MustGet()
	}

	m["sender_email"] = ""
	if c.SenderEmail.IsSpecified() && !c.SenderEmail.IsNull() {
		m["sender_email"] = c.SenderEmail.MustGet().String()
	}

	m["email_subject"] = ""
	if c.EmailSubject.IsSpecified() && !c.EmailSubject.IsNull() {
		m["email_subject"] = c.EmailSubject.MustGet()
	}

	m["email_body"] = ""
	if c.EmailBody.IsSpecified() && !c.EmailBody.IsNull() {
		m["email_body"] = c.EmailBody.MustGet()
	}
	return m
}
