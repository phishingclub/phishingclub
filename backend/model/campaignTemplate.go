package model

import (
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// CampaignTemplate is a campaign template
type CampaignTemplate struct {
	ID        nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt *time.Time                   `json:"createdAt"`
	UpdatedAt *time.Time                   `json:"updatedAt"`

	Name nullable.Nullable[vo.String64] `json:"name"`

	DomainID nullable.Nullable[uuid.UUID] `json:"domainID"`
	Domain   *Domain                      `json:"domain"`

	BeforeLandingPageID nullable.Nullable[uuid.UUID] `json:"beforeLandingPageID"`
	BeforeLandingePage  *Page                        `json:"beforeLandingPage"`

	LandingPageID nullable.Nullable[uuid.UUID] `json:"landingPageID"`
	LandingPage   *Page                        `json:"landingPage"`

	AfterLandingPageID nullable.Nullable[uuid.UUID] `json:"afterLandingPageID"`
	AfterLandingPage   *Page                        `json:"afterLandingPage"`

	AfterLandingPageRedirectURL nullable.Nullable[vo.OptionalString255] `json:"afterLandingPageRedirectURL"`

	URLIdentifierID nullable.Nullable[*uuid.UUID] `json:"urlIdentifierID"`
	URLIdentifier   *Identifier                   `json:"urlIdentifier"`

	StateIdentifierID nullable.Nullable[*uuid.UUID] `json:"stateIdentifierID"`
	StateIdentifier   *Identifier                   `json:"stateIdentifier"`

	URLPath nullable.Nullable[vo.URLPath] `json:"urlPath"`

	EmailID nullable.Nullable[uuid.UUID] `json:"emailID"`
	Email   *Email                       `json:"email"`

	SMTPConfigurationID nullable.Nullable[uuid.UUID] `json:"smtpConfigurationID"`
	SMTPConfiguration   *SMTPConfiguration           `json:"smtpConfiguration"`

	APISenderID nullable.Nullable[uuid.UUID] `json:"apiSenderID"`
	APISender   *APISender                   `json:"apiSender"`

	CompanyID nullable.Nullable[uuid.UUID] `json:"companyID"`
	Company   *Company                     `json:"company"`

	IsUsable nullable.Nullable[bool] `json:"isUsable"`
}

// Validate checks if the campaign template has a valid state
func (c *CampaignTemplate) Validate() error {
	if err := validate.NullableFieldRequired("name", c.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("urlIdentifierID", c.URLIdentifierID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("stateIdentifierID", c.StateIdentifierID); err != nil {
		return err
	}
	if a, err := c.URLIdentifierID.Get(); err == nil {
		if b, err := c.StateIdentifierID.Get(); err == nil {
			if a.String() == b.String() {
				return errs.NewValidationError(
					errors.New("URL and state identifier can not be the same"),
				)
			}
		}
	}
	if err := validate.NullableFieldRequired("urlPath", c.URLPath); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (c *CampaignTemplate) ToDBMap() map[string]any {
	m := map[string]any{}
	if c.Name.IsSpecified() {
		m["name"] = nil
		if name, err := c.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if c.DomainID.IsSpecified() {
		if c.DomainID.IsNull() {
			m["domain_id"] = nil
		} else {
			m["domain_id"] = c.DomainID.MustGet()
		}
	}

	if c.BeforeLandingPageID.IsSpecified() {
		if c.BeforeLandingPageID.IsNull() {
			m["before_landing_page_id"] = nil
		} else {
			m["before_landing_page_id"] = c.BeforeLandingPageID.MustGet()
		}
	}

	if c.LandingPageID.IsSpecified() {
		if c.LandingPageID.IsNull() {
			m["landing_page_id"] = nil
		} else {
			m["landing_page_id"] = c.LandingPageID.MustGet()
		}
	}

	if c.AfterLandingPageID.IsSpecified() {
		if c.AfterLandingPageID.IsNull() {
			m["after_landing_page_id"] = nil
		} else {
			m["after_landing_page_id"] = c.AfterLandingPageID.MustGet()
		}
	}
	if c.AfterLandingPageRedirectURL.IsSpecified() {
		if c.AfterLandingPageRedirectURL.IsNull() {
			m["after_landing_page_redirect_url"] = nil
		} else {
			m["after_landing_page_redirect_url"] = c.AfterLandingPageRedirectURL.MustGet().String()
		}
	}

	if c.EmailID.IsSpecified() {
		if c.EmailID.IsNull() {
			m["email_id"] = nil
		} else {
			m["email_id"] = c.EmailID.MustGet()
		}
	}
	if c.SMTPConfigurationID.IsSpecified() {
		if c.SMTPConfigurationID.IsNull() {
			m["smtp_configuration_id"] = nil
		} else {
			m["smtp_configuration_id"] = c.SMTPConfigurationID.MustGet()
		}
	}
	if c.APISenderID.IsSpecified() {
		if c.APISenderID.IsNull() {
			m["api_sender_id"] = nil
		} else {
			m["api_sender_id"] = c.APISenderID.MustGet()
		}
	}

	if c.CompanyID.IsSpecified() {
		if c.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = c.CompanyID.MustGet()
		}
	}
	if v, err := c.URLIdentifierID.Get(); err == nil {
		m["url_identifier_id"] = v
	}
	if v, err := c.StateIdentifierID.Get(); err == nil {
		m["state_identifier_id"] = v
	}
	if v, err := c.URLPath.Get(); err == nil {
		m["url_path"] = v.String()
	}

	_, errDomain := c.DomainID.Get()
	_, errSMTP := c.SMTPConfigurationID.Get()
	_, errAPISender := c.APISenderID.Get()
	_, errEmail := c.EmailID.Get()
	_, errLandingPage := c.LandingPageID.Get()

	m["is_usable"] = errDomain == nil &&
		errEmail == nil &&
		errLandingPage == nil &&
		(errSMTP == nil || errAPISender == nil)

	return m
}
