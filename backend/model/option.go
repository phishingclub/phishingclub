package model

import (
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

// Option is an Option
type Option struct {
	ID    nullable.Nullable[uuid.UUID] `json:"id"`
	Key   vo.String64                  `json:"key"`
	Value vo.OptionalString1MB         `json:"value"`
}
