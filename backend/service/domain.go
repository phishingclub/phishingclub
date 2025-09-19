package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-errors/errors"

	"github.com/caddyserver/certmagic"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
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

// Create creates a new domain
func (d *Domain) Create(
	ctx context.Context,
	session *model.Session,
	domain *model.Domain,
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
	// validate data
	if err := domain.Validate(); err != nil {
		// d.Logger.Debugf("failed to validate domain", "error", err)
		return nil, errs.Wrap(err)
	}
	// validate template content if present
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
	// set the supplied field on the existing domain
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
	// validate
	if err := current.Validate(); err != nil {
		d.Logger.Errorw("failed to validate domain", "error", err)
		return err
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

// DeleteByID
func (d *Domain) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
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
		path, err := securejoin.SecureJoin(d.OwnManagedCertificatePath, name)
		if err != nil {
			return nil, fmt.Errorf("failed to join cert path and domain name: %s", err)
		}
		err = d.FileService.UploadFile(
			ctx,
			path+"/cert.key",
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
			ctx,
			path+"/cert.pem",
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
		keyBuffer = bytes.NewBufferString(key)
		pemBuffer = bytes.NewBufferString(pem)
		hash, err := d.CertMagicConfig.CacheUnmanagedCertificatePEMBytes(
			ctx,
			pemBuffer.Bytes(),
			keyBuffer.Bytes(),
			[]string{},
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
	path, err := securejoin.SecureJoin(d.OwnManagedCertificatePath, name)
	if err != nil {
		return fmt.Errorf("failed to delete own managed TLS for '%s' as: %s", name, err)
	}
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
