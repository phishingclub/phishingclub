package repository

import (
	"context"
	"encoding/json"
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
	"gorm.io/gorm/clause"
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
	// ExcludeCampaignIDs leaves the rows of these campaigns out of
	// GetUnsendRecipientsForSending. The sender rests a campaign it could not
	// send for a while, so its rows do not fill every batch.
	ExcludeCampaignIDs []*uuid.UUID
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

// cancelBatch is how many ids one cancel write names, so a list of any size
// stays under the bind parameter limit
const cancelBatch = 500

// Cancel cancels recipients. The ids are written in batches of cancelBatch.
func (r *CampaignRecipient) Cancel(
	ctx context.Context,
	campaignRecipientUUIDs []*uuid.UUID,
) error {
	for start := 0; start < len(campaignRecipientUUIDs); start += cancelBatch {
		end := min(start+cancelBatch, len(campaignRecipientUUIDs))
		if err := r.cancelBatch(ctx, campaignRecipientUUIDs[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// cancelBatch cancels one batch of Cancel and sets the cancelled notable event
// on the rows not sent. The rows are found by id: sent_at, cancelled_at and
// notable_event_id are not read through an index, or sqlite picks one of
// those indexes and walks every row not sent or with no notable event.
func (r *CampaignRecipient) cancelBatch(ctx context.Context, ids []*uuid.UUID) error {
	byID := clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids)}
	row := map[string]any{
		"cancelled_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	result := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(byID).
		Updates(row)
	if result.Error != nil {
		return result.Error
	}
	row = map[string]any{
		"notable_event_id": cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED],
	}
	AddUpdatedAt(row)
	notableColumn := notIndexed(clause.Column{Name: "notable_event_id"})
	result = r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(byID).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "cancelled_at"})}}).
		Where(clause.Or(
			clause.Expr{SQL: "? IS NULL", Vars: []any{notableColumn}},
			clause.Expr{SQL: "? IS ?", Vars: []any{notableColumn, cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED]}},
		)).
		Updates(row)
	return result.Error
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
	// ToDBMap omits the template, so it is carried here
	if templateID, err := campaignRecipient.TemplateID.Get(); err == nil {
		row["campaign_template_id"] = templateID.String()
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

// RecipientSendAt pairs a campaign recipient id with a new send time, for a
// batched reschedule.
type RecipientSendAt struct {
	ID     *uuid.UUID
	SendAt time.Time
}

// SetSendAtBatch assigns new send times to many recipients in one transaction, so
// resuming a large campaign is a single commit rather than one fsync per row (the
// same reason schedule() batches its inserts). Only send_at is touched, so a sent,
// cancelled or attempted row is never disturbed. No reads happen inside the
// transaction, keeping it safe on a single connection sqlite.
func (c *CampaignRecipient) SetSendAtBatch(
	ctx context.Context,
	items []RecipientSendAt,
) error {
	if len(items) == 0 {
		return nil
	}
	return c.DB.Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			row := map[string]any{
				"send_at": utils.RFC3339UTC(it.SendAt),
			}
			AddUpdatedAt(row)
			res := tx.
				Model(&database.CampaignRecipient{}).
				Where(
					fmt.Sprintf("%s = ?", TableColumnID(database.CAMPAIGN_RECIPIENT_TABLE_NAME)),
					it.ID.String(),
				).
				Updates(row)
			if res.Error != nil {
				return res.Error
			}
		}
		return nil
	})
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
	res := withPeek(db.
		Joins("LEFT JOIN recipients ON recipients.id = campaign_recipients.recipient_id").
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_RECIPIENT_TABLE_NAME, "campaign_id")),
			campaignID,
		), options.QueryArgs).
		Find(&dbCampaignRecipients)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage := trimPeek(&dbCampaignRecipients, options.QueryArgs)
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
		Joins("JOIN campaigns ON campaigns.id = campaign_recipients.campaign_id").
		// the recipient's own template decides the domain, so a pool spread over
		// several domains matches each code only on its recipient's domain
		Joins("JOIN campaign_templates ON campaign_templates.id = COALESCE(campaign_recipients.campaign_template_id, campaigns.campaign_template_id)").
		Where("campaign_recipients.lure_code = ?", code).
		// restating the index predicate. equality already excludes nulls, so no
		// result changes, but the planner matches the partial unique index without
		// deriving it. this sits on the request path, where a scan is paid on
		// every visit.
		Where("campaign_recipients.lure_code IS NOT NULL")

	if domain.ProxyID != nil {
		// a proxy domain serves whichever template routes a page through that
		// proxy, while the template's own domain still serves evasion and deny.
		// the parentheses are written here rather than left to the builder:
		// unparenthesised this would widen to match any campaign using the proxy
		// regardless of the rest of the clause, and it is what confines a guessed
		// code to one domain.
		query = query.Where(
			"(campaign_templates.domain_id = ? OR campaign_templates.before_landing_proxy_id = ? OR campaign_templates.landing_proxy_id = ? OR campaign_templates.after_landing_proxy_id = ?)",
			domain.ID,
			domain.ProxyID,
			domain.ProxyID,
			domain.ProxyID,
		)
	} else {
		query = query.Where("campaign_templates.domain_id = ?", domain.ID)
	}

	res := query.
		Select("campaign_recipients.*").
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
		// never deliver while the campaign is paused. a subquery keeps the
		// campaign_recipients SELECT from widening with campaign columns, same as the
		// scim guard above.
		Where(fmt.Sprintf(
			"NOT EXISTS (SELECT 1 FROM %s c WHERE c.id = %s.campaign_id AND c.paused_at IS NOT NULL)",
			database.CAMPAIGN_TABLE,
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

	// nothing of a company that is being deleted is sent
	q = q.Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.campaignOfDeletingCompany(ctx)}})
	if len(options.ExcludeCampaignIDs) > 0 {
		q = q.Where(clause.Not(clause.IN{
			Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
			Values: uuidValues(options.ExcludeCampaignIDs),
		}))
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
			return recps, errs.Wrap(err)
		}
		recps = append(recps, r)
	}
	return recps, nil
}

