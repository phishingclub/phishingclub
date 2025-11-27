package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/caddyserver/certmagic"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/acme"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Domain is a Domain service
type Domain struct {
	Common
	OwnManagedCertificatePath string
	CertMagicConfig           *certmagic.Config
	CertMagicCache            *certmagic.Cache
	DomainRepository          *repository.Domain
	CompanyRepository         *repository.Company
	CampaignTemplateService   *CampaignTemplate
	AssetService              *Asset
	FileService               *File
	TemplateService           *Template
}

// CreateProxyDomain creates a proxy domain bypassing direct creation restrictions
func (d *Domain) CreateProxyDomain(
	ctx context.Context,
	session *model.Session,
	domain *model.Domain,
) (*uuid.UUID, error) {
	return d.createDomain(ctx, session, domain, true)
}

// Create creates a new domain
func (d *Domain) Create(
	ctx context.Context,
	session *model.Session,
	domain *model.Domain,
) (*uuid.UUID, error) {
	return d.createDomain(ctx, session, domain, false)
}

// DeleteProxyDomain deletes a proxy domain bypassing direct deletion restrictions
func (d *Domain) DeleteProxyDomain(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	return d.deleteDomain(ctx, session, id, true)
}

// UpdateProxyDomain updates a proxy domain bypassing direct update restrictions
func (d *Domain) UpdateProxyDomain(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Domain,
) error {
	return d.updateDomain(ctx, session, id, incoming, true)
}

// createDomain is the internal domain creation method
func (d *Domain) createDomain(
	ctx context.Context,
	session *model.Session,
	domain *model.Domain,
	allowProxyCreation bool,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Domain.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// prevent direct creation of proxy domains unless explicitly allowed
	if !allowProxyCreation {
		if domainType, err := domain.Type.Get(); err == nil && domainType.String() == "proxy" {
			return nil, validate.WrapErrorWithField(errors.New("proxy domains can only be created through proxy configuration, not directly"), "type")
		}
	}

	// validate data
	if err := domain.Validate(); err != nil {
		d.Logger.Errorw("failed to validate domain", "error", err)
		return nil, errs.Wrap(err)
	}

	// get domain type for specific validation
	domainType, _ := domain.Type.Get()

	if domainType.String() == "proxy" {
		// validate proxy target domain
		if err := d.validateProxyDomain(ctx, domain); err != nil {
			return nil, err
		}
	} else {
		// validate template content for regular domains
		if pageContent, err := domain.PageContent.Get(); err == nil {
			if err := d.TemplateService.ValidateDomainTemplate(pageContent.String()); err != nil {
				d.Logger.Errorw("failed to validate domain page template", "error", err)
				return nil, validate.WrapErrorWithField(errors.New("invalid page template: "+err.Error()), "pageContent")
			}
		}
		if notFoundContent, err := domain.PageNotFoundContent.Get(); err == nil {
			if err := d.TemplateService.ValidateDomainTemplate(notFoundContent.String()); err != nil {
				d.Logger.Errorw("failed to validate domain not found template", "error", err)
				return nil, validate.WrapErrorWithField(errors.New("invalid not found template: "+err.Error()), "pageNotFoundContent")
			}
		}
	}
	// check for uniqueness
	name := domain.Name.MustGet() // safe as we have validated
	_, err = d.DomainRepository.GetByName(
		ctx,
		&name,
		&repository.DomainOption{},
	)
	// we expect not to find a domain with this name
	if err != nil {
		// something went wrong
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			d.Logger.Errorw("failed to create domain", "error", err)
			return nil, errs.Wrap(err)
		}
	}
	// if there is no error, it means we found a domain with this name
	if err == nil {
		d.Logger.Debugw("domain name is already taken", "error", name.String())
		return nil, validate.WrapErrorWithField(errors.New("not unique"), "name")
	}
	domain, err = d.handleOwnManagedTLS(ctx, domain)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	domain, err = d.handleSelfSignedTLS(ctx, domain)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// create domain
	createdDomainID, err := d.DomainRepository.Insert(
		ctx,
		domain,
	)
	if err != nil {
		d.Logger.Errorw("failed to create domain", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = createdDomainID.String()
	d.AuditLogAuthorized(ae)
	if domain.ManagedTLS.MustGet() && build.Flags.Production {
		d.Logger.Debugw("triggering certificate retrieval", "domain", name.String())
		d.triggerCertificateRetrieval(name.String())
	}
	return createdDomainID, nil
}

// triggerCertificateRetrieval attempts to trigger automatic certificate
// by making an HTTPS request to the domain
func (d *Domain) triggerCertificateRetrieval(name string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec
				InsecureSkipVerify: true, // since cert won't be valid yet
			},
			// Set reasonable timeouts
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 5 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       5 * time.Second,
			// disable connection pooling since we only need one request
			DisableKeepAlives: true,
			MaxIdleConns:      -1,
		}

		client := &http.Client{
			Transport: transport,
			// don't need client timeout as we're using context
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "https://"+name, nil)
		if err != nil {
			d.Logger.Errorw("failed to create request",
				"domain", name,
				"error", err)
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			d.Logger.Errorw("failed to trigger certificate retrieval",
				"domain", name,
				"error", err)
			return
		}
		// always close response body
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
		}

		// clean up transport
		transport.CloseIdleConnections()

		d.Logger.Debugw("certificate retrieval triggered",
			"domain", name,
			"status", resp.StatusCode)
	}()
}

