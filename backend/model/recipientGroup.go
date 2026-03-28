package model

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

const RECIPIENT_COUNT_NOT_LOADED = int64(-1)
const RECIPIENT_GROUP_COUNT_NOT_LOADED = int64(-1)

// DynamicGroupAllowedFields are the recipient fields a dynamic group may filter on
var DynamicGroupAllowedFields = []string{
	"position",
	"department",
	"city",
	"country",
	"misc",
}

// RecipientGroup is an entity for recipient group
type RecipientGroup struct {
	ID        nullable.Nullable[uuid.UUID]    `json:"id"`
	CreatedAt *time.Time                      `json:"createdAt"`
	UpdatedAt *time.Time                      `json:"updatedAt"`
	Name      nullable.Nullable[vo.String127] `json:"name"`
	CompanyID nullable.Nullable[uuid.UUID]    `json:"companyID"`

	// IsDynamic indicates members are resolved at query time via FilterField/FilterValue
	IsDynamic   nullable.Nullable[bool]   `json:"isDynamic"`
	FilterField nullable.Nullable[string] `json:"filterField"`
	FilterValue nullable.Nullable[string] `json:"filterValue"`

	Recipients             []*Recipient             `json:"-"`
	IsRecipientsLoaded     bool                     `json:"-"`
	RecipientCount         nullable.Nullable[int64] `json:"recipientCount"`
	IsRecipientCountLoaded bool                     `json:"-"`
	Company                *Company                 `json:"-"`
}

// Validate checks if the recipient group has a valid state
func (rg *RecipientGroup) Validate() error {
	if err := validate.NullableFieldRequired("name", rg.Name); err != nil {
		return err
	}
	return nil
}

// ValidateDynamic checks that a dynamic group has valid filter fields set,
// that filterField is in the allowed list, and that filterValue is a
// non-empty, non-whitespace string of at most 255 characters.
func (rg *RecipientGroup) ValidateDynamic() error {
	if err := validate.NullableFieldRequired("filterField", rg.FilterField); err != nil {
		return err
	}
	ff := rg.FilterField.MustGet()
	if err := validate.ErrorIfNotContains(DynamicGroupAllowedFields, ff); err != nil {
		return validate.WrapErrorWithField(err, "filterField")
	}

	if err := validate.NullableFieldRequired("filterValue", rg.FilterValue); err != nil {
		return err
	}
	fv := strings.TrimSpace(rg.FilterValue.MustGet())
	if err := validate.ErrorIfStringEmpty(fv); err != nil {
		return validate.WrapErrorWithField(err, "filterValue")
	}
	if err := validate.ErrorIfStringGreaterThan(fv, 255); err != nil {
		return validate.WrapErrorWithField(err, "filterValue")
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (rg *RecipientGroup) ToDBMap() map[string]any {
	m := map[string]any{}
	if rg.Name.IsSpecified() {
		name := rg.Name.MustGet()
		m["name"] = name.String()
	}
	if rg.CompanyID.IsSpecified() {
		if rg.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = rg.CompanyID.MustGet()
		}
	}
	if rg.IsDynamic.IsSpecified() {
		m["is_dynamic"] = rg.IsDynamic.MustGet()
	}
	if rg.FilterField.IsSpecified() {
		m["filter_field"] = rg.FilterField.MustGet()
	}
	if rg.FilterValue.IsSpecified() {
		m["filter_value"] = rg.FilterValue.MustGet()
	}
	return m
}
