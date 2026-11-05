package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/wneessen/go-mail"
	"gorm.io/gorm"
)

// reportedEventFields are the request specific fields of a reported event
type reportedEventFields struct {
	IP        *vo.OptionalString64
	UserAgent *vo.OptionalString255
	Data      *vo.OptionalString1MB
	Metadata  *vo.OptionalString1MB
	// At is the time of the report. Nil means now.
	At *time.Time
}

// sentMessageID returns the Message-ID a message was sent with, read back from
// the message so a custom SMTP header that replaced it is taken into account
func sentMessageID(m *mail.Msg, headers []*model.SMTPHeader) string {
	// a custom header spelled differently, such as Message-Id, is sent as a
	// second Message-ID. Which one the receiving side keeps is unknown, so none
	// is stored and matching uses the links.
	for _, h := range headers {
		if h == nil {
			continue
		}
		key, err := h.Key.Get()
		if err != nil {
			continue
		}
		if k := key.String(); strings.EqualFold(k, string(mail.HeaderMessageID)) && k != string(mail.HeaderMessageID) {
			return ""
		}
	}
	values := m.GetGenHeader(mail.HeaderMessageID)
	if len(values) != 1 {
		return ""
	}
	return normalizeMessageID(values[0])
}

// storeMessageID saves the Message-ID of the first email sent to a campaign
// recipient so a reporting inbox can match the reported email later. A failure only costs that match, so
// it is logged and sending goes on.
func (c *Campaign) storeMessageID(ctx context.Context, campaignRecipientID *uuid.UUID, messageID string) {
	if messageID == "" {
		return
	}
	if err := c.CampaignRecipientRepository.SetMessageIDByID(ctx, campaignRecipientID, messageID); err != nil {
		c.Logger.Warnw("failed to store message id for campaign recipient",
			"campaignRecipientID", campaignRecipientID.String(),
			"error", err,
		)
	}
}

// reportableCampaign returns the campaign of a campaign recipient when a report
// may be recorded for it at the time at. ok is false for an anonymized
// recipient, a SCIM disabled recipient and a campaign closed before at.
func (c *Campaign) reportableCampaign(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	at time.Time,
) (*model.Campaign, bool, error) {
	crID := campaignRecipient.ID.MustGet()
	// an anonymized campaign recipient has no recipient id, which happens for a
	// closed anonymous campaign whose link was severed. a token from a delivered
	// email can arrive long after close, so return quietly.
	if campaignRecipient.RecipientID.IsNull() {
		c.Logger.Debugw("skipping report event: recipient is anonymized", "campaignRecipientID", crID.String())
		return nil, false, nil
	}
	recipientID := campaignRecipient.RecipientID.MustGet()
	campaignID := campaignRecipient.CampaignID.MustGet()

	// do not record events for SCIM disabled (deprovisioned) recipients
	if recipient, rErr := c.RecipientRepository.GetByID(ctx, &recipientID, &repository.RecipientOption{}); rErr == nil && recipient.ScimSoftDeletedAt != nil {
		c.Logger.Debugw("skipping report event: recipient is scim-disabled", "campaignRecipientID", crID.String())
		return nil, false, nil
	}
	campaign, err := c.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return nil, false, errs.Wrap(err)
	}
	// do not record events for closed campaigns
	if closedAt, cErr := campaign.ClosedAt.Get(); cErr == nil && closedAt.Before(at) {
		c.Logger.Debugw("skipping report event: campaign is closed", "campaignID", campaignID.String())
		return nil, false, nil
	}
	return campaign, true, nil
}

// recordReported saves a reported event for a campaign recipient, updates the
// most notable event and runs webhooks. A recipient that already reported is not
// recorded again, so a button or mailbox that reports more than once does not
// spam the timeline. recorded is false for such a duplicate.
func (c *Campaign) recordReported(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	campaign *model.Campaign,
	fields reportedEventFields,
) (bool, error) {
	recipientID := campaignRecipient.RecipientID.MustGet()
	campaignID := campaignRecipient.CampaignID.MustGet()
	reportedEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]

	// an anonymous campaign's events carry the pseudonym rather than a recipient
	// id, so dedup on that instead
	var alreadyReported bool
	var err error
	if aid, aErr := campaignRecipient.AnonymizedID.Get(); aErr == nil {
		alreadyReported, err = c.CampaignRepository.HasEventByAnonymizedID(ctx, &campaignID, &aid, reportedEventID)
	} else {
		alreadyReported, err = c.CampaignRepository.HasEvent(ctx, &campaignID, &recipientID, reportedEventID)
	}
	if err != nil {
		c.Logger.Errorw("failed to check existing reported event", "error", err)
		return false, errs.Wrap(err)
	}
	if alreadyReported {
		c.Logger.Debugw("recipient already reported", "campaignRecipientID", campaignRecipient.ID.MustGet().String())
		return false, nil
	}

	newEventID := uuid.New()
	campaignEvent := &model.CampaignEvent{
		ID:          &newEventID,
		CampaignID:  &campaignID,
		RecipientID: &recipientID,
		IP:          fields.IP,
		UserAgent:   fields.UserAgent,
		EventID:     reportedEventID,
		Data:        fields.Data,
		Metadata:    fields.Metadata,
	}
	anonymizeEventForRecipient(campaignAnonymous(campaign), campaignRecipient, campaignEvent)
	if fields.At != nil {
		err = c.saveReportedEvent(campaignEvent, *fields.At)
	} else {
		err = c.CampaignRepository.SaveEvent(ctx, campaignEvent)
	}
	if err != nil {
		c.Logger.Errorw("failed to save reported event", "error", err)
		return false, errs.Wrap(err)
	}
	if err := c.SetNotableCampaignRecipientEvent(
		ctx,
		campaignRecipient,
		data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
	); err != nil {
		// logging was done in the previous call
		return true, errs.Wrap(err)
	}
	if err := c.HandleWebhooks(
		ctx,
		&campaignID,
		&recipientID,
		data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
		nil,
	); err != nil {
		return true, errs.Wrap(err)
	}
	return true, nil
}

