package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-errors/errors"
	"github.com/oapi-codegen/nullable"
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

// Proxy is a Proxy service
type Proxy struct {
	Common
	ProxyRepository         *repository.Proxy
	DomainRepository        *repository.Domain
	CampaignRepository      *repository.Campaign
	CampaignTemplateService *CampaignTemplate
	DomainService           *Domain
}

// ProxyServiceConfig represents the YAML configuration for proxy
type ProxyServiceConfig struct {
	Proxy  string             `yaml:"proxy,omitempty"`
	Global *ProxyServiceRules `yaml:"global,omitempty"`
}

// ProxyServiceDomainConfig represents configuration for a specific domain mapping
type ProxyServiceDomainConfig struct {
	To      string                     `yaml:"to"`
	Access  *ProxyServiceAccessControl `yaml:"access,omitempty"`
	Capture []ProxyServiceCaptureRule  `yaml:"capture,omitempty"`
	Rewrite []ProxyServiceReplaceRule  `yaml:"rewrite,omitempty"`
}

// ProxyServiceRules represents capture and replace rules
// ProxyServiceRules represents global rules that apply to all hosts
type ProxyServiceRules struct {
	Access  *ProxyServiceAccessControl `yaml:"access,omitempty"`
	Capture []ProxyServiceCaptureRule  `yaml:"capture,omitempty"`
	Rewrite []ProxyServiceReplaceRule  `yaml:"rewrite,omitempty"`
}

// ProxyServiceAccessControl represents access control configuration
type ProxyServiceAccessControl struct {
	Mode   string                   `yaml:"mode"` // "allow" | "deny"
	Paths  []string                 `yaml:"paths"`
	OnDeny ProxyServiceDenyResponse `yaml:"on_deny"`
}

// ProxyServiceDenyResponse represents response configuration when access is denied
type ProxyServiceDenyResponse struct {
	WithSession    string `yaml:"with_session"`    // "allow" | "redirect:URL" | status code
	WithoutSession string `yaml:"without_session"` // "allow" | "redirect:URL" | status code
}

