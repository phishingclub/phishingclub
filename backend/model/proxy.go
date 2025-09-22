package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Proxy is a proxy configuration
type Proxy struct {
	ID          nullable.Nullable[uuid.UUID]             `json:"id"`
	CreatedAt   *time.Time                               `json:"createdAt"`
	UpdatedAt   *time.Time                               `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID]             `json:"companyID"`
	Name        nullable.Nullable[vo.String64]           `json:"name"`
	Description nullable.Nullable[vo.OptionalString1024] `json:"description"`
	StartURL    nullable.Nullable[vo.String1024]         `json:"startURL"`
	ProxyConfig nullable.Nullable[vo.String1MB]          `json:"proxyConfig"`

	Company *Company `json:"-"`
}

// Validate checks if the Proxy has a valid state
func (m *Proxy) Validate() error {
	if err := validate.NullableFieldRequired("name", m.Name); err != nil {
		return err
	}

	if err := validate.NullableFieldRequired("startURL", m.StartURL); err != nil {
		return err
	}

	if err := validate.NullableFieldRequired("proxyConfig", m.ProxyConfig); err != nil {
		return err
	}

	// validate start URL format
	startURL, err := m.StartURL.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("start URL is required"), "startURL")
	}

	startURLStr := startURL.String()
	if startURLStr == "" {
		return validate.WrapErrorWithField(errors.New("start URL cannot be empty"), "startURL")
	}

	// validate that start URL is a valid, full URL
	if err := validate.ErrorIfInvalidURL(startURLStr); err != nil {
		return validate.WrapErrorWithField(err, "startURL")
	}

	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (m *Proxy) ToDBMap() map[string]any {
	dbMap := map[string]any{}
	if m.Name.IsSpecified() {
		dbMap["name"] = nil
		if name, err := m.Name.Get(); err == nil {
			dbMap["name"] = name.String()
		}
	}
	if m.Description.IsSpecified() {
		dbMap["description"] = nil
		if description, err := m.Description.Get(); err == nil {
			dbMap["description"] = description.String()
		}
	}
	if m.StartURL.IsSpecified() {
		dbMap["start_url"] = nil
		if startURL, err := m.StartURL.Get(); err == nil {
			dbMap["start_url"] = startURL.String()
		}
	}
	if m.ProxyConfig.IsSpecified() {
		dbMap["proxy_config"] = nil
		if proxyConfig, err := m.ProxyConfig.Get(); err == nil {
			dbMap["proxy_config"] = proxyConfig.String()
		}
	}
	if m.CompanyID.IsSpecified() {
		if m.CompanyID.IsNull() {
			dbMap["company_id"] = nil
		} else {
			dbMap["company_id"] = m.CompanyID.MustGet()
		}
	}
	return dbMap
}

// ProxyOverview is a subset of the Proxy as used as read-only
type ProxyOverview struct {
	ID          uuid.UUID  `json:"id,omitempty"`
	CreatedAt   *time.Time `json:"createdAt"`
	UpdatedAt   *time.Time `json:"updatedAt"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	StartURL    string     `json:"startURL"`
	CompanyID   *uuid.UUID `json:"companyID"`
}
