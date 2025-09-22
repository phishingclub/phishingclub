package service

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-errors/errors"
	"gopkg.in/yaml.v3"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Page is a Page service
type Page struct {
	Common
	PageRepository          *repository.Page
	CampaignRepository      *repository.Campaign
	CampaignTemplateService *CampaignTemplate
	TemplateService         *Template
	DomainRepository        *repository.Domain
}

// ProxyConfig represents the YAML configuration for proxy pages
type ProxyConfig struct {
	Default map[string]interface{}     `yaml:"default,omitempty"`
	Hosts   map[string]ProxyHostConfig `yaml:",inline"`
}

// ProxyHostConfig represents configuration for a specific host
type ProxyHostConfig struct {
	Proxy   string             `yaml:"proxy,omitempty"`
	Domain  string             `yaml:"domain,omitempty"`
	Capture []ProxyCaptureRule `yaml:"capture,omitempty"`
	Replace []ProxyReplaceRule `yaml:"replace,omitempty"`
}

// ProxyCaptureRule represents a capture rule
type ProxyCaptureRule struct {
	Name     string `yaml:"name"`
	Method   string `yaml:"method,omitempty"`
	Path     string `yaml:"path,omitempty"`
	Pattern  string `yaml:"pattern,omitempty"`
	Find     string `yaml:"find"`
	From     string `yaml:"from,omitempty"`
	Required *bool  `yaml:"required,omitempty"`
}

// ProxyReplaceRule represents a replace rule
type ProxyReplaceRule struct {
	Name    string `yaml:"name"`
	Find    string `yaml:"find"`
	Replace string `yaml:"replace"`
	From    string `yaml:"from,omitempty"`
}

