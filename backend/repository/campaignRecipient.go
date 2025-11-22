package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var allowedCampaignRecipientColumns = []string{
	"campaign_recipients.created_at",
	"campaign_recipients.updated_at",
	"campaign_recipients.send_at",
	"campaign_recipients.sent_at",
	"campaign_recipients.cancelled_at",
	"campaign_recipients.notable_event_id",
	"recipients.first_name",
	"recipients.last_name",
	"recipients.email",
}

// CampaignRecipientOption is options for preloading
type CampaignRecipientOption struct {
	*vo.QueryArgs
	WithCampaign  bool
	WithRecipient bool
}

// CampaignRecipient is a CampaignRecipient repository
// this holds campaign-recipients and their campaign results
type CampaignRecipient struct {
	DB *gorm.DB
}

// Preload preloads the campaign recipients
func (r *CampaignRecipient) preload(db *gorm.DB, options *CampaignRecipientOption) *gorm.DB {
	if options.WithRecipient {
		db = db.Preload("Recipient")
	}
	if options.WithCampaign {
		db = db.Preload("Campaign")
	}
	return db
}

// Cancel cancels recipients
func (r *CampaignRecipient) Cancel(
	ctx context.Context,
	campaignRecipientUUIDs []*uuid.UUID,
) error {
	if len(campaignRecipientUUIDs) == 0 {
		return nil
	}
	row := map[string]any{
		"cancelled_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	result := r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf(
				"%s IN ?",
				TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME),
			),
			UUIDsToStrings(campaignRecipientUUIDs),
		).
		Updates(row)

	if result.Error != nil {
		return result.Error
	}
	// set notable event
	if len(campaignRecipientUUIDs) == 0 {
		return nil
	}
	row = map[string]any{
		"notable_event_id": cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED],
	}
	AddUpdatedAt(row)
	result = r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf(
				"%s IN ? AND sent_at IS NULL AND cancelled_at IS NOT NULL",
				TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME),
			),
			UUIDsToStrings(campaignRecipientUUIDs),
		).
		Where(
			"notable_event_id IS NULL OR notable_event_id IS ?",
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED],
		).
		Updates(row)

	if result.Error != nil {
		return result.Error
	}

	return nil
}

// Insert inserts a new campaign recipient
func (r *CampaignRecipient) Insert(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	//campaignRecipient *database.CampaignRecipient,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := campaignRecipient.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// DeleteRecipientsNotIn deletes recipients in campaign that are
// not in the slice recipient ids supplied
func (r *CampaignRecipient) DeleteRecipientsNotIn(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) error {
	res := r.DB.
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		).
		Where(
			fmt.Sprintf("%s NOT IN ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "recipient_id")),
			UUIDsToStrings(recipientIDs),
		).
		Delete(&database.CampaignRecipient{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetRecipiensByCampaignID gets all campaignrecipients by campaign id
func (r *CampaignRecipient) GetByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
	options *CampaignRecipientOption,
) (*model.Result[model.CampaignRecipient], error) {
	result := model.NewEmptyResult[model.CampaignRecipient]()
	db, err := useQuery(r.DB, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignRecipientColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	db = r.preload(db, options)
	var dbCampaignRecipients []database.CampaignRecipient
	res := db.
		Joins("LEFT JOIN recipients ON recipients.id = campaign_recipients.recipient_id").
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		).
		Find(&dbCampaignRecipients)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.CAMPAIGN_RECIPIENT_TABLE_NAME, options.QueryArgs, allowedCampaignRecipientColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaignRecipient := range dbCampaignRecipients {
		r, err := ToCampaignRecipient(&dbCampaignRecipient)
		if err != nil {
			return result, nil
		}
		result.Rows = append(result.Rows, r)
	}
	return result, nil
}

// GetByID gets a campaign recipient by id
func (r *CampaignRecipient) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *CampaignRecipientOption,
) (*model.CampaignRecipient, error) {
	db := r.preload(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_RECIPIENT_TABLE_NAME, options.QueryArgs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var dbCampaignRecipient database.CampaignRecipient
	res := db.
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME)),
			id.String(),
		).
		First(&dbCampaignRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignRecipient(&dbCampaignRecipient)
}

// GetByCampaignAndRecipientID gets a campaign recipient by campaign and recipient id
func (r *CampaignRecipient) GetByCampaignAndRecipientID(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	options *CampaignRecipientOption,
) (*model.CampaignRecipient, error) {
	db := r.preload(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_RECIPIENT_TABLE_NAME, options.QueryArgs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var dbCampaignRecipient database.CampaignRecipient
	res := db.
		Where(
			fmt.Sprintf(
				"%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id"),
			),
			campaignID.String(),
		).
		Where(
			fmt.Sprintf(
				"%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "recipient_id"),
			),
			recipientID.String(),
		).
		First(&dbCampaignRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignRecipient(&dbCampaignRecipient)
}

// GetByCampaignRecipientID gets a campaign and recipient by campaign recipient id
func (r *CampaignRecipient) GetByCampaignRecipientID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.CampaignRecipient, error) {
	var dbCampaignRecipient database.CampaignRecipient
	res := r.DB.
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME)),
			id.String(),
		).
		First(&dbCampaignRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignRecipient(&dbCampaignRecipient)
}

