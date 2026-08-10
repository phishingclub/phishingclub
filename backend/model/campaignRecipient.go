package model

import (
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/validate"
)

// CampaignRecipient is a campaign recipient
// this model must not be consumed from a endpoint
type CampaignRecipient struct {
	ID            nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt     *time.Time                   `json:"createdAt"`
	UpdatedAt     *time.Time                   `json:"updatedAt"`
	CancelledAt   nullable.Nullable[time.Time] `json:"cancelledAt"`
	SendAt        nullable.Nullable[time.Time] `json:"sendAt"`
	LastAttemptAt nullable.Nullable[time.Time] `json:"lastAttemptAt"`
	SentAt        nullable.Nullable[time.Time] `json:"sentAt"`
	SelfManaged   nullable.Nullable[bool]      `json:"selfManaged"`
	AnonymizedID  nullable.Nullable[uuid.UUID] `json:"anonymizedID"`
	CampaignID    nullable.Nullable[uuid.UUID] `json:"campaignID"`
	Campaign      *Campaign                    `json:"campaign"`
	// null recipientID means that the data has been anonymized
	RecipientID      nullable.Nullable[uuid.UUID] `json:"recipientID"`
	Recipient        *Recipient                   `json:"recipient"`
	NotableEventID   nullable.Nullable[uuid.UUID] `json:"notableEventID"`
	NotableEventName string                       `json:"notableEventName"`
	// LureCode is the identifier in the lure URL, stored and matched byte for
	// byte. Null releases it for reuse.
	LureCode nullable.Nullable[string] `json:"lureCode"`
	// LureCodeCustom marks a code set by the operator rather than generated.
	LureCodeCustom nullable.Nullable[bool] `json:"lureCodeCustom"`
}

// Validate validates the campaign recipient
func (c *CampaignRecipient) Validate() error {
	if err := validate.NullableFieldRequired("campaignID", c.CampaignID); err != nil {
		return err
	}
	anonymizedAtErr := validate.NullableFieldRequired("anonymizedID", c.AnonymizedID)
	recipientIDErr := validate.NullableFieldRequired("recipientID", c.RecipientID)
	if anonymizedAtErr == nil && recipientIDErr == nil {
		return recipientIDErr
	}
	if anonymizedAtErr != nil && recipientIDErr != nil {
		return validate.WrapErrorWithField(
			errors.New("AnonymizedID can not be set with recipientID"),
			"recipientID",
		)
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (c *CampaignRecipient) ToDBMap() map[string]any {
	m := map[string]any{}
	if c.CancelledAt.IsSpecified() {
		m["cancelled_at"] = nil
		if v, err := c.CancelledAt.Get(); err == nil {
			m["cancelled_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.SendAt.IsSpecified() {
		m["send_at"] = nil
		if v, err := c.SendAt.Get(); err == nil {
			m["send_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.LastAttemptAt.IsSpecified() {
		m["last_attempt_at"] = nil
		if v, err := c.LastAttemptAt.Get(); err == nil {
			m["last_attempt_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.SentAt.IsSpecified() {
		m["sent_at"] = nil
		if v, err := c.SentAt.Get(); err == nil {
			m["sent_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.SelfManaged.IsSpecified() {
		m["self_managed"] = nil
		if v, err := c.SelfManaged.Get(); err == nil {
			m["self_managed"] = v
		}
	}
	if c.CampaignID.IsSpecified() {
		m["campaign_id"] = nil
		if v, err := c.CampaignID.Get(); err == nil {
			m["campaign_id"] = v
		}
	}
	if c.RecipientID.IsSpecified() {
		m["recipient_id"] = nil
		if v, err := c.RecipientID.Get(); err == nil {
			m["recipient_id"] = v
		}
	}
	if c.NotableEventID.IsSpecified() {
		m["notable_event_id"] = nil
		if v, err := c.NotableEventID.Get(); err == nil {
			m["notable_event_id"] = v
		}
	}
	// the lure code is left out. callers load a whole recipient, change one
	// field and write it back, so a code carried here would be restated on every
	// such write and collide with the unique index once another recipient holds
	// it. written only by Insert, SetLureCodeByID and ReleaseLureCodeByID.

	return m
}
