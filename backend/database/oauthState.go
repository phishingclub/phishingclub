package database

import (
	"time"

	"github.com/google/uuid"
)

const (
	OAUTH_STATE_TABLE = "oauth_states"
)

// OAuthState stores temporary state tokens for oauth flows
// used for csrf protection
type OAuthState struct {
	ID        uuid.UUID  `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`

	// the state token sent to oauth provider (random cryptographic token)
	StateToken string `gorm:"not null;uniqueIndex;type:varchar(255);"`

	// the oauth provider this state is for
	OAuthProviderID uuid.UUID      `gorm:"not null;index;type:uuid"`
	OAuthProvider   *OAuthProvider `gorm:"foreignkey:OAuthProviderID;"`

	// expiration (state tokens expire after 10 minutes)
	ExpiresAt *time.Time `gorm:"not null;index;"`

	// whether this state token has been used (prevent replay attacks)
	Used   bool       `gorm:"not null;default:false;index;"`
	UsedAt *time.Time `gorm:"index;"`

	// outcome of the flow, empty while pending, else success or failed.
	// The admin page polls it when the callback runs on another origin.
	Result string `gorm:"not null;default:'';type:varchar(16);"`
}

// oauth state results
const (
	OAUTH_STATE_RESULT_SUCCESS = "success"
	OAUTH_STATE_RESULT_FAILED  = "failed"
)

func (OAuthState) TableName() string {
	return OAUTH_STATE_TABLE
}