// InboxReportResult is the outcome of recording a report found in a report inbox
type InboxReportResult int

const (
	// InboxReportOutOfScope means the recipient belongs to a campaign outside the
	// inbox scope, so nothing was recorded
	InboxReportOutOfScope InboxReportResult = iota
	// InboxReportSkipped means the recipient is in scope but no event was recorded,
	// because it already reported, the campaign is closed or it is anonymized
	InboxReportSkipped
	// InboxReportRecorded means a reported event was saved
	InboxReportRecorded
)

// RecordInboxReport records a report that was found in a report inbox.
//
// scopeCompanyID is the company of the inbox, nil for the global inbox. The
// campaign must belong to exactly that scope: a company inbox only covers its own
// campaigns and the global inbox only covers global campaigns. Anything else is
// out of scope and ignored, so one company's mailbox can never mark recipients of
// another company.
//
// reportedBy is the address that sent the report, empty when unknown. It is only
// compared with the recipient's own address and the yes or no answer is stored,
// never the address. Nothing is stored about it for anonymous campaigns.
func (c *Campaign) RecordInboxReport(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	scopeCompanyID *uuid.UUID,
	reportedAt time.Time,
	reportedBy string,
	mailbox string,
) (InboxReportResult, error) {
	campaignID, err := campaignRecipient.CampaignID.Get()
	if err != nil {
		return InboxReportOutOfScope, nil
	}
	// the scope check loads the campaign on its own, so it runs before any
	// other lookup and does not depend on the campaign being active
	scoped, err := c.CampaignRepository.GetByID(ctx, &campaignID, &repository.CampaignOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// the campaign was deleted, there is nothing to record
		return InboxReportOutOfScope, nil
	}
	if err != nil {
		return InboxReportOutOfScope, errs.Wrap(err)
	}
	if !sameCompanyScope(companyIDPtr(scoped.CompanyID), scopeCompanyID) {
		return InboxReportOutOfScope, nil
	}

	// a closed campaign takes no more reports, also not one that was made just
	// before the close and read after it, so its saved statistics stay right
	campaign, ok, err := c.reportableCampaign(ctx, campaignRecipient, time.Now())
	if err != nil {
		return InboxReportSkipped, err
	}
	if !ok {
		return InboxReportSkipped, nil
	}

	recipientEmail := ""
	recipientID := campaignRecipient.RecipientID.MustGet()
	recipient, rErr := c.RecipientRepository.GetByID(ctx, &recipientID, &repository.RecipientOption{})
	if errors.Is(rErr, gorm.ErrRecordNotFound) {
		return InboxReportSkipped, nil
	}
	if rErr != nil {
		// without the address the mailbox check below can not run, retry later
		return InboxReportSkipped, errs.Wrap(rErr)
	}
	if email, eErr := recipient.Email.Get(); eErr == nil {
		recipientEmail = email.String()
	}
	// the reporting mailbox can be a recipient itself, for example through an all
	// staff group. A simulation email delivered to it is not a report.
	if recipientEmail != "" && equalFoldEmail(recipientEmail, mailbox) {
		return InboxReportSkipped, nil
	}

	eventData := map[string]any{"source": "reporting inbox"}
	if !campaignAnonymous(campaign) && reportedBy != "" && recipientEmail != "" {
		eventData["reportedByRecipient"] = equalFoldEmail(recipientEmail, reportedBy)
	}
	raw, err := json.Marshal(eventData)
	if err != nil {
		return InboxReportSkipped, errs.Wrap(err)
	}
	at := reportedAt.UTC()
	// once the event is saved the notable update and webhooks must run too, also
	// when the poll is stopped meanwhile, as the report is not read again
	// shorter than the time shutdown waits for polls, so a report is finished
	// before the database closes
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	recorded, err := c.recordReported(rctx, campaignRecipient, campaign, reportedEventFields{
		IP:        vo.NewEmptyOptionalString64(),
		UserAgent: vo.NewEmptyOptionalString255(),
		Data:      vo.NewOptionalString1MBMust(string(raw)),
		Metadata:  vo.NewEmptyOptionalString1MB(),
		At:        &at,
	})
	if recorded {
		// the event is saved, a failing notable update or webhook must not make
		// the inbox read the same report again
		if err != nil {
			c.Logger.Warnw("reported event saved but a follow up step failed", "error", err)
		}
		return InboxReportRecorded, nil
	}
	if err != nil {
		return InboxReportSkipped, err
	}
	return InboxReportSkipped, nil
}

// equalFoldEmail compares two addresses ignoring case and surrounding space
func equalFoldEmail(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// sameCompanyScope is true when both are global or both are the same company
func sameCompanyScope(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
