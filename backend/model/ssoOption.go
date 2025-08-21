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

type SSOOption struct {
	Enabled      bool                  `json:"enabled"`
	ClientID     vo.OptionalString64   `json:"clientID"`
	TenantID     vo.OptionalString64   `json:"tenantID"`
	ClientSecret vo.OptionalString1024 `json:"clientSecret"`
	RedirectURL  vo.OptionalString1024 `json:"redirectURL"`
}

func NewSSOOptionDefault() *SSOOption {
	return &SSOOption{
		Enabled:      false,
		ClientID:     *vo.NewEmptyOptionalString64(),
		TenantID:     *vo.NewEmptyOptionalString64(),
		ClientSecret: *vo.NewEmptyOptionalString1024(),
		RedirectURL:  *vo.NewEmptyOptionalString1024(),
	}
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
		Key:   *vo.NewString64Must(data.OptionKeyAdminSSOLogin),
		Value: *str,
	}, nil
}