// CompilePathPatterns compiles regex patterns for all capture rules
func CompilePathPatterns(config *ProxyServiceConfigYAML) error {
	// Compile global capture rule patterns
	if config.Global != nil && config.Global.Capture != nil {
		for i := range config.Global.Capture {
			if err := compileCapturePath(&config.Global.Capture[i]); err != nil {
				return err
			}
		}
	}

	// Compile host-specific capture rule patterns
	for _, hostConfig := range config.Hosts {
		if hostConfig != nil && hostConfig.Capture != nil {
			for i := range hostConfig.Capture {
				if err := compileCapturePath(&hostConfig.Capture[i]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// compileCapturePath compiles the path pattern for a capture rule
func compileCapturePath(rule *ProxyServiceCaptureRule) error {
	if rule.Path != "" {
		pathRe, err := regexp.Compile(rule.Path)
		if err != nil {
			return fmt.Errorf("invalid regex pattern for path '%s': %w", rule.Path, err)
		}
		rule.PathRe = pathRe
	}
	return nil
}

// ProxyServiceCaptureRule represents a capture rule
type ProxyServiceCaptureRule struct {
	Name     string         `yaml:"name"`
	Method   string         `yaml:"method,omitempty"`
	Path     string         `yaml:"path,omitempty"`
	Find     string         `yaml:"find,omitempty"`
	From     string         `yaml:"from,omitempty"`
	Required *bool          `yaml:"required,omitempty"`
	PathRe   *regexp.Regexp `yaml:"-"` // compiled regex for path matching
}

// ProxyServiceReplaceRule represents a replacement rule
type ProxyServiceReplaceRule struct {
	Name    string `yaml:"name,omitempty"`
	Engine  string `yaml:"engine,omitempty"`  // "regex" (default) or "dom"
	Find    string `yaml:"find,omitempty"`    // regex pattern (regex engine) or css selector (dom engine)
	Replace string `yaml:"replace,omitempty"` // replacement value for both engines
	Action  string `yaml:"action,omitempty"`  // dom action: setText, setHtml, setAttr, removeAttr, addClass, removeClass, remove
	Target  string `yaml:"target,omitempty"`  // target matching: "first", "last", "all" (default), "1,3,5", "2-4"
	From    string `yaml:"from,omitempty"`
}

// ProxyServiceConfigYAML represents the complete YAML configuration structure that matches the actual YAML format
//
// Example YAML configuration with access control:
//
// version: "0.0"
// global:
//
//	access:
//	  mode: "deny"
//	  paths:
//	    - "^/admin/"
//	    - "^/wp-admin/"
//	    - "^/\\.git/"
//	  on_deny:
//	    with_session: 403
//	    without_session: 404
//	capture:
//	  - name: "global_navigation"
//	    path: "/important"
//
// example.com:
//
//	to: "phishing-example.com"
//	access:
//	  mode: "allow"
//	  paths:
//	    - "^/login"
//	    - "^/api/public/"
//	    - "^/assets/"
//	  on_deny:
//	    with_session: "redirect:https://phishing-example.com/"
//	    without_session: 503
//	capture:
//	  - name: "login_capture"
//	    method: "POST"
//	    path: "/login"
//	    find: "password=(.*?)&"
//	    from: "request_body"
type ProxyServiceConfigYAML struct {
	Version string                               `yaml:"version,omitempty"`
	Proxy   string                               `yaml:"proxy,omitempty"`
	Global  *ProxyServiceRules                   `yaml:"global,omitempty"`
	Hosts   map[string]*ProxyServiceDomainConfig `yaml:",inline"` // inline allows domain names as top-level keys
}

// ValidateVersion validates that the version is supported
func ValidateVersion(config *ProxyServiceConfigYAML) error {
	if config.Version != "0.0" {
		return errors.New("only version 0.0 is supported")
	}
	return nil
}

// Create creates a new Proxy
func (m *Proxy) Create(
	ctx context.Context,
	session *model.Session,
	proxy *model.Proxy,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Proxy.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	var companyID *uuid.UUID
	if cid, err := proxy.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	// validate data
	if err := proxy.Validate(); err != nil {
		m.Logger.Errorw("failed to validate proxy", "error", err)
		return nil, errs.Wrap(err)
	}

	// validate Proxy configuration
	if err := m.validateProxyConfig(ctx, proxy); err != nil {
		return nil, err
	}

	// check uniqueness
	name := proxy.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		m.ProxyRepository.DB,
		"proxies",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		m.Logger.Errorw("failed to check proxy uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		m.Logger.Debugw("proxy name is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}

	// create proxy
	id, err := m.ProxyRepository.Insert(
		ctx,
		proxy,
	)
	if err != nil {
		m.Logger.Errorw("failed to create proxy", "error", err)
		return nil, errs.Wrap(err)
	}

	// create associated domains
	err = m.createProxyDomains(ctx, session, id, proxy)
	if err != nil {
		// rollback proxy creation
		m.ProxyRepository.DeleteByID(ctx, id)
		m.Logger.Errorw("failed to create proxy domains", "error", err)
		return nil, errs.Wrap(err)
	}

	ae.Details["id"] = id.String()
	m.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets proxies
func (m *Proxy) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ProxyOption,
) (*model.Result[model.Proxy], error) {
	result := model.NewEmptyResult[model.Proxy]()
	ae := NewAuditEvent("Proxy.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = m.ProxyRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		m.Logger.Errorw("failed to get proxies", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetAllOverview gets proxies with limited data
func (m *Proxy) GetAllOverview(
	companyID *uuid.UUID, // can be null
	ctx context.Context,
	session *model.Session,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.ProxyOverview], error) {
	result := model.NewEmptyResult[model.ProxyOverview]()
	ae := NewAuditEvent("Proxy.GetAllOverview", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get proxies
	result, err = m.ProxyRepository.GetAllSubset(
		ctx,
		companyID,
		&repository.ProxyOption{
			QueryArgs: queryArgs,
		},
	)
	if err != nil {
		m.Logger.Errorw("failed to get proxies subset", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetByID gets a Proxy by ID
func (m *Proxy) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.ProxyOption,
) (*model.Proxy, error) {
	ae := NewAuditEvent("Proxy.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get proxy
	proxy, err := m.ProxyRepository.GetByID(
		ctx,
		id,
		options,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return early this is not a an error
		return nil, errs.Wrap(err)
	}
	if err != nil {
		m.Logger.Errorw("failed to get proxy by ID", "error", err)
		return nil, errs.Wrap(err)
	}

	// apply defaults to Proxy configuration for display
	if err := m.applyConfigurationDefaults(proxy); err != nil {
		m.Logger.Errorw("failed to apply configuration defaults", "error", err)
		// don't fail the request, just log the error
	}

	// no audit log on read
	return proxy, nil
}

// UpdateByID updates a Proxy by ID
func (m *Proxy) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	proxy *model.Proxy,
) error {
	ae := NewAuditEvent("Proxy.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get current
	current, err := m.ProxyRepository.GetByID(
		ctx,
		id,
		&repository.ProxyOption{},
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Debugw("failed to update proxy by ID", "error", err)
		return err
	}
	if err != nil {
		m.Logger.Errorw("failed to update proxy by ID", "error", err)
		return err
	}
	// update proxy - if a field is present and not null, update it
	if v, err := proxy.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := proxy.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			m.ProxyRepository.DB,
			"proxies",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			m.Logger.Errorw("failed to check proxy uniqueness", "error", err)
			return err
		}
		if !isOK {
			m.Logger.Debugw("proxy name is already taken", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(v)
	}
	if v, err := proxy.Description.Get(); err == nil {
		current.Description.Set(v)
	}
	if v, err := proxy.StartURL.Get(); err == nil {
		current.StartURL.Set(v)
	}
	if v, err := proxy.ProxyConfig.Get(); err == nil {
		current.ProxyConfig.Set(v)
	}

	// validate updated Proxy configuration
	if err := m.validateProxyConfigForUpdate(ctx, current, id); err != nil {
		return err
	}

	// update proxy
	err = m.ProxyRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		m.Logger.Errorw("failed to update proxy by ID", "error", err)
		return err
	}

	// update associated domains
	err = m.syncProxyDomains(ctx, session, id, current)
	if err != nil {
		m.Logger.Errorw("failed to sync proxy domains", "error", err)
		return err
	}

	ae.Details["id"] = id.String()
	m.AuditLogAuthorized(ae)

	return nil
}

// validateProxyConfigForUpdate validates Proxy configuration during update, allowing same domains for same proxy
func (m *Proxy) validateProxyConfigForUpdate(ctx context.Context, proxy *model.Proxy, proxyID *uuid.UUID) error {
	// validate Proxy configuration YAML
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("Proxy configuration is required"), "proxyConfig")
	}

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return validate.WrapErrorWithField(errors.New("invalid YAML format: "+err.Error()), "proxyConfig")
	}

	// set default values
	m.setProxyConfigDefaults(&config)

	// validate version (after defaults are applied)
	if err := ValidateVersion(&config); err != nil {
		return validate.WrapErrorWithField(err, "proxyConfig")
	}

	// validate that at least one domain mapping exists
	if len(config.Hosts) == 0 {
		return validate.WrapErrorWithField(errors.New("at least one domain mapping must be specified"), "proxyConfig")
	}

	// validate global uniqueness of capture names across all domains and global rules
	if err := m.validateGlobalCaptureNameUniqueness(&config); err != nil {
		return err
	}

	// ensure that the start URL domain is mentioned in the domain mappings
	startURL, err := proxy.StartURL.Get()
	if err == nil {
		startURLStr := startURL.String()
		var startDomain string

		// extract domain from start URL
		if strings.Contains(startURLStr, "://") {
			// full URL like https://auth.example.com/login
			parts := strings.Split(startURLStr, "://")
			if len(parts) > 1 {
				domainParts := strings.Split(parts[1], "/")
				startDomain = domainParts[0]
			}
		} else if strings.Contains(startURLStr, "/") {
			// domain/path format like auth.example.com/login
			parts := strings.Split(startURLStr, "/")
			startDomain = parts[0]
		} else {
			// just domain like auth.example.com
			startDomain = startURLStr
		}

		// check if start domain is in the domain mappings
		if startDomain != "" {
			found := false
			for originalDomain := range config.Hosts {
				if originalDomain == startDomain {
					found = true
					break
				}
			}
			if !found {
				return validate.WrapErrorWithField(
					errors.New(fmt.Sprintf("start URL domain '%s' must be included in domain mappings", startDomain)),
					"proxyConfig",
				)
			}
		}
	}

	// validate each domain mapping
	for originalDomain, domainConfig := range config.Hosts {
		if domainConfig == nil {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("domain config for '%s' is nil", originalDomain)),
				"proxyConfig",
			)
		}

		// validate that 'to' is specified
		if domainConfig.To == "" {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("'to' field is required for domain '%s'", originalDomain)),
				"proxyConfig",
			)
		}

		// validate domain-specific access control
		if err := m.validateAccessControl(domainConfig.Access); err != nil {
			return err
		}

		// validate domain-specific capture rules
		if err := m.validateCaptureRules(domainConfig.Capture); err != nil {
			return err
		}

		// validate domain-specific rewrite rules
		if err := m.validateReplaceRules(domainConfig.Rewrite); err != nil {
			return err
		}

		// note: domain uniqueness validation is skipped during updates
		// the syncProxyDomains method will handle domain management properly
	}

	// validate global rules
	if config.Global != nil {
		if err := m.validateAccessControl(config.Global.Access); err != nil {
			return err
		}
		if err := m.validateCaptureRules(config.Global.Capture); err != nil {
			return err
		}
		if err := m.validateReplaceRules(config.Global.Rewrite); err != nil {
			return err
		}
	}

	return nil
}

// validateCaptureRules validates a slice of capture rules
func (m *Proxy) validateCaptureRules(captureRules []ProxyServiceCaptureRule) error {
	// track capture names to prevent duplicates
	captureNames := make(map[string]bool)

	for _, capture := range captureRules {
		if capture.Name == "" {
			return validate.WrapErrorWithField(errors.New("capture rule name is required"), "proxyConfig")
		}

		// check for duplicate capture names
		if captureNames[capture.Name] {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("duplicate capture rule name '%s' found - each capture rule must have a unique name", capture.Name)),
				"proxyConfig",
			)
		}
		captureNames[capture.Name] = true

		if capture.Path == "" {
			return validate.WrapErrorWithField(errors.New("capture rule path is required"), "proxyConfig")
		}

		// allow empty find pattern for any method path-based navigation tracking
		isNavigationTracking := capture.Path != "" && capture.Find == ""

		if capture.Find == "" && !isNavigationTracking {
			return validate.WrapErrorWithField(
				errors.New("capture rule must have a find pattern, except for path-based navigation tracking"),
				"proxyConfig",
			)
		}

		if capture.Find != "" {
			// for cookie captures, find field contains cookie name (literal string)
			// for other captures, find field contains regex pattern
			if capture.From != "cookie" {
				if _, err := regexp.Compile(capture.Find); err != nil {
					return validate.WrapErrorWithField(
						errors.New("invalid regex pattern in capture rule: "+err.Error()),
						"proxyConfig",
					)
				}
			}
		}

		// 'from' field defaults to 'any' if not specified (handled in setProxyConfigDefaults)
		// validate 'from' field if specified
		if capture.From != "" {
			validFromValues := []string{"request_body", "request_header", "response_body", "response_header", "cookie", "any"}
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

		// validate cookie-specific rules
		if capture.From == "cookie" {
			if capture.Find == "" {
				return validate.WrapErrorWithField(
					errors.New("capture rule with from='cookie' must specify cookie name in 'find' field"),
					"proxyConfig",
				)
			}

			// validate cookie name format (basic validation)
			cookieName := capture.Find
			if len(cookieName) == 0 {
				return validate.WrapErrorWithField(
					errors.New("cookie name cannot be empty"),
					"proxyConfig",
				)
			}

			// cookie names cannot contain certain characters
			invalidChars := []string{" ", "\t", "\n", "\r", "=", ";", ","}
			for _, char := range invalidChars {
				if strings.Contains(cookieName, char) {
					return validate.WrapErrorWithField(
						errors.New(fmt.Sprintf("cookie name '%s' contains invalid character '%s'", cookieName, char)),
						"proxyConfig",
					)
				}
			}

			// method should be specified for cookie captures
			if capture.Method == "" {
				return validate.WrapErrorWithField(
					errors.New("capture rule with from='cookie' should specify HTTP method"),
					"proxyConfig",
				)
			}
		}
	}
	return nil
}

// setProxyConfigDefaults sets default values for Proxy configuration after YAML parsing
func (m *Proxy) setProxyConfigDefaults(config *ProxyServiceConfigYAML) {
	// set default version to 0.0 if not specified
	if config.Version == "" {
		config.Version = "0.0"
	}

	for domain, domainConfig := range config.Hosts {
		if domainConfig != nil && domainConfig.Capture != nil {
			for i := range domainConfig.Capture {
				// set default required to true if not specified
				if domainConfig.Capture[i].Required == nil {
					trueValue := true
					domainConfig.Capture[i].Required = &trueValue
				}
				// set default 'from' to 'any' if not specified
				if domainConfig.Capture[i].From == "" {
					domainConfig.Capture[i].From = "any"
				}
			}
		}

		// clean up rewrite rules based on engine type
		if domainConfig.Rewrite != nil {
			for i := range domainConfig.Rewrite {
				m.cleanupRewriteRule(&domainConfig.Rewrite[i])
			}
		}
		config.Hosts[domain] = domainConfig
	}

	// set defaults for global capture rules
	if config.Global != nil && config.Global.Capture != nil {
		for i := range config.Global.Capture {
			// set default required to true if not specified
			if config.Global.Capture[i].Required == nil {
				trueValue := true
				config.Global.Capture[i].Required = &trueValue
			}
			// set default 'from' to 'any' if not specified
			if config.Global.Capture[i].From == "" {
				config.Global.Capture[i].From = "any"
			}
		}
	}

	// clean up global rewrite rules based on engine type
	if config.Global != nil && config.Global.Rewrite != nil {
		for i := range config.Global.Rewrite {
			m.cleanupRewriteRule(&config.Global.Rewrite[i])
		}
	}
}

// cleanupRewriteRule ensures only relevant fields are set based on engine type
func (m *Proxy) cleanupRewriteRule(rule *ProxyServiceReplaceRule) {
	// set default engine to regex if not specified
	if rule.Engine == "" {
		rule.Engine = "regex"
	}

	// clean up fields based on engine type
	if rule.Engine == "regex" {
		// for regex engine, completely remove dom-specific fields
		rule.Action = ""
		rule.Target = ""
		// set default 'from' to 'response_body' for regex if not specified
		if rule.From == "" {
			rule.From = "response_body"
		}
	} else if rule.Engine == "dom" {
		// for dom engine, set default target if not specified
		if rule.Target == "" {
			rule.Target = "all"
		}
		// dom engine always uses response_body, force it
		rule.From = "response_body"
	}
}

// validateReplaceRules validates a slice of replace rules
func (m *Proxy) validateReplaceRules(replaceRules []ProxyServiceReplaceRule) error {
	for _, replace := range replaceRules {
		// set default engine to regex if not specified
		engine := replace.Engine
		if engine == "" {
			engine = "regex"
		}

		// validate engine type
		if engine != "regex" && engine != "dom" {
			return validate.WrapErrorWithField(
				errors.New("invalid 'engine' value in replace rule, must be 'regex' or 'dom'"),
				"proxyConfig",
			)
		}

		// validate based on engine type
		if engine == "regex" {
			if replace.Find == "" {
				return validate.WrapErrorWithField(errors.New("replace rule 'find' is required for regex engine"), "proxyConfig")
			}
			if _, err := regexp.Compile(replace.Find); err != nil {
				return validate.WrapErrorWithField(
					errors.New("invalid regex pattern in replace rule 'find': "+err.Error()),
					"proxyConfig",
				)
			}
			if replace.Replace == "" {
				return validate.WrapErrorWithField(errors.New("replace rule 'replace' is required for regex engine"), "proxyConfig")
			}
		} else if engine == "dom" {
			if replace.Find == "" {
				return validate.WrapErrorWithField(errors.New("replace rule 'find' is required for dom engine"), "proxyConfig")
			}
			if replace.Action == "" {
				return validate.WrapErrorWithField(errors.New("replace rule 'action' is required for dom engine"), "proxyConfig")
			}

			// validate dom actions
			validActions := []string{"setText", "setHtml", "setAttr", "removeAttr", "addClass", "removeClass", "remove"}
			validAction := false
			for _, action := range validActions {
				if replace.Action == action {
					validAction = true
					break
				}
			}
			if !validAction {
				return validate.WrapErrorWithField(
					errors.New("invalid 'action' value in replace rule, must be one of: "+strings.Join(validActions, ", ")),
					"proxyConfig",
				)
			}

			// validate that actions requiring a replace value have one
			if (replace.Action == "setText" || replace.Action == "setHtml" || replace.Action == "setAttr" || replace.Action == "addClass" || replace.Action == "removeClass") && replace.Replace == "" {
				return validate.WrapErrorWithField(
					errors.New("replace rule 'replace' is required for action '"+replace.Action+"'"),
					"proxyConfig",
				)
			}

			// validate target field if specified
			if replace.Target != "" {
				validTargets := []string{"first", "last", "all"}
				isValidTarget := false
				for _, target := range validTargets {
					if replace.Target == target {
						isValidTarget = true
						break
					}
				}
				// also check for numeric patterns like "1,3,5" or "2-4"
				if !isValidTarget {
					if matched, _ := regexp.MatchString(`^(\d+,)*\d+$`, replace.Target); matched {
						isValidTarget = true
					} else if matched, _ := regexp.MatchString(`^\d+-\d+$`, replace.Target); matched {
						isValidTarget = true
					}
				}
				if !isValidTarget {
					return validate.WrapErrorWithField(
						errors.New("invalid 'target' value in replace rule, must be 'first', 'last', 'all', numeric list (1,3,5), or range (2-4)"),
						"proxyConfig",
					)
				}
			}

			// dom engine doesn't use 'from' field - it always works on response_body
			// if 'from' is specified for dom engine, it's ignored (will be forced to response_body)
		}

		if replace.From != "" {
			validFromValues := []string{"request_body", "request_header", "response_body", "response_header", "any"}
			valid := false
			for _, validFrom := range validFromValues {
				if replace.From == validFrom {
					valid = true
					break
				}
			}
			if !valid {
				return validate.WrapErrorWithField(
					errors.New("invalid 'from' value in replace rule, must be one of: "+strings.Join(validFromValues, ", ")),
					"proxyConfig",
				)
			}
		}
	}
	return nil
}

// validateAccessControl validates access control configuration
func (m *Proxy) validateAccessControl(accessControl *ProxyServiceAccessControl) error {
	if accessControl == nil {
		return nil // access control is optional
	}

	// validate mode
	if accessControl.Mode != "allow" && accessControl.Mode != "deny" {
		return validate.WrapErrorWithField(
			errors.New("access control mode must be either 'allow' or 'deny'"),
			"proxyConfig",
		)
	}

	// validate paths are valid regex patterns
	for i, path := range accessControl.Paths {
		if path == "" {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("access control path %d cannot be empty", i)),
				"proxyConfig",
			)
		}
		if _, err := regexp.Compile(path); err != nil {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("invalid regex pattern in access control path '%s': %s", path, err.Error())),
				"proxyConfig",
			)
		}
	}

	// validate deny response actions
	if err := m.validateDenyAction(accessControl.OnDeny.WithSession, true); err != nil {
		return err
	}
	if err := m.validateDenyAction(accessControl.OnDeny.WithoutSession, false); err != nil {
		return err
	}

	return nil
}

