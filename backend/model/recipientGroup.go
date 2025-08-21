package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

const RECIPIENT_COUNT_NOT_LOADED = int64(-1)
const RECIPIENT_GROUP_COUNT_NOT_LOADED = int64(-1)

// RecipientGroup is an entity for recipient group
type RecipientGroup struct {
	ID        nullable.Nullable[uuid.UUID]    `json:"id"`
	CreatedAt *time.Time                      `json:"createdAt"`
	UpdatedAt *time.Time                      `json:"updatedAt"`
	Name      nullable.Nullable[vo.String127] `json:"name"`
	CompanyID nullable.Nullable[uuid.UUID]    `json:"companyID"`

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
	return m
}
