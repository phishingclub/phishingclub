package repository

import (
	"context"
	"fmt"
	"strings"
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

var allowedCampaignColumns = []string{
	TableColumn(database.CAMPAIGN_TABLE, "created_at"),
	TableColumn(database.CAMPAIGN_TABLE, "updated_at"),
	TableColumn(database.CAMPAIGN_TABLE, "close_at"),
	TableColumn(database.CAMPAIGN_TABLE, "closed_at"),
	TableColumn(database.CAMPAIGN_TABLE, "anonymize_at"),
	TableColumn(database.CAMPAIGN_TABLE, "anonymized_at"),
	TableColumn(database.CAMPAIGN_TABLE, "send_start_at"),
	TableColumn(database.CAMPAIGN_TABLE, "send_end_at"),
	TableColumn(database.CAMPAIGN_TABLE, "notable_event_id"),
	TableColumn(database.CAMPAIGN_TABLE, "name"),
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "name"),
}

var allowedCampaginEventColumns = []string{
	TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
	TableColumn(database.CAMPAIGN_EVENT_TABLE, "updated_at"),
	TableColumn(database.CAMPAIGN_EVENT_TABLE, "ip_address"),
	TableColumn(database.CAMPAIGN_EVENT_TABLE, "user_agent"),
	TableColumn(database.CAMPAIGN_EVENT_TABLE, "data"),
}

var allowedCampaginEventViewColumns = utils.MergeStringSlices(
	allowedCampaginEventColumns,
	[]string{
		TableColumn(database.RECIPIENT_TABLE, "email"),
		TableColumn(database.RECIPIENT_TABLE, "first_name"),
		TableColumn(database.RECIPIENT_TABLE, "last_name"),
		TableColumn(database.EVENT_TABLE, "name"),
	})

// CampaignOption is options for preloading
type CampaignOption struct {
	*vo.QueryArgs

	WithCompany             bool
	WithCampaignTemplate    bool
	WithRecipientGroups     bool
	WithRecipientGroupCount bool
	WithAllowDeny           bool
	WithDenyPage            bool
	WithEvasionPage         bool
	IncludeTestCampaigns    bool
}

// CampaignEventOption is options for preloading
type CampaignEventOption struct {
	*vo.QueryArgs
	// WithCampaign bool
	WithUser     bool
	EventTypeIDs []string
}

// Campaign is a Campaign repository
type Campaign struct {
	DB *gorm.DB
}

// applyTestCampaignFilter conditionally applies the is_test filter based on options
func (r *Campaign) applyTestCampaignFilter(db *gorm.DB, options *CampaignOption) *gorm.DB {
	if !options.IncludeTestCampaigns {
		db = db.Where("is_test = false")
	}
	return db
}

// load preloads the campaign repository
func (r *Campaign) load(db *gorm.DB, options *CampaignOption) *gorm.DB {
	if options.WithCompany {
		db = db.Preload("Company")
	}
	if options.WithCampaignTemplate {
		db = db.Joins(LeftJoinOn(
			database.CAMPAIGN_TABLE,
			"campaign_template_id",
			database.CAMPAIGN_TEMPLATE_TABLE,
			"id",
		))
	}
	if options.WithRecipientGroups {
		db = db.Preload("RecipientGroups")
	}
	if options.WithAllowDeny {
		db = db.Preload("AllowDeny")
	}
	if options.WithDenyPage {
		db = db.Preload("DenyPage")
	}
	if options.WithEvasionPage {
		db = db.Preload("EvasionPage")
	}
	return db
}

// preloadEventRecipient preloads the event user
func (r *Campaign) preloadEventRecipient(db *gorm.DB, options *CampaignEventOption) *gorm.DB {
	if options.WithUser {
		db = db.Preload("Recipient", func(db *gorm.DB) *gorm.DB {
			return db
		})
	}
	return db
}

// joinEvent joins the event table with the campaign event table
func (r *Campaign) joinEvent(db *gorm.DB) *gorm.DB {
	return db.Joins(LeftJoinOn(
		database.CAMPAIGN_EVENT_TABLE,
		"event_id",
		database.EVENT_TABLE,
		"id",
	))
}

// Insert inserts a new campaign
func (r *Campaign) Insert(
	ctx context.Context,
	campaign *model.Campaign,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := campaign.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.Model(&database.Campaign{}).Create(row)

	if res.Error != nil {
		return nil, res.Error
	}

	err := r.AddRecipientGroups(ctx, &id, campaign.RecipientGroupIDs.MustGet())
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if allowDeny, err := campaign.AllowDenyIDs.Get(); err == nil && len(allowDeny) > 0 {
		err = r.AddAllowDenyLists(ctx, &id, allowDeny)
		if err != nil {
			return nil, errs.Wrap(err)
		}
	}
	return &id, nil
}