// validateDenyAction validates a deny action string
func (m *Proxy) validateDenyAction(action string, withSession bool) error {
	if action == "" {
		return nil // action is optional, will use default
	}

	// check for allow action
	if action == "allow" {
		return nil
	}

	// check for redirect action
	if strings.HasPrefix(action, "redirect:") {
		url := strings.TrimPrefix(action, "redirect:")
		if url == "" {
			return validate.WrapErrorWithField(
				errors.New("redirect action must include URL: 'redirect:https://example.com'"),
				"proxyConfig",
			)
		}
		// basic URL validation
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return validate.WrapErrorWithField(
				errors.New("redirect URL must start with http:// or https://"),
				"proxyConfig",
			)
		}
		return nil
	}

	// check for status code
	if statusCode, err := strconv.Atoi(action); err == nil {
		if statusCode < 100 || statusCode > 599 {
			return validate.WrapErrorWithField(
				errors.New("status code must be between 100 and 599"),
				"proxyConfig",
			)
		}
		return nil
	}

	return validate.WrapErrorWithField(
		errors.New("invalid deny action: must be 'allow', 'redirect:URL', or a status code"),
		"proxyConfig",
	)
}

// validateProxyConfig validates Proxy configuration
func (m *Proxy) validateProxyConfig(ctx context.Context, proxy *model.Proxy) error {
	// validate Proxy configuration YAML
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return validate.WrapErrorWithField(errors.New("Proxy configuration is required"), "proxyConfig")
	}

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return validate.WrapErrorWithField(errors.New("invalid YAML format: "+err.Error()), "proxyConfig")
	}

	// set default values
	m.setProxyConfigDefaults(&config)

	// validate version (after defaults are applied)
	if err := ValidateVersion(&config); err != nil {
		return validate.WrapErrorWithField(err, "proxyConfig")
	}

	// validate that at least one domain mapping exists
	if len(config.Hosts) == 0 {
		return validate.WrapErrorWithField(errors.New("at least one domain mapping must be specified"), "proxyConfig")
	}

	// validate global uniqueness of capture names across all domains and global rules
	if err := m.validateGlobalCaptureNameUniqueness(&config); err != nil {
		return err
	}

	// ensure that the start URL domain is mentioned in the domain mappings
	startURL, err := proxy.StartURL.Get()
	if err == nil {
		startURLStr := startURL.String()
		var startDomain string

		// extract domain from start URL
		if strings.Contains(startURLStr, "://") {
			// full URL like https://auth.example.com/login
			parts := strings.Split(startURLStr, "://")
			if len(parts) > 1 {
				domainParts := strings.Split(parts[1], "/")
				startDomain = domainParts[0]
			}
		} else if strings.Contains(startURLStr, "/") {
			// domain/path format like auth.example.com/login
			parts := strings.Split(startURLStr, "/")
			startDomain = parts[0]
		} else {
			// just domain like auth.example.com
			startDomain = startURLStr
		}

		// check if start domain is in the domain mappings
		if startDomain != "" {
			found := false
			for originalDomain := range config.Hosts {
				if originalDomain == startDomain {
					found = true
					break
				}
			}
			if !found {
				return validate.WrapErrorWithField(
					errors.New(fmt.Sprintf("start URL domain '%s' must be included in domain mappings", startDomain)),
					"proxyConfig",
				)
			}
		}
	}

	// validate each domain mapping
	for originalDomain, domainConfig := range config.Hosts {
		if domainConfig == nil {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("domain config for '%s' is nil", originalDomain)),
				"proxyConfig",
			)
		}

		// validate that 'to' is specified
		if domainConfig.To == "" {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("'to' field is required for domain mapping '%s'", originalDomain)),
				"proxyConfig",
			)
		}

		// validate that phishing domain doesn't already exist (unless it's managed by this proxy)
		phishingDomainVO, err := vo.NewString255(domainConfig.To)
		if err != nil {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("invalid phishing domain format: %s", domainConfig.To)),
				"proxyConfig",
			)
		}

		existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existingDomain != nil {
			// check if this domain is managed by a different proxy or is a regular domain
			if existingDomain.Type.MustGet().String() != "proxy" {
				return validate.WrapErrorWithField(
					errors.New(fmt.Sprintf("domain '%s' already exists as a regular domain", domainConfig.To)),
					"proxyConfig",
				)
			} else {
				// it's a proxy domain, check if it belongs to a different proxy
				existingTarget, err := existingDomain.ProxyTargetDomain.Get()
				if err == nil {
					startURL, err := proxy.StartURL.Get()
					if err == nil {
						// extract domain from start URL for comparison
						startURLParsed, err := url.Parse(startURL.String())
						if err == nil && existingTarget.String() != startURLParsed.Host {
							return validate.WrapErrorWithField(
								errors.New(fmt.Sprintf("phishing domain '%s' is already used by another Proxy configuration", domainConfig.To)),
								"proxyConfig",
							)
						}
					}
				}
			}
		}

		// validate domain-specific access control
		if err := m.validateAccessControl(domainConfig.Access); err != nil {
			return err
		}

		// validate domain-specific capture rules
		if err := m.validateCaptureRules(domainConfig.Capture); err != nil {
			return err
		}

		// validate domain-specific rewrite rules
		if err := m.validateReplaceRules(domainConfig.Rewrite); err != nil {
			return err
		}

		// validate that phishing domain is not used by another proxy
		if err := m.validatePhishingDomainUniquenessByStartURL(ctx, domainConfig.To, proxy.StartURL.MustGet().String()); err != nil {
			return err
		}
	}

	// validate global rules
	if config.Global != nil {
		if err := m.validateAccessControl(config.Global.Access); err != nil {
			return err
		}
		if err := m.validateCaptureRules(config.Global.Capture); err != nil {
			return err
		}
		if err := m.validateReplaceRules(config.Global.Rewrite); err != nil {
			return err
		}
	}

	return nil
}