// DeleteByCampaigID removes all campaign recipients from a campaign, in
// batches with a pause between them, see deleteInBatches
func (r *CampaignRecipient) DeleteByCampaigID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	// a delete that began finishes also when the request ends, so the sends
	// of a campaign are not left half deleted
	return deleteInBatches(context.WithoutCancel(ctx), r.DB, &database.CampaignRecipient{},
		clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}, deleteBatchSends)
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

// campaignOfDeletingCompany selects the campaign of a campaign recipient row
// when the campaign belongs to a company marked as being deleted, for a NOT
// EXISTS guard of the sender
func (r *CampaignRecipient) campaignOfDeletingCompany(ctx context.Context) *gorm.DB {
	deleting := r.DB.WithContext(ctx).
		Model(&database.Company{}).
		Select("id").
		Where(clause.Neq{Column: clause.Column{Table: database.COMPANY_TABLE, Name: "deleting_at"}, Value: nil})
	return r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("1").
		Where(clause.Expr{
			SQL: "? = ?",
			Vars: []any{
				clause.Column{Table: database.CAMPAIGN_TABLE, Name: "id"},
				clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
			},
		}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Table: database.CAMPAIGN_TABLE, Name: "company_id"}, deleting}})
}

// ClaimForSending sets the last attempt time on a row that is still due to be
// sent and reports whether it did. The sender reads a batch of rows and sends
// them one campaign after another, so a row can be cancelled, sent, paused,
// moved later or lose its recipient after it was read, and its recipient can
// be disabled by SCIM. Only a claimed row may be sent. The
// last attempt time also keeps the row out of the next batches, so a crash in
// the middle of a send does not send it twice.
func (r *CampaignRecipient) ClaimForSending(
	ctx context.Context,
	id *uuid.UUID,
	at time.Time,
) (bool, error) {
	row := map[string]any{
		"last_attempt_at": utils.RFC3339UTC(at),
	}
	AddUpdatedAt(row)
	paused := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("1").
		Where(clause.Expr{
			SQL: "? = ?",
			Vars: []any{
				clause.Column{Table: database.CAMPAIGN_TABLE, Name: "id"},
				clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
			},
		}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{clause.Column{Table: database.CAMPAIGN_TABLE, Name: "paused_at"}}})
	disabled := r.DB.WithContext(ctx).
		Model(&database.Recipient{}).
		Select("1").
		Where(clause.Expr{
			SQL: "? = ?",
			Vars: []any{
				clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
				clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "recipient_id"},
			},
		}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{clause.Column{Table: database.RECIPIENT_TABLE, Name: "scim_soft_deleted_at"}}})
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id.String()}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{clause.Column{Name: "cancelled_at"}}}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{clause.Column{Name: "sent_at"}}}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{clause.Column{Name: "last_attempt_at"}}}).
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{clause.Column{Name: "recipient_id"}}}).
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{paused}}).
		// a send moved later after the batch was read, for example by a resume
		// that spread the sends again, waits for its new time
		Where(clause.Lte{Column: clause.Column{Name: "send_at"}, Value: utils.RFC3339UTC(at)}).
		// a person disabled by SCIM after the batch was read gets nothing
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{disabled}}).
		// a company marked as being deleted after the batch was read sends
		// nothing more
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.campaignOfDeletingCompany(ctx)}}).
		Updates(row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// SetLastAttemptAtByID sets only the last attempt time of a campaign recipient
func (r *CampaignRecipient) SetLastAttemptAtByID(
	ctx context.Context,
	id *uuid.UUID,
	at time.Time,
) error {
	row := map[string]any{
		"last_attempt_at": utils.RFC3339UTC(at),
	}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id.String()}).
		Updates(row).Error
}

