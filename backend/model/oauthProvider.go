package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// OAuthProvider is a user-configured OAuth 2.0 provider
type OAuthProvider struct {
	ID        nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt *time.Time                   `json:"createdAt"`
	UpdatedAt *time.Time                   `json:"updatedAt"`

	Name nullable.Nullable[vo.String127] `json:"name"`

	// oauth endpoints (user configurable)
	AuthURL  nullable.Nullable[vo.String512] `json:"authURL"`
	TokenURL nullable.Nullable[vo.String512] `json:"tokenURL"`
	Scopes   nullable.Nullable[vo.String512] `json:"scopes"`

	// user's oauth app credentials
	ClientID     nullable.Nullable[vo.String255]         `json:"clientID"`
	ClientSecret nullable.Nullable[vo.OptionalString255] `json:"clientSecret"` // write-only, never returned

	// current token state (stored as plain text like smtp passwords)
	AccessToken    nullable.Nullable[vo.OptionalString1MB] `json:"-"` // never returned in api
	RefreshToken   nullable.Nullable[vo.OptionalString1MB] `json:"-"` // never returned in api
	TokenExpiresAt *time.Time                              `json:"tokenExpiresAt"`

	// authorization metadata
	AuthorizedEmail nullable.Nullable[vo.OptionalString255] `json:"authorizedEmail"` // email of the account that authorized
	AuthorizedAt    *time.Time                              `json:"authorizedAt"`

	// status
	IsAuthorized nullable.Nullable[bool] `json:"isAuthorized"` // whether oauth flow completed

	// indicates if this provider was created via import (with pre-authorized tokens)
	// imported providers cannot be authorized/reauthorized via oauth flow
	IsImported nullable.Nullable[bool] `json:"isImported"`

	CompanyID nullable.Nullable[uuid.UUID] `json:"companyID"`
	Company   *Company                     `json:"company"`
}

// Validate checks if the oauth provider has a valid state
func (o *OAuthProvider) Validate() error {
	if err := validate.NullableFieldRequired("name", o.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("authURL", o.AuthURL); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("tokenURL", o.TokenURL); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("scopes", o.Scopes); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("clientID", o.ClientID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("clientSecret", o.ClientSecret); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
func (o *OAuthProvider) ToDBMap() map[string]any {
	m := map[string]any{}

	if o.Name.IsSpecified() {
		m["name"] = nil
		if name, err := o.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}

	if o.AuthURL.IsSpecified() {
		m["auth_url"] = nil
		if authURL, err := o.AuthURL.Get(); err == nil {
			m["auth_url"] = authURL.String()
		}
	}

	if o.TokenURL.IsSpecified() {
		m["token_url"] = nil
		if tokenURL, err := o.TokenURL.Get(); err == nil {
			m["token_url"] = tokenURL.String()
		}
	}

	if o.Scopes.IsSpecified() {
		m["scopes"] = nil
		if scopes, err := o.Scopes.Get(); err == nil {
			m["scopes"] = scopes.String()
		}
	}

	if o.ClientID.IsSpecified() {
		m["client_id"] = nil
		if clientID, err := o.ClientID.Get(); err == nil {
			m["client_id"] = clientID.String()
		}
	}

	if o.ClientSecret.IsSpecified() {
		if o.ClientSecret.IsNull() {
			// don't update client secret if null
		} else {
			if v, err := o.ClientSecret.Get(); err == nil {
				// only update if non-empty
				if v.String() != "" {
					m["client_secret"] = v.String()
				}
			}
		}
	}

	if o.AccessToken.IsSpecified() {
		if o.AccessToken.IsNull() {
			m["access_token"] = ""
		} else {
			if v, err := o.AccessToken.Get(); err == nil {
				m["access_token"] = v.String()
			} else {
				m["access_token"] = ""
			}
		}
	}

	if o.RefreshToken.IsSpecified() {
		if o.RefreshToken.IsNull() {
			m["refresh_token"] = ""
		} else {
			if v, err := o.RefreshToken.Get(); err == nil {
				m["refresh_token"] = v.String()
			} else {
				m["refresh_token"] = ""
			}
		}
	}
	if o.TokenExpiresAt != nil {
		m["token_expires_at"] = o.TokenExpiresAt
	}

	if o.AuthorizedEmail.IsSpecified() {
		if o.AuthorizedEmail.IsNull() {
			m["authorized_email"] = ""
		} else {
			if v, err := o.AuthorizedEmail.Get(); err == nil {
				m["authorized_email"] = v.String()
			} else {
				m["authorized_email"] = ""
			}
		}
	}

	if o.AuthorizedAt != nil {
		m["authorized_at"] = o.AuthorizedAt
	}

	if o.IsAuthorized.IsSpecified() {
		m["is_authorized"] = nil
		if isAuthorized, err := o.IsAuthorized.Get(); err == nil {
			m["is_authorized"] = isAuthorized
		}
	}

	if o.CompanyID.IsSpecified() {
		if o.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = o.CompanyID.MustGet()
		}
	}

	if o.IsImported.IsSpecified() {
		m["is_imported"] = nil
		if isImported, err := o.IsImported.Get(); err == nil {
			m["is_imported"] = isImported
		}
	}

	return m
}