// validateGlobalCaptureNameUniqueness ensures all capture rule names are unique across the entire Proxy configuration
func (m *Proxy) validateGlobalCaptureNameUniqueness(config *ProxyServiceConfigYAML) error {
	allCaptureNames := make(map[string]string) // name -> location

	// collect all capture names from domain-specific rules
	for domain, domainConfig := range config.Hosts {
		if domainConfig != nil && domainConfig.Capture != nil {
			for _, capture := range domainConfig.Capture {
				if capture.Name == "" {
					continue // this will be caught by other validation
				}

				if existingLocation, exists := allCaptureNames[capture.Name]; exists {
					return validate.WrapErrorWithField(
						errors.New(fmt.Sprintf("duplicate capture rule name '%s' found in domain '%s' - already used in %s", capture.Name, domain, existingLocation)),
						"proxyConfig",
					)
				}
				allCaptureNames[capture.Name] = fmt.Sprintf("domain '%s'", domain)
			}
		}
	}

	// collect all capture names from global rules
	if config.Global != nil && config.Global.Capture != nil {
		for _, capture := range config.Global.Capture {
			if capture.Name == "" {
				continue // this will be caught by other validation
			}

			if existingLocation, exists := allCaptureNames[capture.Name]; exists {
				return validate.WrapErrorWithField(
					errors.New(fmt.Sprintf("duplicate capture rule name '%s' found in global rules - already used in %s", capture.Name, existingLocation)),
					"proxyConfig",
				)
			}
			allCaptureNames[capture.Name] = "global rules"
		}
	}

	return nil
}

