package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Webhook is a gorm data model for webhooks
type Webhook struct {
	ID        nullable.Nullable[uuid.UUID]             `json:"id"`
	CreatedAt *time.Time                               `json:"createdAt"`
	UpdatedAt *time.Time                               `json:"updatedAt"`
	CompanyID nullable.Nullable[uuid.UUID]             `json:"companyID"`
	Name      nullable.Nullable[vo.String127]          `json:"name"`
	URL       nullable.Nullable[vo.String1024]         `json:"url"`
	Secret    nullable.Nullable[vo.OptionalString1024] `json:"secret"`
}

// Validate runs the validations for this struct
func (w *Webhook) Validate() error {
	if err := validate.NullableFieldRequired("name", w.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("secret", w.URL); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (w *Webhook) ToDBMap() map[string]any {
	m := map[string]any{}
	if w.Name.IsSpecified() {
		m["name"] = nil
		if name, err := w.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if w.URL.IsSpecified() {
		m["url"] = nil
		if url, err := w.URL.Get(); err == nil {
			m["url"] = url.String()
		}
	}
	if w.Secret.IsSpecified() {
		m["secret"] = nil
		if secret, err := w.Secret.Get(); err == nil {
			m["secret"] = secret.String()
		}
	}
	if v, err := w.CompanyID.Get(); err == nil {
		m["company_id"] = v.String()
	}
	return m
}