// Add recipient groups to campaign
func (r *Campaign) AddRecipientGroups(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientGroupIDs []*uuid.UUID,
) error {
	batch := []database.CampaignRecipientGroup{}
	for _, id := range recipientGroupIDs {
		batch = append(batch, database.CampaignRecipientGroup{
			CampaignID:       campaignID,
			RecipientGroupID: id,
		})
	}
	res := r.DB.Create(&batch)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetAllDenyByCampaignID gets all deny lists by campaign id
// not paginated
func (r *Campaign) GetAllDenyByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]*model.AllowDeny, error) {
	allowDeny := []*model.AllowDeny{}
	var dbAllowDeny []database.AllowDeny
	res := r.DB.
		Model(&database.AllowDeny{}).
		Joins("LEFT JOIN campaign_allow_denies ON campaign_allow_denies.allow_deny_id = allow_denies.id").
		Where("campaign_id = ?", campaignID).
		Find(&dbAllowDeny)

	if res.Error != nil {
		return allowDeny, res.Error
	}
	for _, dbAllowDeny := range dbAllowDeny {
		allowDeny = append(allowDeny, ToAllowDeny(&dbAllowDeny))
	}
	return allowDeny, nil
}

// AddAllowDenyLists allow/block lists to campaign
func (r *Campaign) AddAllowDenyLists(
	ctx context.Context,
	campaignID *uuid.UUID,
	allowDenyIDs []*uuid.UUID,
) error {

	batch := []database.CampaignAllowDeny{}
	for _, id := range allowDenyIDs {
		batch = append(batch, database.CampaignAllowDeny{
			CampaignID:  campaignID,
			AllowDenyID: id,
		})
	}
	res := r.DB.Create(&batch)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetByWebhookID gets campaigns by webhook ID
// not paginated
func (r *Campaign) GetByWebhookID(
	ctx context.Context,
	webhookID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	res := r.DB.
		Where("webhook_id = ?", webhookID.String()).
		Find(&rows)

	if res.Error != nil {
		return models, res.Error
	}
	for _, row := range rows {
		c, err := ToCampaign(row)
		if err != nil {
			return models, errs.Wrap(err)
		}
		models = append(models, c)
	}
	return models, nil
}

// GetByTemplateID gets campaigns by template ID
// not paginated
func (r *Campaign) GetByAllowDenyID(
	ctx context.Context,
	allowDenyID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	db := r.DB.InnerJoins(
		LeftJoinOn(
			database.CAMPAIGN_TABLE,
			"id",
			database.CAMPAIGN_ALLOW_DENY_TABLE,
			"campaign_id",
		),
	)
	res := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(
					database.CAMPAIGN_ALLOW_DENY_TABLE,
					"allow_deny_id",
				),
			),
			allowDenyID.String(),
		).
		Find(&rows)

	if res.Error != nil {
		return models, res.Error
	}
	for _, row := range rows {
		c, err := ToCampaign(row)
		if err != nil {
			return models, errs.Wrap(err)
		}
		models = append(models, c)
	}
	return models, nil
}

// GetByTemplateIDs gets campaigns by template IDs
// not paginated
func (r *Campaign) GetByTemplateIDs(
	ctx context.Context,
	templateIDs []*uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	res := r.DB.
		Where(
			"campaign_template_id IN ?",
			UUIDsToStrings(templateIDs),
		).
		Find(&rows)

	if res.Error != nil {
		return models, res.Error
	}
	for _, row := range rows {
		c, err := ToCampaign(row)
		if err != nil {
			return models, errs.Wrap(err)
		}
		models = append(models, c)
	}
	return models, nil
}