// applyConfigurationDefaults applies default values to Proxy configuration for display
func (m *Proxy) applyConfigurationDefaults(proxy *model.Proxy) error {
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return err
	}

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return err
	}

	// apply defaults
	m.setProxyConfigDefaults(&config)

	// marshal back to YAML
	updatedConfigBytes, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}

	// update the Proxy configuration with defaults applied
	updatedConfigVO := vo.NewString1MBMust(string(updatedConfigBytes))
	proxy.ProxyConfig = nullable.NewNullableWithValue(*updatedConfigVO)
	return nil
}

// deleteProxyDomains deletes all domains associated with a proxy
func (m *Proxy) deleteProxyDomains(ctx context.Context, session *model.Session, proxyID *uuid.UUID, proxy *model.Proxy) error {
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return err
	}

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return err
	}

	// set default values
	m.setProxyConfigDefaults(&config)

	// delete domains for each mapping
	for _, domainConfig := range config.Hosts {
		if domainConfig == nil {
			continue
		}

		// get domain by name and delete if it's a proxy domain
		phishingDomainVO, err := vo.NewString255(domainConfig.To)
		if err != nil {
			continue
		}

		existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
		if err == nil && existingDomain != nil {
			// delete old domains that have proxy type
			if existingDomain.Type.MustGet().String() == "proxy" {
				domainID, err := existingDomain.ID.Get()
				if err == nil {
					err = m.DomainService.DeleteProxyDomain(ctx, session, &domainID)
					if err != nil {
						m.Logger.Warnw("failed to delete proxy domain",
							"proxyID", proxyID.String(),
							"domain", domainConfig.To,
							"error", err,
						)
					} else {
						m.Logger.Debugw("deleted proxy domain",
							"proxyID", proxyID.String(),
							"domain", domainConfig.To,
						)
					}
				}
			}
		}
	}

	return nil
}

// validatePhishingDomainUniqueness checks if a phishing domain is already used by another proxy
func (m *Proxy) validatePhishingDomainUniqueness(ctx context.Context, phishingDomain string, excludeProxyID *uuid.UUID) error {
	phishingDomainVO, err := vo.NewString255(phishingDomain)
	if err != nil {
		return validate.WrapErrorWithField(
			errors.New(fmt.Sprintf("invalid phishing domain format: %s", phishingDomain)),
			"proxyConfig",
		)
	}

	existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if existingDomain != nil {
		// check if this domain is managed by a different proxy
		if existingDomain.Type.MustGet().String() == "proxy" {
			// check if it's managed by the same proxy we're updating (allowed)
			if excludeProxyID != nil {
				// this is a bit of a workaround - we'd need to track which proxy owns which domain
				// for now, we'll allow updates to existing proxy domains
				// TODO: add a proxy_id field to domains table for proper tracking
				return nil
			}
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("phishing domain '%s' is already used by another proxy", phishingDomain)),
				"proxyConfig",
			)
		} else {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("domain '%s' already exists as a regular domain", phishingDomain)),
				"proxyConfig",
			)
		}
	}
	return nil
}

