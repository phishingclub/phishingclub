package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	campaignID := campaignRecipient.CampaignID.MustGet()
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
	// a campaign recipient without a recipient id is anonymized. a send of an
	// anonymous program that only lost its person link after the identity
	// retention still takes reports on its pseudonym until the program closes.
	// any other anonymized recipient, such as one of a closed anonymous
	// campaign, takes none. a token from a delivered email can arrive long
	// after close, so return quietly.
	if campaignRecipient.RecipientID.IsNull() {
		if c.ProgramService != nil {
			if _, ok := UnlinkedAnonymousProgramSend(ctx, c.ProgramService.ProgramRepository, campaign, campaignRecipient); ok {
				return campaign, true, nil
			}
		}
		c.Logger.Debugw("skipping report event: recipient is anonymized")
		return nil, false, nil
	}
	recipientID := campaignRecipient.RecipientID.MustGet()
	// do not record events for SCIM disabled (deprovisioned) recipients
	if recipient, rErr := c.RecipientRepository.GetByID(ctx, &recipientID, &repository.RecipientOption{}); rErr == nil && recipient.ScimSoftDeletedAt != nil {
		c.Logger.Debugw("skipping report event: recipient is scim-disabled", "campaignRecipientID", crID.String())
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
	campaignID := campaignRecipient.CampaignID.MustGet()
	reportedEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	// a send that lost its person link has no recipient id, only the pseudonym
	var recipientRef *uuid.UUID
	if rid, err := campaignRecipient.RecipientID.Get(); err == nil {
		recipientRef = &rid
	}

	// an anonymous campaign's events carry the pseudonym rather than a recipient
	// id, so dedup on that instead
	var alreadyReported bool
	var err error
	if aid, aErr := campaignRecipient.AnonymizedID.Get(); aErr == nil {
		alreadyReported, err = c.CampaignRepository.HasEventByAnonymizedID(ctx, &campaignID, &aid, reportedEventID)
	} else {
		alreadyReported, err = c.CampaignRepository.HasEvent(ctx, &campaignID, recipientRef, reportedEventID)
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
		RecipientID: recipientRef,
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
	// a send that lost its person link runs no webhooks or scripts, as for
	// every event of an unlinked anonymous program send
	if recipientRef != nil {
		if err := c.HandleWebhooks(
			ctx,
			&campaignID,
			recipientRef,
			data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
			nil,
		); err != nil {
			return true, errs.Wrap(err)
		}
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
	// InboxReportAnonymous means the report was on a send of an anonymous
	// campaign or program and was handled, recorded or not. The inbox does not
	// count it, so its counters never show whether or when a person of an
	// anonymous campaign reported.
	InboxReportAnonymous
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

	// a send of an anonymous program that lost its person link has no recipient
	// id. its email address is gone, so the mailbox match and the reportedBy
	// comparison below are skipped. the report is still recorded on the
	// pseudonym, matched by the Message-ID still kept on the row.
	recipientEmail := ""
	if rid, ridErr := campaignRecipient.RecipientID.Get(); ridErr == nil {
		recipient, rErr := c.RecipientRepository.GetByID(ctx, &rid, &repository.RecipientOption{})
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
	}

	eventData := map[string]any{"source": "reporting inbox"}
	if !campaignAnonymous(campaign) && reportedBy != "" && recipientEmail != "" {
		eventData["reportedByRecipient"] = equalFoldEmail(recipientEmail, reportedBy)
	}
	raw, err := json.Marshal(eventData)
	if err != nil {
		return InboxReportSkipped, errs.Wrap(err)
	}
	at, err := c.inboxReportTime(ctx, campaign, reportedAt, time.Now())
	if err != nil {
		return InboxReportSkipped, err
	}
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
		if campaignAnonymous(campaign) {
			return InboxReportAnonymous, nil
		}
		return InboxReportRecorded, nil
	}
	if err != nil {
		return InboxReportSkipped, err
	}
	if campaignAnonymous(campaign) {
		return InboxReportAnonymous, nil
	}
	return InboxReportSkipped, nil
}

// inboxReportTime returns the time an inbox report is recorded at. A report
// on a send of an anonymous program is recorded no earlier than when the last
// month the program shows settled, see programAnonymousFixedFrom. A report
// read late, such as from a backfill, then never changes a month that was
// already shown, and counts only in months not shown yet.
func (c *Campaign) inboxReportTime(ctx context.Context, campaign *model.Campaign, reportedAt time.Time, now time.Time) (time.Time, error) {
	at := reportedAt.UTC()
	programID, err := campaign.ProgramID.Get()
	if err != nil {
		return at, nil
	}
	programs := &repository.Program{DB: c.CampaignRepository.DB}
	program, err := programs.GetByID(ctx, &programID, &repository.ProgramOption{})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return at, nil
	}
	if err != nil {
		return at, errs.Wrap(err)
	}
	if !program.IsAnonymousOn() {
		return at, nil
	}
	if fixed := programAnonymousFixedFrom(now, program.Location()); at.Before(fixed) {
		return fixed.UTC(), nil
	}
	return at, nil
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

// reportedCSVMaxErrors is how many failed rows an import lists. The rest are
// only counted.
const reportedCSVMaxErrors = 5

// reportedCSVMaxValueRunes caps how much of a cell is quoted back in a reason
const reportedCSVMaxValueRunes = 64

// reportedDateLayouts are the date formats a reported CSV may use, tried in
// order. A value without a time zone is read as UTC.
var reportedDateLayouts = []string{
	// ISO 8601 with Z or an offset such as +02:00, fractional seconds optional
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	// month first, leading zeros optional
	"1/2/2006 15:04:05",
	"1/2/2006 15:04",
	"1/2/2006",
	// day first, leading zeros optional
	"2-1-2006 15:04:05",
	"2-1-2006 15:04",
	"2-1-2006",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

// parseReportedDate reads a report date in one of reportedDateLayouts, a
// value without a time zone is read as UTC
func parseReportedDate(value string) (time.Time, error) {
	t, _, err := parseReportedDateIn(value, time.UTC)
	return t, err
}

// parseReportedDateIn reads a report date in one of reportedDateLayouts. A
// value with a zone keeps it, a value without one is read in loc. hasTime is
// false for a value that is only a date.
func parseReportedDateIn(value string, loc *time.Location) (t time.Time, hasTime bool, err error) {
	for _, layout := range reportedDateLayouts {
		if reportedLayoutHasZone(layout) {
			t, err = time.Parse(layout, value)
		} else {
			t, err = time.ParseInLocation(layout, value, loc)
		}
		if err == nil {
			return t, strings.Contains(layout, "15"), nil
		}
	}
	return time.Time{}, false, errors.New("unsupported date format")
}

// reportedLayoutHasZone reports whether a reported date layout reads a zone
// from the value
func reportedLayoutHasZone(layout string) bool {
	return strings.Contains(layout, "Z07") || strings.Contains(layout, "-07")
}

// ReportedCSVRow is one data row of a reported CSV and the line it starts on
type ReportedCSVRow struct {
	Line   int
	Fields []string
}

// ReportedCSVRowError is why a row of a reported CSV was not imported
type ReportedCSVRowError struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// ReportedCSVResult is the outcome of a reported CSV import
type ReportedCSVResult struct {
	// Processed is the number of rows marked as reported
	Processed int `json:"processed"`
	// Skipped is every row that was not marked, failed or already reported
	Skipped int `json:"skipped"`
	// AlreadyReported is the number of rows whose recipient had already
	// reported. Always 0 for an anonymous campaign, where those rows count as
	// processed so the import does not tell who reported.
	AlreadyReported int `json:"alreadyReported"`
	// Failed is the number of rows that could not be imported
	Failed int `json:"failed"`
	// Errors are the first failed rows, at most reportedCSVMaxErrors
	Errors []ReportedCSVRowError `json:"errors"`
}

// countRecorded counts a matched row. A row whose recipient already reported
// counts as processed on an anonymous campaign, so the import does not tell
// who reported.
func (r *ReportedCSVResult) countRecorded(recorded bool, anonymous bool) {
	if recorded || anonymous {
		r.Processed++
		return
	}
	r.AlreadyReported++
	r.Skipped++
}

func (r *ReportedCSVResult) fail(line int, reason string) {
	r.Failed++
	r.Skipped++
	if len(r.Errors) < reportedCSVMaxErrors {
		r.Errors = append(r.Errors, ReportedCSVRowError{Line: line, Reason: reason})
	}
}

// quoteCSVValue quotes a cell for a reason, cut to a readable length
func quoteCSVValue(value string) string {
	if utf8.RuneCountInString(value) > reportedCSVMaxValueRunes {
		value = string([]rune(value)[:reportedCSVMaxValueRunes]) + "..."
	}
	return strconv.Quote(value)
}

// reportedCSVRecipients finds who an email of a reported CSV belongs to. An
// exact match wins, otherwise a match that ignores case is used when only one
// entry has that email. More than one is ambiguous and never guessed.
type reportedCSVRecipients[T any] struct {
	exact  map[string]T
	folded map[string][]T
}

func newReportedCSVRecipients[T any]() *reportedCSVRecipients[T] {
	return &reportedCSVRecipients[T]{exact: map[string]T{}, folded: map[string][]T{}}
}

// add registers an entry under its email
func (r *reportedCSVRecipients[T]) add(email string, v T) {
	r.exact[email] = v
	folded := strings.ToLower(email)
	r.folded[folded] = append(r.folded[folded], v)
}

// find returns the entry of an email. ambiguous is true when no exact match
// exists and more than one entry has the email in another case.
func (r *reportedCSVRecipients[T]) find(email string) (v T, found bool, ambiguous bool) {
	if v, ok := r.exact[email]; ok {
		return v, true, false
	}
	vs := r.folded[strings.ToLower(email)]
	if len(vs) == 1 {
		return vs[0], true, false
	}
	return v, false, len(vs) > 1
}

// reportedCSVCell is the email and the date of one reported CSV row
type reportedCSVCell struct {
	email string
	date  string
}

// readReportedCSVRow reads and checks the email and date of a row. A
// dateColumn below zero means the file has no date column. When a date column
// is picked, a row without a date in it fails, also where the date is
// optional, so a missing date is never read as no date. When ok is false the
// row is recorded as failed on result.
func readReportedCSVRow(
	result *ReportedCSVResult,
	row ReportedCSVRow,
	emailColumn int,
	dateColumn int,
	dateRequired bool,
) (cell reportedCSVCell, ok bool) {
	datePicked := dateRequired || dateColumn >= 0
	if len(row.Fields) <= emailColumn || (datePicked && len(row.Fields) <= dateColumn) {
		result.fail(row.Line, "Missing email or date column")
		return cell, false
	}
	cell.email = strings.TrimSpace(row.Fields[emailColumn])
	if dateColumn >= 0 && dateColumn < len(row.Fields) {
		cell.date = strings.TrimSpace(row.Fields[dateColumn])
	}
	if cell.email == "" {
		result.fail(row.Line, "Email is empty")
		return cell, false
	}
	if datePicked && cell.date == "" {
		result.fail(row.Line, "Date is empty")
		return cell, false
	}
	return cell, true
}

// checkReportedCSVColumns checks the column indexes of a reported CSV. A
// dateColumn below zero is allowed only when the date is optional.
func checkReportedCSVColumns(header []string, rows []ReportedCSVRow, emailColumn, dateColumn int, dateRequired bool) error {
	if len(rows) == 0 {
		return errs.NewValidationError(errors.New("CSV must have at least a header row and one data row"))
	}
	if len(rows) > ReportedCSVMaxRows {
		return errs.NewValidationError(fmt.Errorf("CSV has more than %d rows", ReportedCSVMaxRows))
	}
	if emailColumn < 0 || emailColumn >= len(header) {
		return errs.NewValidationError(errors.New("invalid email column index"))
	}
	if dateColumn >= len(header) || (dateRequired && dateColumn < 0) {
		return errs.NewValidationError(errors.New("invalid date column index"))
	}
	return nil
}

// ReportedCSVMaxRows bounds how many data rows one reported CSV may hold. The
// upload stops reading once a file has more
const ReportedCSVMaxRows = 100000

// ProcessReportedCSV marks the campaign recipients in a CSV as reported at the
// date in the row. Rows that can not be imported are counted and the first of
// them are returned with the reason, the other rows are still imported.
func (c *Campaign) ProcessReportedCSV(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	header []string,
	rows []ReportedCSVRow,
	emailColumnIndex int,
	dateColumnIndex int,
) (_ *ReportedCSVResult, err error) {
	ae := NewAuditEvent("Campaign.ProcessReportedCSV", session)
	defer c.auditDone(ae, &err)
	ae.Details["campaignId"] = campaignID.String()

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	campaign, err := c.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{})
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// a program owns its hidden campaigns, they are only changed through the program
	if campaign.IsProgramManaged() {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	// anonymizing removes the link from campaign recipients to recipients, so no
	// row could be matched
	if campaign.AnonymizedAt.IsSpecified() && !campaign.AnonymizedAt.IsNull() {
		return nil, errs.NewValidationError(errors.New("the campaign is anonymized, reports can not be imported"))
	}

	if err := checkReportedCSVColumns(header, rows, emailColumnIndex, dateColumnIndex, true); err != nil {
		return nil, err
	}

	campaignRecipients, err := c.CampaignRecipientRepository.GetByCampaignID(
		ctx,
		campaignID,
		&repository.CampaignRecipientOption{WithRecipient: true},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipients", "error", err)
		return nil, errs.Wrap(err)
	}
	recipients := newReportedCSVRecipients[*model.CampaignRecipient]()
	for _, cr := range campaignRecipients.Rows {
		if cr.RecipientID.IsNull() || cr.Recipient == nil {
			continue
		}
		email, err := cr.Recipient.Email.Get()
		if err != nil {
			continue
		}
		recipients.add(email.String(), cr)
	}

	anonymous := campaignAnonymous(campaign)
	result := &ReportedCSVResult{Errors: []ReportedCSVRowError{}}

	for _, row := range rows {
		cell, ok := readReportedCSVRow(result, row, emailColumnIndex, dateColumnIndex, true)
		if !ok {
			continue
		}
		parsedDate, err := parseReportedDate(cell.date)
		if err != nil {
			result.fail(row.Line, "Invalid date "+quoteCSVValue(cell.date))
			continue
		}
		if _, err := vo.NewEmail(cell.email); err != nil {
			result.fail(row.Line, "Invalid email "+quoteCSVValue(cell.email))
			continue
		}
		campaignRecipient, found, ambiguous := recipients.find(cell.email)
		if ambiguous {
			result.fail(row.Line, quoteCSVValue(cell.email)+" matches more than one recipient, use the exact case")
			continue
		}
		if !found {
			result.fail(row.Line, quoteCSVValue(cell.email)+" is not a recipient of this campaign")
			continue
		}
		recorded, err := c.recordReportedEvent(ctx, campaign, campaignRecipient, parsedDate)
		if err != nil {
			c.Logger.Errorw("failed to record reported event", "error", err, "line", row.Line)
			result.fail(row.Line, "Could not be saved")
			continue
		}
		result.countRecorded(recorded, anonymous)
	}

	ae.Details["processed"] = result.Processed
	ae.Details["skipped"] = result.Skipped
	ae.Details["failed"] = result.Failed
	if !anonymous {
		ae.Details["alreadyReported"] = result.AlreadyReported
	}
	c.AuditLogAuthorized(ae)

	return result, nil
}
