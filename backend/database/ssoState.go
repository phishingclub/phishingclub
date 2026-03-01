package database

import "time"

const (
	SSO_STATE_TABLE = "sso_states"
)

// SSOState stores short-lived state tokens for the generic OIDC login flow.
// Each record ties together the CSRF state token, the PKCE code verifier and
// an optional nonce so that the callback handler can verify the round-trip and
// complete the token exchange securely.
type SSOState struct {
	// ID is a random UUID primary key.
	ID string `gorm:"primary_key;not null;unique;type:varchar(36)"`

	// StateToken is the value sent as the OAuth2 'state' parameter.
	// It is compared on callback to prevent CSRF attacks.
	StateToken string `gorm:"not null;uniqueIndex;type:varchar(255)"`

	// CodeVerifier is the plain-text PKCE verifier (RFC 7636).
	// The SHA-256 hash of this value was sent to the provider as the
	// code_challenge during the authorization request.
	CodeVerifier string `gorm:"not null;type:varchar(255)"`

	// Nonce is the OIDC nonce embedded in the id_token claim.
	// It is verified after token exchange to prevent replay attacks.
	Nonce string `gorm:"not null;type:varchar(255)"`

	// CreatedAt is the creation timestamp.
	CreatedAt *time.Time `gorm:"not null;index"`

	// ExpiresAt is when this record should be considered invalid.
	// State tokens are valid for 10 minutes.
	ExpiresAt *time.Time `gorm:"not null;index"`

	// Used marks the token as consumed so it cannot be replayed.
	Used bool `gorm:"not null;default:false;index"`

	// UsedAt is the time the token was consumed.
	UsedAt *time.Time `gorm:"index"`
}

// TableName returns the table name for gorm.
func (SSOState) TableName() string {
	return SSO_STATE_TABLE
}
