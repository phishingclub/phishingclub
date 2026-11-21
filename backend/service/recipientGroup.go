package service

import (
	"context"
	"strings"
	"time"

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
	ProgramRepository           *repository.Program
	DB                          *gorm.DB
	// deletes finishes the deletes of the groups marked as being deleted
	deletes deletePass
	// CompanyRepository refuses new data in a company that is being deleted
	CompanyRepository *repository.Company
}

// errGroupDeleting refuses a change to a group that is being deleted
var errGroupDeleting = errors.New("the group is being deleted")

// refuseDeleting refuses a change to a group that is being deleted
func refuseDeleting(group *model.RecipientGroup) error {
	if group.DeletingAt != nil {
		return validate.WrapErrorWithField(errGroupDeleting, "group")
	}
	return nil
}

// Create inserts a new recipient group
func (r *RecipientGroup) Create(
	ctx context.Context,
	session *model.Session,
	group *model.RecipientGroup,
) (_ *uuid.UUID, err error) {
	ae := NewAuditEvent("RecipientGroup.Create", session)
	defer r.auditDone(ae, &err)
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
	// if dynamic, validate that filter fields are present, allowed, and non-empty
	isDynamic := group.IsDynamic.IsSpecified() && !group.IsDynamic.IsNull() && group.IsDynamic.MustGet()
	if isDynamic {
		if err := group.ValidateDynamic(); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := group.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	unlockCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
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
) (_ *RecipientImportResult, err error) {
	ae := NewAuditEvent("RecipientGroup.Import", session)
	defer r.auditDone(ae, &err)
	ae.Details["recipientGroupId"] = recipientGroupID.String()
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return nil, err
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// the read lock of the group keeps a delete mark from passing between
	// the check that the group is not being deleted and the membership
	// writes, see deleteMarkLock
	defer groupDeleteLock.read(recipientGroupID)()
	if len(recipients) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("no recipients"), "add recipients")
	}
	// check that the recipient group exists
	group, err := r.RecipientGroupRepository.GetByID(
		ctx,
		recipientGroupID,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		r.Logger.Debugw("failed to import recipients - failed to get recipient group", "error", err)
		return nil, err
	}
	if err := refuseDeleting(group); err != nil {
		return nil, err
	}
	// the company of the group may not be being deleted either
	unlockGroupCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, optionalID(group.CompanyID))
	if err != nil {
		return nil, err
	}
	defer unlockGroupCompany()
	unlockCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, companyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
	// dynamic groups do not support explicit membership via import
	if group.IsDynamic.IsSpecified() && !group.IsDynamic.IsNull() && group.IsDynamic.MustGet() {
		return nil, validate.WrapErrorWithField(errors.New("cannot import recipients into a dynamic group"), "group")
	}
	// recipients are saved under companyID before they are added to the group,
	// so check the group can hold them first or they are left behind
	if !model.UsableInCompany(companyID, companyIDPtr(group.CompanyID)) {
		return nil, validate.WrapErrorWithField(errors.New("company id does not match group id"), "recipient")
	}
	result, err := r.RecipientService.Import(
		ctx,
		session,
		recipients,
		ignoreOverwriteEmptyFields,
		companyID,
	)
	if err != nil {
		return result, err
	}
	// add recpients to group
	err = r.AddRecipients(
		ctx,
		session,
		recipientGroupID,
		result.SuccessIDs,
	)
	if err != nil {
		r.Logger.Debugw("failed to import recipients - failed to add recipients to group",
			"error", err,
		)
		return result, err
	}
	r.AuditLogAuthorized(ae)

	return result, nil
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
	r.applyDeleteFailure(recipientGroup)
	// no audit on read

	return recipientGroup, nil
}

