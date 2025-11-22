package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/vo"
)

// OAuthState represents a temporary state token for oauth flow
type OAuthState struct {
	ID        nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt *time.Time                   `json:"createdAt"`

	// the state token sent to oauth provider
	StateToken nullable.Nullable[vo.String255] `json:"stateToken"`

	// the oauth provider this state is for
	OAuthProviderID nullable.Nullable[uuid.UUID] `json:"oauthProviderID"`
	OAuthProvider   *OAuthProvider               `json:"oauthProvider"`

	// expiration
	ExpiresAt *time.Time `json:"expiresAt"`

	// whether this state token has been used
	Used   bool       `json:"used"`
	UsedAt *time.Time `json:"usedAt"`
}

// OAuthStateFromDB converts database model to model
func OAuthStateFromDB(db *database.OAuthState) *OAuthState {
	if db == nil {
		return nil
	}

	stateToken, err := vo.NewString255(db.StateToken)
	if err != nil {
		// fallback to empty if token is invalid (should not happen)
		stateToken = vo.NewString255Must("")
	}

	state := &OAuthState{
		ID:              nullable.NewNullableWithValue(db.ID),
		CreatedAt:       db.CreatedAt,
		StateToken:      nullable.NewNullableWithValue(*stateToken),
		OAuthProviderID: nullable.NewNullableWithValue(db.OAuthProviderID),
		ExpiresAt:       db.ExpiresAt,
		Used:            db.Used,
		UsedAt:          db.UsedAt,
	}

	return state
}
