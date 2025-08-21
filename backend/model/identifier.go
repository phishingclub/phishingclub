package model

import (
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
)

type Identifier struct {
	ID   nullable.Nullable[*uuid.UUID] `json:"id"`
	Name nullable.Nullable[string]     `json:"name"`
}

func (i *Identifier) Validate() error {
	if err := validate.NullableFieldRequired("name", i.Name); err != nil {
		return err
	}
	return nil
}

func (i *Identifier) ToDBMap() map[string]any {
	m := make(map[string]any)
	if v, err := i.Name.Get(); err == nil {
		m["name"] = v
	}
	return m
}
