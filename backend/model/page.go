package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Page is a Page
type Page struct {
	ID        nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt *time.Time                              `json:"createdAt"`
	UpdatedAt *time.Time                              `json:"updatedAt"`
	CompanyID nullable.Nullable[uuid.UUID]            `json:"companyID"`
	Name      nullable.Nullable[vo.String64]          `json:"name"`
	Content   nullable.Nullable[vo.OptionalString1MB] `json:"content"`

	Company *Company `json:"-"`
}

// Validate checks if the page has a valid state
func (p *Page) Validate() error {
	if err := validate.NullableFieldRequired("name", p.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("content", p.Content); err != nil {
		return err
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
	if p.CompanyID.IsSpecified() {
		if p.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = p.CompanyID.MustGet()
		}
	}
	return m
}
