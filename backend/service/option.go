package service

import (
	"context"
	"strconv"

	"github.com/go-errors/errors"
	"github.com/google/uuid"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Option is a service for Option
type Option struct {
	Common
	OptionRepository *repository.Option
	DomainRepository *repository.Domain
}

// GetOption gets an option
func (o *Option) GetOption(
	ctx context.Context,
	session *model.Session,
	key string,
) (*model.Option, error) {
	ae := NewAuditEvent("Option.GetOption", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	opt, err := o.OptionRepository.GetByKey(
		ctx,
		key,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		o.Logger.Errorw("failed to get option with key",
			"key", key,
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	return opt, nil
}

// GetOption gets an option
func (o *Option) GetOptionWithoutAuth(
	ctx context.Context,
	key string,
) (*model.Option, error) {
	opt, err := o.OptionRepository.GetByKey(
		ctx,
		key,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		o.Logger.Errorw("failed to get option with key",
			"key", key,
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	return opt, nil
}

// MaskSSOSecret masks the sso secret
func (o *Option) MaskSSOSecret(opt *model.Option) (*model.Option, error) {
	a, err := model.NewSSOOptionFromJSON([]byte(opt.Value.String()))
	if err != nil {
		o.Logger.Errorw("failed to read sso option", "error", err)
		return nil, errs.Wrap(err)
	}
	// mask the key
	a.ClientSecret = *vo.NewOptionalString1024Must("")
	b, err := a.ToJSON()
	if err != nil {
		o.Logger.Errorw("failed to mask sso secret", "error", err)
		return nil, errs.Wrap(err)
	}
	c, err := vo.NewOptionalString1MB(string(b))
	if err != nil {
		o.Logger.Errorw("failed to mask secret option", "error", err)
		return nil, errs.Wrap(err)
	}
	opt.Value = *c
	return opt, nil
}

// SetOptionByKey sets an option
func (o *Option) SetOptionByKey(
	ctx context.Context,
	session *model.Session,
	option *model.Option,
) error {
	ae := NewAuditEvent("Option.SetOptionByKey", session)
	ae.Details["key"] = option.Key.String()
	ae.Details["value"] = option.Value.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	k := option.Key.String()
	v := option.Value.String()
	switch k {
	case data.OptionKeyMaxFileUploadSizeMB:
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			o.Logger.Debugw("invalid max file size",
				"n", n,
				"error", err,
			)
			return validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("invalid max"),
				),
				"max file size",
			)
		}
	case data.OptionKeyRepeatOffenderMonths:
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			o.Logger.Debugw("invalid repeat offender months",
				"n", n,
				"error", err,
			)
			return validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("invalid months"),
				),
				"repeat offender months",
			)
		}
	case data.OptionKeyLogLevel:
		// validate log level value
		if v != "debug" && v != "info" && v != "warn" && v != "error" {
			o.Logger.Debugw("invalid log level value",
				"value", v,
			)
			return validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("invalid log level"),
				),
				"log level",
			)
		}
	case data.OptionKeyDBLogLevel:
		// validate db log level value
		if v != "silent" && v != "info" && v != "warn" && v != "error" {
			o.Logger.Debugw("invalid db log level value",
				"value", v,
			)
			return validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("invalid db log level"),
				),
				"db log level",
			)
		}
	case data.OptionKeyAdminSSOLogin:
		// is allow listed
	case data.OptionKeyDisplayMode:
		// validate display mode value
		if v != data.OptionValueDisplayModeWhitebox && v != data.OptionValueDisplayModeBlackbox {
			o.Logger.Debugw("invalid display mode value",
				"value", v,
			)
			return validate.WrapErrorWithField(
				errs.NewValidationError(
					errors.New("invalid display mode"),
				),
				"display mode",
			)
		}
	case data.OptionKeyAutoPruneOrphanedRecipients:
		// stored as JSON — validate by parsing
		if _, err := model.NewAutoPruneOptionFromJSON([]byte(v)); err != nil {
			o.Logger.Debugw("invalid auto-prune option value", "value", v)
			return validate.WrapErrorWithField(
				errs.NewValidationError(errors.New("invalid value")),
				"value",
			)
		}
	case data.OptionKeyReportPDFEnabled:
		if v != "true" && v != "false" {
			return validate.WrapErrorWithField(
				errs.NewValidationError(errors.New("invalid value")),
				"value",
			)
		}
	case data.OptionKeyObfuscationTemplate:
		// is allow listed
	default:
		o.Logger.Debugw("invalid settings key", "key", k)
		return validate.WrapErrorWithField(
			errs.NewValidationError(
				errors.New("invalid option"),
			),
			"key",
		)
	}
	// update options
	err = o.OptionRepository.UpdateByKey(
		ctx,
		option,
	)
	if err != nil {
		o.Logger.Errorw("failed to update option by key", "error", err)
		return err
	}
	o.AuditLogAuthorized(ae)
	return nil
}

