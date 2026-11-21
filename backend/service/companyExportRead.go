package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// authorizeExportRead reports whether the session may read the data of an
// export. The caller writes the denied audit event.
func (c *Common) authorizeExportRead(session *model.Session) (bool, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return false, errs.Wrap(err)
	}
	return isAuthorized, nil
}

// GetExportGroupCount returns the most groups any one recipient of exactly
// the company is in, or of the shared recipients when companyID is nil. The
// export has that many group columns.
func (r *Recipient) GetExportGroupCount(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (int, error) {
	ae := NewAuditEvent("Recipient.GetExportGroupCount", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if ok, err := r.authorizeExportRead(session); err != nil {
		return 0, err
	} else if !ok {
		r.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	n, err := r.RecipientRepository.GetMaxGroupCountByCompanyID(ctx, companyID)
	if err != nil {
		r.Logger.Errorw("failed to count recipient groups for export", "error", err)
		return 0, errs.Wrap(err)
	}
	// no audit on read
	return n, nil
}

// EachExportPage calls fn with the recipients of exactly the company, or the
// shared recipients when companyID is nil, one page at a time, each with its
// groups sorted by name. A page is never held after fn returns.
func (r *Recipient) EachExportPage(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	fn func([]*model.Recipient) error,
) error {
	ae := NewAuditEvent("Recipient.EachExportPage", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	if ok, err := r.authorizeExportRead(session); err != nil {
		return err
	} else if !ok {
		r.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// no audit on read
	return r.RecipientRepository.EachExportPageByCompanyID(ctx, companyID, fn)
}

// EachEventForExport calls fn with every event of a campaign with its
// recipient, newest first when desc is set and oldest first otherwise, read
// one row at a time. A campaign a program owns is not found. The row passed
// to fn is reused, fn must copy what it keeps.
func (c *Campaign) EachEventForExport(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	desc bool,
	fn func(*repository.CampaignEventExportRow) error,
) error {
	ae := NewAuditEvent("Campaign.EachEventForExport", session)
	ae.Details["campaignId"] = campaignID.String()
	if ok, err := c.authorizeExportRead(session); err != nil {
		return err
	} else if !ok {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// a program's hidden campaigns are read through the program
	if err := c.rejectProgramCampaign(ctx, campaignID); err != nil {
		return err
	}
	// no audit on read
	return c.CampaignRepository.EachCampaignEventForExport(ctx, campaignID, desc, fn)
}
