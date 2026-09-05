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
	// ToDBMap omits the lure code, so it is carried here
	if code, err := campaignRecipient.LureCode.Get(); err == nil && code != "" {
		row["lure_code"] = code
	}
	if custom, err := campaignRecipient.LureCodeCustom.Get(); err == nil {
		row["lure_code_custom"] = custom
	}

	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// SetLureCodeByID assigns an operator chosen code to one recipient. The only
// path that writes a code after insert, and it touches nothing else, so a caller
// holding a stale recipient model cannot restate a code through it.
func (r *CampaignRecipient) SetLureCodeByID(
	ctx context.Context,
	id *uuid.UUID,
	code string,
) error {
	row := map[string]any{
		"lure_code":        code,
		"lure_code_custom": true,
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME)),
			id.String(),
		).
		Updates(row)

	return res.Error
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

// GetByLureCodeOnDomain gets a campaign recipient by the code carried in a
// request, but only when the code belongs to a campaign reachable on the domain
// serving it.
//
// The match is case sensitive, keeping Special-42 distinct from special-42 as
// base58 requires. A released code is null and so drops out without a separate
// predicate, which is also what stops an anonymized recipient's link working.
//
// The domain predicate keeps a guessed code from reaching across the instance.
// Codes are unique instance wide and short enough to guess, so without it a
// guess on any domain would resolve a recipient of any campaign, including
// another company's. A campaign is reachable on its template's own domain and on
// a proxy domain whose proxy the template uses for one of its pages.
func (r *CampaignRecipient) GetByLureCodeOnDomain(
	ctx context.Context,
	code string,
	domain *database.Domain,
) (*model.CampaignRecipient, error) {
	if code == "" || domain == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var dbCampaignRecipient database.CampaignRecipient
	query := r.DB.
		Model(&database.CampaignRecipient{}).
		Joins(fmt.Sprintf(
			"JOIN `%s` ON %s = %s",
			database.CAMPAIGN_TABLE,
			TableColumnID(database.CAMPAIGN_TABLE),
			TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id"),
		)).
		Joins(fmt.Sprintf(
			"JOIN `%s` ON %s = %s",
			database.CAMPAIGN_TEMPLATE_TABLE,
			TableColumnID(database.CAMPAIGN_TEMPLATE_TABLE),
			TableColumn(database.CAMPAIGN_TABLE, "campaign_template_id"),
		)).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
			code,
		).
		// restating the index predicate. equality already excludes nulls, so no
		// result changes, but the planner matches the partial unique index without
		// deriving it. this sits on the request path, where a scan is paid on
		// every visit.
		Where(
			fmt.Sprintf("%s IS NOT NULL", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
		)

	if domain.ProxyID != nil {
		// a proxy domain serves whichever template routes a page through that
		// proxy, while the template's own domain still serves evasion and deny.
		// the parentheses are written here rather than left to the builder:
		// unparenthesised this would widen to match any campaign using the proxy
		// regardless of the rest of the clause, and it is what confines a guessed
		// code to one domain.
		query = query.Where(
			fmt.Sprintf(
				"(%s = ? OR %s = ? OR %s = ? OR %s = ?)",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "domain_id"),
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "before_landing_proxy_id"),
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "landing_proxy_id"),
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "after_landing_proxy_id"),
			),
			domain.ID,
			domain.ProxyID,
			domain.ProxyID,
			domain.ProxyID,
		)
	} else {
		query = query.Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "domain_id")),
			domain.ID,
		)
	}

	res := query.
		Select(TableColumnAll(database.CAMPAIGN_RECIPIENT_TABLE_NAME)).
		First(&dbCampaignRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignRecipient(&dbCampaignRecipient)
}

// findTakenLureCodesChunkSize keeps each IN clause inside the sqlite bound
// variable limit, 999 on the most restrictive builds.
const findTakenLureCodesChunkSize = 500

