package model

import (
	"fmt"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/lure"
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

	// before landing page can also be a proxy
	BeforeLandingProxyID nullable.Nullable[uuid.UUID] `json:"beforeLandingProxyID"`
	BeforeLandingProxy   *Proxy                       `json:"beforeLandingProxy"`

	LandingPageID nullable.Nullable[uuid.UUID] `json:"landingPageID"`
	LandingPage   *Page                        `json:"landingPage"`

	// landing page can also be a proxy
	LandingProxyID nullable.Nullable[uuid.UUID] `json:"landingProxyID"`
	LandingProxy   *Proxy                       `json:"landingProxy"`

	AfterLandingPageID nullable.Nullable[uuid.UUID] `json:"afterLandingPageID"`
	AfterLandingPage   *Page                        `json:"afterLandingPage"`

	// after landing page can also be a proxy
	AfterLandingProxyID nullable.Nullable[uuid.UUID] `json:"afterLandingProxyID"`
	AfterLandingProxy   *Proxy                       `json:"afterLandingProxy"`

	AfterLandingPageRedirectURL nullable.Nullable[vo.OptionalString255] `json:"afterLandingPageRedirectURL"`

	URLIdentifierID nullable.Nullable[*uuid.UUID] `json:"urlIdentifierID"`
	URLIdentifier   *Identifier                   `json:"urlIdentifier"`

	StateIdentifierID nullable.Nullable[*uuid.UUID] `json:"stateIdentifierID"`
	StateIdentifier   *Identifier                   `json:"stateIdentifier"`

	URLPath nullable.Nullable[vo.URLPath] `json:"urlPath"`

	// defaults, snapshotted onto a campaign when it is scheduled, so editing them
	// here never changes a campaign already running.
	LureURLMode    nullable.Nullable[string] `json:"lureURLMode"`
	LureCodeAlgo   nullable.Nullable[string] `json:"lureCodeAlgo"`
	LureCodeLength nullable.Nullable[int]    `json:"lureCodeLength"`

	EmailID nullable.Nullable[uuid.UUID] `json:"emailID"`
	Email   *Email                       `json:"email"`

	SMTPConfigurationID nullable.Nullable[uuid.UUID] `json:"smtpConfigurationID"`
	SMTPConfiguration   *SMTPConfiguration           `json:"smtpConfiguration"`

	APISenderID nullable.Nullable[uuid.UUID] `json:"apiSenderID"`
	APISender   *APISender                   `json:"apiSender"`

	CompanyID nullable.Nullable[uuid.UUID] `json:"companyID"`
	Company   *Company                     `json:"company"`

	IsUsable nullable.Nullable[bool] `json:"isUsable"`

	IsTraining nullable.Nullable[bool] `json:"isTraining"`
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
	// URLPath is optional, no validation needed

	if v, err := c.LureURLMode.Get(); err == nil && !data.IsValidLureURLMode(v) {
		return errs.NewValidationError(
			errors.New("lure URL mode must be query or path"),
		)
	}
	if v, err := c.LureCodeAlgo.Get(); err == nil && !lure.IsValidAlgorithm(lure.Algorithm(v)) {
		return errs.NewValidationError(
			errors.New("unknown lure code algorithm"),
		)
	}
	if v, err := c.LureCodeLength.Get(); err == nil && (v < lure.MinLength || v > lure.MaxLength) {
		return errs.NewValidationError(
			fmt.Errorf("lure code length must be between %d and %d", lure.MinLength, lure.MaxLength),
		)
	}

	// validate that only one type is set per stage
	// before landing page: can have neither (optional), or one type, but not both
	_, errBeforePage := c.BeforeLandingPageID.Get()
	_, errBeforeProxy := c.BeforeLandingProxyID.Get()
	if errBeforePage == nil && errBeforeProxy == nil {
		return errs.NewValidationError(
			errors.New("before landing page cannot be both a page and a proxy"),
		)
	}

	// landing page: must have exactly one type (required)
	_, errLandingPage := c.LandingPageID.Get()
	_, errLandingProxy := c.LandingProxyID.Get()
	if errLandingPage == nil && errLandingProxy == nil {
		return errs.NewValidationError(
			errors.New("landing page cannot be both a page and a proxy"),
		)

	}
	if errLandingPage != nil && errLandingProxy != nil {
		return errs.NewValidationError(
			errors.New("landing page is required (must be either a page or a proxy)"),
		)
	}

	// after landing page: can have neither (optional), or one type, but not both
	_, errAfterPage := c.AfterLandingPageID.Get()
	_, errAfterProxy := c.AfterLandingProxyID.Get()
	if errAfterPage == nil && errAfterProxy == nil {
		return errs.NewValidationError(
			errors.New("after landing page cannot be both a page and a proxy"),
		)
	}
	// the after landing page is what marks a training as completed, without it
	// there is nothing to record completion on
	if v, err := c.IsTraining.Get(); err == nil && v {
		if errAfterPage != nil && errAfterProxy != nil {
			return errs.NewValidationError(
				errors.New("after landing page is required for awareness training"),
			)
		}
	}

	// validate that smtp configuration and api sender are mutually exclusive
	_, errSMTP := c.SMTPConfigurationID.Get()
	_, errAPISender := c.APISenderID.Get()
	if errSMTP == nil && errAPISender == nil {
		return errs.NewValidationError(
			errors.New("smtp configuration and api sender cannot both be set"),
		)
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

	if c.BeforeLandingProxyID.IsSpecified() {
		if c.BeforeLandingProxyID.IsNull() {
			m["before_landing_proxy_id"] = nil
		} else {
			m["before_landing_proxy_id"] = c.BeforeLandingProxyID.MustGet()
		}
	}

	if c.LandingPageID.IsSpecified() {
		if c.LandingPageID.IsNull() {
			m["landing_page_id"] = nil
		} else {
			m["landing_page_id"] = c.LandingPageID.MustGet()
		}
	}

	if c.LandingProxyID.IsSpecified() {
		if c.LandingProxyID.IsNull() {
			m["landing_proxy_id"] = nil
		} else {
			m["landing_proxy_id"] = c.LandingProxyID.MustGet()
		}
	}

	if c.AfterLandingPageID.IsSpecified() {
		if c.AfterLandingPageID.IsNull() {
			m["after_landing_page_id"] = nil
		} else {
			m["after_landing_page_id"] = c.AfterLandingPageID.MustGet()
		}
	}

	if c.AfterLandingProxyID.IsSpecified() {
		if c.AfterLandingProxyID.IsNull() {
			m["after_landing_proxy_id"] = nil
		} else {
			m["after_landing_proxy_id"] = c.AfterLandingProxyID.MustGet()
		}
	}
	if c.AfterLandingPageRedirectURL.IsSpecified() {
		if c.AfterLandingPageRedirectURL.IsNull() {
			m["after_landing_page_redirect_url"] = ""
		} else {
			if v, err := c.AfterLandingPageRedirectURL.Get(); err == nil {
				m["after_landing_page_redirect_url"] = v.String()
			} else {
				m["after_landing_page_redirect_url"] = ""
			}
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
	if c.URLPath.IsSpecified() {
		if c.URLPath.IsNull() {
			m["url_path"] = ""
		} else {
			if v, err := c.URLPath.Get(); err == nil {
				m["url_path"] = v.String()
			} else {
				m["url_path"] = ""
			}
		}
	}
	if c.LureURLMode.IsSpecified() {
		m["lure_url_mode"] = data.LureURLModeQuery
		if v, err := c.LureURLMode.Get(); err == nil && data.IsValidLureURLMode(v) {
			m["lure_url_mode"] = v
		}
	}
	if c.LureCodeAlgo.IsSpecified() {
		m["lure_code_algo"] = string(lure.DefaultAlgorithm)
		if v, err := c.LureCodeAlgo.Get(); err == nil && lure.IsValidAlgorithm(lure.Algorithm(v)) {
			m["lure_code_algo"] = v
		}
	}
	if c.LureCodeLength.IsSpecified() {
		m["lure_code_length"] = lure.DefaultLength
		if v, err := c.LureCodeLength.Get(); err == nil && v >= lure.MinLength && v <= lure.MaxLength {
			m["lure_code_length"] = v
		}
	}

	if c.IsTraining.IsSpecified() {
		m["is_training"] = false
		if v, err := c.IsTraining.Get(); err == nil {
			m["is_training"] = v
		}
	}

	_, errDomain := c.DomainID.Get()
	_, errSMTP := c.SMTPConfigurationID.Get()
	_, errAPISender := c.APISenderID.Get()
	_, errEmail := c.EmailID.Get()
	_, errLandingPage := c.LandingPageID.Get()
	_, errLandingProxy := c.LandingProxyID.Get()

	// landing page is required (either page or proxy)
	hasLanding := errLandingPage == nil || errLandingProxy == nil

	m["is_usable"] = errDomain == nil &&
		errEmail == nil &&
		hasLanding &&
		(errSMTP == nil || errAPISender == nil)

	return m
}
