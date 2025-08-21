package service

import (
	"context"
	"fmt"

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

// Recipient is the Recipient service
type Recipient struct {
	Common
	RecipientRepository         *repository.Recipient
	RecipientGroupRepository    *repository.RecipientGroup
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
}

// Create creates a new recipient
func (r *Recipient) Create(
	ctx context.Context,
	session *model.Session,
	recipient *model.Recipient,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Recipient.Create", session)
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
	// validate recipient
	if err := recipient.Validate(); err != nil {
		r.Logger.Debugw("failed to create recipient - recipient is invalid", "error", err)
		return nil, errs.Wrap(err)
	}
	email := recipient.Email.MustGet()
	// check if recipient already exists to avoid a unique constraint error
	// and gorm does not return a unique constraint error but a string error depending on DB
	_, err = r.RecipientRepository.GetByEmail(
		ctx,
		&email,
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.Logger.Errorw("failed to create recipient - failed to get recipient by any identifier", "error", err)
		return nil, errs.Wrap(err)
	}
	if err == nil {
		r.Logger.Debugw("email is already taken", "email", email.String())
		return nil, validate.WrapErrorWithField(errors.New("not unique"), "email")
	}
	id, err := r.RecipientRepository.Insert(
		ctx,
		recipient,
	)
	if err != nil {
		r.Logger.Errorw("failed to create recipient - failed to insert recipient", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	r.AuditLogAuthorized(ae)

	return id, nil
}

// UpdateByID updates a recipient by ID
func (r *Recipient) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Recipient,
) error {
	ae := NewAuditEvent("Recipient.UpdateByID", session)
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
	// check if recipient already exists and the user is allowed to update the recipient
	current, err := r.RecipientRepository.GetByID(
		ctx,
		id,
		&repository.RecipientOption{},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.Logger.Errorw("failed to update recipient - failed to get recipient by any identifier", "error", err)
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r.Logger.Debug("failed to update recipient - recipient not found")
		return err
	}
	// update config - if a field is present and not null, update it

	// if the email is changed, check that another recipient is not using this email already
	if v, err := incoming.Email.Get(); err != nil {
		if v.String() != current.Email.MustGet().String() {
			var companyID *uuid.UUID
			if current.CompanyID != nil {
				if cid, err := current.CompanyID.Get(); err != nil {
					companyID = &cid
				}
			}
			_, err := r.RecipientRepository.GetByEmailAndCompanyID(
				ctx,
				&v,
				companyID,
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				r.Logger.Errorw("failed check existing recipient email", "error", err)
				return err
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				r.Logger.Debugw("email is already taken", "email", v.String())
				s := fmt.Sprintf("email '%s' is already used by another recipient", v.String())
				return validate.WrapErrorWithField(errors.New("not unique"), s)
			}
		}
		current.Email.Set(v)
	}
	if v, err := incoming.Phone.Get(); err == nil {
		current.Phone.Set(v)
	}
	if v, err := incoming.ExtraIdentifier.Get(); err == nil {
		current.ExtraIdentifier.Set(v)
	}
	if v, err := incoming.FirstName.Get(); err == nil {
		current.FirstName.Set(v)
	}
	if v, err := incoming.LastName.Get(); err == nil {
		current.LastName.Set(v)
	}
	if v, err := incoming.Position.Get(); err == nil {
		current.Position.Set(v)
	}
	if v, err := incoming.Department.Get(); err == nil {
		current.Department.Set(v)
	}
	if v, err := incoming.City.Get(); err == nil {
		current.City.Set(v)
	}
	if v, err := incoming.Country.Get(); err == nil {
		current.Country.Set(v)
	}
	if v, err := incoming.Misc.Get(); err == nil {
		current.Misc.Set(v)
	}
	// validate
	if err := current.Validate(); err != nil {
		r.Logger.Debugw("failed to update recipient - recipient is invalid", "error", err)
		return err
	}
	// save config
	err = r.RecipientRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		r.Logger.Errorw("failed to update recipient - failed to update recipient", "error", err)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}

// GetByID gets a recipient by ID
func (r *Recipient) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RecipientOption,
) (*model.Recipient, error) {
	ae := NewAuditEvent("Recipient.GetByID", session)
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
	// get recipient
	recipient, err := r.RecipientRepository.GetByID(
		ctx,
		id,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipient by id - failed to get recipient", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return recipient, nil
}

// GetByCompanyID gets a recipients by company ID
func (r *Recipient) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.RecipientOption,
) (*model.Result[model.Recipient], error) {
	result := model.NewEmptyResult[model.Recipient]()
	ae := NewAuditEvent("Recipient.GetByCompanyID", session)
	ae.Details["id"] = id.String()
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
	result, err = r.RecipientRepository.GetAllByCompanyID(
		ctx,
		id,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get recipients by company id - failed to get recipient", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetStatsByID get campaign events stats by recipient ID
func (r *Recipient) GetStatsByID(
	ctx context.Context,
	session *model.Session,
	recipientID *uuid.UUID,
) (*model.RecipientCampaignStatsView, error) {
	ae := NewAuditEvent("Recipient.GetStatsByID", session)
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
	// get stats
	stats, err := r.RecipientRepository.GetStatsByID(
		ctx,
		recipientID,
	)
	if err != nil {
		r.Logger.Errorw("failed to get all recipient events", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return stats, nil
}

// GetAllCampaignEvents get events by recipient ID
// gets all events if campaignID is nil
func (r *Recipient) GetAllCampaignEvents(
	ctx context.Context,
	session *model.Session,
	recipientID *uuid.UUID,
	campaignID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.RecipientCampaignEvent], error) {
	result := model.NewEmptyResult[model.RecipientCampaignEvent]()
	ae := NewAuditEvent("Recipient.GetAllCampaignEvents", session)
	if recipientID != nil {
		ae.Details["recipientId"] = recipientID.String()
	}
	if campaignID != nil {
		ae.Details["campaignId"] = campaignID.String()
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
	// get all events
	result, err = r.RecipientRepository.GetAllCampaignEvents(
		ctx,
		recipientID,
		campaignID,
		queryArgs,
	)
	if err != nil {
		r.Logger.Errorw("failed to get all recipient events", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read
	return result, nil
}

// GetAll gets all recipients
func (r *Recipient) GetAll(
	ctx context.Context,
	companyID *uuid.UUID, // can be null
	session *model.Session,
	options *repository.RecipientOption,
) (*model.Result[model.RecipientView], error) {
	result := model.NewEmptyResult[model.RecipientView]()
	ae := NewAuditEvent("Recipient.GetAll", session)
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
	// get all recipients
	result, err = r.RecipientRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		r.Logger.Errorw("failed to get all recipients - failed to get all recipients", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

func (r *Recipient) GetRepeatOffenderCount(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (int64, error) {
	ae := NewAuditEvent("Recipient.GetRepeatOffenderCount", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}

	count, err := r.RecipientRepository.GetRepeatOffenderCount(ctx, companyID)
	if err != nil {
		r.Logger.Errorw("failed to get repeat offender count", "error", err)
		return 0, errs.Wrap(err)
	}

	return count, nil
}

// GetByEmail gets a recipient by email
func (r *Recipient) GetByEmail(
	ctx context.Context,
	session *model.Session,
	email *vo.Email,
	companyID *uuid.UUID,
) (*model.Recipient, error) {
	ae := NewAuditEvent("Recipient.GetByEmail", session)
	ae.Details["email"] = email.String()
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
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
	// get recipient
	recipient, err := r.RecipientRepository.GetByEmailAndCompanyID(
		ctx,
		email,
		companyID,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		r.Logger.Errorw("failed to get recipient by any identifier - failed to get recipient",
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	// no audit on read

	return recipient, nil
}

// Import imports recipients
// if the recipient does not exists, it will be created and added to the group
// if the recipient exits, it will be updated and added to the group
func (r *Recipient) Import(
	ctx context.Context,
	session *model.Session,
	recipients []*model.Recipient,
	ignoreOverwriteEmptyFields bool,
	companyID *uuid.UUID,
) ([]*uuid.UUID, error) {
	ae := NewAuditEvent("Recipient.Import", session)
	recipientsIDs := []*uuid.UUID{}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return recipientsIDs, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return recipientsIDs, errs.ErrAuthorizationFailed
	}
	if len(recipients) == 0 {
		return recipientsIDs, validate.WrapErrorWithField(errors.New("no recipients"), "add recipients")
	}
	// first validate all the entries
	for _, recipient := range recipients {
		if err := recipient.Validate(); err != nil {
			return recipientsIDs, errs.Wrap(err)
		}
	}
	// if the recipient does not exist, create it
	// if the recipient exists, update it
	for _, incoming := range recipients {
		// check if the recipient exists
		email := incoming.Email.MustGet()
		current, err := r.RecipientRepository.GetByEmail(
			ctx,
			&email,
			"id", "email", "company_id",
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			r.Logger.Debugw("failed to import recipients - failed to get recipient", "error", err)
			return recipientsIDs, errs.Wrap(err)
		}
		if current == nil {
			// create recipient
			if companyID != nil {
				incoming.CompanyID.Set(*companyID)
			}
			recipientID, err := r.Create(
				ctx,
				session,
				incoming,
			)
			if err != nil {
				r.Logger.Debugw("failed to import recipients - failed to create recipient",
					"error", err,
				)
				return recipientsIDs, errs.Wrap(err)
			}
			recipientsIDs = append(recipientsIDs, recipientID)
		} else {
			// set the companyID to NOT SET, so it is not overwritten if supplied
			incoming.CompanyID.SetUnspecified()
			if ignoreOverwriteEmptyFields {
				incoming.NullifyEmptyOptionals()
			} else {
				incoming.EmptyStringNulledOptionals()
			}
			// update recipient
			recipientID := current.ID.MustGet()
			err = r.UpdateByID(
				ctx,
				session,
				&recipientID,
				incoming,
			)
			if err != nil {
				r.Logger.Debugw("failed to import recipients - failed to update recipient",
					"error", err,
				)
				return recipientsIDs, errs.Wrap(err)
			}
			recipientsIDs = append(recipientsIDs, &recipientID)
		}
	}
	r.AuditLogAuthorized(ae)

	return recipientsIDs, nil
}

// Delete deletes a recipient
func (r *Recipient) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Recipient.DeleteByID", session)
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
	// remove recipient from all groups
	err = r.RecipientGroupRepository.RemoveRecipientByIDFromAllGroups(ctx, id)
	if err != nil {
		r.Logger.Errorw("failed to delete recipient - failed to remove recipient from all groups",
			"error", err,
		)
		return err
	}
	// if the recipient is in any active campaign, cancel the recipient sending
	err = r.CampaignRecipientRepository.CancelInActiveCampaigns(ctx, id)
	if err != nil {
		r.Logger.Errorw("failed to cancel campaign recipient in active campaigns", "error", err)
		return err
	}
	// anonymize all recipient data
	anonymizedID := uuid.New()
	err = r.CampaignRecipientRepository.Anonymize(
		ctx,
		id,
		&anonymizedID,
	)
	if err != nil {
		r.Logger.Errorw("failed to add anonymized ID to campaign recipient", "error", err)
		return err
	}
	// anonymize events and assign each anonymized ID so the events can still be tracked
	err = r.CampaignRepository.AnonymizeCampaignEventsByRecipientID(
		ctx,
		id,
		&anonymizedID,
	)
	if err != nil {
		r.Logger.Errorw("failed to anonymize campaign event", "error", err)
		return err
	}
	// remove recipient id from all campaign recipients
	err = r.CampaignRecipientRepository.RemoveRecipientIDByRecipientID(
		ctx,
		id,
	)
	if err != nil {
		r.Logger.Errorw("failed to remove recipient id from campaign recipient", "error", err)
		return err
	}
	// delete recipient
	err = r.RecipientRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		r.Logger.Errorw("failed to delete recipient - failed to delete recipient", "error", err)
		return err
	}
	r.AuditLogAuthorized(ae)

	return nil
}