// RemoveWebhookByCampaignIDs removes the webhook from campaigns by ids
func (r *Campaign) RemoveWebhookByCampaignIDs(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
) error {
	row := map[string]interface{}{}
	ids := UUIDsToStrings(campaignIDs)
	AddUpdatedAt(row)
	row["webhook_id"] = nil
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id IN ?", ids).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveDenyPageByCampaignIDs remove the deny page from the campaign IDs
func (r *Campaign) RemoveDenyPageByCampaignIDs(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
) error {
	row := map[string]interface{}{}
	ids := UUIDsToStrings(campaignIDs)
	AddUpdatedAt(row)
	row["deny_page_id"] = nil
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id IN ?", ids).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveDenyPageByDenyPageIDs removes deny page id from campaigns by page idsj
func (r *Campaign) RemoveDenyPageByDenyPageIDs(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
) error {
	row := map[string]interface{}{}
	ids := UUIDsToStrings(campaignIDs)
	AddUpdatedAt(row)
	row["deny_page_id"] = nil
	res := r.DB.
		Model(&database.Campaign{}).
		Where("deny_page_id IN ?", ids).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveAllowDenyListsByID removes allow/block lists from campaign by allow deny list id
func (r *Campaign) RemoveAllowDenyListsByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("allow_deny_id = ?", id).
		Delete(&database.CampaignAllowDeny{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveAllowDenyListsByCampaignID removes allow/block lists from campaign
func (r *Campaign) RemoveAllowDenyListsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where("campaign_id = ?", campaignID).
		Delete(&database.CampaignAllowDeny{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetRecipientGroupCount gets the count of recipient groups
func (r *Campaign) GetRecipientGroupCount(
	ctx context.Context,
	campaignID *uuid.UUID,
) (int, error) {
	var count int64
	res := r.DB.
		Model(&database.CampaignRecipientGroup{}).
		Where("campaign_id = ?", campaignID).
		Count(&count)

	if res.Error != nil {
		return 0, res.Error
	}
	return int(count), nil
}

// GetAllActive gets the active campaigns
func (r *Campaign) GetAllActive(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}

	if strings.Contains(options.QueryArgs.OrderBy, "send_start_at") {
		db = db.Order("send_start_at IS NULL DESC")
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	// Apply test campaign filter based on options
	db = r.applyTestCampaignFilter(db, options)

	var dbCampaigns []database.Campaign
	res := db.
		Where(
			"((send_start_at <= ? OR send_start_at IS NULL) AND closed_at IS NULL)",
			utils.NowRFC3339UTC(),
		).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TABLE,
		options.QueryArgs,
		allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetAllUpcoming gets the upcoming campaigns
func (r *Campaign) GetAllUpcoming(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	// Apply test campaign filter based on options
	db = r.applyTestCampaignFilter(db, options)

	var dbCampaigns []database.Campaign
	res := db.
		Where("((send_start_at > ?) AND closed_at IS NULL)", utils.NowRFC3339UTC()).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TABLE,
		options.QueryArgs,
		allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetAllFinished gets the finished campaigns
func (r *Campaign) GetAllFinished(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}
	if strings.Contains(options.QueryArgs.OrderBy, "send_start_at") {
		db = db.Order("send_start_at IS NULL DESC")
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	// Apply test campaign filter based on options
	db = r.applyTestCampaignFilter(db, options)

	var dbCampaigns []database.Campaign
	res := db.
		Where("closed_at IS NOT NULL").
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TABLE,
		options.QueryArgs,
		allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetEventsByCampaignID gets all campaign events by campaign id
func (r *Campaign) GetEventsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
	options *CampaignEventOption,
	since *time.Time,
) (*model.Result[model.CampaignEvent], error) {
	result := model.NewEmptyResult[model.CampaignEvent]()
	db := r.preloadEventRecipient(r.DB, options)
	db = r.joinEvent(db)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaginEventViewColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaignEvents []database.CampaignEvent
	db = db.
		Joins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"recipient_id",
			database.RECIPIENT_TABLE,
			"id",
		)).
		Where("campaign_id = ?", campaignID)

	if since != nil {
		db = db.Where(
			TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at")+" > ?",
			utils.RFC3339UTC(*since),
		)
	}

	if len(options.EventTypeIDs) > 0 {
		db = db.Where(
			TableColumn(database.CAMPAIGN_EVENT_TABLE, "event_id")+" IN ?",
			options.EventTypeIDs,
		)
	}

	res := db.Find(&dbCampaignEvents)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TABLE,
		options.QueryArgs,
		allowedCampaginEventViewColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaignEvent := range dbCampaignEvents {
		c, err := ToCampaignEvent(&dbCampaignEvent)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, c)
	}
	return result, nil
}

// GetCampaignCountByTemplateID gets the count of campaigns by template id
func (r *Campaign) GetCampaignCountByTemplateID(
	ctx context.Context,
	templateID *uuid.UUID,
) (int, error) {
	var count int64
	res := r.DB.
		Model(&database.Campaign{}).
		Where("campaign_template_id = ?", templateID).
		Count(&count)

	if res.Error != nil {
		return 0, res.Error
	}
	return int(count), nil
}

// GetResultStats gets the read, clicked and submitted data grouped per recipient
// or by anon id if anonymized data
func (r *Campaign) GetResultStats(
	ctx context.Context,
	campaignID *uuid.UUID,
) (*model.CampaignResultView, error) {
	stats := &model.CampaignResultView{}

	// get recipients count for campaign
	res := r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_recipients
        WHERE campaign_id = ?
        AND recipient_id IS NOT NULL
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_recipients
        WHERE campaign_id = ?
        AND anonymized_id IS NOT NULL
    ) as unique_ids
    `, campaignID, campaignID).Scan(&stats.Recipients)

	if res.Error != nil {
		return nil, res.Error
	}

	// get sent email count
	res = r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND recipient_id IS NOT NULL
        AND event_id = ?
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_events
        WHERE campaign_id = ? AND anonymized_id IS NOT NULL
        AND event_id = ?
    ) as unique_ids
`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT],
	).Scan(&stats.EmailsSent)

	if res.Error != nil {
		return nil, res.Error
	}

	// get unique tracking pixels loaded
	res = r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ?
        AND recipient_id IS NOT NULL
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ? AND anonymized_id IS NOT NULL
    ) as unique_ids
`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
	).Scan(&stats.TrackingPixelLoaded)

	if res.Error != nil {
		return nil, res.Error
	}

	// Get any phishing page loaded distinct by recipent and campaign
	res = r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id IN (?, ?, ?)
        AND recipient_id IS NOT NULL
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id IN (?, ?, ?)
        AND anonymized_id IS NOT NULL
    ) as unique_ids
`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
	).Scan(&stats.WebsiteLoaded)

	if res.Error != nil {
		return nil, res.Error
	}

	// Get unique submits
	res = r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ?
        AND recipient_id IS NOT NULL
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ?
        AND anonymized_id IS NOT NULL
    ) as unique_ids
`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
	).Scan(&stats.SubmittedData)

	if res.Error != nil {
		return nil, res.Error
	}

	// Get unique reported
	res = r.DB.Raw(`
    SELECT COUNT(*) FROM (
        SELECT DISTINCT recipient_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ?
        AND recipient_id IS NOT NULL
        UNION
        SELECT DISTINCT anonymized_id
        FROM campaign_events
        WHERE campaign_id = ?
        AND event_id = ?
        AND anonymized_id IS NOT NULL
    ) as unique_ids
`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
	).Scan(&stats.Reported)

	if res.Error != nil {
		return nil, res.Error
	}

	return stats, nil
}

// GetAll gets all campaigns with pagination
func (r *Campaign) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	if companyID == nil {
		db = whereCompanyIsNull(db, database.CAMPAIGN_TABLE)
	} else {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaigns []database.Campaign
	res := db.Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetAllCampaignWithinDates gets all campaigns that are active or scheduled within two dates, including the dates themself.
// if no company id is set, it retrieves all contexts
func (r *Campaign) GetAllCampaignWithinDates(
	ctx context.Context,
	companyID *uuid.UUID,
	startDate time.Time,
	endDate time.Time,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)

	// Handle company ID filter
	/*
		if companyID == nil {
			db = whereCompanyIsNull(db, database.CAMPAIGN_TABLE)
		} else {
			db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
		}
	*/
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}

	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}

	var dbCampaigns []database.Campaign

	// Apply test campaign filter based on options
	db = r.applyTestCampaignFilter(db, options)

	// Query campaigns that:
	// 1. Are self-managed (no send_start_at)
	// 2. Start within the date range
	res := db.Where(
		"(send_start_at IS NULL) OR "+ // self managed
			"(send_start_at BETWEEN ? AND ?)", // is within time
		utils.RFC3339UTC(startDate),
		utils.RFC3339UTC(endDate),
	).Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TABLE,
		options.QueryArgs,
		allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}

	return result, nil
}

// GetAllByCompanyID gets all campaigns with pagination by company id
func (r *Campaign) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaigns []database.Campaign
	res := db.Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetByID gets a campaign by id
func (r *Campaign) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *CampaignOption,
) (*model.Campaign, error) {
	db := r.load(r.DB, options)
	var dbCampaign database.Campaign
	res := db.
		Where("campaigns.id = ?", id.String()).
		First(&dbCampaign)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaign(&dbCampaign)
}

// GetNameByID gets a campaign name by id
func (r *Campaign) GetNameByID(
	ctx context.Context,
	id *uuid.UUID,
) (string, error) {
	var dbCampaign database.Campaign
	res := r.DB.
		Model(&database.Campaign{}).
		Select("name").
		Where("id = ?", id).
		First(&dbCampaign)

	if res.Error != nil {
		return "", res.Error
	}
	return dbCampaign.Name, nil
}

// GetByNameAndCompanyID gets a campaign by name and company id
func (r *Campaign) GetByNameAndCompanyID(
	ctx context.Context,
	name string,
	companyID *uuid.UUID,
	options *CampaignOption,
) (*model.Campaign, error) {
	db := r.load(r.DB, options)
	db = withCompanyIncludingNullContext(db, companyID, database.CAMPAIGN_TABLE)
	var dbCampaign database.Campaign
	res := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TABLE, "name"),
			),
			name,
		).
		First(&dbCampaign)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaign(&dbCampaign)
}

// GetWebhookIDByCampaignID gets a webhook id by campaign id
func (r *Campaign) GetWebhookIDByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) (*uuid.UUID, error) {
	var campaign database.Campaign
	res := r.DB.
		Model(&database.Campaign{}).
		Select("webhook_id").
		Where("id = ?", campaignID.String()).
		First(&campaign)

	if res.Error != nil {
		return nil, res.Error
	}
	return campaign.WebhookID, nil
}

// GetAllReadyToClose gets all campaigns that are ready to close
func (r *Campaign) GetAllReadyToClose(
	ctx context.Context,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaigns []database.Campaign
	res := db.
		Where("close_at <= ? AND closed_at IS NULL", utils.NowRFC3339UTC()).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetReadyToAnonymize gets all campaigns that are ready to be anonymized
func (r *Campaign) GetReadyToAnonymize(
	ctx context.Context,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaigns []database.Campaign
	res := db.
		Where("anonymize_at <= ? AND anonymized_at IS NULL", utils.NowRFC3339UTC()).
		Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// SaveEvent saves a campaign event
func (r *Campaign) SaveEvent(
	ctx context.Context,
	campaignEvent *model.CampaignEvent,
) error {
	row := map[string]any{
		"id":          campaignEvent.ID.String(),
		"event_id":    campaignEvent.EventID.String(),
		"campaign_id": campaignEvent.CampaignID.String(),
		"ip_address":  campaignEvent.IP.String(),
		"user_agent":  campaignEvent.UserAgent.String(),
		"data":        campaignEvent.Data.String(),
		"metadata":    campaignEvent.Metadata.String(),
	}
	if campaignEvent.RecipientID != nil {
		row["recipient_id"] = campaignEvent.RecipientID.String()
	}
	AddTimestamps(row)
	res := r.DB.Model(&database.CampaignEvent{}).Create(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// HasMessageReadEvent checks if a recipient has a MESSAGE_READ event for a campaign
// returns true if the recipient has already opened the email (or has a synthetic event)
func (r *Campaign) HasMessageReadEvent(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	messageReadEventID *uuid.UUID,
) (bool, error) {
	var count int64

	query := r.DB.Model(&database.CampaignEvent{}).
		Where("campaign_id = ? AND event_id = ?", campaignID, messageReadEventID)

	if recipientID != nil {
		query = query.Where("recipient_id = ?", recipientID)
	}

	res := query.Count(&count)
	if res.Error != nil {
		return false, res.Error
	}

	return count > 0, nil
}

// UpdateByID updates a campaign by id
// does not update the campaign recipient groups and campaign recipients
func (r *Campaign) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
) error {
	row := campaign.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row)

	if allowDeny, err := campaign.AllowDenyIDs.Get(); err == nil {
		denyLen := len(allowDeny)
		err = r.RemoveAllowDenyListsByCampaignID(ctx, id)
		if err != nil {
			return err
		}
		if denyLen > 0 {
			err = r.AddAllowDenyLists(ctx, id, allowDeny)
			if err != nil {
				return err
			}
		}
	}

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveCampaignRecipientGroups removes all recipient groups from a campaign
func (r *Campaign) RemoveCampaignRecipientGroups(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where("campaign_id = ?", campaignID).
		Delete(&database.CampaignRecipientGroup{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveCampaignRecipientGroupByGroupID removes a group from a campaign
func (r *Campaign) RemoveCampaignRecipientGroupByGroupID(
	ctx context.Context,
	recipientGroupID *uuid.UUID,
) error {
	res := r.DB.
		Where("recipient_group_id = ?", recipientGroupID).
		Delete(&database.CampaignRecipientGroup{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveCampaignTemplateIDFromCampaigns removes campaign template id from all
// campaign that use it.
func (r *Campaign) RemoveCampaignTemplateIDFromCampaigns(
	ctx context.Context,
	campaignTemplateID *uuid.UUID,
) error {
	row := map[string]interface{}{}
	AddUpdatedAt(row)
	row["campaign_template_id"] = nil
	res := r.DB.
		Model(&database.Campaign{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TABLE, "campaign_template_id"),
			),
			campaignTemplateID.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a campaign by id including its stats
func (r *Campaign) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", id).
		Delete(&database.Campaign{})

	if res.Error != nil {
		return res.Error
	}
	return r.DeleteCampaignStats(ctx, id)
}

// DeleteEventsByCampaignID deletes all events by campaign id
func (r *Campaign) DeleteEventsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where("campaign_id = ?", campaignID).
		Delete(&database.CampaignEvent{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// AddAnonymizedAt adds an anonymized at time to a campaign
func (r *Campaign) AddAnonymizedAt(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]interface{}{
		"anonymized_at": utils.NowRFC3339UTC(),
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// AnonymizeCampaignEvent anonymizes a campaign event
func (r *Campaign) AnonymizeCampaignEvent(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) error {
	row := map[string]any{
		"recipient_id":  nil,
		"anonymized_id": anonymizedID.String(),
		"user_agent":    "anonymized",
		"ip_address":    nil,
		"data":          "anonymized",
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignEvent{}).
		Where("campaign_id = ? AND recipient_id = ?", campaignID, recipientID).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// AnonymizeCampaignEventsByRecipientID anonymizes campaign events by recipient ID
func (r *Campaign) AnonymizeCampaignEventsByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) error {
	row := map[string]interface{}{
		"recipient_id":  nil,
		"anonymized_id": anonymizedID,
		"user_agent":    "anonymized",
		"ip_address":    nil,
		"data":          "anonymized",
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignEvent{}).
		Where("recipient_id = ?", recipientID).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetActiveCount get the number running campaigns
// if no company ID is selected it gets the global count including all companies
func (r *Campaign) GetActiveCount(ctx context.Context, companyID *uuid.UUID, includeTestCampaigns bool) (int64, error) {
	var c int64
	db := r.DB
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}

	whereClause := "((send_start_at <= ? OR send_start_at IS NULL) AND closed_at IS NULL)"
	if !includeTestCampaigns {
		whereClause += " AND is_test = false"
	}

	res := db.
		Model(&database.Campaign{}).
		Where(whereClause, utils.NowRFC3339UTC()).
		Count(&c)

	return c, res.Error
}

// GetUpcomingCount get the upcoming campaign count
// if no company ID is selected it gets the global count including all companies
func (r *Campaign) GetUpcomingCount(ctx context.Context, companyID *uuid.UUID, includeTestCampaigns bool) (int64, error) {
	var c int64
	db := r.DB
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}

	whereClause := "((send_start_at > ?) AND closed_at IS NULL)"
	if !includeTestCampaigns {
		whereClause += " AND is_test = false"
	}

	res := db.
		Model(&database.Campaign{}).
		Where(whereClause, utils.NowRFC3339UTC()).
		Count(&c)

	return c, res.Error
}

// GetFinishedCount get the finished campaign count
// if no company ID is selected it gets the global count including all companies
func (r *Campaign) GetFinishedCount(ctx context.Context, companyID *uuid.UUID, includeTestCampaigns bool) (int64, error) {
	var c int64
	db := r.DB
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}

	whereClause := "closed_at IS NOT NULL"
	if !includeTestCampaigns {
		whereClause += " AND is_test = false"
	}

	res := db.
		Model(&database.Campaign{}).
		Where(whereClause).
		Count(&c)

	return c, res.Error
}

func ToCampaign(row *database.Campaign) (*model.Campaign, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	var company *model.Company
	if row.Company != nil {
		company = ToCompany(row.Company)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	var closeAt nullable.Nullable[time.Time]
	closeAt.SetNull()
	if row.CloseAt != nil {
		closeAt = nullable.NewNullableWithValue(*row.CloseAt)
	}
	var closedAt nullable.Nullable[time.Time]
	closedAt.SetNull()
	if row.ClosedAt != nil {
		closedAt = nullable.NewNullableWithValue(*row.ClosedAt)
	}
	var sortField nullable.Nullable[vo.CampaignSortField]
	if row.SortField != "" {
		sf, err := vo.NewCampaignSortField(row.SortField)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		sortField = nullable.NewNullableWithValue(*sf)
	}
	var sortOrder nullable.Nullable[vo.CampaignSendingOrder]
	if row.SortOrder != "" {
		so, err := vo.NewCampaignSendingOrder(row.SortOrder)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		sortOrder = nullable.NewNullableWithValue(*so)
	}
	var sendStartAt nullable.Nullable[time.Time]
	if row.SendStartAt != nil {
		sendStartAt = nullable.NewNullableWithValue(*row.SendStartAt)
	} else {
		sendStartAt.SetNull()
	}
	var sendEndAt nullable.Nullable[time.Time]
	if row.SendEndAt != nil {
		sendEndAt = nullable.NewNullableWithValue(*row.SendEndAt)
	} else {
		sendEndAt.SetNull()
	}
	saveSubmittedData := nullable.NewNullableWithValue(row.SaveSubmittedData)
	saveBrowserMetadata := nullable.NewNullableWithValue(row.SaveBrowserMetadata)
	isAnonymous := nullable.NewNullableWithValue(row.IsAnonymous)
	isTest := nullable.NewNullableWithValue(row.IsTest)
	obfuscate := nullable.NewNullableWithValue(row.Obfuscate)
	webhookIncludeData := nullable.NewNullableWithValue(row.WebhookIncludeData)
	webhookEvents := nullable.NewNullableWithValue(row.WebhookEvents)
	var templateID nullable.Nullable[uuid.UUID]
	if row.CampaignTemplateID != nil {
		templateID = nullable.NewNullableWithValue(*row.CampaignTemplateID)
	}
	var template *model.CampaignTemplate
	if row.CampaignTemplate != nil {
		var err error
		template, err = ToCampaignTemplate(row.CampaignTemplate)
		if err != nil {
			return nil, errs.Wrap(err)
		}
	}
	recipientGroups := []*model.RecipientGroup{}
	recipientGroupIDs := []*uuid.UUID{}
	if row.RecipientGroups != nil {
		for _, rg := range row.RecipientGroups {
			r, err := ToRecipientGroup(rg)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			recipientGroups = append(recipientGroups, r)
			recipientGroupIDs = append(recipientGroupIDs, rg.ID)
		}
	}
	allowDeny := []*model.AllowDeny{}
	if row.AllowDeny != nil {
		for _, ad := range row.AllowDeny {
			allowDeny = append(allowDeny, ToAllowDeny(ad))
		}
	}
	var denyPage *model.Page
	if row.DenyPage != nil {
		dp, err := ToPage(row.DenyPage)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		denyPage = dp
	}
	denyPageID := nullable.NewNullNullable[uuid.UUID]()
	if row.DenyPageID != nil {
		denyPageID.Set(*row.DenyPageID)
	} else {
		denyPageID.SetNull()
	}

	var evasionPage *model.Page
	if row.EvasionPage != nil {
		ep, err := ToPage(row.EvasionPage)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		evasionPage = ep
	}
	evasionPageID := nullable.NewNullNullable[uuid.UUID]()
	if row.EvasionPageID != nil {
		evasionPageID.Set(*row.EvasionPageID)
	} else {
		evasionPageID.SetNull()
	}

	constraintWeekDays := nullable.NewNullNullable[vo.CampaignWeekDays]()
	if row.ConstraintWeekDays != nil {
		weekDays, err := vo.NewCampaignWeekDays(*row.ConstraintWeekDays)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		constraintWeekDays.Set(*weekDays)
	}
	constraintStartTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintStartTime != nil {
		t, err := vo.NewCampaignTimeConstraint(*row.ConstraintStartTime)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		constraintStartTime.Set(*t)
	}
	constraintEndTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintEndTime != nil {
		t, err := vo.NewCampaignTimeConstraint(*row.ConstraintEndTime)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		constraintEndTime.Set(*t)
	}
	webhookID := nullable.NewNullNullable[uuid.UUID]()
	if row.WebhookID != nil {
		webhookID.Set(*row.WebhookID)
	}
	anonymizeAt := nullable.NewNullNullable[time.Time]()
	if row.AnonymizeAt != nil {
		anonymizeAt.Set(*row.AnonymizeAt)
	}
	anonymizedAt := nullable.NewNullNullable[time.Time]()
	if row.AnonymizedAt != nil {
		anonymizedAt.Set(*row.AnonymizedAt)
	}

	var notableEventName string
	var notableEventID nullable.Nullable[uuid.UUID]
	notableEventID.SetNull()
	if row.NotableEventID != nil {
		notableEventID = nullable.NewNullableWithValue(*row.NotableEventID)
		notableEventName = cache.EventNameByID[row.NotableEventID.String()]
	}

	return &model.Campaign{
		ID:                  id,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		CompanyID:           companyID,
		Company:             company,
		Name:                name,
		CloseAt:             closeAt,
		ClosedAt:            closedAt,
		AnonymizeAt:         anonymizeAt,
		AnonymizedAt:        anonymizedAt,
		SortField:           sortField,
		SortOrder:           sortOrder,
		SendStartAt:         sendStartAt,
		SendEndAt:           sendEndAt,
		ConstraintWeekDays:  constraintWeekDays,
		ConstraintStartTime: constraintStartTime,
		ConstraintEndTime:   constraintEndTime,
		SaveSubmittedData:   saveSubmittedData,
		SaveBrowserMetadata: saveBrowserMetadata,
		IsAnonymous:         isAnonymous,
		IsTest:              isTest,
		Obfuscate:           obfuscate,
		WebhookIncludeData:  webhookIncludeData,
		WebhookEvents:       webhookEvents,
		TemplateID:          templateID,
		Template:            template,
		RecipientGroups:     recipientGroups,
		RecipientGroupIDs:   nullable.NewNullableWithValue(recipientGroupIDs),
		AllowDeny:           allowDeny,
		DenyPage:            denyPage,
		DenyPageID:          denyPageID,
		EvasionPage:         evasionPage,
		EvasionPageID:       evasionPageID,
		WebhookID:           webhookID,
		NotableEventID:      notableEventID,
		NotableEventName:    notableEventName,
	}, nil
}

func ToCampaignEvent(row *database.CampaignEvent) (*model.CampaignEvent, error) {
	var recipient *model.Recipient
	if row.Recipient != nil {
		r, err := ToRecipient(row.Recipient)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		recipient = r
	}
	ip := vo.NewOptionalString64Must(row.IPAddress)
	userAgent := vo.NewOptionalString255Must(row.UserAgent)
	data := vo.NewOptionalString1MBMust(row.Data)
	metadata := vo.NewOptionalString1MBMust(row.Metadata)

	return &model.CampaignEvent{
		ID:           row.ID,
		CreatedAt:    row.CreatedAt,
		CampaignID:   row.CampaignID,
		IP:           ip,
		UserAgent:    userAgent,
		Data:         data,
		Metadata:     metadata,
		AnonymizedID: row.AnonymizedID,
		RecipientID:  row.RecipientID,
		EventID:      row.EventID,
		Recipient:    recipient,
	}, nil
}

func ToRecipientCampaignEvent(row *database.RecipientCampaignEventView) (*model.RecipientCampaignEvent, error) {
	campaignEvent, err := ToCampaignEvent(&row.CampaignEvent)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &model.RecipientCampaignEvent{
		CampaignEvent: *campaignEvent,
		Name:          row.Name,
		CampaignName:  row.CampaignName,
	}, nil
}

func appendWhereCampaignIsActive(db *gorm.DB) *gorm.DB {
	return db.Where(
		fmt.Sprintf(
			"((%s <= ? OR %s IS NULL) AND %s IS NULL)",
			TableColumn(database.CAMPAIGN_TABLE, "send_start_at"),
			TableColumn(database.CAMPAIGN_TABLE, "send_start_at"),
			TableColumn(database.CAMPAIGN_TABLE, "closed_at"),
		),
		utils.NowRFC3339UTC(),
	)
}

// InsertCampaignStats inserts campaign statistics when a campaign is closed
func (r *Campaign) InsertCampaignStats(ctx context.Context, stats *database.CampaignStats) error {
	return r.DB.WithContext(ctx).Create(stats).Error
}

// GetCampaignStats retrieves campaign statistics by campaign ID
func (r *Campaign) GetCampaignStats(ctx context.Context, campaignID *uuid.UUID) (*database.CampaignStats, error) {
	var stats database.CampaignStats
	res := r.DB.WithContext(ctx).Where("campaign_id = ?", campaignID).First(&stats)
	if res.Error != nil {
		return nil, res.Error
	}
	return &stats, nil
}

// GetAllCampaignStats retrieves all campaign statistics
func (r *Campaign) GetAllCampaignStats(ctx context.Context, companyID *uuid.UUID) ([]database.CampaignStats, error) {
	var stats []database.CampaignStats

	db := r.DB.WithContext(ctx).Order("created_at DESC")

	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}

	res := db.Find(&stats)
	return stats, res.Error
}

// GetCampaignStatsCount returns the total count of campaign statistics
func (r *Campaign) GetCampaignStatsCount(ctx context.Context, companyID *uuid.UUID) (int64, error) {
	var count int64

	db := r.DB.WithContext(ctx).Model(&database.CampaignStats{})

	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}

	res := db.Count(&count)
	return count, res.Error
}

// DeleteCampaignStats deletes campaign statistics by campaign ID
func (r *Campaign) DeleteCampaignStats(ctx context.Context, campaignID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).Where("campaign_id = ?", campaignID).Delete(&database.CampaignStats{})
	return res.Error
}

// GetManualCampaignStats retrieves manual campaign statistics (those with null campaignID)
func (r *Campaign) GetManualCampaignStats(ctx context.Context, companyID *uuid.UUID) ([]database.CampaignStats, error) {
	var stats []database.CampaignStats

	db := r.DB.WithContext(ctx).Where("campaign_id IS NULL").Order("created_at DESC")

	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}

	res := db.Find(&stats)
	return stats, res.Error
}

// GetManualCampaignStatsCount returns the total count of manual campaign statistics
func (r *Campaign) GetManualCampaignStatsCount(ctx context.Context, companyID *uuid.UUID) (int64, error) {
	var count int64

	db := r.DB.WithContext(ctx).Model(&database.CampaignStats{}).Where("campaign_id IS NULL")

	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}

	res := db.Count(&count)
	return count, res.Error
}

// GetCampaignStatsByID retrieves campaign statistics by stats ID
func (r *Campaign) GetCampaignStatsByID(ctx context.Context, statsID *uuid.UUID) (*database.CampaignStats, error) {
	var stats database.CampaignStats
	res := r.DB.WithContext(ctx).Where("id = ?", statsID).First(&stats)
	if res.Error != nil {
		return nil, res.Error
	}
	return &stats, nil
}

// UpdateCampaignStats updates campaign statistics by ID
func (r *Campaign) UpdateCampaignStats(ctx context.Context, statsID *uuid.UUID, updateData map[string]interface{}) error {
	res := r.DB.WithContext(ctx).Model(&database.CampaignStats{}).Where("id = ?", statsID).Updates(updateData)
	return res.Error
}

// DeleteCampaignStatsByID deletes campaign statistics by stats ID
func (r *Campaign) DeleteCampaignStatsByID(ctx context.Context, statsID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).Where("id = ?", statsID).Delete(&database.CampaignStats{})
	return res.Error
}
