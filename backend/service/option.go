package service

import (
	"context"
	"strconv"

	"github.com/go-errors/errors"

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
		// is allow listed
		fallthrough
	case data.OptionKeyDBLogLevel:
		// is allow listed
		fallthrough
	case data.OptionKeyAdminSSOLogin:
		// is allow listed
		fallthrough
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
