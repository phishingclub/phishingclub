package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Company is a company
type Company struct {
	ID        nullable.Nullable[uuid.UUID]   `json:"id"`
	CreatedAt *time.Time                     `json:"createdAt"`
	UpdatedAt *time.Time                     `json:"updatedAt"`
	Name      nullable.Nullable[vo.String64] `json:"name"`
}

// Validate checks if the Company configuration with a valid state
func (c *Company) Validate() error {
	if err := validate.NullableFieldRequired("name", c.Name); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (c *Company) ToDBMap() map[string]any {
	m := map[string]any{}
	if c.Name.IsSpecified() {
		m["name"] = nil
		if name, err := c.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	return m
}