// GetUnsendRecipients gets all campaign recipients that are not sent
// and have been attempted or been cancelled
// if limit is larger than 0 it will limit the number of results
// if campaignID is not nil, it will filter by that campaign
func (r *CampaignRecipient) GetUnsendRecipients(
	ctx context.Context,
	campaignID *uuid.UUID,
	limit int,
	options *CampaignRecipientOption,
) ([]*model.CampaignRecipient, error) {
	recps := []*model.CampaignRecipient{}
	db := r.preload(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_RECIPIENT_TABLE_NAME, options.QueryArgs)
	if err != nil {
		return recps, errs.Wrap(err)
	}
	var dbCampaignRecipients []database.CampaignRecipient

	q := db.Where(
		fmt.Sprintf(
			"%s IS NULL AND %s IS NULL",
			TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "cancelled_at"),
			TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "last_attempt_at"),
		),
	)
	if campaignID != nil {
		q = q.Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	res := q.
		Find(&dbCampaignRecipients)

	if res.Error != nil {
		return recps, res.Error
	}
	for _, dbCampaignRecipient := range dbCampaignRecipients {
		r, err := ToCampaignRecipient(&dbCampaignRecipient)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		recps = append(recps, r)
	}
	return recps, nil
}

// GetUnsendRecipientsForSending gets all campaign recipients that are not sent
// and have not reached the max send attempts or been cancelled
// the limit is only used if it is larger than 0
func (r *CampaignRecipient) GetUnsendRecipientsForSending(
	ctx context.Context,
	limit int,
	options *CampaignRecipientOption,
) ([]*model.CampaignRecipient, error) {
	recps := []*model.CampaignRecipient{}
	db := r.preload(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_RECIPIENT_TABLE_NAME, options.QueryArgs)
	if err != nil {
		return recps, errs.Wrap(err)
	}
	var dbCampaignRecipients []database.CampaignRecipient
	q := db.
		Where(
			fmt.Sprintf(
				"%s IS NULL"+
					" AND %s <= ?"+
					" AND %s IS NULL"+
					" AND %s IS NULL"+
					" AND %s = false",
				TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "sent_at"),
				TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "send_at"),
				TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "cancelled_at"),
				TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "last_attempt_at"),
				TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "self_managed"),
			), utils.NowRFC3339UTC(),
		)

	if limit > 0 {
		q = q.Limit(limit)
	}
	res := q.
		Find(&dbCampaignRecipients)

	if res.Error != nil {
		return recps, res.Error
	}
	for _, dbCampaignRecipient := range dbCampaignRecipients {
		r, err := ToCampaignRecipient(&dbCampaignRecipient)
		if err != nil {
			return recps, errs.Wrap(err)
		}
		recps = append(recps, r)
	}
	return recps, nil
}

