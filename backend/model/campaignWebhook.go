package model

import (
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/validate"
)

// CampaignWebhook represents a webhook configuration for a campaign
// allows per-webhook event and data level settings
type CampaignWebhook struct {
	WebhookID          nullable.Nullable[uuid.UUID] `json:"webhookID"`
	WebhookIncludeData nullable.Nullable[string]    `json:"webhookIncludeData"`
	WebhookEvents      nullable.Nullable[int]       `json:"webhookEvents"`
}

// Validate checks if the campaign webhook has valid configuration
func (cw *CampaignWebhook) Validate() error {
	if err := validate.NullableFieldRequired("webhookID", cw.WebhookID); err != nil {
		return err
	}

	// validate webhookincludedata is one of the allowed values
	if cw.WebhookIncludeData.IsSpecified() && !cw.WebhookIncludeData.IsNull() {
		dataLevel := cw.WebhookIncludeData.MustGet()
		if dataLevel != WebhookDataLevelNone &&
			dataLevel != WebhookDataLevelBasic &&
			dataLevel != WebhookDataLevelFull {
			return validate.WrapErrorWithField(
				errors.New("must be 'none', 'basic', or 'full'"),
				"webhookIncludeData",
			)
		}
	}

	// validate webhookevents is a valid binary value
	if cw.WebhookEvents.IsSpecified() && !cw.WebhookEvents.IsNull() {
		events := cw.WebhookEvents.MustGet()
		// check if any invalid bits are set (only bits 0-9 are valid)
		maxValidBits := 0
		for _, bit := range data.WebhookEventToBit {
			maxValidBits |= bit
		}
		if events < 0 || (events > 0 && events&^maxValidBits != 0) {
			return validate.WrapErrorWithField(
				errors.New("invalid webhook events binary value"),
				"webhookEvents",
			)
		}
	}

	return nil
}

// GetWebhookIncludeDataOrDefault returns the data level or default to "full"
func (cw *CampaignWebhook) GetWebhookIncludeDataOrDefault() string {
	if cw.WebhookIncludeData.IsSpecified() && !cw.WebhookIncludeData.IsNull() {
		return cw.WebhookIncludeData.MustGet()
	}
	return WebhookDataLevelFull
}

// GetWebhookEventsOrDefault returns the webhook events binary or default to 0 (all events)
func (cw *CampaignWebhook) GetWebhookEventsOrDefault() int {
	if cw.WebhookEvents.IsSpecified() && !cw.WebhookEvents.IsNull() {
		return cw.WebhookEvents.MustGet()
	}
	return 0 // 0 means all events
}
