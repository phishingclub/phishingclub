package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
)

// ReportInbox is the repository for report inbox configuration and the ids of
// processed mailbox messages
type ReportInbox struct {
	DB *gorm.DB
}

// ReportInboxPollState is the outcome of one poll, written back to the config
type ReportInboxPollState struct {
	Watermark *time.Time
	// ResumeAt is where the next poll continues, nil to go back to the watermark.
	// Only written on success.
	ResumeAt      *time.Time
	NextPollAt    time.Time
	PolledAt      time.Time
	Success       bool
	LastError     string
	FailureCount  int
	ProcessedDiff int
	MatchedDiff   int
}

// Insert inserts a new report inbox config. readSince is set when the inbox
// is created turned on.
func (r *ReportInbox) Insert(ctx context.Context, inbox *model.ReportInbox, readSince *time.Time) (*uuid.UUID, error) {
	id := uuid.New()
	row := inbox.ToDBMap()
	row["id"] = id
	if readSince != nil {
		row["read_since"] = utils.RFC3339UTC(*readSince)
	}
	AddTimestamps(row)
	row["company_id"] = nil
	if companyID, err := inbox.CompanyID.Get(); err == nil {
		row["company_id"] = companyID.String()
	}
	res := r.DB.WithContext(ctx).Model(&database.ReportInbox{}).Create(row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetByCompanyID returns the inbox of a company, or the global inbox when nil
func (r *ReportInbox) GetByCompanyID(ctx context.Context, companyID *uuid.UUID) (*model.ReportInbox, error) {
	var row database.ReportInbox
	db := r.DB.WithContext(ctx)
	if companyID == nil {
		db = db.Where("company_id IS NULL")
	} else {
		db = db.Where("company_id = ?", companyID.String())
	}
	if res := db.First(&row); res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToReportInbox(&row), nil
}

// GetByID returns an inbox by id
func (r *ReportInbox) GetByID(ctx context.Context, id *uuid.UUID) (*model.ReportInbox, error) {
	var row database.ReportInbox
	res := r.DB.WithContext(ctx).Where("id = ?", id.String()).First(&row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return ToReportInbox(&row), nil
}

// ReportInboxUpdate is the poll state that changes together with the settings
type ReportInboxUpdate struct {
	// ResetPoll starts reading the mailbox from scratch, used when the mailbox or
	// the credentials change. The next poll runs right away.
	ResetPoll bool
	// RetryNow clears the last error and polls right away, used when the
	// credentials change but the mailbox stays the same
	RetryNow bool
	// ReadSince sets the time from which the mailbox is read, nil keeps it
	ReadSince *time.Time
}

// UpdateByID updates the user editable fields and the poll state in one write,
// so a poll never sees new settings with old poll state
func (r *ReportInbox) UpdateByID(ctx context.Context, id *uuid.UUID, inbox *model.ReportInbox, u ReportInboxUpdate) error {
	row := inbox.ToDBMap()
	AddUpdatedAt(row)
	row["config_version"] = gorm.Expr("config_version + 1")
	if u.ResetPoll {
		row["watermark"] = nil
		row["resume_at"] = nil
		row["next_poll_at"] = nil
		row["last_error"] = ""
		row["failure_count"] = 0
	}
	if u.RetryNow {
		row["next_poll_at"] = nil
		row["last_error"] = ""
		row["failure_count"] = 0
	}
	if u.ReadSince != nil {
		row["read_since"] = utils.RFC3339UTC(*u.ReadSince)
	}
	// both writes in one transaction, the messages of the old mailbox go with
	// the reset so they are not retried against the new one
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if res := tx.Model(&database.ReportInbox{}).Where("id = ?", id.String()).Updates(row); res.Error != nil {
			return res.Error
		}
		if u.ResetPoll {
			if res := tx.Where("report_inbox_id = ?", id.String()).Delete(&database.ReportInboxMessage{}); res.Error != nil {
				return res.Error
			}
		}
		return nil
	}))
}

// AddCounts adds to the processed and matched counts. A deleted inbox is not
// changed.
func (r *ReportInbox) AddCounts(ctx context.Context, id *uuid.UUID, processed, matched int) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInbox{}).
		Where("id = ?", id.String()).
		Updates(map[string]any{
			"processed_count": gorm.Expr("processed_count + ?", processed),
			"matched_count":   gorm.Expr("matched_count + ?", matched),
		})
	return errs.Wrap(res.Error)
}

// CountByOAuthProviderID counts the inboxes that sign in with an oauth provider
func (r *ReportInbox) CountByOAuthProviderID(ctx context.Context, providerID uuid.UUID) (int64, error) {
	var n int64
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInbox{}).
		Where("o_auth_provider_id = ?", providerID.String()).
		Count(&n)
	return n, errs.Wrap(res.Error)
}

