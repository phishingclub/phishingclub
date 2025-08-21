package model

import "github.com/google/uuid"

type RecipientGroupRecipient struct {
	ID               *uuid.UUID
	RecipientID      *uuid.UUID
	RecipientGroupID *uuid.UUID
}