// SetSentAtByID sets only the sent time of a campaign recipient. The other
// columns are left as they are, so a change made by another writer while the
// email was sent, such as a recipient delete or an anonymize, is kept.
func (r *CampaignRecipient) SetSentAtByID(
	ctx context.Context,
	id *uuid.UUID,
	at time.Time,
) error {
	row := map[string]any{
		"sent_at": utils.RFC3339UTC(at),
	}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id.String()}).
		Updates(row).Error
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
	// the message id goes too, as it is in the delivered email and would tie the
	// mailbox copy back to this row.
	lureRow := map[string]interface{}{"lure_code": nil, "message_id": nil}
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

// updateCampaignRecipientsPaced sets fields on the campaign recipients that
// match find, at most ProgramWriteChunk rows per write with PauseAfterWrite
// between two writes. The rowids of a chunk are read first and the update
// then writes those rows by rowid, so the write holds the database only for
// the update and not for the search. The update repeats guard, so a row
// another writer changed between the read and the write is left as it is.
// guard must not read an index, see notIndexedEq, so the update finds the
// rows by rowid.
//
// Without keyset each read starts from the beginning of the matching rows, so
// the fields must make a row stop matching find, or the loop would find the
// same rows again. With keyset each read continues in rowid order after the
// last row of the chunk before, for fields that leave a row in the index range
// find reads, where a read from the beginning would pass every written row
// again. It must not be called inside a transaction or while the program
// lifecycle lock is held.
func (r *CampaignRecipient) updateCampaignRecipientsPaced(
	ctx context.Context,
	find clause.Expression,
	guard clause.Expression,
	fields map[string]any,
	keyset bool,
) error {
	rowidColumn := clause.Column{Name: "rowid"}
	var last int64
	for {
		q := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where(find).
			Limit(ProgramWriteChunk)
		if keyset {
			q = q.Where(clause.Gt{Column: rowidColumn, Value: last}).
				Order(clause.OrderByColumn{Column: rowidColumn})
		}
		var rowids []int64
		if err := q.Pluck("rowid", &rowids).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rowids) == 0 {
			return nil
		}
		last = rowids[len(rowids)-1]
		start := time.Now()
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where(clause.IN{Column: rowidColumn, Values: int64Values(rowids)}).
			Where(guard).
			Updates(fields)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if len(rowids) < ProgramWriteChunk {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// notIndexedEq matches a null column without reading an index on it, see
// notIndexed. A guard of a write by rowid uses it, so the rows are found by
// rowid and not through an index that holds many other rows.
func notIndexedEq(column string) clause.Expr {
	return clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: column})}}
}

