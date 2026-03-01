package model

import (
	"encoding/json"
	"fmt"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// SSOOption holds all SSO configuration.
//
// Legacy Entra ID fields (ClientID, TenantID, ClientSecret, RedirectURL) are
// preserved so existing installations continue to work without any admin
// intervention.  When TenantID is non-empty the system treats the config as a
// legacy Entra ID installation and uses the MSAL path.
//
// For new generic OIDC providers the operator sets IssuerURL together with the
// shared ClientID / ClientSecret / RedirectURL fields and leaves TenantID
// empty.
type SSOOption struct {
	// --- shared fields (legacy + oidc) ---

	Enabled bool `json:"enabled"`

	// ClientID is the OAuth2 / OIDC application client id
	ClientID vo.OptionalString64 `json:"clientID"`

	// ClientSecret is the OAuth2 / OIDC application client secret
	ClientSecret vo.OptionalString1024 `json:"clientSecret"`

	// RedirectURL is the callback URL registered with the provider
	RedirectURL vo.OptionalString1024 `json:"redirectURL"`

	// --- legacy entra id fields ---

	// TenantID is the Azure AD tenant identifier.
	// Non-empty value indicates that this is a legacy Entra ID configuration.
	TenantID vo.OptionalString64 `json:"tenantID"`

	// --- generic oidc fields ---

	// IssuerURL is the OIDC provider issuer, e.g.
	// "https://keycloak.example.com/realms/myrealm"
	// The library appends /.well-known/openid-configuration automatically.
	IssuerURL vo.OptionalString1024 `json:"issuerURL"`

	// --- authorization ---

	// RequiredRoleClaim is the JWT / userinfo claim name that contains the
	// user's roles, e.g. "roles", "groups", "realm_access.roles".
	// Empty means role-checking is disabled.
	RequiredRoleClaim vo.OptionalString255 `json:"requiredRoleClaim"`

	// RequiredRoleValue is the value that must appear inside the claim
	// identified by RequiredRoleClaim before login is permitted.
	RequiredRoleValue vo.OptionalString255 `json:"requiredRoleValue"`

	// --- access policy ---

	// SSOOnly disables local username/password login when true.
	// Admins should ensure at least one SSO-capable account exists before
	// enabling this to avoid lockout.
	SSOOnly bool `json:"ssoOnly"`

	// --- acr ---

	// ACRValues is the space-separated list of Authentication Context Class
	// Reference values to request from the provider, e.g. "urn:mfa".
	// Empty means no ACR is requested.
	ACRValues vo.OptionalString255 `json:"acrValues"`
}

// IsLegacyEntraID returns true when the configuration describes a legacy
// Microsoft Entra ID (Azure AD) setup identified by a non-empty TenantID.
func (s *SSOOption) IsLegacyEntraID() bool {
	return s.TenantID.String() != ""
}

// IsOIDC returns true when the configuration describes a generic OIDC provider.
func (s *SSOOption) IsOIDC() bool {
	return s.IssuerURL.String() != "" && !s.IsLegacyEntraID()
}

// HasRoleGating returns true when role-based login gating is configured.
func (s *SSOOption) HasRoleGating() bool {
	return s.RequiredRoleClaim.String() != "" && s.RequiredRoleValue.String() != ""
}

// HasACR returns true when an ACR value is configured.
func (s *SSOOption) HasACR() bool {
	return s.ACRValues.String() != ""
}

// NewSSOOptionDefault returns a zeroed SSOOption ready to be stored.
func NewSSOOptionDefault() *SSOOption {
	return &SSOOption{
		Enabled:           false,
		ClientID:          *vo.NewEmptyOptionalString64(),
		TenantID:          *vo.NewEmptyOptionalString64(),
		ClientSecret:      *vo.NewEmptyOptionalString1024(),
		RedirectURL:       *vo.NewEmptyOptionalString1024(),
		IssuerURL:         *vo.NewEmptyOptionalString1024(),
		RequiredRoleClaim: *vo.NewEmptyOptionalString255(),
		RequiredRoleValue: *vo.NewEmptyOptionalString255(),
		SSOOnly:           false,
		ACRValues:         *vo.NewEmptyOptionalString255(),
	}
}

// NewSSOOptionFromJSON unmarshals a JSON blob into an SSOOption.
func NewSSOOptionFromJSON(jsonData []byte) (*SSOOption, error) {
	option := &SSOOption{}
	err := json.Unmarshal(jsonData, option)
	if err != nil {
		return nil, validate.WrapErrorWithField(
			errs.NewValidationError(
				errors.New("invalid format"),
			),
			"Option",
		)
	}
	return option, nil
}

// NewSSOOptionFromOption converts a generic Option row into an SSOOption.
func NewSSOOptionFromOption(option *Option) (*SSOOption, error) {
	if option == nil {
		return nil, fmt.Errorf("option cannot be nil")
	}
	ssooption, err := NewSSOOptionFromJSON([]byte(option.Value.String()))
	if err != nil {
		return nil, validate.WrapErrorWithField(
			errs.NewValidationError(
				errors.New("invalid format"),
			),
			"SSOOption",
		)
	}
	return ssooption, nil
}

// ToJSON serialises the SSOOption to JSON.
func (l *SSOOption) ToJSON() ([]byte, error) {
	return json.Marshal(l)
}

// ToOption converts the SSOOption into a generic Option row for persistence.
func (l *SSOOption) ToOption() (*Option, error) {
	json, err := l.ToJSON()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	str, err := vo.NewOptionalString1MB(string(json))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &Option{
		Key:   *vo.NewString64Must(data.OptionKeyAdminSSOLogin),
		Value: *str,
	}, nil
}
