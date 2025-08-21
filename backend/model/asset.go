package model

import (
	"mime/multipart"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Asset is a file Asset entity
type Asset struct {
	ID          nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt   *time.Time                              `json:"createdAt"`
	UpdatedAt   *time.Time                              `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID]            `json:"companyID"`
	DomainName  nullable.Nullable[vo.String255]         `json:"domainName"`
	DomainID    nullable.Nullable[uuid.UUID]            `json:"domainID"`
	Name        nullable.Nullable[vo.OptionalString127] `json:"name"`
	Description nullable.Nullable[vo.OptionalString255] `json:"description"`
	Path        nullable.Nullable[vo.RelativeFilePath]  `json:"path"`
	File        multipart.FileHeader                    `json:"-"`
}

// Validate checks if the Asset has a valid state
func (a *Asset) Validate() error {
	if err := validate.NullableFieldRequired("name", a.Name); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (a *Asset) ToDBMap() map[string]any {
	m := map[string]any{}
	if a.CompanyID.IsSpecified() {
		if a.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = a.CompanyID.MustGet()
		}
	}
	if a.DomainName.IsSpecified() {
		m["domain_name"] = nil
		if domainName, err := a.DomainName.Get(); err == nil {
			m["domain_name"] = domainName.String()
		}
	}
	// TODO is a global asset attached to a domain? if not then this should be possible to set to null like company ID
	if a.DomainID.IsSpecified() {
		m["domain_id"] = nil
		if domainID, err := a.DomainID.Get(); err == nil {
			m["domain_id"] = domainID.String()
		}
	}
	if a.Name.IsSpecified() {
		m["name"] = nil
		if name, err := a.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if a.Description.IsSpecified() {
		m["description"] = nil
		if description, err := a.Description.Get(); err == nil {
			m["description"] = description.String()
		}
	}
	if a.Path.IsSpecified() {
		m["path"] = nil
		if path, err := a.Path.Get(); err == nil {
			m["path"] = path.String()
		}
	}
	return m
}