// validatePhishingDomainUniquenessByStartURL checks if a phishing domain is already used by another proxy using start URL comparison
func (m *Proxy) validatePhishingDomainUniquenessByStartURL(ctx context.Context, phishingDomain string, currentStartURL string) error {
	phishingDomainVO, err := vo.NewString255(phishingDomain)
	if err != nil {
		return validate.WrapErrorWithField(
			errors.New(fmt.Sprintf("invalid phishing domain format: %s", phishingDomain)),
			"proxyConfig",
		)
	}

	existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if existingDomain != nil {
		if existingDomain.Type.MustGet().String() == "proxy" {
			// check if it belongs to a different proxy by comparing target domains
			existingTarget, err := existingDomain.ProxyTargetDomain.Get()
			if err == nil {
				// extract domain from current start URL for comparison
				currentStartURLParsed, err := url.Parse(currentStartURL)
				if err != nil {
					return validate.WrapErrorWithField(
						errors.New(fmt.Sprintf("invalid start URL format: %s", currentStartURL)),
						"proxyConfig",
					)
				}

				// normalize and extract domain for comparison
				existingTargetStr := strings.ToLower(strings.TrimSpace(existingTarget.String()))
				currentHostNormalized := strings.ToLower(strings.TrimSpace(currentStartURLParsed.Host))

				// if existing target is a full URL, extract just the host part
				var existingTargetNormalized string
				if strings.Contains(existingTargetStr, "://") {
					// it's a full URL, parse it to get the host
					existingTargetParsed, err := url.Parse(existingTargetStr)
					if err != nil {
						return validate.WrapErrorWithField(
							errors.New(fmt.Sprintf("invalid existing target URL format: %s", existingTargetStr)),
							"proxyConfig",
						)
					}
					existingTargetNormalized = strings.ToLower(strings.TrimSpace(existingTargetParsed.Host))
				} else {
					// it's already just a domain
					existingTargetNormalized = existingTargetStr
				}

				if existingTargetNormalized != currentHostNormalized {
					return validate.WrapErrorWithField(
						errors.New(fmt.Sprintf("phishing domain '%s' is already used by another Proxy configuration", phishingDomain)),
						"proxyConfig",
					)
				}
			}
		} else {
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("domain '%s' already exists as a regular domain", phishingDomain)),
				"proxyConfig",
			)
		}
	}
	return nil
}

// validatePhishingDomainUniquenessForUpdate validates phishing domain uniqueness during proxy updates
func (m *Proxy) validatePhishingDomainUniquenessForUpdate(ctx context.Context, phishingDomain string, currentStartURL string, currentProxyID *uuid.UUID) error {
	m.Logger.Debugw("validating phishing domain uniqueness for update",
		"phishingDomain", phishingDomain,
		"currentStartURL", currentStartURL,
		"currentProxyID", currentProxyID.String(),
	)

	phishingDomainVO, err := vo.NewString255(phishingDomain)
	if err != nil {
		m.Logger.Errorw("invalid phishing domain format",
			"phishingDomain", phishingDomain,
			"error", err,
		)
		return validate.WrapErrorWithField(
			errors.New(fmt.Sprintf("invalid phishing domain format: %s", phishingDomain)),
			"proxyConfig",
		)
	}

	existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Errorw("error getting existing domain",
			"phishingDomain", phishingDomain,
			"error", err,
		)
		return err
	}

	if existingDomain == nil {
		m.Logger.Debugw("no existing domain found, validation passed",
			"phishingDomain", phishingDomain,
		)
		return nil
	}

	m.Logger.Debugw("existing domain found",
		"phishingDomain", phishingDomain,
		"existingDomainType", existingDomain.Type.MustGet().String(),
	)

	if existingDomain.Type.MustGet().String() == "proxy" {
		// check if it belongs to a different proxy by comparing target domains
		existingTarget, err := existingDomain.ProxyTargetDomain.Get()
		if err != nil {
			m.Logger.Errorw("error getting existing domain proxy target",
				"phishingDomain", phishingDomain,
				"error", err,
			)
			return err
		}

		m.Logger.Debugw("existing domain proxy target found",
			"phishingDomain", phishingDomain,
			"existingTarget", existingTarget.String(),
		)

		// extract domain from current start URL for comparison
		currentStartURLParsed, err := url.Parse(currentStartURL)
		if err != nil {
			m.Logger.Errorw("error parsing current start URL",
				"currentStartURL", currentStartURL,
				"error", err,
			)
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("invalid start URL format: %s", currentStartURL)),
				"proxyConfig",
			)
		}

		// normalize and extract domain for comparison
		existingTargetStr := strings.ToLower(strings.TrimSpace(existingTarget.String()))
		currentHostNormalized := strings.ToLower(strings.TrimSpace(currentStartURLParsed.Host))

		// if existing target is a full URL, extract just the host part
		var existingTargetNormalized string
		if strings.Contains(existingTargetStr, "://") {
			// it's a full URL, parse it to get the host
			existingTargetParsed, err := url.Parse(existingTargetStr)
			if err != nil {
				m.Logger.Errorw("error parsing existing target URL",
					"existingTarget", existingTargetStr,
					"error", err,
				)
				return err
			}
			existingTargetNormalized = strings.ToLower(strings.TrimSpace(existingTargetParsed.Host))
		} else {
			// it's already just a domain
			existingTargetNormalized = existingTargetStr
		}

		m.Logger.Debugw("comparing normalized domains",
			"phishingDomain", phishingDomain,
			"existingTargetStr", existingTargetStr,
			"existingTargetNormalized", existingTargetNormalized,
			"currentHostNormalized", currentHostNormalized,
		)

		// if target domains don't match, it belongs to a different proxy
		if existingTargetNormalized != currentHostNormalized {
			m.Logger.Warnw("phishing domain belongs to different proxy",
				"phishingDomain", phishingDomain,
				"existingTargetNormalized", existingTargetNormalized,
				"currentHostNormalized", currentHostNormalized,
				"currentProxyID", currentProxyID.String(),
			)
			return validate.WrapErrorWithField(
				errors.New(fmt.Sprintf("phishing domain '%s' is already used by another Proxy configuration (existing target: %s, current target: %s)", phishingDomain, existingTargetNormalized, currentHostNormalized)),
				"proxyConfig",
			)
		}

		// if target domains match, this domain belongs to the current proxy being updated, so it's allowed
		m.Logger.Debugw("phishing domain belongs to current proxy, allowing reuse",
			"domain", phishingDomain,
			"proxyID", currentProxyID.String(),
			"existingTarget", existingTargetNormalized,
			"currentHost", currentHostNormalized,
		)
	} else {
		m.Logger.Warnw("domain exists as regular domain, not proxy",
			"phishingDomain", phishingDomain,
			"existingDomainType", existingDomain.Type.MustGet().String(),
		)
		return validate.WrapErrorWithField(
			errors.New(fmt.Sprintf("domain '%s' already exists as a regular domain", phishingDomain)),
			"proxyConfig",
		)
	}
	return nil
}