// Create creates a new page
func (p *Page) Create(
	ctx context.Context,
	session *model.Session,
	page *model.Page,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Page.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	var companyID *uuid.UUID
	if cid, err := page.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	// validate data
	if err := page.Validate(); err != nil {
		p.Logger.Errorw("failed to validate page", "error", err)
		return nil, errs.Wrap(err)
	}
	// validate based on page type
	pageType, _ := page.Type.Get()
	if pageType.String() == "proxy" {
		// validate proxy configuration
		if err := p.validateProxyPage(ctx, page); err != nil {
			return nil, err
		}
	} else {
		// validate template content for regular pages
		if content, err := page.Content.Get(); err == nil {
			if err := p.TemplateService.ValidatePageTemplate(content.String()); err != nil {
				p.Logger.Errorw("failed to validate page template", "error", err)
				return nil, validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
			}
		}
	}
	// check uniqueness
	name := page.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		p.PageRepository.DB,
		"pages",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		p.Logger.Errorw("failed to check page uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		p.Logger.Debugw("page name is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// create page
	id, err := p.PageRepository.Insert(
		ctx,
		page,
	)
	if err != nil {
		p.Logger.Errorw("failed to create page", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	p.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets pages
func (p *Page) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	ae := NewAuditEvent("Page.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = p.PageRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		p.Logger.Errorw("failed to get pages", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetByID gets a page by ID
func (p *Page) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.PageOption,
) (*model.Page, error) {
	ae := NewAuditEvent("Page.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get page
	page, err := p.PageRepository.GetByID(
		ctx,
		id,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not a an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		p.Logger.Errorw("failed to get page by ID", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read

	return page, nil
}

// GetByCompanyID gets a page by company ID
func (p *Page) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	ae := NewAuditEvent("Page.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get pages
	result, err = p.PageRepository.GetAllByCompanyID(
		ctx,
		companyID,
		&repository.PageOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not a an error
		return result, errs.Wrap(err)
	}
	if err != nil {
		p.Logger.Errorw("failed to get page by company ID", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read

	return result, nil
}

// UpdateByID updates a page by ID
func (p *Page) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	page *model.Page,
) error {
	ae := NewAuditEvent("Page.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get current
	current, err := p.PageRepository.GetByID(
		ctx,
		id,
		&repository.PageOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		p.Logger.Debugw("failed to update page by ID", "error", err)
		return err
	}
	if err != nil {
		p.Logger.Errorw("failed to update page by ID", "error", err)
		return err
	}
	// update page - if a field is present and not null, update it
	if v, err := page.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := page.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			p.PageRepository.DB,
			"pages",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			p.Logger.Errorw("failed to check page uniqueness", "error", err)
			return err
		}
		if !isOK {
			p.Logger.Debugw("page name is already taken", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(v)
	}
	if v, err := page.Type.Get(); err == nil {
		current.Type.Set(v)
	}
	if v, err := page.TargetURL.Get(); err == nil {
		current.TargetURL.Set(v)
	}
	if v, err := page.ProxyConfig.Get(); err == nil {
		current.ProxyConfig.Set(v)
	}
	if v, err := page.Content.Get(); err == nil {
		current.Content.Set(v)
	}

	// validate based on updated page type
	updatedPageType, _ := current.Type.Get()
	if updatedPageType.String() == "proxy" {
		// validate proxy configuration
		if err := p.validateProxyPage(ctx, current); err != nil {
			return err
		}
	} else {
		// validate template content for regular pages
		if content, err := current.Content.Get(); err == nil {
			if err := p.TemplateService.ValidatePageTemplate(content.String()); err != nil {
				p.Logger.Errorw("failed to validate page template", "error", err)
				return validate.WrapErrorWithField(errors.New("invalid template: "+err.Error()), "content")
			}
		}
	}
	// update page
	err = p.PageRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		p.Logger.Errorw("failed to update page by ID", "error", err)
		return err
	}
	ae.Details["id"] = id.String()
	p.AuditLogNotAuthorized(ae)

	return nil
}

// validateProxyPage validates proxy page configuration
func (p *Page) validateProxyPage(ctx context.Context, page *model.Page) error {
	// validate target URL format
	targetURL, err := page.TargetURL.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("target URL is required for proxy pages"), "targetURL")
	}

	parsedURL, err := url.Parse(targetURL.String())
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return validate.WrapErrorWithField(errors.New("invalid target URL format - must be a valid HTTP or HTTPS URL"), "targetURL")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return validate.WrapErrorWithField(errors.New("target URL must use HTTP or HTTPS protocol"), "targetURL")
	}

	// validate proxy configuration YAML
	proxyConfig, err := page.ProxyConfig.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("proxy configuration is required for proxy pages"), "proxyConfig")
	}

	var config ProxyConfig
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return validate.WrapErrorWithField(errors.New("invalid YAML format: "+err.Error()), "proxyConfig")
	}

	// validate that all referenced domains in the config support proxy
	for hostname, hostConfig := range config.Hosts {
		if hostConfig.Domain != "" {
			domainName, err := vo.NewString255(hostConfig.Domain)
			if err != nil {
				return validate.WrapErrorWithField(
					errors.New("invalid domain name format"),
					"proxyConfig",
				)
			}

			_, err = p.DomainRepository.GetByName(ctx, domainName, &repository.DomainOption{})
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return validate.WrapErrorWithField(
						errors.New("referenced domain '"+hostConfig.Domain+"' not found"),
						"proxyConfig",
					)
				}
				return err
			}
		}

		// validate capture rules
		for _, capture := range hostConfig.Capture {
			if capture.Name == "" {
				return validate.WrapErrorWithField(errors.New("capture rule name is required"), "proxyConfig")
			}
			if capture.Pattern == "" && capture.Path == "" {
				return validate.WrapErrorWithField(
					errors.New("capture rule must have either pattern or path"),
					"proxyConfig",
				)
			}
			if capture.Pattern != "" {
				if _, err := regexp.Compile(capture.Pattern); err != nil {
					return validate.WrapErrorWithField(
						errors.New("invalid regex pattern in capture rule: "+err.Error()),
						"proxyConfig",
					)
				}
			}
			if capture.Path != "" {
				if _, err := regexp.Compile(capture.Path); err != nil {
					return validate.WrapErrorWithField(
						errors.New("invalid regex pattern for path in capture rule: "+err.Error()),
						"proxyConfig",
					)
				}
			}
			if capture.From != "" {
				validFromValues := []string{"request_body", "request_header", "response_body", "response_header", "any"}
				valid := false
				for _, validFrom := range validFromValues {
					if capture.From == validFrom {
						valid = true
						break
					}
				}
				if !valid {
					return validate.WrapErrorWithField(
						errors.New("invalid 'from' value in capture rule, must be one of: "+strings.Join(validFromValues, ", ")),
						"proxyConfig",
					)
				}
			}
		}

		// validate replace rules
		for _, replace := range hostConfig.Replace {
			if replace.Find == "" {
				return validate.WrapErrorWithField(errors.New("replace rule 'find' is required"), "proxyConfig")
			}
			if _, err := regexp.Compile(replace.Find); err != nil {
				return validate.WrapErrorWithField(
					errors.New("invalid regex pattern in replace rule 'find': "+err.Error()),
					"proxyConfig",
				)
			}
		}

		p.Logger.Debugw("validated proxy host config", "hostname", hostname)
	}

	return nil
}

// DeleteByID deletes a page by ID
func (p *Page) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Page.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		p.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		p.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// remove the relation to campaign allow deny PageRepository
	err = p.CampaignRepository.RemoveDenyPageByDenyPageIDs(
		ctx,
		[]*uuid.UUID{id},
	)
	// delete the relation from campaign templates
	err = p.CampaignTemplateService.RemovePagesByPageID(
		ctx,
		session,
		id,
	)
	if err != nil {
		p.Logger.Errorw("failed to remove page ID relations from campaign templates", "error", err)
		return err
	}
	// delete page
	err = p.PageRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		p.Logger.Errorw("failed to delete page by ID", "error", err)
		return err
	}
	p.AuditLogAuthorized(ae)

	return nil
}