// FindTakenLureCodes returns the subset of codes already claimed by a recipient
// whose code has not been released.
//
// Querying lure_code, the column carrying the uniqueness constraint, catches a
// collision with an operator written code here rather than through a failed
// insert part way through scheduling. Probing a whole batch costs a handful of
// queries instead of one insert and retry per recipient.
func (r *CampaignRecipient) FindTakenLureCodes(
	ctx context.Context,
	codes []string,
) ([]string, error) {
	taken := []string{}
	for start := 0; start < len(codes); start += findTakenLureCodesChunkSize {
		end := start + findTakenLureCodesChunkSize
		if end > len(codes) {
			end = len(codes)
		}
		chunk := []string{}
		res := r.DB.
			Model(&database.CampaignRecipient{}).
			Where(
				fmt.Sprintf("%s IN ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
				codes[start:end],
			).
			Where(
				fmt.Sprintf("%s IS NOT NULL", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
			).
			Pluck("lure_code", &chunk)

		if res.Error != nil {
			return nil, res.Error
		}
		taken = append(taken, chunk...)
	}
	return taken, nil
}

// GetActiveByLureCode returns the recipient holding a code, so the owning
// campaign can be named when an operator tries to reuse it.
func (r *CampaignRecipient) GetActiveByLureCode(
	ctx context.Context,
	code string,
) (*model.CampaignRecipient, error) {
	var dbCampaignRecipient database.CampaignRecipient
	res := r.DB.
		Preload("Campaign").
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
			code,
		).
		Where(
			fmt.Sprintf("%s IS NOT NULL", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code")),
		).
		First(&dbCampaignRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignRecipient(&dbCampaignRecipient)
}

// ReleaseLureCodeByID frees a recipient's code for reuse. Nulling rather than
// flagging takes it out of the unique index and stops it showing against a
// recipient who no longer owns it. The row and its events are untouched.
func (r *CampaignRecipient) ReleaseLureCodeByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]any{
		"lure_code": nil,
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME)),
			id.String(),
		).
		Updates(row)

	return res.Error
}

// HasRecipientsByCampaignID reports whether the campaign still holds any
// recipient row. Scheduling reads it to decide whether the lure settings are
// settled: a campaign holding recipients has links out resolving through those
// rows. A non self managed reschedule deletes them first, which is what lets it
// pick up a template change, its old links being dead either way.
func (r *CampaignRecipient) HasRecipientsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) (bool, error) {
	// campaign_id leads the campaign and recipient unique index, so this stops at
	// the first entry rather than counting the campaign
	ids := []string{}
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		).
		Limit(1).
		Pluck("id", &ids)

	if res.Error != nil {
		return false, res.Error
	}
	return len(ids) > 0, nil
}

// HasCustomLureCodesByCampaignID reports whether any recipient in the campaign
// carries an operator set code. The recipient table is paginated, so this cannot
// be derived from one page of rows.
func (r *CampaignRecipient) HasCustomLureCodesByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) (bool, error) {
	// the flag is a literal because sqlite chooses the partial index at prepare
	// time, where a bound value is unknown. keep it in step with the predicate
	// in database.CampaignRecipient.Migrate. campaign_id is plucked so the
	// answer comes from the index without reading the row.
	ids := []string{}
	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		).
		Where(
			fmt.Sprintf("%s = 1", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "lure_code_custom")),
		).
		Limit(1).
		Pluck("campaign_id", &ids)

	if res.Error != nil {
		return false, res.Error
	}
	return len(ids) > 0, nil
}

// GetUnsendRecipients gets campaign recipients that were never attempted and are
// not already cancelled (cancelled_at IS NULL AND last_attempt_at IS NULL). Used
// at campaign close to cancel only sends that never started; recipients that were
// attempted (sent, failed, or still in flight) are left untouched so their
// existing state is not overwritten.
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
		// authoritative guard: never deliver to a SCIM-disabled recipient, even if
		// their row was not cancelled (e.g. disabled while the campaign was not yet
		// active). A subquery is used instead of a join so the campaign_recipients
		// SELECT is never widened with ambiguous recipient columns.
		Where(fmt.Sprintf(
			"NOT EXISTS (SELECT 1 FROM %s sd WHERE sd.id = %s.recipient_id AND sd.scim_soft_deleted_at IS NOT NULL)",
			database.RECIPIENT_TABLE,
			database.CAMPAIGN_RECIPIENT_TABLE_NAME,
		)).
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