// PollNow makes the inbox due on the next poller round
func (r *ReportInbox) PollNow(ctx context.Context, id *uuid.UUID) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInbox{}).
		Where("id = ?", id.String()).
		Update("next_poll_at", nil)
	return errs.Wrap(res.Error)
}

// DeleteByID deletes an inbox and the ids of its processed messages
func (r *ReportInbox) DeleteByID(ctx context.Context, id *uuid.UUID) error {
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if res := tx.Where("report_inbox_id = ?", id.String()).Delete(&database.ReportInboxMessage{}); res.Error != nil {
			return res.Error
		}
		return tx.Where("id = ?", id.String()).Delete(&database.ReportInbox{}).Error
	}))
}

// ListDue returns enabled inboxes whose next poll time has come
func (r *ReportInbox) ListDue(ctx context.Context, now time.Time, limit int) ([]*model.ReportInbox, error) {
	var rows []database.ReportInbox
	res := r.DB.WithContext(ctx).
		Where("enabled = ?", true).
		Where("next_poll_at IS NULL OR next_poll_at <= ?", utils.RFC3339UTC(now)).
		// inboxes asked to poll now (NULL) first, the same on SQLite and Postgres
		Order("next_poll_at IS NOT NULL").
		Order("next_poll_at").
		Limit(limit).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*model.ReportInbox, 0, len(rows))
	for i := range rows {
		out = append(out, ToReportInbox(&rows[i]))
	}
	return out, nil
}

// SavePollState writes the outcome of a poll. It only writes when the settings
// are still at configVersion, so a poll that started before a change or a delete
// does not write over it. saved is false when nothing was written.
func (r *ReportInbox) SavePollState(ctx context.Context, id *uuid.UUID, configVersion int, s *ReportInboxPollState) (bool, error) {
	row := map[string]any{
		"next_poll_at":    utils.RFC3339UTC(s.NextPollAt),
		"last_polled_at":  utils.RFC3339UTC(s.PolledAt),
		"last_error":      s.LastError,
		"failure_count":   s.FailureCount,
		"processed_count": gorm.Expr("processed_count + ?", s.ProcessedDiff),
		"matched_count":   gorm.Expr("matched_count + ?", s.MatchedDiff),
	}
	if s.Watermark != nil {
		row["watermark"] = utils.RFC3339UTC(*s.Watermark)
	}
	if s.Success {
		row["last_success_at"] = utils.RFC3339UTC(s.PolledAt)
		row["resume_at"] = nil
		if s.ResumeAt != nil {
			row["resume_at"] = utils.RFC3339UTC(*s.ResumeAt)
		}
	}
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInbox{}).
		Where("id = ?", id.String()).
		Where("config_version = ?", configVersion).
		Updates(row)
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	return res.RowsAffected > 0, nil
}

// SeenMessages returns which of the message ids were already processed
func (r *ReportInbox) SeenMessages(ctx context.Context, inboxID *uuid.UUID, messageIDs []string) (map[string]bool, error) {
	seen := make(map[string]bool, len(messageIDs))
	if len(messageIDs) == 0 {
		return seen, nil
	}
	var found []string
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInboxMessage{}).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("message_id IN ?", messageIDs).
		Pluck("message_id", &found)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	for _, id := range found {
		seen[id] = true
	}
	return seen, nil
}

// ResolveMessage records a message as handled, also one that was pending
func (r *ReportInbox) ResolveMessage(
	ctx context.Context,
	inboxID *uuid.UUID,
	messageID string,
	receivedAt time.Time,
	matched bool,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ReportInboxMessage{}).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("message_id = ?", messageID).
		Updates(map[string]any{"pending": false, "matched": matched})
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	if res.RowsAffected > 0 {
		return nil
	}
	res = r.DB.WithContext(ctx).
		Model(&database.ReportInboxMessage{}).
		Create(map[string]any{
			"id":              uuid.New(),
			"created_at":      fixedTime(time.Now()),
			"report_inbox_id": inboxID.String(),
			"message_id":      messageID,
			"received_at":     utils.RFC3339UTC(receivedAt),
			"matched":         matched,
			"pending":         false,
			"failures":        0,
		})
	return errs.Wrap(res.Error)
}

