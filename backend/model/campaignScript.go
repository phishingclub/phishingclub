package model

import (
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/validate"
)

// CampaignScript represents a script configuration for a campaign.
// It allows per script event and data level settings and mirrors
// CampaignWebhook so scripts attach to a campaign exactly like webhooks.
type CampaignScript struct {
	ScriptID          nullable.Nullable[uuid.UUID] `json:"scriptID"`
	ScriptIncludeData nullable.Nullable[string]    `json:"scriptIncludeData"`
	ScriptEvents      nullable.Nullable[int]       `json:"scriptEvents"`
}

// Validate checks if the campaign script has valid configuration
func (ca *CampaignScript) Validate() error {
	if err := validate.NullableFieldRequired("scriptID", ca.ScriptID); err != nil {
		return err
	}

	// validate scriptincludedata is one of the allowed values
	if ca.ScriptIncludeData.IsSpecified() && !ca.ScriptIncludeData.IsNull() {
		dataLevel := ca.ScriptIncludeData.MustGet()
		if dataLevel != WebhookDataLevelNone &&
			dataLevel != WebhookDataLevelBasic &&
			dataLevel != WebhookDataLevelFull {
			return validate.WrapErrorWithField(
				errors.New("must be 'none', 'basic', or 'full'"),
				"scriptIncludeData",
			)
		}
	}

	// validate scriptevents is a valid binary value
	if ca.ScriptEvents.IsSpecified() && !ca.ScriptEvents.IsNull() {
		events := ca.ScriptEvents.MustGet()
		// check if any invalid bits are set, the valid ones are the mapped events
		maxValidBits := 0
		for _, bit := range data.WebhookEventToBit {
			maxValidBits |= bit
		}
		if events < 0 || (events > 0 && events&^maxValidBits != 0) {
			return validate.WrapErrorWithField(
				errors.New("invalid script events binary value"),
				"scriptEvents",
			)
		}
	}

	return nil
}

// GetScriptIncludeDataOrDefault returns the data level or default to "full"
func (ca *CampaignScript) GetScriptIncludeDataOrDefault() string {
	if ca.ScriptIncludeData.IsSpecified() && !ca.ScriptIncludeData.IsNull() {
		return ca.ScriptIncludeData.MustGet()
	}
	return WebhookDataLevelFull
}

// GetScriptEventsOrDefault returns the script events binary or default to 0 (all events)
func (ca *CampaignScript) GetScriptEventsOrDefault() int {
	if ca.ScriptEvents.IsSpecified() && !ca.ScriptEvents.IsNull() {
		return ca.ScriptEvents.MustGet()
	}
	return 0 // 0 means all events
}
