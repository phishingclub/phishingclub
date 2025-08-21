package model

import (
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// SMTPConfiguration is a configuration for sending mails
type SMTPConfiguration struct {
	ID               nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt        *time.Time                              `json:"createdAt"`
	UpdatedAt        *time.Time                              `json:"updatedAt"`
	Name             nullable.Nullable[vo.String127]         `json:"name"`
	Host             nullable.Nullable[vo.String255]         `json:"host"`
	Port             nullable.Nullable[vo.Port]              `json:"port"`
	Username         nullable.Nullable[vo.OptionalString255] `json:"username"`
	Password         nullable.Nullable[vo.OptionalString255] `json:"password"`
	IgnoreCertErrors nullable.Nullable[bool]                 `json:"ignoreCertErrors"`
	CompanyID        nullable.Nullable[uuid.UUID]            `json:"companyID"`
	Company          *Company                                `json:"company"`
	Headers          []*SMTPHeader                           `json:"headers"`
}

// Validate checks if the SMTP configuration has a valid state
func (s *SMTPConfiguration) Validate() error {
	if err := validate.NullableFieldRequired("name", s.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("host", s.Host); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("port", s.Port); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("username", s.Username); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("password", s.Password); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("ignoreCertErrors", s.IgnoreCertErrors); err != nil {
		return err
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (s *SMTPConfiguration) ToDBMap() map[string]any {
	m := map[string]any{}
	if s.Name.IsSpecified() {
		m["name"] = nil
		if name, err := s.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if s.Host.IsSpecified() {
		m["host"] = nil
		if host, err := s.Host.Get(); err == nil {
			m["host"] = host.String()
		}
	}
	if s.Port.IsSpecified() {
		m["port"] = nil
		if port, err := s.Port.Get(); err == nil {
			m["port"] = port.Uint16()
		}
	}
	if s.Username.IsSpecified() {
		m["username"] = nil
		if username, err := s.Username.Get(); err == nil {
			m["username"] = username.String()
		}
	}
	if s.Password.IsSpecified() {
		m["password"] = nil
		if password, err := s.Password.Get(); err == nil {
			m["password"] = password.String()
		}
	}
	if s.IgnoreCertErrors.IsSpecified() {
		m["ignore_cert_errors"] = nil
		if ignoreCertErrors, err := s.IgnoreCertErrors.Get(); err == nil {
			m["ignore_cert_errors"] = ignoreCertErrors
		}
	}
	if v, err := s.CompanyID.Get(); err == nil {
		m["company_id"] = v.String()
	}
	return m
}

// SMTPHeader is a header for a specific SMTP configuration
type SMTPHeader struct {
	ID        uuid.UUID                       `json:"id"`
	CreatedAt *time.Time                      `json:"createdAt"`
	UpdatedAt *time.Time                      `json:"updatedAt"`
	SmtpID    nullable.Nullable[uuid.UUID]    `json:"smtpID"`
	Key       nullable.Nullable[vo.String127] `json:"key"`
	Value     nullable.Nullable[vo.String255] `json:"value"`
}

func (s *SMTPHeader) Validate() error {
	if err := validate.NullableFieldRequired("smtpID", s.SmtpID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("key", s.Key); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("value", s.Value); err != nil {
		return err
	}
	return nil
}

func (s *SMTPHeader) ToDBMap() map[string]interface{} {
	m := map[string]interface{}{}
	if s.SmtpID.IsSpecified() {
		if smtpID, err := s.SmtpID.Get(); err == nil {
			m["smtp_configuration_id"] = smtpID.String()
		}
	}
	if s.Key.IsSpecified() {
		m["key"] = nil
		if key, err := s.Key.Get(); err == nil {
			m["key"] = key.String()
		}
	}
	if s.Value.IsSpecified() {
		m["value"] = nil
		if value, err := s.Value.Get(); err == nil {
			m["value"] = value.String()
		}
	}
	return m
}
