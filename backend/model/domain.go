package model

import (
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

// Validate checks if the Domain configuration with a valid state
func (d *Domain) Validate() error {
	if err := validate.NullableFieldRequired("name", d.Name); err != nil {
		return err
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

	// cant both have managed and own managed tls
	if managedTLS, err := d.ManagedTLS.Get(); err == nil && managedTLS && ownManagedTLSSet {
		return errs.NewValidationError(errors.New(
			"Domain TLS can not both be managed and own managed",
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
	RedirectURL       string     `json:"redirectURL"`
	CompanyID         *uuid.UUID `json:"companyID"`
	ProxyID           *uuid.UUID `json:"proxyID"`
}
