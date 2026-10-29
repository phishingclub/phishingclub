package model

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

type Domain struct {
	ID                nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt         *time.Time                              `json:"createdAt"`
	UpdatedAt         *time.Time                              `json:"updatedAt"`
	Name              nullable.Nullable[vo.String255]         `json:"name"`
	Type              nullable.Nullable[vo.String32]          `json:"type"`              // "regular" or "proxy"
	ProxyTargetDomain nullable.Nullable[vo.OptionalString255] `json:"proxyTargetDomain"` // target URL for proxy (can be full URL or domain)
	HostWebsite       nullable.Nullable[bool]                 `json:"hostWebsite"`
	ManagedTLS        nullable.Nullable[bool]                 `json:"managedTLS"`
	OwnManagedTLS     nullable.Nullable[bool]                 `json:"ownManagedTLS"`
	SelfSignedTLS     nullable.Nullable[bool]                 `json:"selfSignedTLS"`
	// private key
	OwnManagedTLSKey nullable.Nullable[string] `json:"ownManagedTLSKey"`
	// cert
	OwnManagedTLSPem    nullable.Nullable[string]                `json:"ownManagedTLSPem"`
	PageContent         nullable.Nullable[vo.OptionalString1MB]  `json:"pageContent"`
	PageNotFoundContent nullable.Nullable[vo.OptionalString1MB]  `json:"pageNotFoundContent"`
	RedirectURL         nullable.Nullable[vo.OptionalString1024] `json:"redirectURL"`
	CompanyID           nullable.Nullable[uuid.UUID]             `json:"companyID"`
	ProxyID             nullable.Nullable[uuid.UUID]             `json:"proxyID"`
	Company             *Company                                 `json:"company"`
}

// domain name length limits from RFC 1035
const (
	domainNameMaxLength  = 253
	domainLabelMaxLength = 63
)

// ValidateDomainName only accepts a plain lowercase hostname, since the server
// matches the request host against the stored name exactly. A name equal to a
// reserved host is refused so a phishing domain can not take over the admin host.
func ValidateDomainName(name string, reservedHosts ...string) error {
	if err := domainNameFormat(name); err != nil {
		return validate.WrapErrorWithField(err, "name")
	}
	if IsReservedDomainName(name, reservedHosts...) {
		return validate.WrapErrorWithField(
			errors.New("is reserved for the administration server"),
			"name",
		)
	}
	return nil
}

// IsReservedDomainName reports if name equals one of the reserved hosts. The
// compare ignores case, a trailing dot and any port on either side.
func IsReservedDomainName(name string, reservedHosts ...string) bool {
	n := normalizeHostForCompare(name)
	if n == "" {
		return false
	}
	for _, h := range reservedHosts {
		if r := normalizeHostForCompare(h); r != "" && r == n {
			return true
		}
	}
	return false
}

// normalizeHostForCompare lowercases a host, drops a port and a trailing dot
func normalizeHostForCompare(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.TrimSuffix(s, ".")
	return strings.ToLower(s)
}

func domainNameFormat(name string) error {
	if name == "" {
		return errors.New("is required")
	}
	if len(name) > domainNameMaxLength {
		return fmt.Errorf("must be at most %d characters", domainNameMaxLength)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return errors.New("must not have empty labels, leading or trailing dots")
		}
		if len(label) > domainLabelMaxLength {
			return fmt.Errorf("labels must be at most %d characters", domainLabelMaxLength)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("labels must not start or end with a hyphen")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			isLower := c >= 'a' && c <= 'z'
			isDigit := c >= '0' && c <= '9'
			if isLower || isDigit || c == '-' {
				continue
			}
			if c >= 'A' && c <= 'Z' {
				return errors.New("must be lowercase")
			}
			return errors.New("may only contain letters, digits, dots and hyphens, underscores and IPv6 addresses are not supported")
		}
	}
	return nil
}

// NormalizeDomainName trims spaces, lowercases and drops one trailing dot
func NormalizeDomainName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(name, ".")
}

// ValidateDomainRedirectURL checks a domain redirect url, see ValidateRedirectURL
func ValidateDomainRedirectURL(s string) error {
	return ValidateRedirectURL(s, "redirectURL")
}