// GetAutoPruneOption returns the single auto-prune option row (requires global auth).
// Both the global flag and all per-company entries are embedded in the returned value.
func (o *Option) GetAutoPruneOption(
	ctx context.Context,
	session *model.Session,
) (*model.AutoPruneOption, error) {
	ae := NewAuditEvent("Option.GetAutoPruneOption", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	o.AuditLogAuthorized(ae)
	return o.getAutoPruneOption(ctx)
}

// SetAutoPruneOption persists the full auto-prune option as a single JSON row
// (requires global auth). The caller supplies the complete AutoPruneOption
// value including any per-company entries.
func (o *Option) SetAutoPruneOption(
	ctx context.Context,
	session *model.Session,
	autoPruneOpt *model.AutoPruneOption,
) error {
	ae := NewAuditEvent("Option.SetAutoPruneOption", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := o.upsertAutoPruneOption(ctx, autoPruneOpt); err != nil {
		return err
	}
	o.AuditLogAuthorized(ae)
	return nil
}

// GetCompanyAutoPruneOption returns whether the given company has opted in to auto-pruning
// by reading the single shared auto-prune option row (requires global auth).
func (o *Option) GetCompanyAutoPruneOption(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (bool, error) {
	ae := NewAuditEvent("Option.GetCompanyAutoPruneOption", session)
	ae.Details["companyID"] = companyID.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return false, errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return false, errs.ErrAuthorizationFailed
	}
	opt, err := o.getAutoPruneOption(ctx)
	if err != nil {
		return false, err
	}
	o.AuditLogAuthorized(ae)
	return opt.IsCompanyEnabled(companyID), nil
}

// SetCompanyAutoPruneOption updates the per-company enabled flag within the
// single shared auto-prune option row (requires global auth).
func (o *Option) SetCompanyAutoPruneOption(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	enabled bool,
) error {
	ae := NewAuditEvent("Option.SetCompanyAutoPruneOption", session)
	ae.Details["companyID"] = companyID.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// read-modify-write the single row
	opt, err := o.getAutoPruneOption(ctx)
	if err != nil {
		return err
	}
	opt.SetCompanyEnabled(companyID, enabled)
	if err := o.upsertAutoPruneOption(ctx, opt); err != nil {
		return err
	}
	o.AuditLogAuthorized(ae)
	return nil
}

// GetAutoPruneOptionInternal returns the full auto-prune option without any
// authorization check. intended for internal/task-runner use only.
func (o *Option) GetAutoPruneOptionInternal(ctx context.Context) (*model.AutoPruneOption, error) {
	return o.getAutoPruneOption(ctx)
}

// getAutoPruneOption reads the single auto-prune option row, returning the
// default (all-disabled) value when the row does not exist yet.
func (o *Option) getAutoPruneOption(ctx context.Context) (*model.AutoPruneOption, error) {
	raw, err := o.OptionRepository.GetByKey(ctx, data.OptionKeyAutoPruneOrphanedRecipients)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.NewAutoPruneOptionDefault(), nil
		}
		o.Logger.Errorw("failed to get auto-prune option", "error", err)
		return nil, errs.Wrap(err)
	}
	return model.NewAutoPruneOptionFromJSON([]byte(raw.Value.String()))
}

// upsertAutoPruneOption inserts or updates the single auto-prune option row.
func (o *Option) upsertAutoPruneOption(ctx context.Context, autoPruneOpt *model.AutoPruneOption) error {
	opt, err := autoPruneOpt.ToOption()
	if err != nil {
		return errs.Wrap(err)
	}
	_, getErr := o.OptionRepository.GetByKey(ctx, data.OptionKeyAutoPruneOrphanedRecipients)
	if getErr != nil {
		if !errors.Is(getErr, gorm.ErrRecordNotFound) {
			o.Logger.Errorw("failed to check auto-prune option existence", "error", getErr)
			return errs.Wrap(getErr)
		}
		// row does not exist yet — insert
		if _, insertErr := o.OptionRepository.Insert(ctx, opt); insertErr != nil {
			o.Logger.Errorw("failed to insert auto-prune option", "error", insertErr)
			return errs.Wrap(insertErr)
		}
		return nil
	}
	// row exists — update
	if updateErr := o.OptionRepository.UpdateByKey(ctx, opt); updateErr != nil {
		o.Logger.Errorw("failed to update auto-prune option", "error", updateErr)
		return errs.Wrap(updateErr)
	}
	return nil
}