// GetAll gets domains
func (d *Domain) GetAll(
	companyID *uuid.UUID, // can be null
	ctx context.Context,
	session *model.Session,
	queryArgs *vo.QueryArgs,
	withCompany bool,
) (*model.Result[model.Domain], error) {
	result := model.NewEmptyResult[model.Domain]()
	ae := NewAuditEvent("Domain.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get domains
	result, err = d.DomainRepository.GetAll(
		ctx,
		companyID,
		&repository.DomainOption{
			QueryArgs:   queryArgs,
			WithCompany: withCompany,
		},
	)
	if err != nil {
		d.Logger.Errorw("failed to get domains", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetByName gets a domain by name
func (d *Domain) GetByName(
	ctx context.Context,
	session *model.Session,
	name *vo.String255,
	options *repository.DomainOption,
) (*model.Domain, error) {
	ae := NewAuditEvent("Domain.GetByName", session)
	ae.Details["name"] = name.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get domain
	domain, err := d.DomainRepository.GetByName(
		ctx,
		name,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early, this is not an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		d.Logger.Errorw("failed to get domain by name", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return domain, nil
}

// GetAllOverview gets domains with limited data
func (d *Domain) GetAllOverview(
	companyID *uuid.UUID, // can be null
	ctx context.Context,
	session *model.Session,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.DomainOverview], error) {
	result := model.NewEmptyResult[model.DomainOverview]()
	ae := NewAuditEvent("Domain.GetAllOverview", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get domains
	result, err = d.DomainRepository.GetAllSubset(
		ctx,
		companyID,
		&repository.DomainOption{
			QueryArgs: queryArgs,
		},
	)
	if err != nil {
		d.Logger.Errorw("failed to get domains subset", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAllOverviewWithoutProxies gets domains with limited data, excluding proxy domains for asset management
func (d *Domain) GetAllOverviewWithoutProxies(
	companyID *uuid.UUID, // can be null
	ctx context.Context,
	session *model.Session,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.DomainOverview], error) {
	result := model.NewEmptyResult[model.DomainOverview]()
	ae := NewAuditEvent("Domain.GetAllOverviewWithoutProxies", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get domains excluding proxy domains for asset management
	result, err = d.DomainRepository.GetAllSubset(
		ctx,
		companyID,
		&repository.DomainOption{
			QueryArgs:           queryArgs,
			ExcludeProxyDomains: true,
		},
	)
	if err != nil {
		d.Logger.Errorw("failed to get domains subset for assets", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetByID is a function to get domain by id
func (d *Domain) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.DomainOption,
) (*model.Domain, error) {
	ae := NewAuditEvent("Domain.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get domain
	domain, err := d.DomainRepository.GetByID(
		ctx,
		id,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early, this is not an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		d.Logger.Errorw("failed to get domain by id", "error", err)
		return nil, errs.Wrap(err)
	}
	return domain, nil
}

// GetByCompanyID is a function to get domain by company id
func (d *Domain) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.DomainOption,
) (*model.Result[model.Domain], error) {
	result := model.NewEmptyResult[model.Domain]()
	ae := NewAuditEvent("Domain.GetByCompanyID", session)
	ae.Details["companyId"] = companyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get domains
	result, err = d.DomainRepository.GetAllByCompanyID(
		ctx,
		companyID,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early, this is not an error
		return result, errs.Wrap(err)
	}
	if err != nil {
		d.Logger.Errorw("failed to get domain by company id", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// UpdateByID updates domain by id
func (d *Domain) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Domain,
) error {
	return d.updateDomain(ctx, session, id, incoming, false)
}

// updateDomain is the internal domain update method
func (d *Domain) updateDomain(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Domain,
	allowProxyUpdate bool,
) error {
	ae := NewAuditEvent("Domain.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get the domain that is being updated
	current, err := d.DomainRepository.GetByID(
		ctx,
		id,
		&repository.DomainOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		d.Logger.Debugw("domain not found", "error", err)
		return err
	}
	if err != nil {
		d.Logger.Errorw("failed to update domain", "error", err)
		return err
	}

	// check if this is a proxy domain and restrict editable fields
	isProxyDomain := false
	if domainType, err := current.Type.Get(); err == nil && domainType.String() == "proxy" {
		isProxyDomain = true
		// for proxy domains, only allow updating ManagedTLS and custom certificate fields
		if incoming.Type.IsSpecified() {
			incomingType, _ := incoming.Type.Get()
			if incomingType.String() != "proxy" {
				return validate.WrapErrorWithField(errors.New("cannot change type of proxy domain"), "type")
			}
		}
	} else {
		// prevent changing regular domains to proxy type
		if incoming.Type.IsSpecified() {
			incomingType, _ := incoming.Type.Get()
			if incomingType.String() == "proxy" {
				return validate.WrapErrorWithField(errors.New("cannot change domain to proxy type - proxy domains can only be created through proxy configuration"), "type")
			}
		}
	}

	// set the supplied field on the existing domain
	if isProxyDomain && !allowProxyUpdate {
		// for proxy domains, prevent changing proxy-specific fields unless explicitly allowed
		if incoming.ProxyTargetDomain.IsSpecified() {
			return validate.WrapErrorWithField(errors.New("cannot change proxy target domain - edit the proxy configuration instead"), "proxyTargetDomain")
		}
		if incoming.HostWebsite.IsSpecified() {
			return validate.WrapErrorWithField(errors.New("cannot change host website setting for proxy domain"), "hostWebsite")
		}
		if incoming.PageContent.IsSpecified() {
			return validate.WrapErrorWithField(errors.New("cannot change page content for proxy domain"), "pageContent")
		}
		if incoming.PageNotFoundContent.IsSpecified() {
			return validate.WrapErrorWithField(errors.New("cannot change page not found content for proxy domain"), "pageNotFoundContent")
		}
		if incoming.RedirectURL.IsSpecified() {
			return validate.WrapErrorWithField(errors.New("cannot change redirect URL for proxy domain"), "redirectURL")
		}
	} else {
		// for regular domains or proxy domains with allowed updates, allow updating all fields
		if v, err := incoming.Type.Get(); err == nil {
			current.Type.Set(v)
		}
		if v, err := incoming.ProxyTargetDomain.Get(); err == nil {
			current.ProxyTargetDomain.Set(v)
		}
		if v, err := incoming.HostWebsite.Get(); err == nil {
			current.HostWebsite.Set(v)
		}
		if v, err := incoming.PageContent.Get(); err == nil {
			// validate template content before updating
			if err := d.TemplateService.ValidateDomainTemplate(v.String()); err != nil {
				d.Logger.Errorw("failed to validate domain page template", "error", err)
				return validate.WrapErrorWithField(errors.New("invalid page template: "+err.Error()), "pageContent")
			}
			current.PageContent.Set(v)
		}
		if v, err := incoming.PageNotFoundContent.Get(); err == nil {
			// validate template content before updating
			if err := d.TemplateService.ValidateDomainTemplate(v.String()); err != nil {
				d.Logger.Errorw("failed to validate domain not found template", "error", err)
				return validate.WrapErrorWithField(errors.New("invalid not found template: "+err.Error()), "pageNotFoundContent")
			}
			current.PageNotFoundContent.Set(v)
		}
		if v, err := incoming.RedirectURL.Get(); err == nil {
			current.RedirectURL.Set(v)
		}
	}

	wasManagedTLS := current.ManagedTLS.MustGet()
	if v, err := incoming.ManagedTLS.Get(); err == nil {
		current.ManagedTLS.Set(v)
	}
	wasOwnManagedTLS := current.OwnManagedTLS.MustGet()
	ownManagedTLSIsSet := false
	if v, err := incoming.OwnManagedTLS.Get(); err == nil {
		current.OwnManagedTLS.Set(v)
		ownManagedTLSIsSet = v
	}
	wasSelfSignedTLS := current.SelfSignedTLS.MustGet()
	selfSignedTLSIsSet := false
	if v, err := incoming.SelfSignedTLS.Get(); err == nil {
		current.SelfSignedTLS.Set(v)
		selfSignedTLSIsSet = v
	}
	ownManagedTLSKeyIsSet := false
	if v, err := incoming.OwnManagedTLSKey.Get(); err == nil {
		current.OwnManagedTLSKey.Set(v)
		ownManagedTLSKeyIsSet = len(v) > 0
	}
	ownManagedTLSPemIsSet := false
	if v, err := incoming.OwnManagedTLSPem.Get(); err == nil {
		current.OwnManagedTLSPem.Set(v)
		ownManagedTLSPemIsSet = len(v) > 0
	}

	// validate
	if err := current.Validate(); err != nil {
		d.Logger.Errorw("failed to validate domain", "error", err)
		return err
	}

	// validate proxy domain if type is proxy
	if domainType, err := current.Type.Get(); err == nil && domainType.String() == "proxy" {
		if err := d.validateProxyDomain(ctx, current); err != nil {
			return err
		}
	} else {
		// validate template content for regular domains only
		if pageContent, err := current.PageContent.Get(); err == nil {
			if err := d.TemplateService.ValidateDomainTemplate(pageContent.String()); err != nil {
				d.Logger.Errorw("failed to validate domain page template", "error", err)
				return validate.WrapErrorWithField(errors.New("invalid page template: "+err.Error()), "pageContent")
			}
		}
		if notFoundContent, err := current.PageNotFoundContent.Get(); err == nil {
			if err := d.TemplateService.ValidateDomainTemplate(notFoundContent.String()); err != nil {
				d.Logger.Errorw("failed to validate domain not found template", "error", err)
				return validate.WrapErrorWithField(errors.New("invalid not found template: "+err.Error()), "pageNotFoundContent")
			}
		}
	}
	// clean up if TLS was previous managed but no longer is
	if managedTLS, err := incoming.ManagedTLS.Get(); err == nil && !managedTLS {
		if wasManagedTLS {
			d.removeManagedDomainTLS(ctx, current.Name.MustGet().String())
		}
	}
	// if previously was own managed but not anymore, remove the certs and cache
	if wasOwnManagedTLS && !ownManagedTLSIsSet {
		err = d.removeOwnManagedTLS(current)
		if err != nil {
			d.Logger.Warnf("failed to remove own managed TLS", "error", err)
		}
	}
	// if previously was self-signed but not anymore, remove the certs and cache
	if wasSelfSignedTLS && !selfSignedTLSIsSet {
		err = d.removeSelfSignedTLS(current)
		if err != nil {
			d.Logger.Warnf("failed to remove self-signed TLS", "error", err)
		}
	}
	// if previously own managed TLS and now is own managed
	if !wasOwnManagedTLS && ownManagedTLSIsSet {
		if ownManagedTLSKeyIsSet && ownManagedTLSPemIsSet {
			current, err = d.handleOwnManagedTLS(ctx, current)
			if err != nil {
				return fmt.Errorf("faile to handle own managed TLS: %s", err)
			}
		} else {
			return errs.NewValidationError(
				errors.New("Private key and certificate must be provided for own managed TLS"),
			)
		}
	}
	// if previously was own managed TLS
	if wasOwnManagedTLS && ownManagedTLSIsSet {
		// only if both a key and a certificate is provided, overwrite it
		if ownManagedTLSKeyIsSet && ownManagedTLSPemIsSet {
			current, err = d.handleOwnManagedTLS(ctx, current)
			if err != nil {
				return fmt.Errorf("faile to handle own managed TLS: %s", err)
			}
		}
	}
	// if previously not self-signed and now is self-signed
	if !wasSelfSignedTLS && selfSignedTLSIsSet {
		current, err = d.handleSelfSignedTLS(ctx, current)
		if err != nil {
			return fmt.Errorf("failed to handle self-signed TLS: %s", err)
		}
	}
	// when updating, the own managed tls can previous be set with uploaded
	// key and cert, so only if all of them are provided, we handle them
	// update domain
	err = d.DomainRepository.UpdateByID(
		ctx,
		current,
	)
	if err != nil {
		d.Logger.Errorw("failed to update domain by id", "error", err)
		return err
	}
	d.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes a domain by ID
func (d *Domain) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	return d.deleteDomain(ctx, session, id, false)
}

// deleteDomain is the internal domain deletion method
func (d *Domain) deleteDomain(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	allowProxyDeletion bool,
) error {
	ae := NewAuditEvent("Domain.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// get the domain to check if it's a proxy domain
	current, err := d.DomainRepository.GetByID(ctx, id, &repository.DomainOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		d.Logger.Debugw("domain not found", "error", err)
		return err
	}
	if err != nil {
		d.Logger.Errorw("failed to get domain for deletion", "error", err)
		return err
	}

	// prevent deletion of proxy domains unless explicitly allowed
	if !allowProxyDeletion {
		if domainType, err := current.Type.Get(); err == nil && domainType.String() == "proxy" {
			return validate.WrapErrorWithField(errors.New("proxy domains can only be deleted by deleting the associated proxy configuration"), "domain")
		}
	}
	// get the domain
	domain, err := d.DomainRepository.GetByID(
		ctx,
		id,
		&repository.DomainOption{},
	)
	if err != nil {
		return err
	}
	// delete the relation from the campaign templates
	err = d.CampaignTemplateService.RemoveDomainByDomainID(
		ctx,
		session,
		id,
	)
	if err != nil {
		d.Logger.Error("failed to remove domain relation from campaign templates")
		return err
	}
	// delete all asset related to the domain
	err = d.AssetService.DeleteAllByDomainID(
		ctx,
		session,
		id,
	)
	if err != nil {
		d.Logger.Errorw("failed to delete assets related to domain", "error", err)
		return err
	}
	err = d.DomainRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		d.Logger.Errorw("failed to delete domain by id", "error", err)
		return err
	}
	// clean up if TLS was managed
	if domain.ManagedTLS.MustGet() {
		d.removeManagedDomainTLS(ctx, domain.Name.MustGet().String())
	}
	// clean up if TLS was own managed
	if domain.OwnManagedTLS.MustGet() {
		err = d.removeOwnManagedTLS(domain)
		if err != nil {
			d.Logger.Warnf("failed to remove own managed TLS during deletion", "error", err)
		}
	}
	// clean up if TLS was self-signed
	if domain.SelfSignedTLS.MustGet() {
		err = d.removeSelfSignedTLS(domain)
		if err != nil {
			d.Logger.Warnf("failed to remove self-signed TLS during deletion", "error", err)
		}
	}
	d.AuditLogAuthorized(ae)
	return nil
}

// removeManagedDomainTLS
func (d *Domain) removeManagedDomainTLS(ctx context.Context, domain string) {
	issuerKey := certmagic.DefaultACME.IssuerKey()
	// check if managed certs exists
	sitePrefix := certmagic.StorageKeys.CertsSitePrefix(issuerKey, domain)
	if !d.CertMagicConfig.Storage.Exists(ctx, sitePrefix) {
		d.Logger.Debugw("cache storage does not exist for", "error", sitePrefix)
		return
	}
	// remove pem
	certPath := certmagic.StorageKeys.SiteCert(issuerKey, domain)
	err := d.CertMagicConfig.Storage.Delete(ctx, certPath)
	if err != nil {
		d.Logger.Debugw("attempted to remove managed TLS cert pem", "error", err)
	} else {
		d.Logger.Debugw("removed managed TLS cert pem", "error", certPath)
	}
	// remove .key
	certKey := certmagic.StorageKeys.SitePrivateKey(issuerKey, domain)
	err = d.CertMagicConfig.Storage.Delete(ctx, certKey)
	if err != nil {
		d.Logger.Debugw("attempted to remove managed TLS cert key", "error", err)
	} else {
		d.Logger.Debugw("removed managed TLS cert key", "error", certKey)
	}
	// remove .json info file
	certMeta := certmagic.StorageKeys.SiteMeta(issuerKey, domain)
	err = d.CertMagicConfig.Storage.Delete(ctx, certMeta)
	if err != nil {
		d.Logger.Debugw("attempted to remove managed TLS cert meta", "error", err)
	} else {
		d.Logger.Debugw("removed managed TLS cert meta", "error", certMeta)
	}
	// remove domain cert folder
	err = d.CertMagicConfig.Storage.Delete(ctx, sitePrefix)
	if err != nil {
		d.Logger.Debugw("attempted to remove managed TLS cert folder", "error", err)
	} else {
		d.Logger.Debugw("removed managed TLS folder", "error", sitePrefix)
	}
	// remove from certmagic cache
	certs := d.CertMagicCache.AllMatchingCertificates(domain)
	for _, cert := range certs {
		d.CertMagicCache.Remove([]string{cert.Hash()})
		d.Logger.Debugw("removed cached TLS",
			"domain", domain,
			"hash", cert.Hash(),
		)
	}
}

func (d *Domain) handleOwnManagedTLS(
	ctx context.Context,
	domain *model.Domain) (*model.Domain, error) {
	name := domain.Name.MustGet().String()
	// if the domain is set with self managed TLS
	// upload the pem and key
	key, _ := domain.OwnManagedTLSKey.Get()
	pem, _ := domain.OwnManagedTLSPem.Get()
	if len(key) > 0 && len(pem) > 0 {
		keyBuffer := bytes.NewBufferString(key)
		pemBuffer := bytes.NewBufferString(pem)

		// create root filesystem for secure certificate operations
		root, err := os.OpenRoot(d.OwnManagedCertificatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open certificate path: %s", err)
		}
		defer root.Close()

		// validate domain name directory access
		_, err = root.Stat(name)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("invalid domain name for certificate path: %s", err)
		}

		// build path for certificate operations
		// build safe path for certificate operations (validated by OpenRoot)
		// use secure file upload for certificate operations
		err = d.FileService.UploadFile(
			root,
			name+"/cert.key",
			keyBuffer,
			true,
		)
		if err != nil {
			d.Logger.Errorw(
				"failed to upload TLS private key (.key)",
				"error", err,
			)
			return nil, errs.Wrap(err)
		}
		err = d.FileService.UploadFile(
			root,
			name+"/cert.pem",
			pemBuffer,
			true,
		)
		if err != nil {
			d.Logger.Errorw(
				"failed to upload TLS certificate (.pem)",
				"error", err,
			)
			return nil, errs.Wrap(err)
		}
		// Create fresh buffers for caching since upload consumed the original buffers
		keyBufferForCache := bytes.NewBufferString(key)
		pemBufferForCache := bytes.NewBufferString(pem)
		hash, err := d.CertMagicConfig.CacheUnmanagedCertificatePEMBytes(
			ctx,
			pemBufferForCache.Bytes(),
			keyBufferForCache.Bytes(),
			[]string{name},
		)
		if err != nil {
			d.Logger.Errorw(
				"failed to cache unmanaged cert for", name,
				"error", err,
			)
			return nil, errs.Wrap(err)
		}
		d.Logger.Debugw("Cached own managed TLS",
			"domain", name,
			"hash", hash,
		)
		domain.OwnManagedTLS = nullable.NewNullableWithValue(true)
		domain.ManagedTLS = nullable.NewNullableWithValue(false)
	} else {
		domain.OwnManagedTLS = nullable.NewNullableWithValue(false)
	}
	return domain, nil
}

func (d *Domain) removeOwnManagedTLS(
	domain *model.Domain,
) error {
	name := domain.Name.MustGet().String()

	// create root filesystem for secure certificate operations
	root, err := os.OpenRoot(d.OwnManagedCertificatePath)
	if err != nil {
		return fmt.Errorf("failed to open certificate path for '%s': %s", name, err)
	}
	defer root.Close()

	// validate domain name directory exists
	_, err = root.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			// directory doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to access certificate directory for '%s': %s", name, err)
	}

	// build safe path for deletion (validated by OpenRoot)
	path := filepath.Join(d.OwnManagedCertificatePath, name)
	err = d.FileService.DeleteAll(path)
	if err != nil {
		return fmt.Errorf("failed to delete own managed TLS for '%s' as: %s", name, err)
	}
	d.Logger.Debugw("removed storage for own managed TLS", "name", name)
	certs := d.CertMagicCache.AllMatchingCertificates(name)
	for _, cert := range certs {
		d.CertMagicCache.Remove([]string{cert.Hash()})
		d.Logger.Debugw("removed cached TLS",
			"domain", name,
			"hash", cert.Hash(),
		)
	}
	return nil
}

func (d *Domain) handleSelfSignedTLS(
	ctx context.Context,
	domain *model.Domain) (*model.Domain, error) {
	selfSignedTLS, err := domain.SelfSignedTLS.Get()
	if err != nil || !selfSignedTLS {
		return domain, nil
	}

	name := domain.Name.MustGet().String()

	// create root filesystem for secure certificate operations
	root, err := os.OpenRoot(d.OwnManagedCertificatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open certificate path: %s", err)
	}
	defer root.Close()

	// validate domain name directory access
	_, err = root.Stat(name)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("invalid domain name for certificate path: %s", err)
	}

	// build path for certificate operations
	certDir := filepath.Join(d.OwnManagedCertificatePath, name)
	certKeyPath := filepath.Join(certDir, "cert.key")
	certPemPath := filepath.Join(certDir, "cert.pem")

	// generate self-signed certificate
	info := acme.NewInformationWithDefault()
	err = acme.CreateSelfSignedCert(
		d.Logger,
		info,
		[]string{name},
		certPemPath,
		certKeyPath,
	)
	if err != nil {
		d.Logger.Errorw(
			"failed to generate self-signed certificate",
			"domain", name,
			"error", err,
		)
		return nil, errs.Wrap(err)
	}

	// read generated certificate files for caching
	keyBytes, err := os.ReadFile(certKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated key file: %s", err)
	}

	pemBytes, err := os.ReadFile(certPemPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated cert file: %s", err)
	}

	// cache in certmagic
	hash, err := d.CertMagicConfig.CacheUnmanagedCertificatePEMBytes(
		ctx,
		pemBytes,
		keyBytes,
		[]string{name},
	)
	if err != nil {
		d.Logger.Errorw(
			"failed to cache self-signed cert for", name,
			"error", err,
		)
		return nil, errs.Wrap(err)
	}

	d.Logger.Debugw("cached self-signed TLS",
		"domain", name,
		"hash", hash,
	)

	domain.SelfSignedTLS = nullable.NewNullableWithValue(true)
	domain.ManagedTLS = nullable.NewNullableWithValue(false)
	domain.OwnManagedTLS = nullable.NewNullableWithValue(false)

	return domain, nil
}

func (d *Domain) removeSelfSignedTLS(
	domain *model.Domain,
) error {
	name := domain.Name.MustGet().String()

	// create root filesystem for secure certificate operations
	root, err := os.OpenRoot(d.OwnManagedCertificatePath)
	if err != nil {
		return fmt.Errorf("failed to open certificate path for '%s': %s", name, err)
	}
	defer root.Close()

	// validate domain name directory exists
	_, err = root.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			// directory doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to access certificate directory for '%s': %s", name, err)
	}

	// build safe path for deletion (validated by OpenRoot)
	path := filepath.Join(d.OwnManagedCertificatePath, name)
	err = d.FileService.DeleteAll(path)
	if err != nil {
		return fmt.Errorf("failed to delete self-signed TLS for '%s' as: %s", name, err)
	}
	d.Logger.Debugw("removed storage for self-signed TLS", "name", name)

	// remove from certmagic cache
	certs := d.CertMagicCache.AllMatchingCertificates(name)
	for _, cert := range certs {
		d.CertMagicCache.Remove([]string{cert.Hash()})
		d.Logger.Debugw("removed cached TLS",
			"domain", name,
			"hash", cert.Hash(),
		)
	}
	return nil
}

// validateProxyDomain validates proxy domain configuration
func (d *Domain) validateProxyDomain(ctx context.Context, domain *model.Domain) error {
	// validate proxy target domain format
	proxyTargetDomain, err := domain.ProxyTargetDomain.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("proxy target domain is required for proxy domains"), "proxyTargetDomain")
	}

	targetDomainStr := proxyTargetDomain.String()
	if targetDomainStr == "" {
		return validate.WrapErrorWithField(errors.New("proxy target domain cannot be empty"), "proxyTargetDomain")
	}

	// validate proxy target format - can be full URL or domain (with optional port)
	if strings.Contains(targetDomainStr, "://") {
		// full URL format - basic validation
		if !strings.HasPrefix(targetDomainStr, "http://") && !strings.HasPrefix(targetDomainStr, "https://") {
			return validate.WrapErrorWithField(errors.New("proxy target domain URL must start with http:// or https://"), "proxyTargetDomain")
		}
	} else {
		// domain-only format (with optional port) - validate as domain, ip address, or localhost
		domainPart := targetDomainStr

		// try to split host and port using net.SplitHostPort for proper handling of ipv6
		host, port, err := net.SplitHostPort(targetDomainStr)
		if err == nil {
			// port was present and successfully split
			domainPart = host

			// validate port is numeric and in valid range
			portNum, err := strconv.Atoi(port)
			if err != nil {
				return validate.WrapErrorWithField(errors.New("port must be numeric in proxy target domain"), "proxyTargetDomain")
			}
			if portNum < 1 || portNum > 65535 {
				return validate.WrapErrorWithField(errors.New("port must be between 1 and 65535"), "proxyTargetDomain")
			}
		}
		// if SplitHostPort fails, targetDomainStr has no port, which is fine

		// check if it's localhost (special case for single-label hostname)
		if strings.ToLower(domainPart) == "localhost" {
			// localhost is always valid
			return nil
		}

		// check if it's an ip address (ipv4 or ipv6)
		if net.ParseIP(domainPart) == nil {
			// not an ip address, validate as domain
			if !isValidDomain(domainPart) {
				return validate.WrapErrorWithField(errors.New("invalid domain format for proxy target domain"), "proxyTargetDomain")
			}
		}
		// if it's a valid ip address, no further validation needed
	}

	return nil
}

// isValidDomain performs basic domain name validation
func isValidDomain(domain string) bool {
	// basic checks - must have at least one dot and valid characters
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	// must contain at least one dot
	if !strings.Contains(domain, ".") {
		return false
	}

	// cannot start or end with dash or dot
	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	// check each label
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}

		// label cannot start or end with dash
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}

		// basic character check - alphanumeric and dash only
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') || char == '-') {
				return false
			}
		}
	}

	return true
}

// GetByProxyID gets domains by proxy ID
func (d *Domain) GetByProxyID(
	ctx context.Context,
	session *model.Session,
	proxyID *uuid.UUID,
) (*model.Result[model.Domain], error) {
	ae := NewAuditEvent("Domain.GetByProxyID", session)
	ae.Details["proxyID"] = proxyID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		d.LogAuthError(err)
		return nil, err
	}
	if !isAuthorized {
		d.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	result, err := d.DomainRepository.GetByProxyID(
		ctx,
		proxyID,
		&repository.DomainOption{},
	)
	if err != nil {
		d.Logger.Errorw("failed to get domains by proxy id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}