// UpdateNotableEventByID updates only the notable event id for a campaign recipient
func (c *CampaignRecipient) UpdateNotableEventByID(
	ctx context.Context,
	campaignRecipientID *uuid.UUID,
	notableEventTypeID *uuid.UUID,
) error {
	if campaignRecipientID == nil {
		return nil
	}

	row := map[string]any{
		"notable_event_id": notableEventTypeID,
	}
	AddUpdatedAt(row)

	res := c.DB.
		WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("id = ?", campaignRecipientID).
		Updates(row)

	return res.Error
}

// Anonymize ensures a campaign recipient carries a pseudonym and releases its
// lure code. An existing pseudonym is never overwritten: an anonymous campaign
// assigns a stable one at materialization and its events already carry it, so a
// later anonymize (at close, or when the recipient is deleted) must keep it or the
// events would be orphaned from the recipient row.
func (r *CampaignRecipient) Anonymize(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) error {
	// where clause: a specific campaign, or all campaigns for the recipient (used
	// when deleting the recipient).
	scope := func(db *gorm.DB) *gorm.DB {
		if campaignID != nil {
			return db.Where("campaign_id = ? AND recipient_id = ?", campaignID, recipientID)
		}
		return db.Where("recipient_id = ?", recipientID)
	}

	// release the lure code so the link stops resolving and the code can be reused.
	lureRow := map[string]interface{}{"lure_code": nil}
	AddUpdatedAt(lureRow)
	if res := scope(r.DB.Model(&database.CampaignRecipient{})).Updates(lureRow); res.Error != nil {
		return res.Error
	}

	// assign the pseudonym only where none exists yet. an anonymous campaign already
	// has a stable one whose events reference it, so overwriting would orphan them.
	// a plain assignment (not COALESCE) keeps the uuid column type unambiguous on
	// both sqlite and postgres.
	idRow := map[string]interface{}{"anonymized_id": anonymizedID.String()}
	AddUpdatedAt(idRow)
	if res := scope(r.DB.Model(&database.CampaignRecipient{})).
		Where("anonymized_id IS NULL").
		Updates(idRow); res.Error != nil {
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

// CancelUnsentInOpenCampaigns cancels a recipient's not-yet-sent rows in any
// campaign that is not closed, regardless of whether it has started. Used when a
// recipient is deleted/anonymized so no uncancelled pending row is left behind in
// a future-scheduled campaign. Already-sent rows are left untouched as history.
func (r *CampaignRecipient) CancelUnsentInOpenCampaigns(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	row := map[string]any{
		"cancelled_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	openCampaigns := r.DB.Table(database.CAMPAIGN_TABLE).
		Select("id").
		Where(fmt.Sprintf("%s IS NULL", TableColumn(database.CAMPAIGN_TABLE, "closed_at")))

	res := r.DB.
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", openCampaigns).
		Where("recipient_id = ?", recipientID).
		Where(fmt.Sprintf("%s IS NULL", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "sent_at"))).
		Where(fmt.Sprintf("%s IS NULL", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "cancelled_at"))).
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
	var lureCode nullable.Nullable[string]
	lureCode.SetNull()
	if row.LureCode != nil {
		lureCode = nullable.NewNullableWithValue(*row.LureCode)
	}
	position := nullable.NewNullableWithValue(row.Position)
	department := nullable.NewNullableWithValue(row.Department)
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
		Position:         position,
		Department:       department,
		NotableEventID:   notableEventID,
		NotableEventName: notableEventName,
		LureCode:         lureCode,
		LureCodeCustom:   nullable.NewNullableWithValue(row.LureCodeCustom),
	}, nil
}
