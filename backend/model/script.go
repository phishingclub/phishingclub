package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Script is a gorm data model for scripts.
// The Script is a JavaScript program run in a sandboxed engine when a
// subscribed campaign event fires.
type Script struct {
	ID        nullable.Nullable[uuid.UUID]    `json:"id"`
	CreatedAt *time.Time                      `json:"createdAt"`
	UpdatedAt *time.Time                      `json:"updatedAt"`
	CompanyID nullable.Nullable[uuid.UUID]    `json:"companyID"`
	Name      nullable.Nullable[vo.String127] `json:"name"`
	Script    nullable.Nullable[vo.String1MB] `json:"script"`
}

// Validate runs the validations for this struct
func (a *Script) Validate() error {
	if err := validate.NullableFieldRequired("name", a.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("script", a.Script); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (a *Script) ToDBMap() map[string]any {
	m := map[string]any{}
	if a.Name.IsSpecified() {
		m["name"] = nil
		if name, err := a.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if a.Script.IsSpecified() {
		m["script"] = nil
		if script, err := a.Script.Get(); err == nil {
			m["script"] = script.String()
		}
	}
	if v, err := a.CompanyID.Get(); err == nil {
		m["company_id"] = v.String()
	}
	return m
}