// MarkMessageFailed records a failed read of a message, which is then pending
// and tried again by its id. It returns how often it failed and when first.
func (r *ReportInbox) MarkMessageFailed(
	ctx context.Context,
	inboxID *uuid.UUID,
	messageID string,
	receivedAt time.Time,
) (int, time.Time, error) {
	now := time.Now()
	var rows []database.ReportInboxMessage
	res := r.DB.WithContext(ctx).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("message_id = ?", messageID).
		Limit(1).
		Find(&rows)
	if res.Error != nil {
		return 0, now, errs.Wrap(res.Error)
	}
	if len(rows) == 0 {
		res = r.DB.WithContext(ctx).
			Model(&database.ReportInboxMessage{}).
			Create(map[string]any{
				"id":              uuid.New(),
				"created_at":      fixedTime(time.Now()),
				"report_inbox_id": inboxID.String(),
				"message_id":      messageID,
				"received_at":     utils.RFC3339UTC(receivedAt),
				"matched":         false,
				"pending":         true,
				"failures":        1,
				"first_failed_at": fixedTime(now),
				"last_failed_at":  fixedTime(now),
			})
		return 1, now, errs.Wrap(res.Error)
	}
	row := rows[0]
	first := now
	if row.FirstFailedAt != nil {
		first = *row.FirstFailedAt
	}
	res = r.DB.WithContext(ctx).
		Model(&database.ReportInboxMessage{}).
		Where("id = ?", row.ID.String()).
		Updates(map[string]any{
			"pending":         true,
			"failures":        gorm.Expr("failures + 1"),
			"first_failed_at": fixedTime(first),
			"last_failed_at":  fixedTime(now),
		})
	return row.Failures + 1, first, errs.Wrap(res.Error)
}

// PendingMessage is a message that failed to read and is tried again
type PendingMessage struct {
	MessageID  string
	ReceivedAt time.Time
}

// PendingMessages returns pending messages of an inbox that last failed before
// the given time, the ones that failed longest ago first
func (r *ReportInbox) PendingMessages(ctx context.Context, inboxID *uuid.UUID, limit int, failedBefore time.Time) ([]PendingMessage, error) {
	var rows []database.ReportInboxMessage
	res := r.DB.WithContext(ctx).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("pending = ?", true).
		Where("last_failed_at IS NULL OR last_failed_at < ?", fixedTime(failedBefore)).
		Order("last_failed_at").
		Limit(limit).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]PendingMessage, 0, len(rows))
	for _, row := range rows {
		p := PendingMessage{MessageID: row.MessageID}
		if row.ReceivedAt != nil {
			p.ReceivedAt = *row.ReceivedAt
		}
		out = append(out, p)
	}
	return out, nil
}

// PruneMessages forgets processed messages received before the given time. They
// are older than the watermark, so the mailbox query never returns them again.
func (r *ReportInbox) PruneMessages(ctx context.Context, inboxID *uuid.UUID, before time.Time) error {
	res := r.DB.WithContext(ctx).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("received_at < ?", utils.RFC3339UTC(before)).
		// a pending message stays until it is read or passed over
		Where("pending = ?", false).
		Delete(&database.ReportInboxMessage{})
	return errs.Wrap(res.Error)
}

// SampleRecipientEmails returns up to limit recipient addresses in the scope on
// the same domain as the mailbox, the mailbox itself left out
func (r *ReportInbox) SampleRecipientEmails(ctx context.Context, companyID *uuid.UUID, mailbox string, limit int) ([]string, error) {
	at := strings.LastIndex(mailbox, "@")
	if at < 0 || at == len(mailbox)-1 {
		return nil, nil
	}
	domain := strings.ToLower(mailbox[at+1:])
	// LIKE wildcards in the domain would widen the match, so only plain
	// domains are searched
	if strings.ContainsAny(domain, "%_\\") {
		return nil, nil
	}
	db := r.DB.WithContext(ctx).Model(&database.Recipient{}).
		Where("email IS NOT NULL").
		Where("LOWER(email) LIKE ?", "%@"+domain).
		Where("LOWER(email) <> ?", strings.ToLower(mailbox)).
		// a deactivated recipient often has no mailbox any more
		Where("scim_soft_deleted_at IS NULL")
	if companyID == nil {
		db = db.Where("company_id IS NULL")
	} else {
		db = db.Where("company_id = ?", companyID.String())
	}
	var emails []string
	if res := db.Order("created_at").Limit(limit).Pluck("email", &emails); res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return emails, nil
}

// Senders are the sender addresses and domains used by the emails of a scope
type Senders struct {
	Domains   map[string]bool
	Addresses map[string]bool
	// Served are the domains Phishing Club serves, by lower case name, used to
	// read lure codes from links. The inbox scope is checked when a match is
	// recorded.
	Served map[string]*database.Domain
}

