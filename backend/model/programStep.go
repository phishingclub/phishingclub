package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
)

// ProgramStep is one ordered item in a program's content list.
type ProgramStep struct {
	ID                 nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt          *time.Time                   `json:"createdAt"`
	UpdatedAt          *time.Time                   `json:"updatedAt"`
	ProgramID          nullable.Nullable[uuid.UUID] `json:"programID"`
	CampaignTemplateID nullable.Nullable[uuid.UUID] `json:"campaignTemplateID"`
	Position           nullable.Nullable[int]       `json:"position"`

	CampaignTemplate *CampaignTemplate `json:"campaignTemplate"`
}

// Validate checks the step references a template.
func (s *ProgramStep) Validate() error {
	if err := validate.NullableFieldRequired("campaignTemplateID", s.CampaignTemplateID); err != nil {
		return err
	}
	return nil
}