// ValidateDomainRedirectURLUpdate checks an incoming redirect url only when
// it differs from the stored one, since the client sends it on every update
// and an older stored value must not block other edits.
func ValidateDomainRedirectURLUpdate(stored string, incoming string) error {
	if incoming == stored {
		return nil
	}
	return ValidateDomainRedirectURL(incoming)
}

// ValidateRedirectURL allows an empty value, a path on the same host such as
// /landing, or an absolute http or https url. A value starting with // or
// holding a backslash or control character is refused, since browsers may
// read it as a url on another host. Errors are wrapped with field.
func ValidateRedirectURL(s string, field string) error {
	if s == "" {
		return nil
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return validate.WrapErrorWithField(
				errors.New("must not contain backslashes or control characters"),
				field,
			)
		}
	}
	if strings.HasPrefix(s, "/") {
		if strings.HasPrefix(s, "//") {
			return validate.WrapErrorWithField(
				errors.New("must be a path starting with a single / or an http or https URL"),
				field,
			)
		}
		return nil
	}
	if err := validate.ErrorIfNotHTTPURL(s); err != nil {
		return validate.WrapErrorWithField(err, field)
	}
	return nil
}

// Validate checks a new domain, including the name format and redirect url
func (d *Domain) Validate() error {
	return d.validate(true)
}

// ValidateExisting checks a stored domain on update. The name can not change
// on update, so its format is not checked again, which keeps domains created
// before the format rules editable. The redirect url is checked by the caller
// only when a new value is supplied.
func (d *Domain) ValidateExisting() error {
	return d.validate(false)
}