// notIndexedNeq matches a column that is not null without reading an index
// on it, see notIndexedEq
func notIndexedNeq(column string) clause.Expr {
	return clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: column})}}
}

// int64Values returns the values as a list for an IN clause
func int64Values(values []int64) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// AnonymizeRowsOfCampaign anonymizes every campaign recipient row of a
// campaign with paced writes, without loading the rows. It runs in three
// passes:
//
//  1. a row with a person and no pseudonym gets a fresh random pseudonym, and
//     the events of that person in the campaign are moved onto it. An
//     anonymous campaign assigned a pseudonym at materialization, so it has no
//     such rows.
//  2. a row with a person has its lure code and message id released and its
//     person link removed.
//  3. a row without a person that still holds a lure code or a message id
//     has both released. An anonymous program send keeps its code and its
//     message id after its person link is cleared, so the report inbox can
//     still match it while the program is open, and a link cleared while
//     pass 2 runs is found here too.
//
// Each pass writes at most ProgramWriteChunk rows per write with a pause
// between writes. It must not be called inside a transaction or while the
// program lifecycle lock is held. Running it again finds nothing to do.
func (r *CampaignRecipient) AnonymizeRowsOfCampaign(ctx context.Context, campaignID *uuid.UUID) error {
	byCampaign := clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}
	linked := clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil}
	if err := r.assignMissingPseudonyms(ctx, campaignID); err != nil {
		return err
	}
	now := utils.NowRFC3339UTC()
	// a written row leaves the linked rows of the campaign, so each read
	// starts at the first row still linked
	if err := r.updateCampaignRecipientsPaced(ctx, clause.And(byCampaign, linked), notIndexedNeq("recipient_id"), map[string]any{
		"lure_code":    nil,
		"message_id":   nil,
		"recipient_id": nil,
		"updated_at":   now,
	}, false); err != nil {
		return err
	}
	if err := PauseBetweenWrites(ctx); err != nil {
		return err
	}
	// a written row stays among the unlinked rows of the campaign, which the
	// campaign and recipient index holds in rowid order, so the reads walk
	// that range once by rowid
	unlinkedWithCode := clause.And(
		clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: nil},
		clause.Or(
			clause.Neq{Column: clause.Column{Name: "lure_code"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "message_id"}, Value: nil},
		),
	)
	return r.updateCampaignRecipientsPaced(ctx, clause.And(byCampaign, unlinkedWithCode), clause.And(
		notIndexedEq("recipient_id"),
		clause.Or(notIndexedNeq("lure_code"), notIndexedNeq("message_id")),
	), map[string]any{
		"lure_code":  nil,
		"message_id": nil,
		"updated_at": now,
	}, true)
}

// pseudonymChunk is how many rows one transaction of assignMissingPseudonyms
// writes. Each row takes two writes, one for the row and one for its events,
// so a chunk is smaller than ProgramWriteChunk to keep the hold short.
const pseudonymChunk = 100