// DeleteByCampaigID removes all campaign recipients from a campaign
func (r *CampaignRecipient) DeleteByCampaigID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id"),
			),
			campaignID,
		).
		Delete(&database.CampaignRecipient{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// UpdateByID updates a campaign recipient by id
func (c *CampaignRecipient) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	campaignRecipient *model.CampaignRecipient,
) error {
	row := campaignRecipient.ToDBMap()
	AddUpdatedAt(row)

	res := c.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf(
				"%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// Anonymize adds an anonymized id to a campaign recipient
func (r *CampaignRecipient) Anonymize(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) error {
	row := map[string]interface{}{
		"anonymized_id": anonymizedID.String(),
	}
	AddUpdatedAt(row)
	db := r.DB.Model(&database.CampaignRecipient{})

	// if campaignID is nil, anonymize across all campaigns (e.g., when deleting recipient)
	// otherwise, only anonymize for the specific campaign
	if campaignID != nil {
		db = db.Where("campaign_id = ? AND recipient_id = ?", campaignID, recipientID)
	} else {
		db = db.Where("recipient_id = ?", recipientID)
	}

	res := db.Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (r *CampaignRecipient) CancelInActiveCampaigns(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	row := map[string]any{
		"cancelled_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	subSelect := r.DB.Table(database.CAMPAIGN_TABLE).Select("id")
	subSelect = appendWhereCampaignIsActive(subSelect)

	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", subSelect).
		Where("recipient_id = ?", recipientID).
		Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveRecipientIDByCampaignID removes a recipient id from all campaign recipients
// related to a campaign, this is used when anonymizing a campaign
func (r *CampaignRecipient) RemoveRecipientIDByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	row := map[string]interface{}{
		"recipient_id": nil,
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where("campaign_id = ?", campaignID).
		Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveRecipientIDByRecipientID removes a recipient id from a campaign recipient
func (r *CampaignRecipient) RemoveRecipientIDByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	row := map[string]interface{}{
		"recipient_id": nil,
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where("recipient_id = ?", recipientID).
		Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// ToCampaignRecipient converts a database campaign recipient to a model campaign recipient
func ToCampaignRecipient(row *database.CampaignRecipient) (*model.CampaignRecipient, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	var cancelledAt nullable.Nullable[time.Time]
	cancelledAt.SetNull()
	if row.CancelledAt != nil {
		cancelledAt = nullable.NewNullableWithValue(*row.CancelledAt)
	}
	var sendAt nullable.Nullable[time.Time]
	sendAt.SetNull()
	if row.SendAt != nil {
		sendAt = nullable.NewNullableWithValue(*row.SendAt)
	}
	var sentAt nullable.Nullable[time.Time]
	sentAt.SetNull()
	if row.SentAt != nil {
		sentAt = nullable.NewNullableWithValue(*row.SentAt)
	}
	var lastAttemptAt nullable.Nullable[time.Time]
	lastAttemptAt.SetNull()
	if row.LastAttemptAt != nil {
		lastAttemptAt = nullable.NewNullableWithValue(*row.LastAttemptAt)
	}
	selfManaged := nullable.NewNullableWithValue(row.SelfManaged)
	campaignID := nullable.NewNullableWithValue(*row.CampaignID)
	var recipientID nullable.Nullable[uuid.UUID]
	recipientID.SetNull()
	if row.RecipientID != nil {
		recipientID = nullable.NewNullableWithValue(*row.RecipientID)
	}
	var anonymizedID nullable.Nullable[uuid.UUID]
	anonymizedID.SetNull()
	if row.AnonymizedID != nil {
		anonymizedID = nullable.NewNullableWithValue(*row.AnonymizedID)
	}
	var recipient *model.Recipient
	if row.Recipient != nil {
		r, err := ToRecipient(row.Recipient)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		recipient = r
	}
	var campaign *model.Campaign
	if row.Campaign != nil {
		campaign, _ = ToCampaign(row.Campaign)
	}
	var notableEventName string
	var notableEventID nullable.Nullable[uuid.UUID]
	notableEventID.SetNull()
	if row.NotableEventID != nil {
		notableEventID = nullable.NewNullableWithValue(*row.NotableEventID)
		notableEventName = cache.EventNameByID[row.NotableEventID.String()]
	}
	return &model.CampaignRecipient{
		ID:               id,
		CancelledAt:      cancelledAt,
		SendAt:           sendAt,
		SentAt:           sentAt,
		LastAttemptAt:    lastAttemptAt,
		SelfManaged:      selfManaged,
		CampaignID:       campaignID,
		Campaign:         campaign,
		AnonymizedID:     anonymizedID,
		RecipientID:      recipientID,
		Recipient:        recipient,
		NotableEventID:   notableEventID,
		NotableEventName: notableEventName,
	}, nil
}
