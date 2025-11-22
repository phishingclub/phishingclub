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
	"gorm.io/gorm"
)

// RecipientGroup is a recipient group service
type RecipientGroup struct {
	Common
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
	RecipientGroupRepository    *repository.RecipientGroup
	RecipientRepository         *repository.Recipient
	RecipientService            *Recipient
	DB                          *gorm.DB
}

// Create inserts a new recipient group
func (r *RecipientGroup) Create(
	ctx context.Context,
	session *model.Session,
	group *model.RecipientGroup,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("RecipientGroup.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := group.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := group.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		r.RecipientRepository.DB,
		"recipient_groups",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		r.Logger.Errorw("failed to check recipient group uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		r.Logger.Debugw("recipient group is already taken", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// insert recipient group
	recipientGroupID, err := r.RecipientGroupRepository.Insert(
		ctx,
		group,
	)
	if err != nil {
		r.Logger.Debugw("failed to create recipient group - failed to insert recipient group",
			"error", err,
		)

		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = recipientGroupID.String()
	r.AuditLogAuthorized(ae)

	return recipientGroupID, nil
}

// Import imports recipients into a recipient group
// if the recipient does not exists, it will be created and added to the group
// if the recipient exits, it will be updated and added to the group
func (r *RecipientGroup) Import(
	ctx context.Context,
	session *model.Session,
	recipients []*model.Recipient,
	ignoreOverwriteEmptyFields bool,
	recipientGroupID *uuid.UUID,
	companyID *uuid.UUID,
) error {
	ae := NewAuditEvent("RecipientGroup.Import", session)
	ae.Details["recipientGroupId"] = recipientGroupID.String()
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if len(recipients) == 0 {
		return validate.WrapErrorWithField(errors.New("no recipients"), "add recipients")
	}
	// check that the recipient group exists
	_, err = r.RecipientGroupRepository.GetByID(
		ctx,
		recipientGroupID,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		r.Logger.Debugw("failed to import recipients - failed to get recipient group", "error", err)
		return err
	}
	recipientIDs, err := r.RecipientService.Import(
		ctx,
		session,
		recipients,
		ignoreOverwriteEmptyFields,
		companyID,
	)
	if err != nil {
		return err
	}
	// add recpients to group
	err = r.AddRecipients(
		ctx,
		session,
		recipientGroupID,
		recipientIDs,
	)
	if err != nil {
		r.Logger.Debugw("failed to import recipients - failed to add recipients to group",
			"error", err,
		)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}

// GetByID returns a recipient group by ID
func (r *RecipientGroup) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RecipientGroupOption,
) (*model.RecipientGroup, error) {
	ae := NewAuditEvent("RecipientGroup.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get recipient group
	recipientGroup, err := r.RecipientGroupRepository.GetByID(
		ctx,
		id,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipient group by id - failed to get recipient group",
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return recipientGroup, nil
}

// GetByCompanyID returns recipient groups by company ID
func (r *RecipientGroup) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RecipientGroupOption,
) ([]*model.RecipientGroup, error) {
	ae := NewAuditEvent("RecipientGroup.GetByCompanyID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get recipient group
	recipientGroups, err := r.RecipientGroupRepository.GetAllByCompanyID(
		ctx,
		id,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipient groups by id - failed to get recipient group",
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return recipientGroups, nil
}

// GetAll returns all recipient groups using pagination
func (r *RecipientGroup) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID, // can be null
	options *repository.RecipientGroupOption,
) (*model.Result[model.RecipientGroup], error) {
	result := model.NewEmptyResult[model.RecipientGroup]()
	ae := NewAuditEvent("RecipientGroup.GetAll", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get recipient groups
	result, err = r.RecipientGroupRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get all recipient groups - failed to get all recipient groups",
			"error", err,
		)
		return result, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetRecipientsByID returns all recipients of a recipient group
func (r *RecipientGroup) GetRecipientsByGroupID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RecipientOption,
) (*model.Result[model.Recipient], error) {
	result := model.NewEmptyResult[model.Recipient]()
	ae := NewAuditEvent("RecipientGroup.GetRecipientsByGroupID", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// get recipients
	result, err = r.RecipientGroupRepository.GetRecipientsByGroupID(
		ctx,
		id,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipients by id - failed to get recipients", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// UpdateByID updates a recipient group by ID
func (r *RecipientGroup) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.RecipientGroup,
) error {
	ae := NewAuditEvent("RecipientGroup.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get current
	current, err := r.RecipientGroupRepository.GetByID(
		ctx,
		id,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipient group", "error", err)
		return err
	}
	if incoming.Name.IsSpecified() && !incoming.Name.IsNull() {
		var companyID *uuid.UUID
		if cid, err := current.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := incoming.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			r.RecipientRepository.DB,
			"recipient_groups",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			r.Logger.Errorw("failed to check recipient group uniqueness", "error", err)
			return err
		}
		if !isOK {
			r.Logger.Debugw("recipient group is already taken", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
		current.Name.Set(name)
	}
	// update recipient group
	err = r.RecipientGroupRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		r.Logger.Errorw("failed to update recipient group by id - failed to update recipient group",
			"error", err,
		)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}

// AddRecipients adds recipients to a recipient group
func (r *RecipientGroup) AddRecipients(
	ctx context.Context,
	session *model.Session,
	groupID *uuid.UUID,
	recipients []*uuid.UUID,
) error {
	ae := NewAuditEvent("RecipientGroup.AddRecipients", session)
	ae.Details["id"] = groupID.String()
	ae.Details["recipientIds"] = repository.UUIDsToStrings(recipients)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// ensure that the recipient group exists
	group, err := r.RecipientGroupRepository.GetByID(
		ctx,
		groupID,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		r.Logger.Errorw("failed to add recipients - failed to get recipient group", "error", err)
		return err
	}
	// check if the recipients can be added to group
	for _, recipientID := range recipients {
		recipient, err := r.RecipientRepository.GetByID(
			ctx,
			recipientID,
			&repository.RecipientOption{},
		)
		if err != nil {
			r.Logger.Errorw("failed to add recipients - failed to get recipient by id",
				"error", err,
			)
			return err
		}
		// if the group has a company ID then the recipients company ID must match
		// unless the recipient has no company id as it is global
		if v, err := group.CompanyID.Get(); err == nil {
			// if the recipient company is set and does not match the groups
			if recipient.CompanyID.IsSpecified() && !recipient.CompanyID.IsNull() && v.String() != recipient.CompanyID.MustGet().String() {

				r.Logger.Errorw("failed to add recipients - recipient company id does not match group id",
					"error", err,
				)
				return validate.WrapErrorWithField(errors.New("company id does not match group id"), "recipient")
			}
		} else {
			// if the group does not have a company ID then the recipient must not have a company ID
			if recipient.CompanyID.IsSpecified() && !recipient.CompanyID.IsNull() {
				r.Logger.Errorw("failed to add recipients - recipient company id is not nil", "error", err)
				return validate.WrapErrorWithField(errors.New("cant add recipient belonging to a company to a global group"), "recipient")
			}
		}
	}
	// add recipients to group
	err = r.RecipientGroupRepository.AddRecipients(
		ctx,
		groupID,
		recipients,
	)
	if err != nil {
		r.Logger.Errorw("failed to add recipients - failed to add recipients to group", "error", err)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}

// RemoveRecipients removes a recipient from a recipient group
func (r *RecipientGroup) RemoveRecipients(
	ctx context.Context,
	session *model.Session,
	groupID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) error {
	ae := NewAuditEvent("RecipientGroup.RemoveRecipients", session)
	ae.Details["groupId"] = groupID.String()
	ae.Details["recipientIds"] = repository.UUIDsToStrings(recipientIDs)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// anonymize recipients in any recipient-campaign data
	for _, recpID := range recipientIDs {
		// if the recipient is in any active campaign, cancel the recipient sending
		err = r.CampaignRecipientRepository.CancelInActiveCampaigns(ctx, recpID)
		if err != nil {
			r.Logger.Errorw("failed to cancel campaign recipient", "error", err)
			return err
		}
		anonymizedID := uuid.New()
		err = r.RecipientService.CampaignRecipientRepository.Anonymize(
			ctx,
			nil, // nil campaignID means anonymize across all campaigns
			recpID,
			&anonymizedID,
		)
		if err != nil {
			r.Logger.Errorw(
				"failed to remove recipient - failed to anonymized recipiet campaign data",
				"error", err,
			)
			return err
		}
		err = r.CampaignRepository.AnonymizeCampaignEventsByRecipientID(
			ctx,
			recpID,
			&anonymizedID,
		)
		if err != nil {
			r.Logger.Errorw("failed to anonymize campaign event", "error", err)
			return err
		}
	}
	// remove recipient from group
	err = r.RecipientGroupRepository.RemoveRecipients(
		ctx,
		groupID,
		recipientIDs,
	)
	if err != nil {
		r.Logger.Errorw("failed to remove recipient - failed to remove recipient from group",
			"error", err,
		)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}

// DeleteByID deletes a recipient group by ID
func (r *RecipientGroup) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("RecipientGroup.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get all recipients in group
	group, err := r.RecipientGroupRepository.GetByID(
		ctx,
		id,
		&repository.RecipientGroupOption{
			WithRecipients: true,
		},
	)
	if len(group.Recipients) > 0 {
		// anonymize recipients in any recipient-campaign data
		for _, recipient := range group.Recipients {
			anonymizedID := uuid.New()
			recpID := recipient.ID.MustGet()

			// if the recipient is in any active campaign, cancel the recipient sending
			err = r.CampaignRecipientRepository.CancelInActiveCampaigns(ctx, &recpID)
			if err != nil {
				r.Logger.Errorw("failed to cancel campaign recipient", "error", err)
				return err
			}
			err = r.RecipientService.CampaignRecipientRepository.Anonymize(
				ctx,
				nil, // nil campaignID means anonymize across all campaigns
				&recpID,
				&anonymizedID,
			)
			if err != nil {
				r.Logger.Errorw(
					"failed to remove recipient - failed to anonymized recipiet campaign data",
					"error", err,
				)
				return err
			}
			err = r.CampaignRepository.AnonymizeCampaignEventsByRecipientID(
				ctx,
				&recpID,
				&anonymizedID,
			)
			if err != nil {
				r.Logger.Errorw("failed to anonymize campaign event", "error", err)
				return err
			}
		}
	}
	// remove group from campaign groups
	err = r.CampaignRepository.RemoveCampaignRecipientGroupByGroupID(
		ctx,
		id,
	)
	if err != nil {
		r.Logger.Errorw(
			"failed to delete group - failed remove group from campaign data",
			"error", err,
		)
		return err
	}
	// remove group and recipients from group
	err = r.RecipientGroupRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		r.Logger.Errorw("failed to delete recipient group by id - failed to delete recipient group",
			"error", err,
		)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}