// assignMissingPseudonyms gives each row of a campaign that has a person and
// no pseudonym a fresh random one, and moves the events of that person in the
// campaign onto it with their identifying data replaced. The rows are walked
// in the order of the campaign and person index, at most pseudonymChunk rows
// at a time, so each read covers only its own part of the index. The ids of a
// chunk are read and then written in one transaction, with PauseAfterWrite
// between two chunks. A row whose pseudonym or person link was set by another
// writer in between is left as it is.
func (r *CampaignRecipient) assignMissingPseudonyms(ctx context.Context, campaignID *uuid.UUID) error {
	recipientColumn := clause.Column{Name: "recipient_id"}
	var after *uuid.UUID
	for {
		var rows []struct {
			ID          *uuid.UUID
			RecipientID *uuid.UUID
		}
		q := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select("id, recipient_id").
			Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
			Where(clause.Neq{Column: recipientColumn, Value: nil}).
			Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil})
		if after != nil {
			q = q.Where(clause.Gt{Column: recipientColumn, Value: after})
		}
		if err := q.Order(clause.OrderByColumn{Column: recipientColumn}).
			Limit(pseudonymChunk).
			Scan(&rows).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		after = rows[len(rows)-1].RecipientID
		start := time.Now()
		err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, row := range rows {
				now := utils.NowRFC3339UTC()
				pseudonym := uuid.New()
				res := tx.Model(&database.CampaignRecipient{}).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: row.ID}).
					Where(clause.Eq{Column: recipientColumn, Value: row.RecipientID}).
					Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
					Updates(map[string]any{"anonymized_id": pseudonym.String(), "updated_at": now})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					continue
				}
				// the events are found through the person index, the campaign
				// index would read every event of the campaign for each row
				if err := tx.Model(&database.CampaignEvent{}).
					Where(clause.Eq{Column: recipientColumn, Value: row.RecipientID}).
					Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), campaignID}}).
					Updates(movedEventFields(pseudonym, now)).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if len(rows) < pseudonymChunk {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// movedEventFields are the values of an event moved onto a pseudonym: no
// person, the pseudonym, and no submitted data, IP address, user agent or
// browser metadata, see scrubbedEventFields
func movedEventFields(pseudonym uuid.UUID, now string) map[string]any {
	fields := scrubbedEventFields()
	fields["recipient_id"] = nil
	fields["anonymized_id"] = pseudonym.String()
	fields["updated_at"] = now
	return fields
}

// CancelInActiveCampaigns cancels a recipient's rows that are neither sent nor
// cancelled in every active campaign. The hidden campaigns of a program are
// left out: the sender never sends to a disabled person, and the program
// membership pass cancels their sends once they are gone from the groups for
// longer than the grace period, so a person enabled again within it keeps
// their sends. A sent row is kept as it is. Both IS NULL predicates are
// written as the partial index idx_campaign_recipients_unsent_campaign states
// them, so the rows are found through it.
func (r *CampaignRecipient) CancelInActiveCampaigns(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	row := map[string]any{
		"cancelled_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	subSelect := r.DB.Table(database.CAMPAIGN_TABLE).Select("id").
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TABLE, Name: "program_id"}, Value: nil})
	subSelect = appendWhereCampaignIsActive(subSelect)

	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", subSelect).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil}).
		Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetOpenCampaignGroupsOfGroup returns the campaigns that a recipient group
// is linked to and that are not closed, scheduled ones included, each with
// the other groups linked to it. A hidden campaign of a program is never
// returned, as a program enrolls through its own groups.
func (r *CampaignRecipient) GetOpenCampaignGroupsOfGroup(
	ctx context.Context,
	groupID *uuid.UUID,
) (map[uuid.UUID][]*uuid.UUID, error) {
	openCampaigns := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.Campaign{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: nil})
	linked := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignRecipientGroup{}).
		Select("campaign_id").
		Where(clause.Eq{Column: clause.Column{Name: "recipient_group_id"}, Value: groupID})
	rows := []struct {
		CampaignID       uuid.UUID
		RecipientGroupID uuid.UUID
	}{}
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipientGroup{}).
		Select("campaign_id", "recipient_group_id").
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "campaign_id"}, linked}}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "campaign_id"}, openCampaigns}}).
		Scan(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := map[uuid.UUID][]*uuid.UUID{}
	for i := range rows {
		others := out[rows[i].CampaignID]
		if rows[i].RecipientGroupID != *groupID {
			others = append(others, &rows[i].RecipientGroupID)
		}
		out[rows[i].CampaignID] = others
	}
	return out, nil
}

