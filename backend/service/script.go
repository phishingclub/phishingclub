package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/script"
	"github.com/phishingclub/phishingclub/validate"
)

type Script struct {
	Common
	CampaignRepository *repository.Campaign
	ScriptRepository   *repository.Script
	// TestRunner runs scripts in capture mode for the editor test panel. Nil when
	// the feature is disabled.
	TestRunner *script.Runner
}

// Test runs a script against a simulated event and returns what it did, without
// touching any campaign. Gated on the global admin permission.
func (a *Script) Test(
	ctx context.Context,
	session *model.Session,
	script string,
	event script.EventContext,
) (*script.TestResult, error) {
	ae := NewAuditEvent("Script.Test", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if a.TestRunner == nil {
		return nil, errs.Wrap(errors.New("script is not enabled"))
	}
	// match the saved script cap (vo.String1MB) so a test run cannot submit an
	// unbounded script
	if len(script) > 1_000_000 {
		return nil, validate.WrapErrorWithField(errors.New("script is too large"), "script")
	}
	result := a.TestRunner.RunTest(script, event)
	a.AuditLogAuthorized(ae)
	return result, nil
}

// Create creates a new script
func (a *Script) Create(
	ctx context.Context,
	session *model.Session,
	script *model.Script,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Script.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errors.New("unauthorized")
	}
	// validate data
	if err := script.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := script.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := script.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		a.ScriptRepository.DB,
		"scripts",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		a.Logger.Errorw("failed to check script uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		a.Logger.Debugw("script name is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// insert
	id, err := a.ScriptRepository.Insert(ctx, script)
	if err != nil {
		a.Logger.Errorw("failed to insert script", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	a.AuditLogAuthorized(ae)

	return id, nil
}

// GetAll gets all scripts
func (a *Script) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ScriptOption,
) (*model.Result[model.Script], error) {
	result := model.NewEmptyResult[model.Script]()
	ae := NewAuditEvent("Script.GetAll", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get
	result, err = a.ScriptRepository.GetAll(ctx, companyID, options)
	if err != nil {
		a.Logger.Errorw("failed to get scripts", "error", err)
		return result, errs.Wrap(err)
	}
	a.AuditLogAuthorized(ae)

	return result, nil
}

// GetByID gets a script by id
func (a *Script) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Script, error) {
	ae := NewAuditEvent("Script.GetByID", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get
	out, err := a.ScriptRepository.GetByID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to get script", "error", err)
		return out, errs.Wrap(err)
	}
	// no audit on read

	return out, nil
}

// GetByCompanyID gets scripts by company id
func (a *Script) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) ([]*model.Script, error) {
	ae := NewAuditEvent("Script.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get
	models, err := a.ScriptRepository.GetAllByCompanyID(ctx, companyID, &repository.ScriptOption{})
	if err != nil {
		a.Logger.Errorw("failed to get scripts", "error", err)
		return models, errs.Wrap(err)
	}
	// no audit on read

	return models, nil
}

// Update updates a script
func (a *Script) Update(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	script *model.Script,
) error {
	ae := NewAuditEvent("Script.Update", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}
	// confirm the script exists before updating
	if _, err := a.ScriptRepository.GetByID(ctx, id); err != nil {
		a.Logger.Errorw("failed to get script", "error", err)
		return err
	}
	// the repository update reads the changed fields from the incoming script,
	// so only the name uniqueness needs checking here
	if v, err := script.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := script.CompanyID.Get(); err == nil {
			companyID = &cid
		}

		isOK, err := repository.CheckNameIsUnique(
			ctx,
			a.ScriptRepository.DB,
			"scripts",
			v.String(),
			companyID,
			id,
		)
		if err != nil {
			a.Logger.Errorw("failed to check script uniqueness", "error", err)
			return err
		}
		if !isOK {
			a.Logger.Debugw("script name is already taken", "name", v.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
	}
	// update
	err = a.ScriptRepository.UpdateByID(ctx, id, script)
	if err != nil {
		a.Logger.Errorw("failed to update script", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a script
func (a *Script) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Script.DeleteByID", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}
	// remove junction table rows for this script so no campaign retains
	// a dangling reference
	err = a.CampaignRepository.RemoveScriptFromJunctionByScriptID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to remove script from campaign_scripts junction", "error", err)
		return err
	}
	// delete
	err = a.ScriptRepository.DeleteByID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to delete script", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)

	return nil
}