// GetObfuscationTemplate gets the obfuscation template from options or returns default
func (o *Option) GetObfuscationTemplate(ctx context.Context) (string, error) {
	opt, err := o.OptionRepository.GetByKey(ctx, data.OptionKeyObfuscationTemplate)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// return default template if not found
			return data.OptionValueObfuscationTemplateDefault, nil
		}
		o.Logger.Errorw("failed to get obfuscation template option", "error", err)
		return "", errs.Wrap(err)
	}
	template := opt.Value.String()
	if template == "" {
		// return default if empty
		return data.OptionValueObfuscationTemplateDefault, nil
	}
	return template, nil
}

// GetScimDomainInternal returns the configured global SCIM domain without any
// authorization check. intended for the phishing-server host gate. returns an
// empty string when SCIM serving is not configured.
func (o *Option) GetScimDomainInternal(ctx context.Context) (string, error) {
	opt, err := o.OptionRepository.GetByKey(ctx, data.OptionKeyScimDomain)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		o.Logger.Errorw("failed to get scim domain option", "error", err)
		return "", errs.Wrap(err)
	}
	return opt.Value.String(), nil
}

// GetScimDomain returns the configured global SCIM domain (requires global auth).
func (o *Option) GetScimDomain(
	ctx context.Context,
	session *model.Session,
) (string, error) {
	ae := NewAuditEvent("Option.GetScimDomain", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	return o.GetScimDomainInternal(ctx)
}

// SetScimDomain persists the global SCIM domain (requires global auth). An empty
// string disables SCIM serving. A non-empty value must be an existing global
// domain (one not tied to a company).
func (o *Option) SetScimDomain(
	ctx context.Context,
	session *model.Session,
	domain string,
) error {
	ae := NewAuditEvent("Option.SetScimDomain", session)
	ae.Details["domain"] = domain
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		o.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		o.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// a non-empty value must resolve to an existing global domain
	if domain != "" {
		nameVO, err := vo.NewString255(domain)
		if err != nil {
			return errs.NewValidationError(err)
		}
		d, err := o.DomainRepository.GetByName(ctx, nameVO, &repository.DomainOption{})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.NewValidationError(errors.Errorf("domain %q does not exist", domain))
			}
			o.Logger.Errorw("failed to look up scim domain", "error", err)
			return errs.Wrap(err)
		}
		// only global domains (not tied to a company) may be used for SCIM
		if d.CompanyID.IsSpecified() && !d.CompanyID.IsNull() {
			return errs.NewValidationError(errors.Errorf("domain %q is not a global domain", domain))
		}
		// AiTM proxy domains route traffic to a target site and must never serve
		// the SCIM provisioning API
		if d.ProxyID.IsSpecified() && !d.ProxyID.IsNull() {
			return errs.NewValidationError(errors.Errorf("domain %q is a proxy domain and cannot be used for SCIM", domain))
		}
	}
	if err := o.setScimDomainValue(ctx, domain); err != nil {
		return err
	}
	o.AuditLogAuthorized(ae)
	return nil
}

// setScimDomainValue inserts or updates the single SCIM domain option row.
func (o *Option) setScimDomainValue(ctx context.Context, domain string) error {
	valueVO, err := vo.NewOptionalString1MB(domain)
	if err != nil {
		return errs.NewValidationError(err)
	}
	opt := &model.Option{
		Key:   *vo.NewString127Must(data.OptionKeyScimDomain),
		Value: *valueVO,
	}
	_, getErr := o.OptionRepository.GetByKey(ctx, data.OptionKeyScimDomain)
	if getErr != nil {
		if !errors.Is(getErr, gorm.ErrRecordNotFound) {
			o.Logger.Errorw("failed to check scim domain option existence", "error", getErr)
			return errs.Wrap(getErr)
		}
		if _, insertErr := o.OptionRepository.Insert(ctx, opt); insertErr != nil {
			o.Logger.Errorw("failed to insert scim domain option", "error", insertErr)
			return errs.Wrap(insertErr)
		}
		return nil
	}
	if updateErr := o.OptionRepository.UpdateByKey(ctx, opt); updateErr != nil {
		o.Logger.Errorw("failed to update scim domain option", "error", updateErr)
		return errs.Wrap(updateErr)
	}
	return nil
}
