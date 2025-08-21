package model

// RecipientView extends Recipient with additional presentation fields
type RecipientView struct {
	*Recipient            // Embed the base Recipient model
	IsRepeatOffender bool `json:"isRepeatOffender"`
}

// NewRecipientView creates a RecipientView from a Recipient
func NewRecipientView(r *Recipient) *RecipientView {
	return &RecipientView{
		Recipient:        r,
		IsRepeatOffender: false,
	}
}