// createProxyDomains creates domains for the proxy based on the configuration
func (m *Proxy) createProxyDomains(ctx context.Context, session *model.Session, proxyID *uuid.UUID, proxy *model.Proxy) error {
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return fmt.Errorf("failed to parse proxy config YAML: %w", err)
	}

	// set default values
	m.setProxyConfigDefaults(&config)

	var companyID *uuid.UUID
	if cid, err := proxy.CompanyID.Get(); err == nil {
		companyID = &cid
	}

	startURL := proxy.StartURL.MustGet()
	createdDomains := make([]string, 0)

	// create domains for each mapping
	for originalDomain, domainConfig := range config.Hosts {
		if domainConfig == nil {
			continue
		}

		if domainConfig.To == "" {
			m.Logger.Warnw("empty 'to' field in domain config",
				"proxyID", proxyID.String(),
				"originalDomain", originalDomain,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("'to' field is required for domain mapping '%s'", originalDomain)
		}

		// check if domain already exists (might be from previous failed attempt)
		phishingDomainVO, err := vo.NewString255(domainConfig.To)
		if err != nil {
			m.Logger.Warnw("invalid phishing domain format",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("invalid phishing domain format %s: %w", domainConfig.To, err)
		}

		existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
		if err == nil && existingDomain != nil {
			// domain already exists, check if it's compatible
			if existingDomain.Type.MustGet().String() == "proxy" {
				existingTarget, err := existingDomain.ProxyTargetDomain.Get()
				if err == nil && existingTarget.String() == startURL.String() {
					// compatible existing domain, skip creation
					m.Logger.Debugw("proxy domain already exists, skipping creation",
						"proxyID", proxyID.String(),
						"domain", domainConfig.To,
					)
					createdDomains = append(createdDomains, domainConfig.To)
					continue
				}
			}
			// incompatible domain exists
			m.Logger.Warnw("incompatible domain already exists",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"existingType", existingDomain.Type.MustGet().String(),
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("domain %s already exists and is incompatible", domainConfig.To)
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			// database error
			m.Logger.Errorw("failed to check existing domain",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to check existing domain %s: %w", domainConfig.To, err)
		}

		// create new domain
		domain := &model.Domain{}
		domain.Name.Set(*vo.NewString255Must(domainConfig.To))
		domain.Type.Set(*vo.NewString32Must("proxy"))
		domain.ProxyID.Set(*proxyID)

		// set the target domain to the original domain from the YAML config
		proxyTargetDomain, err := vo.NewOptionalString255(originalDomain)
		if err != nil {
			m.Logger.Errorw("failed to create proxy target domain",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"startURL", startURL.String(),
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to create proxy target domain for %s: %w", domainConfig.To, err)
		}
		domain.ProxyTargetDomain.Set(*proxyTargetDomain)

		domain.HostWebsite.Set(false)
		domain.ManagedTLS.Set(true)
		domain.OwnManagedTLS.Set(false)

		pageContent, err := vo.NewOptionalString1MB("")
		if err != nil {
			m.Logger.Errorw("failed to create page content",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to create page content for %s: %w", domainConfig.To, err)
		}
		domain.PageContent.Set(*pageContent)

		pageNotFoundContent, err := vo.NewOptionalString1MB("")
		if err != nil {
			m.Logger.Errorw("failed to create page not found content",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to create page not found content for %s: %w", domainConfig.To, err)
		}
		domain.PageNotFoundContent.Set(*pageNotFoundContent)

		redirectURL, err := vo.NewOptionalString1024("")
		if err != nil {
			m.Logger.Errorw("failed to create redirect URL",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to create redirect URL for %s: %w", domainConfig.To, err)
		}
		domain.RedirectURL.Set(*redirectURL)

		if companyID != nil {
			domain.CompanyID.Set(*companyID)
		}

		_, err = m.DomainService.CreateProxyDomain(ctx, session, domain)
		if err != nil {
			m.Logger.Errorw("failed to create proxy domain",
				"proxyID", proxyID.String(),
				"domain", domainConfig.To,
				"error", err,
			)
			// rollback created domains on error
			m.rollbackCreatedDomains(ctx, session, createdDomains)
			return fmt.Errorf("failed to create domain %s: %w", domainConfig.To, err)
		}

		createdDomains = append(createdDomains, domainConfig.To)
		m.Logger.Debugw("created proxy domain",
			"proxyID", proxyID.String(),
			"domain", domainConfig.To,
		)
	}

	m.Logger.Infow("successfully created all proxy domains",
		"proxyID", proxyID.String(),
		"domainsCreated", len(createdDomains),
		"domains", createdDomains,
	)

	return nil
}

// rollbackCreatedDomains attempts to delete domains that were created during a failed proxy creation
func (m *Proxy) rollbackCreatedDomains(ctx context.Context, session *model.Session, createdDomains []string) {
	for _, domainName := range createdDomains {
		phishingDomainVO, err := vo.NewString255(domainName)
		if err != nil {
			m.Logger.Warnw("failed to create domain VO for rollback",
				"domain", domainName,
				"error", err,
			)
			continue
		}

		existingDomain, err := m.DomainRepository.GetByName(ctx, phishingDomainVO, &repository.DomainOption{})
		if err != nil {
			m.Logger.Warnw("failed to get domain for rollback",
				"domain", domainName,
				"error", err,
			)
			continue
		}

		if existingDomain != nil && existingDomain.Type.MustGet().String() == "proxy" {
			domainID, err := existingDomain.ID.Get()
			if err == nil {
				err = m.DomainService.DeleteProxyDomain(ctx, session, &domainID)
				if err != nil {
					m.Logger.Warnw("failed to rollback proxy domain",
						"domain", domainName,
						"error", err,
					)
				} else {
					m.Logger.Debugw("rolled back proxy domain",
						"domain", domainName,
					)
				}
			}
		}
	}
}

// syncProxyDomains synchronizes domains for the proxy based on the configuration
func (m *Proxy) syncProxyDomains(ctx context.Context, session *model.Session, proxyID *uuid.UUID, proxy *model.Proxy) error {
	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return fmt.Errorf("failed to get proxy config for sync: %w", err)
	}

	// get current proxy domains by proxy ID
	currentDomainsResult, err := m.DomainService.GetByProxyID(ctx, session, proxyID)
	if err != nil {
		return fmt.Errorf("failed to get current proxy domains: %w", err)
	}

	// collect specific errors for better reporting
	var syncErrors []string

	currentDomains := make(map[string]*model.Domain)
	for _, domain := range currentDomainsResult.Rows {
		currentDomains[domain.Name.MustGet().String()] = domain
	}

	m.Logger.Debugw("found existing proxy domains for sync",
		"proxyID", proxyID.String(),
		"currentDomainCount", len(currentDomains),
		"currentDomains", func() []string {
			domains := make([]string, 0, len(currentDomains))
			for name := range currentDomains {
				domains = append(domains, name)
			}
			return domains
		}(),
	)

	// parse complete YAML structure
	var config ProxyServiceConfigYAML
	if err := yaml.Unmarshal([]byte(proxyConfig.String()), &config); err != nil {
		return fmt.Errorf("failed to parse proxy config YAML for sync: %w", err)
	}

	// set default values
	m.setProxyConfigDefaults(&config)

	// get desired domains from config
	desiredDomains := make(map[string]string) // phishing domain -> original domain
	for originalDomain, domainConfig := range config.Hosts {
		if domainConfig == nil {
			continue
		}

		if domainConfig.To != "" {
			desiredDomains[domainConfig.To] = originalDomain
		}
	}

	m.Logger.Debugw("parsed desired domains from config",
		"proxyID", proxyID.String(),
		"desiredDomainCount", len(desiredDomains),
		"desiredDomains", func() []string {
			domains := make([]string, 0, len(desiredDomains))
			for name := range desiredDomains {
				domains = append(domains, name)
			}
			return domains
		}(),
	)

	// delete domains that are no longer needed
	deletedCount := 0
	for phishingDomain, domain := range currentDomains {
		if _, exists := desiredDomains[phishingDomain]; !exists {
			m.Logger.Debugw("domain marked for deletion",
				"proxyID", proxyID.String(),
				"domain", phishingDomain,
			)
			domainID, err := domain.ID.Get()
			if err == nil {
				err = m.DomainService.DeleteProxyDomain(ctx, session, &domainID)
				if err != nil {
					m.Logger.Warnw("failed to delete removed proxy domain",
						"proxyID", proxyID.String(),
						"domain", phishingDomain,
						"error", err,
					)
				} else {
					m.Logger.Infow("deleted removed proxy domain",
						"proxyID", proxyID.String(),
						"domain", phishingDomain,
					)
					deletedCount++
				}
			} else {
				m.Logger.Warnw("failed to get domain ID for deletion",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"error", err,
				)
			}
		} else {
			m.Logger.Debugw("domain still needed, keeping",
				"proxyID", proxyID.String(),
				"domain", phishingDomain,
			)
		}
	}

	// create or update domains that are needed
	createdCount := 0
	updatedCount := 0
	errorCount := 0

	for phishingDomain, originalDomain := range desiredDomains {
		if existingDomain, exists := currentDomains[phishingDomain]; exists {
			// domain already exists, check if target domain needs updating
			needsUpdate := false
			currentTarget, err := existingDomain.ProxyTargetDomain.Get()

			if err != nil || currentTarget.String() != originalDomain {
				// update the target domain
				proxyTargetDomain, err := vo.NewOptionalString255(originalDomain)
				if err == nil {
					existingDomain.ProxyTargetDomain.Set(*proxyTargetDomain)
					needsUpdate = true
				} else {
					m.Logger.Warnw("failed to create proxy target domain for update",
						"proxyID", proxyID.String(),
						"domain", phishingDomain,
						"error", err,
					)
					errorCount++
					continue
				}
			}

			if needsUpdate {
				domainID, err := existingDomain.ID.Get()
				if err == nil {
					err = m.DomainService.UpdateProxyDomain(ctx, session, &domainID, existingDomain)
					if err != nil {
						m.Logger.Warnw("failed to update existing proxy domain",
							"proxyID", proxyID.String(),
							"domain", phishingDomain,
							"error", err,
						)
						errorCount++
					} else {
						m.Logger.Debugw("updated existing proxy domain",
							"proxyID", proxyID.String(),
							"domain", phishingDomain,
						)
						updatedCount++
					}
				} else {
					m.Logger.Warnw("failed to get domain ID for update",
						"proxyID", proxyID.String(),
						"domain", phishingDomain,
						"error", err,
					)
					errorCount++
				}
			}
		} else {
			// create new domain
			domain := &model.Domain{}
			domain.Name.Set(*vo.NewString255Must(phishingDomain))
			domain.Type.Set(*vo.NewString32Must("proxy"))
			domain.ProxyID.Set(*proxyID)

			proxyTargetDomain, err := vo.NewOptionalString255(originalDomain)
			if err != nil {
				m.Logger.Warnw("failed to create proxy target domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"originalDomain", originalDomain,
					"error", err,
				)
				errorCount++
				continue
			}
			domain.ProxyTargetDomain.Set(*proxyTargetDomain)

			domain.HostWebsite.Set(false)
			domain.ManagedTLS.Set(true)
			domain.OwnManagedTLS.Set(false)

			pageContent, err := vo.NewOptionalString1MB("")
			if err != nil {
				m.Logger.Warnw("failed to create page content for proxy domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"error", err,
				)
				errorCount++
				continue
			}
			domain.PageContent.Set(*pageContent)

			pageNotFoundContent, err := vo.NewOptionalString1MB("")
			if err != nil {
				m.Logger.Warnw("failed to create page not found content for proxy domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"error", err,
				)
				errorCount++
				continue
			}
			domain.PageNotFoundContent.Set(*pageNotFoundContent)

			redirectURL, err := vo.NewOptionalString1024("")
			if err != nil {
				errMsg := fmt.Sprintf("failed to create redirect URL for domain '%s': %v", phishingDomain, err)
				m.Logger.Warnw("failed to create redirect URL for proxy domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"error", err,
				)
				syncErrors = append(syncErrors, errMsg)
				errorCount++
				continue
			}
			domain.RedirectURL.Set(*redirectURL)

			var companyID *uuid.UUID
			if cid, err := proxy.CompanyID.Get(); err == nil {
				companyID = &cid
				domain.CompanyID.Set(*companyID)
			}

			_, err = m.DomainService.CreateProxyDomain(ctx, session, domain)
			if err != nil {
				errMsg := fmt.Sprintf("failed to create domain '%s': %v", phishingDomain, err)
				m.Logger.Warnw("failed to create new proxy domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
					"error", err,
				)
				syncErrors = append(syncErrors, errMsg)
				errorCount++
			} else {
				m.Logger.Debugw("created new proxy domain",
					"proxyID", proxyID.String(),
					"domain", phishingDomain,
				)
				createdCount++
			}
		}
	}

	m.Logger.Infow("completed proxy domain synchronization",
		"proxyID", proxyID.String(),
		"domainsDeleted", deletedCount,
		"domainsCreated", createdCount,
		"domainsUpdated", updatedCount,
		"errors", errorCount,
	)

	if errorCount > 0 {
		errorDetails := strings.Join(syncErrors, "; ")
		return fmt.Errorf("proxy domain sync failed with %d errors: %s", errorCount, errorDetails)
	}

	return nil
}

// DeleteByID deletes a proxy by ID
func (m *Proxy) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Proxy.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		m.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		m.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// get current proxy before deletion to access its domains
	current, err := m.ProxyRepository.GetByID(ctx, id, &repository.ProxyOption{})
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		m.Logger.Errorw("failed to get proxy for domain cleanup", "error", err)
		return err
	}

	// delete associated proxy domains
	if current != nil {
		err = m.deleteProxyDomains(ctx, session, id, current)
		if err != nil {
			m.Logger.Errorw("failed to delete proxy domains", "error", err)
			// continue with proxy deletion even if domain cleanup fails
		}
	}

	// remove the relation from campaign templates
	err = m.CampaignTemplateService.RemoveProxiesByProxyID(
		ctx,
		session,
		id,
	)
	if err != nil {
		m.Logger.Errorw("failed to remove proxy ID relations from campaign templates", "error", err)
		return err
	}

	// delete proxy
	err = m.ProxyRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		m.Logger.Errorw("failed to delete proxy by ID", "error", err)
		return err
	}
	m.AuditLogAuthorized(ae)

	return nil
}
