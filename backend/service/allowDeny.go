package service

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
)

type AllowDeny struct {
	Common
	AllowDenyRepository *repository.AllowDeny
	CampaignRepository  *repository.Campaign
}

func (s *AllowDeny) Create(
	ctx context.Context,
	session *model.Session,
	allowDeny *model.AllowDeny,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("AllowDeny.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errors.New("unauthorized")
	}
	// validate ata
	if err := allowDeny.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := allowDeny.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := allowDeny.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		s.AllowDenyRepository.DB,
		"allow_denies",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		s.Logger.Errorw("failed to check SMTP uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		s.Logger.Debugw("smtp configuration name is already used", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// insert
	id, err := s.AllowDenyRepository.Insert(ctx, allowDeny)
	if err != nil {
		s.Logger.Errorw("failed to insert allow deny", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)

	return id, nil
}

func (s *AllowDeny) Update(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.AllowDeny,
) error {
	ae := NewAuditEvent("AllowDeny.Update", session)
	ae.Details["updateID"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}
	// get current
	current, err := s.AllowDenyRepository.GetByID(ctx, id, &repository.AllowDenyOption{})
	if err != nil {
		s.Logger.Errorw("failed to get allow deny", "error", err)
	}
	// update config - if a field is not set, it is not updated
	if v, err := incoming.Name.Get(); err == nil {
		// check uniquness
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}

		isOK, err := repository.CheckNameIsUnique(
			ctx,
			s.AllowDenyRepository.DB,
			"allow_denies",
			v.String(),
			companyID,
			id,
		)
		if err != nil {
			s.Logger.Errorw("failed to check allow deny name uniqueness",
				"error", err,
			)
			return err
		}
		if !isOK {
			s.Logger.Debugw("allow deny is name is not unique", "name", v.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(v)
	}
	if v, err := incoming.Cidrs.Get(); err == nil {
		current.Cidrs.Set(v)
	}
	if v, err := incoming.JA4Fingerprints.Get(); err == nil {
		current.JA4Fingerprints.Set(v)
	}
	if v, err := incoming.CountryCodes.Get(); err == nil {
		current.CountryCodes.Set(v)
	}
	// allow can not be changed as it could mess up a campaign that
	// uses multiple entries as all entries must be allow or deny.

	// validate data
	if err := current.Validate(); err != nil {
		s.Logger.Errorw("failed to validate allow deny", "error", err)
		return err
	}
	// update
	err = s.AllowDenyRepository.Update(ctx, *id, current)
	if err != nil {
		s.Logger.Errorw("failed to update allow deny",
			"id", id.String(),
			"error", err,
		)
		return err
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// GetAll gets all allow deny lists
func (s *AllowDeny) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.AllowDenyOption,
) (*model.Result[model.AllowDeny], error) {
	ae := NewAuditEvent("AllowDeny.GetAll", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return &model.Result[model.AllowDeny]{}, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return &model.Result[model.AllowDeny]{}, errors.New("unauthorized")
	}
	// get
	allowDenies, err := s.AllowDenyRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		s.Logger.Errorw("failed to get allow deny", "error", err)
		return &model.Result[model.AllowDeny]{}, errs.Wrap(err)
	}

	// no audit logs for read
	return allowDenies, nil
}

// GetByID gets an allow deny list by ID
func (s *AllowDeny) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.AllowDeny, error) {
	ae := NewAuditEvent("AllowDeny.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errors.New("unauthorized")
	}
	// get
	allowDeny, err := s.AllowDenyRepository.GetByID(ctx, id, &repository.AllowDenyOption{})
	if err != nil {
		s.Logger.Errorw("failed to get allow deny", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log for read
	return allowDeny, nil
}

// GetByCompanyID gets an allow denies by ID
func (s *AllowDeny) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Result[model.AllowDeny], error) {
	ae := NewAuditEvent("AllowDeny.GetByCompanyID", session)
	ae.Details["id"] = id.String()
	results := model.NewEmptyResult[model.AllowDeny]()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return results, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return results, errors.New("unauthorized")
	}
	// get
	allowDenies, err := s.AllowDenyRepository.GetAllByCompanyID(
		ctx,
		id,
		&repository.AllowDenyOption{},
	)
	if err != nil {
		s.Logger.Errorw("failed to get allow denies", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log of read
	return allowDenies, nil
}

// DeleteByID deletes an allow deny list by ID
func (s *AllowDeny) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("AllowDeny.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get campaigns afffected so we can remove any deny_page_id
	affectedCampaigns, err := s.CampaignRepository.GetByAllowDenyID(
		ctx,
		id,
	)
	if err != nil {
		s.Logger.Errorw("failed to get campaigns afffected by removing allow deny list",
			"allowDenyID", id.String(),
			"error", err,
		)
		return err
	}
	cids := []*uuid.UUID{}
	for _, campaign := range affectedCampaigns {
		cid := campaign.ID.MustGet()
		cids = append(cids, &cid)
	}
	err = s.CampaignRepository.RemoveDenyPageByCampaignIDs(
		ctx,
		cids,
	)
	if err != nil {
		s.Logger.Errorw("failed to remove deny page from campaigns", "error", err)
		return err
	}
	// remove allow deny list from campaigns using it
	err = s.CampaignRepository.RemoveAllowDenyListsByID(
		ctx,
		id,
	)
	if err != nil {
		s.Logger.Errorw("failed to remove allow / deny lists from campaigns", "error", err)
		return err
	}
	// delete
	err = s.AllowDenyRepository.Delete(ctx, *id)
	if err != nil {
		s.Logger.Errorw("failed to delete allow deny", "error", err)
		return err
	}
	s.AuditLogAuthorized(ae)
	return nil
}