func (d *Domain) validate(isNew bool) error {
	if err := validate.NullableFieldRequired("name", d.Name); err != nil {
		return err
	}
	name, err := d.Name.Get()
	if err != nil || name.String() == "" {
		return validate.WrapErrorWithField(errors.New("is required"), "name")
	}
	if isNew {
		if err := ValidateDomainName(name.String()); err != nil {
			return err
		}
		if redirectURL, err := d.RedirectURL.Get(); err == nil {
			if err := ValidateDomainRedirectURL(redirectURL.String()); err != nil {
				return err
			}
		}
	}

	// set default type if not specified
	if !d.Type.IsSpecified() {
		d.Type.Set(*vo.NewString32Must("regular"))
	}

	domainType, err := d.Type.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("type is required"), "type")
	}

	// validate type is either "regular" or "proxy"
	if domainType.String() != "regular" && domainType.String() != "proxy" {
		return validate.WrapErrorWithField(errors.New("type must be 'regular' or 'proxy'"), "type")
	}

	if domainType.String() == "proxy" {
		// proxy domains require proxyTargetDomain
		if err := validate.NullableFieldRequired("proxyTargetDomain", d.ProxyTargetDomain); err != nil {
			return err
		}
		// proxy domains don't need page content validation
	} else {
		// regular domains need standard validation
		if err := validate.NullableFieldRequired("hostWebsite", d.HostWebsite); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("managedTLS", d.ManagedTLS); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("pageContent", d.PageContent); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("pageNotFoundContent", d.PageNotFoundContent); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("redirectURL", d.RedirectURL); err != nil {
			return err
		}
	}
	//
	//
	ownManagedTLS, err := d.OwnManagedTLS.Get()
	ownManagedTLSSet := err == nil && ownManagedTLS

	selfSignedTLS, err := d.SelfSignedTLS.Get()
	selfSignedTLSSet := err == nil && selfSignedTLS

	managedTLS, err := d.ManagedTLS.Get()
	managedTLSSet := err == nil && managedTLS

	// count how many TLS options are enabled
	tlsCount := 0
	if managedTLSSet {
		tlsCount++
	}
	if ownManagedTLSSet {
		tlsCount++
	}
	if selfSignedTLSSet {
		tlsCount++
	}

	// only one TLS option can be enabled at a time
	if tlsCount > 1 {
		return errs.NewValidationError(errors.New(
			"Domain TLS can only have one option enabled: managed, own managed, or self-signed",
		))
	}
	if ownManagedTLS {
		// handle own managed ManagedTLS
		ownManagedTLSKey, err := d.OwnManagedTLSKey.Get()
		ownManagedTLSPem, err := d.OwnManagedTLSPem.Get()
		ownManagedTLSKeyIsSet := err == nil && len(ownManagedTLSKey) > 0
		ownManagedTLSPemIsSet := err == nil && len(ownManagedTLSPem) > 0
		// both must be set, not one of
		if (ownManagedTLSKeyIsSet && !ownManagedTLSPemIsSet) ||
			(!ownManagedTLSKeyIsSet && ownManagedTLSPemIsSet) {
			return errs.NewValidationError(errors.New(
				"Own managed TLS requires a private key (.key) and a certificate (.pem)",
			))
		}
	}
	/*
		// TODO hostWebsite vs redirectURL are mutually exclusive
		hostWebsite := d.HostWebsite.MustGet()
		redirectURL := d.RedirectURL.MustGet()
			redirectURLLen := len(redirectURL.String())
			if hostWebsite && redirectURLLen > 0 {
				return validate.WrapErrorWithField(
					errors.New("both can not be set"),
					"Host website and redirect url",
				)
				} */
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (d *Domain) ToDBMap() map[string]any {
	m := map[string]any{}
	if d.Name.IsSpecified() {
		m["name"] = nil
		if name, err := d.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if d.Type.IsSpecified() {
		m["type"] = "regular"
		if domainType, err := d.Type.Get(); err == nil {
			m["type"] = domainType.String()
		}
	}
	if d.ProxyTargetDomain.IsSpecified() {
		m["proxy_target_domain"] = nil
		if proxyTargetDomain, err := d.ProxyTargetDomain.Get(); err == nil {
			m["proxy_target_domain"] = proxyTargetDomain.String()
		}
	}
	if d.HostWebsite.IsSpecified() {
		m["host_website"] = nil
		if hostWebsite, err := d.HostWebsite.Get(); err == nil {
			m["host_website"] = hostWebsite
		}
		m["redirect_url"] = ""
	}
	if d.RedirectURL.IsSpecified() {
		m["redirect_url"] = nil
		if redirectURL, err := d.RedirectURL.Get(); err == nil {
			m["redirect_url"] = redirectURL.String()
		}
	}
	if d.PageContent.IsSpecified() {
		m["page_content"] = nil
		if staticPage, err := d.PageContent.Get(); err == nil {
			m["page_content"] = staticPage.String()
		}
	}
	if d.PageNotFoundContent.IsSpecified() {
		m["page_not_found_content"] = nil
		if staticNotFound, err := d.PageNotFoundContent.Get(); err == nil {
			m["page_not_found_content"] = staticNotFound.String()
		}
	}
	if d.CompanyID.IsSpecified() {
		if d.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = d.CompanyID.MustGet()
		}
	}
	if d.ManagedTLS.IsSpecified() {
		m["managed_tls_certs"] = false
		if d.ManagedTLS.IsNull() {
			m["managed_tls_certs"] = nil
		} else {
			m["managed_tls_certs"] = d.ManagedTLS.MustGet()
		}
	}
	if d.OwnManagedTLS.IsSpecified() {
		m["own_managed_tls"] = false
		if d.OwnManagedTLS.IsNull() {
			m["own_managed_tls"] = nil
		} else {
			m["own_managed_tls"] = d.OwnManagedTLS.MustGet()
		}
	}
	if d.SelfSignedTLS.IsSpecified() {
		m["self_signed_tls"] = false
		if d.SelfSignedTLS.IsNull() {
			m["self_signed_tls"] = nil
		} else {
			m["self_signed_tls"] = d.SelfSignedTLS.MustGet()
		}
	}
	if d.ProxyID.IsSpecified() {
		if d.ProxyID.IsNull() {
			m["proxy_id"] = nil
		} else {
			m["proxy_id"] = d.ProxyID.MustGet()
		}
	}
	return m
}

// DomainOverview is a subset of the domain as used as read-only
type DomainOverview struct {
	ID                uuid.UUID  `json:"id,omitempty"`
	CreatedAt         *time.Time `json:"createdAt"`
	UpdatedAt         *time.Time `json:"updatedAt"`
	Name              string     `json:"name"`
	Type              string     `json:"type"`
	ProxyTargetDomain string     `json:"proxyTargetDomain"`
	HostWebsite       bool       `json:"hostWebsite"`
	ManagedTLS        bool       `json:"managedTLS"`
	OwnManagedTLS     bool       `json:"ownManagedTLS"`
	SelfSignedTLS     bool       `json:"selfSignedTLS"`
	RedirectURL       string     `json:"redirectURL"`
	CompanyID         *uuid.UUID `json:"companyID"`
	ProxyID           *uuid.UUID `json:"proxyID"`
}
