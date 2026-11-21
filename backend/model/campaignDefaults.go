package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/validate"
)

// CampaignDefaultsMaxDays is the largest day offset a default may use
const CampaignDefaultsMaxDays = 3650

// CampaignDefaults are the values the new campaign form starts with. A nil
// field is not set on this level and is taken from the level above: a company
// takes it from the global default, the global default from the built in
// form values.
type CampaignDefaults struct {
	ID        *uuid.UUID `json:"id"`
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
	CompanyID *uuid.UUID `json:"companyID"`

	// CloseAfterDays is the days after the last send that the campaign closes
	CloseAfterDays *int `json:"closeAfterDays"`
	// AnonymizeAfterDays is the days after close that all data is anonymized
	AnonymizeAfterDays *int `json:"anonymizeAfterDays"`
	// DataAnonymizeAfterDays is the days after close that the submitted data is
	// anonymized
	DataAnonymizeAfterDays *int  `json:"dataAnonymizeAfterDays"`
	IsAnonymous            *bool `json:"isAnonymous"`
	SaveSubmittedData      *bool `json:"saveSubmittedData"`
}

// Validate checks the day offsets are in range and the row does not ask for
// an anonymous campaign that saves submitted data
func (c *CampaignDefaults) Validate() error {
	days := []struct {
		field string
		value *int
	}{
		{"closeAfterDays", c.CloseAfterDays},
		{"anonymizeAfterDays", c.AnonymizeAfterDays},
		{"dataAnonymizeAfterDays", c.DataAnonymizeAfterDays},
	}
	for _, d := range days {
		if d.value == nil {
			continue
		}
		if *d.value < 0 || *d.value > CampaignDefaultsMaxDays {
			return validate.WrapErrorWithField(
				errors.Errorf("must be between 0 and %d days", CampaignDefaultsMaxDays),
				d.field,
			)
		}
	}
	if c.IsAnonymous != nil && *c.IsAnonymous &&
		c.SaveSubmittedData != nil && *c.SaveSubmittedData {
		return validate.WrapErrorWithField(
			errors.New("an anonymous campaign can not save submitted data"),
			"saveSubmittedData",
		)
	}
	return nil
}

// ToDBMap returns every editable field, a nil field is stored as NULL
func (c *CampaignDefaults) ToDBMap() map[string]any {
	return map[string]any{
		"close_after_days":          c.CloseAfterDays,
		"anonymize_after_days":      c.AnonymizeAfterDays,
		"data_anonymize_after_days": c.DataAnonymizeAfterDays,
		"is_anonymous":              c.IsAnonymous,
		"save_submitted_data":       c.SaveSubmittedData,
	}
}

// Campaign defaults sources, where a resolved value comes from
const (
	CampaignDefaultsSourceCompany = "company"
	CampaignDefaultsSourceGlobal  = "global"
)

// ResolvedCampaignDefaults are the defaults the new campaign form uses for a
// company. Sources names where each set field comes from. Fields are resolved
// one by one, so isAnonymous true may come with saveSubmittedData true.
type ResolvedCampaignDefaults struct {
	CloseAfterDays         *int              `json:"closeAfterDays"`
	AnonymizeAfterDays     *int              `json:"anonymizeAfterDays"`
	DataAnonymizeAfterDays *int              `json:"dataAnonymizeAfterDays"`
	IsAnonymous            *bool             `json:"isAnonymous"`
	SaveSubmittedData      *bool             `json:"saveSubmittedData"`
	Sources                map[string]string `json:"sources"`
}

// ResolveCampaignDefaults takes each field from the company row when it sets
// it, else from the global row. Either row may be nil.
func ResolveCampaignDefaults(company, global *CampaignDefaults) *ResolvedCampaignDefaults {
	out := &ResolvedCampaignDefaults{Sources: map[string]string{}}
	pickInt := func(field string, get func(*CampaignDefaults) *int) *int {
		if company != nil && get(company) != nil {
			out.Sources[field] = CampaignDefaultsSourceCompany
			return get(company)
		}
		if global != nil && get(global) != nil {
			out.Sources[field] = CampaignDefaultsSourceGlobal
			return get(global)
		}
		return nil
	}
	pickBool := func(field string, get func(*CampaignDefaults) *bool) *bool {
		if company != nil && get(company) != nil {
			out.Sources[field] = CampaignDefaultsSourceCompany
			return get(company)
		}
		if global != nil && get(global) != nil {
			out.Sources[field] = CampaignDefaultsSourceGlobal
			return get(global)
		}
		return nil
	}
	out.CloseAfterDays = pickInt("closeAfterDays", func(c *CampaignDefaults) *int { return c.CloseAfterDays })
	out.AnonymizeAfterDays = pickInt("anonymizeAfterDays", func(c *CampaignDefaults) *int { return c.AnonymizeAfterDays })
	out.DataAnonymizeAfterDays = pickInt("dataAnonymizeAfterDays", func(c *CampaignDefaults) *int { return c.DataAnonymizeAfterDays })
	out.IsAnonymous = pickBool("isAnonymous", func(c *CampaignDefaults) *bool { return c.IsAnonymous })
	out.SaveSubmittedData = pickBool("saveSubmittedData", func(c *CampaignDefaults) *bool { return c.SaveSubmittedData })
	// each field is resolved on its own. An anonymous campaign never saves
	// submitted data, the campaign form and the campaign model enforce that,
	// so the save data value is kept for when anonymous is turned off.
	return out
}