// CancelUnsentOfMembersInCampaign cancels the rows of one campaign that are
// neither sent nor cancelled for every person that members selects, except a
// person that one of the keep queries selects. members and keep are queries
// of recipient ids such as RecipientGroup.MemberIDsQuery, so no id is bound
// as a value and any number of people fits. The rows are cancelled in paced
// chunks, see updateCampaignRecipientsPaced. It must not be called inside a
// transaction or while the program lifecycle lock is held.
func (r *CampaignRecipient) CancelUnsentOfMembersInCampaign(
	ctx context.Context,
	campaignID *uuid.UUID,
	members *gorm.DB,
	keep []*gorm.DB,
) error {
	recipientColumn := clause.Column{Name: "recipient_id"}
	where := []clause.Expression{
		clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID},
		clause.Expr{SQL: "? IN (?)", Vars: []any{recipientColumn, members}},
		clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil},
		clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil},
	}
	for _, k := range keep {
		where = append(where, clause.Expr{SQL: "? NOT IN (?)", Vars: []any{recipientColumn, k}})
	}
	now := utils.NowRFC3339UTC()
	// a cancelled row leaves the unsent rows, so each read starts at the
	// first row still unsent. A row sent or cancelled since the read is left.
	return r.updateCampaignRecipientsPaced(ctx, clause.And(where...), clause.And(
		notIndexedEq("sent_at"),
		notIndexedEq("cancelled_at"),
	), map[string]any{
		"cancelled_at": now,
		"updated_at":   now,
	}, false)
}

// CancelUnsentOfRecipientsInCampaign cancels the rows of one campaign that are
// neither sent nor cancelled for the given recipients, except a person that
// one of the keep queries selects, see CancelUnsentOfMembersInCampaign. The
// recipients are bound in batches to stay under the bind parameter limit. It
// must not be called inside a transaction or while the program lifecycle
// lock is held.
func (r *CampaignRecipient) CancelUnsentOfRecipientsInCampaign(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	keep []*gorm.DB,
) error {
	for start := 0; start < len(recipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(recipientIDs))
		members := r.DB.Session(&gorm.Session{NewDB: true}).
			Model(&database.Recipient{}).
			Select("id").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(recipientIDs[start:end])})
		if err := r.CancelUnsentOfMembersInCampaign(ctx, campaignID, members, keep); err != nil {
			return err
		}
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
	var templateID nullable.Nullable[uuid.UUID]
	templateID.SetNull()
	if row.CampaignTemplateID != nil {
		templateID = nullable.NewNullableWithValue(*row.CampaignTemplateID)
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
		Position:         position,
		Department:       department,
		NotableEventID:   notableEventID,
		NotableEventName: notableEventName,
		LureCode:         lureCode,
		LureCodeCustom:   nullable.NewNullableWithValue(row.LureCodeCustom),
		TemplateID:       templateID,
	}, nil
}

// SetMessageIDByID stores the Message-ID of the email sent to a campaign recipient
func (r *CampaignRecipient) SetMessageIDByID(
	ctx context.Context,
	id *uuid.UUID,
	messageID string,
) error {
	// only the first sent email's Message-ID is kept, a resend does not replace it
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("id = ?", id.String()).
		Where("message_id IS NULL").
		Update("message_id", messageID)
	return res.Error
}

// GetByMessageID gets the campaign recipient that was sent the email with the
// given Message-ID, without angle brackets
func (r *CampaignRecipient) GetByMessageID(
	ctx context.Context,
	messageID string,
) (*model.CampaignRecipient, error) {
	if messageID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	// a Message-ID shared by several recipients, for example a fixed one set by a
	// custom SMTP header, can not tell them apart, so it does not match
	var rows []database.CampaignRecipient
	res := r.DB.WithContext(ctx).
		Where("message_id = ?", messageID).
		Limit(2).
		Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	if len(rows) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return ToCampaignRecipient(&rows[0])
}

