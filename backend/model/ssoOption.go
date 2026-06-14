package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

type SSOOption struct {
	Enabled bool `json:"enabled"`
	// ProviderType is "entra" or "oidc". An empty value is treated as "entra"
	// so configurations stored before generic OIDC support keep working.
	ProviderType string                `json:"providerType"`
	ClientID     vo.OptionalString64   `json:"clientID"`
	TenantID     vo.OptionalString64   `json:"tenantID"`
	ClientSecret vo.OptionalString1024 `json:"clientSecret"`
	RedirectURL  vo.OptionalString1024 `json:"redirectURL"`
	// OIDC only fields
	IssuerURL vo.OptionalString1024 `json:"issuerURL"`
	Scopes    vo.OptionalString1024 `json:"scopes"`
	ACRValues vo.OptionalString1024 `json:"acrValues"`
	// ExclusiveLogin disables username and password login when SSO is enabled.
	// A server level break glass in config.json can still allow local login.
	ExclusiveLogin bool `json:"exclusiveLogin"`
}

func NewSSOOptionDefault() *SSOOption {
	return &SSOOption{
		Enabled:      false,
		ProviderType: data.SSOProviderEntra,
		ClientID:     *vo.NewEmptyOptionalString64(),
		TenantID:     *vo.NewEmptyOptionalString64(),
		ClientSecret: *vo.NewEmptyOptionalString1024(),
		RedirectURL:  *vo.NewEmptyOptionalString1024(),
		IssuerURL:    *vo.NewEmptyOptionalString1024(),
		Scopes:       *vo.NewEmptyOptionalString1024(),
		ACRValues:    *vo.NewEmptyOptionalString1024(),
	}
}

// Provider returns the configured provider type, defaulting to Entra ID when
// the stored value is empty so older configurations keep working.
func (l *SSOOption) Provider() string {
	if l.ProviderType == data.SSOProviderOIDC {
		return data.SSOProviderOIDC
	}
	return data.SSOProviderEntra
}

// ScopesOrDefault returns the configured OIDC scopes or the default set.
func (l *SSOOption) ScopesOrDefault() string {
	s := strings.TrimSpace(l.Scopes.String())
	if s == "" {
		return data.SSODefaultScopes
	}
	return s
}

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

func (l *SSOOption) ToJSON() ([]byte, error) {
	return json.Marshal(l)
}

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
		Key:   *vo.NewString127Must(data.OptionKeyAdminSSOLogin),
		Value: *str,
	}, nil
}