// SenderInfo returns the From and envelope From addresses and domains of the
// emails a scope can send with: a company's own and the global ones, or only
// the global ones for the global scope
func (r *ReportInbox) SenderInfo(ctx context.Context, companyID *uuid.UUID) (*Senders, error) {
	db := r.DB.WithContext(ctx).Model(&database.Email{}).Select([]string{"from", "mail_from"})
	if companyID == nil {
		db = db.Where("company_id IS NULL")
	} else {
		db = db.Where("company_id = ? OR company_id IS NULL", companyID.String())
	}
	var rows []database.Email
	if res := db.Find(&rows); res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := &Senders{Domains: map[string]bool{}, Addresses: map[string]bool{}, Served: map[string]*database.Domain{}}
	// the domains the scope's campaigns can use: a company's own and the global
	// ones, or only the global ones for the global scope
	ddb := r.DB.WithContext(ctx).Model(&database.Domain{}).Select([]string{"id", "name", "proxy_id"})
	if companyID == nil {
		ddb = ddb.Where("company_id IS NULL")
	} else {
		ddb = ddb.Where("company_id = ? OR company_id IS NULL", companyID.String())
	}
	var domains []database.Domain
	if res := ddb.Find(&domains); res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	for i := range domains {
		out.Served[strings.ToLower(domains[i].Name)] = &domains[i]
	}
	for _, row := range rows {
		for _, v := range []string{row.From, row.MailFrom} {
			if d := AddressDomain(v); d != "" {
				out.Domains[d] = true
			}
			if a := BareAddress(v); a != "" {
				out.Addresses[a] = true
			}
		}
	}
	return out, nil
}

// BareAddress returns the lower case address of "Name <a@b.example>" or
// "a@b.example", empty when there is none
func BareAddress(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.LastIndex(v, "<"); i >= 0 {
		v = v[i+1:]
	}
	v = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), ">")))
	if !strings.Contains(v, "@") {
		return ""
	}
	return v
}

// MessageIDUsed tells if any campaign recipient was sent an email with this
// Message-ID
func (r *ReportInbox) MessageIDUsed(ctx context.Context, messageID string) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	var n int64
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("message_id = ?", messageID).
		Limit(1).
		Count(&n)
	return n > 0, errs.Wrap(res.Error)
}

// AddressDomain returns the lower case domain of an address such as
// "Name <a@b.example>" or "a@b.example", empty when there is none
func AddressDomain(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.LastIndex(v, "<"); i >= 0 {
		v = v[i+1:]
	}
	v = strings.TrimSuffix(strings.TrimSpace(v), ">")
	at := strings.LastIndex(v, "@")
	if at < 0 || at == len(v)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(v[at+1:]))
}

// ToReportInbox converts a database row to the model
func ToReportInbox(row *database.ReportInbox) *model.ReportInbox {
	m := &model.ReportInbox{
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Enabled:         row.Enabled,
		AuthMode:        row.AuthMode,
		Mailbox:         row.Mailbox,
		TenantID:        row.TenantID,
		ClientID:        row.ClientID,
		ClientSecret:    row.ClientSecret,
		HasClientSecret: row.ClientSecret != "",
		ReadSince:       row.ReadSince,
		Watermark:       row.Watermark,
		ResumeAt:        row.ResumeAt,
		NextPollAt:      row.NextPollAt,
		LastPolledAt:    row.LastPolledAt,
		LastSuccessAt:   row.LastSuccessAt,
		LastError:       row.LastError,
		FailureCount:    row.FailureCount,
		ProcessedCount:  row.ProcessedCount,
		MatchedCount:    row.MatchedCount,
		ConfigVersion:   row.ConfigVersion,
	}
	if row.ID != nil {
		m.ID = nullable.NewNullableWithValue(*row.ID)
	}
	m.CompanyID = nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		m.CompanyID = nullable.NewNullableWithValue(*row.CompanyID)
	}
	m.OAuthProviderID = nullable.NewNullNullable[uuid.UUID]()
	if row.OAuthProviderID != nil {
		m.OAuthProviderID = nullable.NewNullableWithValue(*row.OAuthProviderID)
	}
	return m
}

// DeleteMessagesSince removes the message rows of an inbox written since a time,
// used when a poll of a mailbox that was changed meanwhile wrote them
func (r *ReportInbox) DeleteMessagesSince(ctx context.Context, inboxID *uuid.UUID, since time.Time) error {
	res := r.DB.WithContext(ctx).
		Where("report_inbox_id = ?", inboxID.String()).
		Where("created_at >= ?", fixedTime(since)).
		Delete(&database.ReportInboxMessage{})
	return errs.Wrap(res.Error)
}

// fixedTime formats a time with a fixed number of fraction digits, so stored
// times of report inbox messages compare correctly as text in SQLite
func fixedTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}
