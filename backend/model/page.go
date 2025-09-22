package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Page is a Page
type Page struct {
	ID          nullable.Nullable[uuid.UUID]             `json:"id"`
	CreatedAt   *time.Time                               `json:"createdAt"`
	UpdatedAt   *time.Time                               `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID]             `json:"companyID"`
	Name        nullable.Nullable[vo.String64]           `json:"name"`
	Content     nullable.Nullable[vo.OptionalString1MB]  `json:"content"`
	Type        nullable.Nullable[vo.String32]           `json:"type"`        // "regular" or "proxy"
	TargetURL   nullable.Nullable[vo.OptionalString1024] `json:"targetURL"`   // target url for proxy pages
	ProxyConfig nullable.Nullable[vo.OptionalString1MB]  `json:"proxyConfig"` // yaml configuration for proxy

	Company *Company `json:"-"`
}

// Validate checks if the page has a valid state
func (p *Page) Validate() error {
	if err := validate.NullableFieldRequired("name", p.Name); err != nil {
		return err
	}

	// set default type if not specified
	if !p.Type.IsSpecified() {
		p.Type.Set(*vo.NewString32Must("regular"))
	}

	pageType, err := p.Type.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("type is required"), "type")
	}

	// validate type is either "regular" or "proxy"
	if pageType.String() != "regular" && pageType.String() != "proxy" {
		return validate.WrapErrorWithField(errors.New("type must be 'regular' or 'proxy'"), "type")
	}

	if pageType.String() == "proxy" {
		// proxy pages require targetURL and proxyConfig
		if err := validate.NullableFieldRequired("targetURL", p.TargetURL); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("proxyConfig", p.ProxyConfig); err != nil {
			return err
		}
	} else {
		// regular pages require content
		if err := validate.NullableFieldRequired("content", p.Content); err != nil {
			return err
		}
	}

	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (p *Page) ToDBMap() map[string]any {
	m := map[string]any{}
	if p.Name.IsSpecified() {
		m["name"] = nil
		if name, err := p.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if p.Content.IsSpecified() {
		m["content"] = nil
		if content, err := p.Content.Get(); err == nil {
			m["content"] = content.String()
		}
	}
	if p.Type.IsSpecified() {
		m["type"] = "regular"
		if pageType, err := p.Type.Get(); err == nil {
			m["type"] = pageType.String()
		}
	}
	if p.TargetURL.IsSpecified() {
		m["target_url"] = nil
		if targetURL, err := p.TargetURL.Get(); err == nil {
			m["target_url"] = targetURL.String()
		}
	}
	if p.ProxyConfig.IsSpecified() {
		m["proxy_config"] = nil
		if proxyConfig, err := p.ProxyConfig.Get(); err == nil {
			m["proxy_config"] = proxyConfig.String()
		}
	}
	if p.CompanyID.IsSpecified() {
		if p.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = p.CompanyID.MustGet()
		}
	}
	return m
}