// TemplateAssignment is a campaign recipient's stored template and the fields
// templates can be mixed by. Sent is true once the recipient was sent an email
// or has any event.
type TemplateAssignment struct {
	ID         uuid.UUID
	TemplateID *uuid.UUID
	Department string
	Position   string
	City       string
	Country    string
	Misc       string
	Sent       bool
}

// GetTemplateAssignments returns every recipient of a campaign with its stored
// template, the fields templates can be mixed by (department, position, city,
// country, misc) and whether it was already sent to, in one query
func (r *CampaignRecipient) GetTemplateAssignments(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]TemplateAssignment, error) {
	var rows []TemplateAssignment
	res := r.DB.WithContext(ctx).
		Table("campaign_recipients AS cr").
		Select(`cr.id AS id, cr.campaign_template_id AS template_id,
			COALESCE(NULLIF(cr.department, ''), r.department, '') AS department,
			COALESCE(NULLIF(cr.position, ''), r.position, '') AS position,
			COALESCE(r.city, '') AS city,
			COALESCE(r.country, '') AS country,
			COALESCE(r.misc, '') AS misc,
			CASE WHEN cr.sent_at IS NOT NULL OR cr.recipient_id IN (
				SELECT e.recipient_id FROM campaign_events e
				WHERE e.campaign_id = ? AND e.event_id <> ? AND e.recipient_id IS NOT NULL
			) THEN 1 ELSE 0 END AS sent`, campaignID.String(), failedSendEventID()).
		Joins("LEFT JOIN recipients r ON r.id = cr.recipient_id").
		Where("cr.campaign_id = ?", campaignID.String()).
		Order("cr.id ASC").
		Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	return rows, nil
}

// templateMove is one recipient and its new template, sent to sqlite as json
type templateMove struct {
	ID         string `json:"i"`
	TemplateID string `json:"t"`
}

// setTemplateForIDsTx gives each listed recipient of the campaign the template
// it is keyed under. Only the template column is touched. The whole list goes
// in as one bound json value, so one update writes it all. With onlyUnsent a
// recipient already sent to or with any event other than a failed send is left
// alone; the events of the campaign are then read once for the whole list.
func setTemplateForIDsTx(
	tx *gorm.DB,
	campaignID *uuid.UUID,
	idsByTemplate map[uuid.UUID][]uuid.UUID,
	onlyUnsent bool,
) error {
	list := []templateMove{}
	for templateID, ids := range idsByTemplate {
		for _, id := range ids {
			list = append(list, templateMove{ID: id.String(), TemplateID: templateID.String()})
		}
	}
	if len(list) == 0 {
		return nil
	}
	payload, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// the unary plus keeps sqlite on the primary key for each listed id
	// instead of walking every row of the campaign or every unsent row
	if onlyUnsent {
		return tx.Exec(`UPDATE campaign_recipients
			SET campaign_template_id = j.value->>'t', updated_at = ?
			FROM json_each(?) AS j
			WHERE campaign_recipients.id = j.value->>'i'
			AND +campaign_recipients.campaign_id = ?
			AND +campaign_recipients.sent_at IS NULL
			AND (campaign_recipients.recipient_id IS NULL OR campaign_recipients.recipient_id NOT IN (
				SELECT e.recipient_id FROM campaign_events e
				WHERE e.campaign_id = ? AND e.event_id <> ? AND e.recipient_id IS NOT NULL
			))`,
			utils.NowRFC3339UTC(), string(payload), campaignID.String(),
			campaignID.String(), failedSendEventID(),
		).Error
	}
	return tx.Exec(`UPDATE campaign_recipients
		SET campaign_template_id = j.value->>'t', updated_at = ?
		FROM json_each(?) AS j
		WHERE campaign_recipients.id = j.value->>'i'
		AND +campaign_recipients.campaign_id = ?`,
		utils.NowRFC3339UTC(), string(payload), campaignID.String(),
	).Error
}

// failedSendEventID is the id of the failed send event. A failed send is not a
// delivery, so it does not keep a recipient on its template.
func failedSendEventID() string {
	if id := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED]; id != nil {
		return id.String()
	}
	return ""
}
