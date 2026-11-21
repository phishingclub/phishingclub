package model

import (
	"fmt"
	"net"
	"strings"
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
	Helo             nullable.Nullable[vo.OptionalString255] `json:"helo"`
	// PublicSendingIPs lists the public IPs or CIDR ranges the mail leaves
	// from, one per line. It only fills in allow listing information and is
	// never used to send, connect or look anything up.
	PublicSendingIPs nullable.Nullable[string]    `json:"publicSendingIPs"`
	CompanyID        nullable.Nullable[uuid.UUID] `json:"companyID"`
	Company          *Company                     `json:"company"`
	Headers          []*SMTPHeader                `json:"headers"`
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
	// helo is sent as a line in the SMTP dialog, so a control character such as
	// a newline must never pass into it
	if s.Helo.IsSpecified() {
		if helo, err := s.Helo.Get(); err == nil {
			if err := validate.ErrorIfContainsControlChars("helo", helo.String()); err != nil {
				return err
			}
		}
	}
	if s.PublicSendingIPs.IsSpecified() {
		if v, err := s.PublicSendingIPs.Get(); err == nil {
			if _, err := ParsePublicSendingIPs(v); err != nil {
				return validate.WrapErrorWithField(err, "publicSendingIPs")
			}
		}
	}
	return nil
}

// MaxPublicSendingIPs is the most public sending IPs one SMTP configuration holds
const MaxPublicSendingIPs = 10

// maxPublicSendingIPsLength bounds the raw text before it is split
const maxPublicSendingIPsLength = 1024

// ParsePublicSendingIPs splits a list of IPs or CIDR ranges on new lines and
// commas. Each entry must be an IPv4 or IPv6 address or CIDR range, never a
// hostname. The list only fills in allow listing text, so what an address
// means is not checked. Each entry is written in its normal form, a range as
// its network address. Duplicates are dropped and the order is kept.
func ParsePublicSendingIPs(input string) ([]string, error) {
	if len(input) > maxPublicSendingIPsLength {
		return nil, fmt.Errorf("must be at most %d characters", maxPublicSendingIPsLength)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, entry := range parseList(input) {
		var normalized string
		if strings.Contains(entry, "/") {
			_, ipNet, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("%q is not an IP address or CIDR range", entry)
			}
			normalized = ipNet.String()
		} else {
			ip := net.ParseIP(entry)
			if ip == nil {
				return nil, fmt.Errorf("%q is not an IP address or CIDR range", entry)
			}
			normalized = ip.String()
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	if len(out) > MaxPublicSendingIPs {
		return nil, fmt.Errorf("at most %d entries", MaxPublicSendingIPs)
	}
	return out, nil
}

// PublicSendingIPList returns the stored public sending IPs, an empty list
// when none are set or the stored text does not parse
func (s *SMTPConfiguration) PublicSendingIPList() []string {
	v, err := s.PublicSendingIPs.Get()
	if err != nil {
		return []string{}
	}
	ips, err := ParsePublicSendingIPs(v)
	if err != nil {
		return []string{}
	}
	return ips
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
	if s.Helo.IsSpecified() {
		m["helo"] = ""
		if helo, err := s.Helo.Get(); err == nil {
			m["helo"] = helo.String()
		}
	}
	if s.PublicSendingIPs.IsSpecified() {
		m["public_sending_ips"] = ""
		if v, err := s.PublicSendingIPs.Get(); err == nil {
			// stored one per line in the normalized form, Validate has
			// already refused text that does not parse
			if ips, err := ParsePublicSendingIPs(v); err == nil {
				m["public_sending_ips"] = strings.Join(ips, "\n")
			}
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