// applyDeleteFailure fills why the last delete of a group stopped
func (r *RecipientGroup) applyDeleteFailure(g *model.RecipientGroup) {
	if id, err := g.ID.Get(); err == nil {
		g.DeleteFailure = r.deletes.failure(id)
	}
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
	for _, g := range result.Rows {
		r.applyDeleteFailure(g)
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
) (err error) {
	ae := NewAuditEvent("RecipientGroup.UpdateByID", session)
	defer r.auditDone(ae, &err)
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
	if err := refuseDeleting(current); err != nil {
		return err
	}
	// the company of the group may not be being deleted either
	unlockGroupCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, optionalID(current.CompanyID))
	if err != nil {
		return err
	}
	defer unlockGroupCompany()
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
	// disallow toggling isDynamic after creation
	if incoming.IsDynamic.IsSpecified() && !incoming.IsDynamic.IsNull() {
		currentIsDynamic := current.IsDynamic.IsSpecified() && !current.IsDynamic.IsNull() && current.IsDynamic.MustGet()
		if incoming.IsDynamic.MustGet() != currentIsDynamic {
			return validate.WrapErrorWithField(errors.New("cannot change after creation"), "isDynamic")
		}
	}

	isDynamic := current.IsDynamic.IsSpecified() && !current.IsDynamic.IsNull() && current.IsDynamic.MustGet()
	if isDynamic {
		// propagate filter field/value updates; ValidateDynamic carries the allowlist check
		if incoming.FilterField.IsSpecified() && !incoming.FilterField.IsNull() {
			current.FilterField.Set(incoming.FilterField.MustGet())
		}
		if incoming.FilterValue.IsSpecified() && !incoming.FilterValue.IsNull() {
			current.FilterValue.Set(incoming.FilterValue.MustGet())
		}
		// re-validate the merged state so allowlist and length rules are enforced
		if err := current.ValidateDynamic(); err != nil {
			return errs.Wrap(err)
		}
	} else {
		// reject filter field/value changes on a static group
		if (incoming.FilterField.IsSpecified() && !incoming.FilterField.IsNull()) ||
			(incoming.FilterValue.IsSpecified() && !incoming.FilterValue.IsNull()) {
			return validate.WrapErrorWithField(errors.New("cannot set filter fields on a static group"), "filterField")
		}
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

// AddRecipients adds recipients to a recipient group.
// returns an error if the group is dynamic, as membership is derived from the filter query.
func (r *RecipientGroup) AddRecipients(
	ctx context.Context,
	session *model.Session,
	groupID *uuid.UUID,
	recipients []*uuid.UUID,
) (err error) {
	ae := NewAuditEvent("RecipientGroup.AddRecipients", session)
	defer r.auditDone(ae, &err)
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
	// the read lock of the group keeps a delete mark from passing between
	// the check that the group is not being deleted and the membership
	// writes, see deleteMarkLock
	defer groupDeleteLock.read(groupID)()
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
	if err := refuseDeleting(group); err != nil {
		return err
	}
	// the company of the group may not be being deleted either
	unlockGroupCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, optionalID(group.CompanyID))
	if err != nil {
		return err
	}
	defer unlockGroupCompany()
	// dynamic groups do not support explicit membership
	if group.IsDynamic.IsSpecified() && !group.IsDynamic.IsNull() && group.IsDynamic.MustGet() {
		return validate.WrapErrorWithField(errors.New("cannot add recipients to a dynamic group"), "group")
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

// RemoveRecipients removes recipients from a recipient group.
// returns an error if the group is dynamic, as membership is derived from the filter query.
func (r *RecipientGroup) RemoveRecipients(
	ctx context.Context,
	session *model.Session,
	groupID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) (err error) {
	ae := NewAuditEvent("RecipientGroup.RemoveRecipients", session)
	defer r.auditDone(ae, &err)
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
	// the read lock of the group keeps a delete mark from passing between
	// the check that the group is not being deleted and the membership
	// writes, see deleteMarkLock
	defer groupDeleteLock.read(groupID)()
	// dynamic groups do not support explicit membership removal
	existingGroup, err := r.RecipientGroupRepository.GetByID(
		ctx,
		groupID,
		&repository.RecipientGroupOption{},
	)
	if err != nil {
		r.Logger.Errorw("failed to remove recipients - failed to get recipient group", "error", err)
		return err
	}
	if err := refuseDeleting(existingGroup); err != nil {
		return err
	}
	// the company of the group may not be being deleted either
	unlockGroupCompany, err := refuseDeletingCompany(ctx, r.CompanyRepository, optionalID(existingGroup.CompanyID))
	if err != nil {
		return err
	}
	defer unlockGroupCompany()
	if existingGroup.IsDynamic.IsSpecified() && !existingGroup.IsDynamic.IsNull() && existingGroup.IsDynamic.MustGet() {
		return validate.WrapErrorWithField(errors.New("cannot remove recipients from a dynamic group"), "group")
	}
	// cancel the unsent sends of these recipients in the open campaigns that
	// use the group, the same rule as a group delete: a person still in
	// another group of the campaign keeps their send. The hidden campaigns of
	// a program are left to the membership pass of the program, which keeps a
	// person who is still in another of its groups. The recipients are kept
	// and their campaign history is left intact.
	campaignGroups, err := r.CampaignRecipientRepository.GetOpenCampaignGroupsOfGroup(ctx, groupID)
	if err != nil {
		r.Logger.Errorw("failed to get the campaigns of the recipient group", "error", err)
		return err
	}
	for campaignID, others := range campaignGroups {
		keep := make([]*gorm.DB, 0, len(others))
		for _, other := range others {
			q, err := r.RecipientGroupRepository.MemberIDsQuery(ctx, other)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				r.Logger.Errorw("failed to get the members of another campaign group", "error", err)
				return err
			}
			keep = append(keep, q)
		}
		if err := r.CampaignRecipientRepository.CancelUnsentOfRecipientsInCampaign(ctx, &campaignID, recipientIDs, keep); err != nil {
			r.Logger.Errorw("failed to cancel campaign recipient", "error", err)
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

// DeleteByID marks a recipient group as being deleted and deletes it in the
// background, see deleteGroupNow. A group used by a program that is not
// closed is refused. A group already marked is tried again at once. While
// marked, the group can not be changed or used by a new or updated campaign
// or program. The background pass audits how the delete ends.
func (r *RecipientGroup) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (err error) {
	ae := NewAuditEvent("RecipientGroup.DeleteByID", session)
	defer r.auditDone(ae, &err)
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
	if _, err := r.RecipientGroupRepository.GetByID(ctx, id, &repository.RecipientGroupOption{}); err != nil {
		return err
	}
	system, err := model.NewSystemSession()
	if err != nil {
		return errs.Wrap(err)
	}
	// what keeps the group from being deleted is refused before the mark, so
	// the background delete can always finish. A save that uses the group
	// holds the read lock from its check to its last write, so the checks and
	// the mark see every such save.
	unlock, err := groupDeleteLock.mark(ctx, *id)
	if err != nil {
		return err
	}
	// deferred, so a panic never leaves the lock held
	defer unlock()
	if err := r.deleteBlocked(ctx, id); err != nil {
		return err
	}
	if err := r.RecipientGroupRepository.MarkDeleting(ctx, id, time.Now()); err != nil {
		return err
	}
	unlock()
	r.deletes.clearFailure(*id)
	r.deletes.request(*id, requesterID(session))
	ae.Details["reason"] = "delete started"
	r.AuditLogAuthorized(ae)
	r.startDeletes(system, id)
	return nil
}

// deleteBlocked refuses the delete of a group a program enrolls from or the
// report delivery sends to
func (r *RecipientGroup) deleteBlocked(ctx context.Context, id *uuid.UUID) error {
	if err := r.refuseProgramUse(ctx, id); err != nil {
		return err
	}
	return r.refuseReportConfigUse(ctx, id)
}

// forgetDelete forgets what the delete pass keeps about a group that is gone
func (r *RecipientGroup) forgetDelete(id uuid.UUID) {
	r.deletes.forget(id)
	r.deletes.clearFailure(id)
}

// refuseProgramUse refuses the delete of a group a program enrolls from
func (r *RecipientGroup) refuseProgramUse(ctx context.Context, id *uuid.UUID) error {
	if r.ProgramRepository == nil {
		return nil
	}
	programNames, err := r.ProgramRepository.GetNamesByRecipientGroupID(ctx, id)
	if err != nil {
		r.Logger.Errorw("failed to get programs using the recipient group", "error", err)
		return errs.Wrap(err)
	}
	if len(programNames) > 0 {
		return errs.NewValidationError(errors.Errorf(
			"group is used by program: %s. Remove it from the program first",
			strings.Join(programNames, ", "),
		))
	}
	return nil
}

// refuseReportConfigUse refuses the delete of a group a report delivery
// setting sends to
func (r *RecipientGroup) refuseReportConfigUse(ctx context.Context, id *uuid.UUID) error {
	n, err := r.RecipientGroupRepository.CountReportConfigUse(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	if n > 0 {
		return errs.NewValidationError(errors.New(
			"group receives the automated reports. Choose another group in the report delivery settings first",
		))
	}
	return nil
}

// FinishDeletesInBackground starts the pass that finishes the deletes of the
// groups marked as being deleted, for example one stopped by a restart. The
// task runner calls it on every tick.
func (r *RecipientGroup) FinishDeletesInBackground(ctx context.Context, session *model.Session) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(NewAuditEvent("RecipientGroup.FinishDeletes", session))
		return errs.ErrAuthorizationFailed
	}
	r.startDeletes(session, nil)
	return nil
}

// startDeletes starts the background delete pass, see deletePass
func (r *RecipientGroup) startDeletes(session *model.Session, first *uuid.UUID) {
	r.deletes.start(r.Logger, first,
		func(ctx context.Context, skip []*uuid.UUID) ([]*uuid.UUID, error) {
			return r.RecipientGroupRepository.GetIDsBeingDeleted(ctx, deletePassScanLimit, skip)
		},
		func(ctx context.Context, id *uuid.UUID) error {
			return r.finishDelete(ctx, session, id)
		},
	)
}

// finishDelete deletes one group marked as being deleted and audits it as
// done by the system for the user who asked. The mark never goes: when a
// program or a report setting uses the group, the reason is kept to show on
// the group and the delete is tried again, as after any other failure. A
// failure is audited at most once per deletePassFailureAuditEvery.
func (r *RecipientGroup) finishDelete(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	ae := NewAuditEvent("RecipientGroup.DeleteByID", session)
	ae.Details["id"] = id.String()
	if by := r.deletes.requester(*id); by != "" {
		ae.Details["requestedBy"] = by
	}
	if _, err := r.RecipientGroupRepository.GetByID(ctx, id, &repository.RecipientGroupOption{}); errors.Is(err, gorm.ErrRecordNotFound) {
		r.deletes.clearFailure(*id)
		return nil
	}
	// the uses are checked again before anything is removed. The mark never
	// goes, the delete waits until they are removed
	if err := r.deleteBlocked(ctx, id); err != nil {
		var v errs.ValidationError
		if errors.As(err, &v) {
			// the mark stays, the reason is shown on the group and the delete
			// goes on once the use is removed
			r.deletes.stopped(*id, v.Err.Error())
			if r.deletes.auditFailure(*id) {
				ae.Details["reason"] = "delete waiting"
				r.AuditLogFailed(ae, err)
			}
		}
		return err
	}
	err := r.deleteGroupNow(ctx, id)
	if err != nil {
		// the reason stays shown until the delete goes on
		r.deletes.stopped(*id, deleteWaitReason(err))
		if r.deletes.auditFailure(*id) {
			ae.Details["reason"] = "delete failed"
			r.AuditLogFailed(ae, err)
		}
		return err
	}
	r.deletes.clearFailure(*id)
	ae.Details["reason"] = "delete finished"
	r.AuditLogAuthorized(ae)
	return nil
}

// deleteGroupNow deletes a recipient group. The pending sends of its members
// are cancelled in the campaigns that use it, the group is unlinked from them
// and deleted. Every step can run again, so a delete that stops half way is
// finished by the next try. A group that is gone counts as deleted.
func (r *RecipientGroup) deleteGroupNow(ctx context.Context, id *uuid.UUID) error {
	if _, err := r.RecipientGroupRepository.GetByID(ctx, id, &repository.RecipientGroupOption{}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	// a program enrolls from its groups, so the group must be removed from it first
	if err := r.refuseProgramUse(ctx, id); err != nil {
		return err
	}
	if err := r.refuseReportConfigUse(ctx, id); err != nil {
		return err
	}
	if r.ProgramRepository != nil {
		// a closed program keeps its results but no longer holds the group
		if err := r.ProgramRepository.RemoveRecipientGroupFromClosedPrograms(ctx, id); err != nil {
			r.Logger.Errorw("failed to remove the group from closed programs", "error", err)
			return errs.Wrap(err)
		}
	}
	// cancel the pending sends of the members in the campaigns that use the
	// group and are not closed, scheduled ones included. A member still in
	// another group of the campaign keeps the send, and campaigns that do not
	// use the group, hidden program campaigns included, are left as they are.
	// The recipients are kept and their history is left intact. The members
	// are selected by a subquery, the junction rows of a static group or the
	// filter of a dynamic one, so a group of any size is cancelled in paced
	// chunks without reading its members.
	members, err := r.RecipientGroupRepository.MemberIDsQuery(ctx, id)
	if err != nil {
		r.Logger.Errorw("failed to delete recipient group - failed to get recipient group", "error", err)
		return err
	}
	campaignGroups, err := r.CampaignRecipientRepository.GetOpenCampaignGroupsOfGroup(ctx, id)
	if err != nil {
		r.Logger.Errorw("failed to get the campaigns of the recipient group", "error", err)
		return err
	}
	for campaignID, others := range campaignGroups {
		keep := make([]*gorm.DB, 0, len(others))
		for _, other := range others {
			q, err := r.RecipientGroupRepository.MemberIDsQuery(ctx, other)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				r.Logger.Errorw("failed to get the members of another campaign group", "error", err)
				return err
			}
			keep = append(keep, q)
		}
		if err := r.CampaignRecipientRepository.CancelUnsentOfMembersInCampaign(ctx, &campaignID, members, keep); err != nil {
			r.Logger.Errorw("failed to cancel campaign recipients of group members", "error", err)
			return err
		}
	}
	// a program may have started to use the group while the sends were
	// cancelled, so the check is made again right before the group is
	// unlinked and deleted
	if err := r.refuseProgramUse(ctx, id); err != nil {
		return err
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
	return nil
}
