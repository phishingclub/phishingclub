package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/vo"
)

type CampaignEvent struct {
	ID           *uuid.UUID            `json:"id"`
	CreatedAt    *time.Time            `json:"createdAt"`
	CampaignID   *uuid.UUID            `json:"campaignID"`
	IP           *vo.OptionalString64  `json:"ip"`
	UserAgent    *vo.OptionalString255 `json:"userAgent"`
	Data         *vo.OptionalString1MB `json:"data"`
	Metadata     *vo.OptionalString1MB `json:"metadata"`
	AnonymizedID *uuid.UUID            `json:"anonymizedID"`
	// if null the recipient has been anonymized
	RecipientID *uuid.UUID `json:"recipientID"`
	Recipient   *Recipient `json:"recipient,omitempty"`
	EventID     *uuid.UUID `json:"eventID"`
}
