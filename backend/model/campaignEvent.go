package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/vo"
)

type CampaignEvent struct {
	ID         *uuid.UUID            `json:"id"`
	CreatedAt  *time.Time            `json:"createdAt"`
	CampaignID *uuid.UUID            `json:"campaignID"`
	Campaign   *Campaign             `json:"campaign,omitempty"`
	IP         *vo.OptionalString64  `json:"ip"`
	UserAgent  *vo.OptionalString255 `json:"userAgent"`
	Data       *vo.OptionalString1MB `json:"data"`
	Metadata   *vo.OptionalString1MB `json:"metadata"`
	// AnonymizedID is the pseudonym that links an anonymous campaign's events to one
	// another and to the recipient row. It is an internal join key and must never be
	// serialized: exposing it would let a reader group a pseudonym's events into a
	// per-person journey and re-identify it by matching the send event to a
	// recipient. Stats and grouping read it server-side, off the database.
	AnonymizedID *uuid.UUID `json:"-"`
	// if null the recipient has been anonymized
	RecipientID *uuid.UUID `json:"recipientID"`
	Recipient   *Recipient `json:"recipient,omitempty"`
	EventID     *uuid.UUID `json:"eventID"`
}

// Anonymize strips identity from an event and stamps the stable pseudonym in its
// place. The event stays countable and groupable through the pseudonym but
// carries no recipient link, ip, user agent, submitted data or metadata. Used by
// anonymous campaigns, where every event is written this way from the outset.
func (e *CampaignEvent) Anonymize(anonymizedID *uuid.UUID) {
	e.RecipientID = nil
	e.AnonymizedID = anonymizedID
	e.IP = vo.NewEmptyOptionalString64()
	e.UserAgent = vo.NewEmptyOptionalString255()
	e.Data = vo.NewEmptyOptionalString1MB()
	e.Metadata = vo.NewEmptyOptionalString1MB()
}
