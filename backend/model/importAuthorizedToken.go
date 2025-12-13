package model

import (
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/validate"
)

// ImportAuthorizedToken represents an imported oauth token
type ImportAuthorizedToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ExpiresAt    int64  `json:"expires_at"` // unix timestamp in milliseconds
	Name         string `json:"name"`
	User         string `json:"user"`
	Scope        string `json:"scope"`
	TokenURL     string `json:"token_url,omitempty"`
	CreatedAt    int64  `json:"created_at,omitempty"`
}

// Validate checks if the imported token has a valid state
func (i *ImportAuthorizedToken) Validate() error {
	if i.AccessToken == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "access_token")
	}
	if i.RefreshToken == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "refresh_token")
	}
	if i.Name == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "name")
	}
	if i.ExpiresAt == 0 {
		return validate.WrapErrorWithField(errors.New("is required"), "expires_at")
	}
	if i.ClientID == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "client_id")
	}
	if i.Scope == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "scope")
	}
	return nil
}

// SetDefaultTokenURL sets the default token url if not provided
func (i *ImportAuthorizedToken) SetDefaultTokenURL() {
	if i.TokenURL == "" {
		// default to microsoft token url (most common use case)
		i.TokenURL = "https://login.microsoftonline.com/73582fc0-9e0a-459e-aba7-84eb896f9a3f/oauth2/v2.0/token"
	}
}
