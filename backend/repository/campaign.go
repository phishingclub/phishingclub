package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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
	"gorm.io/gorm/clause"
)

var allowedCampaignColumns = []string{
	TableColumn(database.CAMPAIGN_TABLE, "created_at"),
	TableColumn(database.CAMPAIGN_TABLE, "updated_at"),
	TableColumn(database.CAMPAIGN_TABLE, "close_at"),
	TableColumn(database.CAMPAIGN_TABLE, "closed_at"),
	TableColumn(database.CAMPAIGN_TABLE, "anonymize_at"),
	TableColumn(database.CAMPAIGN_TABLE, "anonymized_at"),
	TableColumn(database.CAMPAIGN_TABLE, "data_anonymize_at"),
	TableColumn(database.CAMPAIGN_TABLE, "data_anonymized_at"),
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
	WithWebhooks            bool
	WithScripts             bool
	// WithLanguageRoutes loads the language routes, by GetByID only
	WithLanguageRoutes   bool
	IncludeTestCampaigns bool
	// IncludeProgramManaged includes the hidden campaigns a program creates.
	// Off by default so program campaigns never surface in the campaign UI.
	IncludeProgramManaged bool
}

// CampaignEventOption is options for preloading
type CampaignEventOption struct {
	*vo.QueryArgs
	WithCampaign bool
	WithUser     bool
	EventTypeIDs []string
	// RecipientID limits the events to one recipient when set
	RecipientID *uuid.UUID
	// CreatedFrom and CreatedTo, when set, limit the events to those created
	// in that time. A bound lets a read sorted by time start at it on the
	// created_at index
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// CampaignEventAllOption is options for preloading events across all campaigns
type CampaignEventAllOption struct {
	*vo.QueryArgs
	WithCampaign          bool
	WithUser              bool
	IncludeTestCampaigns  bool
	IncludeProgramManaged bool
	// ProgramEventTypeIDs, when set, adds the events of these types from the
	// hidden campaigns of programs that are not anonymous
	ProgramEventTypeIDs []string
	// EventTypeIDs, when set, keeps only these event types
	EventTypeIDs []string
}

// eventIDValues turns event type ids into IN clause values
func eventIDValues(ids []string) []any {
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id)
	}
	return values
}

// whereDashboardEvents limits campaign events joined with their campaign to the
// dashboard scope: one company or all of them, test campaigns when asked, and
// program events of the given types from programs that are not anonymous.
// With indexed false the company and program columns of the campaign are not
// read through their indexes, so a page sorted by time walks the created_at
// index.
func (r *Campaign) whereDashboardEvents(
	db *gorm.DB,
	companyID *uuid.UUID,
	options *CampaignEventAllOption,
	indexed bool,
) *gorm.DB {
	campaignCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	eventCol := clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "event_id"}
	if companyID != nil {
		var company any = campaignCol("company_id")
		if !indexed {
			company = notIndexed(campaignCol("company_id"))
		}
		db = db.Where(clause.Expr{SQL: "? = ?", Vars: []any{company, companyID}})
	}
	if !options.IncludeTestCampaigns {
		db = db.Where(clause.Eq{Column: campaignCol("is_test"), Value: false})
	}
	if len(options.EventTypeIDs) > 0 {
		db = db.Where(clause.IN{Column: eventCol, Values: eventIDValues(options.EventTypeIDs)})
	}
	switch {
	case options.IncludeProgramManaged:
	case len(options.ProgramEventTypeIDs) > 0:
		notAnonymousPrograms := r.DB.Session(&gorm.Session{NewDB: true}).
			Table(database.PROGRAM_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{{Table: database.PROGRAM_TABLE, Name: "id"}}}).
			Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "is_anonymous"}, Value: false}).
			Where(programNotAnonymizing(database.PROGRAM_TABLE))
		// with the program column read through its index in either branch,
		// SQLite reads the OR through two indexes, then every event of those
		// campaigns, which suits a count of one event type. A page sorted by
		// time would sort every event, while a walk of the created_at index
		// stops after one page.
		var program any = programColumn(database.CAMPAIGN_TABLE)
		if !indexed {
			program = notIndexed(programColumn(database.CAMPAIGN_TABLE))
		}
		db = db.Where(clause.Or(
			clause.Expr{SQL: "? IS NULL", Vars: []any{program}},
			clause.And(
				clause.Expr{SQL: "? IN (?)", Vars: []any{program, notAnonymousPrograms}},
				clause.IN{Column: eventCol, Values: eventIDValues(options.ProgramEventTypeIDs)},
			),
		))
	default:
		// hide events belonging to a program's hidden campaigns
		db = db.Where(clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: nil})
	}
	return db
}

// dashboardEventsSortLimit is the most events the dashboard scope may hold for
// a page of them sorted by time to be read campaign by campaign and sorted.
// Sorting this many rows takes a few milliseconds. A scope with more events
// walks the created_at index instead, which stops after one page and is fast
// when the scope has many of the events of its time.
var dashboardEventsSortLimit = 20000

// dashboardCampaigns limits campaign events to the campaigns of the company,
// or of every company when companyID is nil. The scope is already kept by the
// join with the campaign, this only lets SQLite read the events campaign by
// campaign through the campaign and event index.
func (r *Campaign) dashboardCampaigns(companyID *uuid.UUID) clause.Expr {
	campaigns := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.CAMPAIGN_TABLE, Name: "id"}}})
	if companyID != nil {
		campaigns = campaigns.Where(clause.Eq{Column: companyColumn(database.CAMPAIGN_TABLE), Value: companyID})
	}
	return clause.Expr{SQL: "? IN (?)", Vars: []any{
		clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
		campaigns,
	}}
}

// dashboardHasFewEvents reports whether the dashboard scope holds at most
// dashboardEventsSortLimit events. The count reads the events campaign by
// campaign and stops after the limit, so it stays cheap for a large scope.
func (r *Campaign) dashboardHasFewEvents(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignEventAllOption,
) (bool, error) {
	capped := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignEvent{}).
		Select("1").
		Joins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"campaign_id",
			database.CAMPAIGN_TABLE,
			"id",
		)).
		Where(r.dashboardCampaigns(companyID)).
		Limit(dashboardEventsSortLimit + 1)
	capped = r.whereDashboardEvents(capped, companyID, options, true)
	var count int64
	if err := r.DB.WithContext(ctx).Table("(?) AS capped", capped).Count(&count).Error; err != nil {
		return false, errs.Wrap(err)
	}
	return count <= int64(dashboardEventsSortLimit), nil
}

// CountDashboardEvents counts the campaign events created since the given
// time in the dashboard scope
func (r *Campaign) CountDashboardEvents(
	ctx context.Context,
	companyID *uuid.UUID,
	since time.Time,
	options *CampaignEventAllOption,
) (int64, error) {
	db := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Joins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"campaign_id",
			database.CAMPAIGN_TABLE,
			"id",
		)).
		Where(clause.Gte{
			Column: clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "created_at"},
			Value:  utils.RFC3339UTC(since),
		})
	db = r.whereDashboardEvents(db, companyID, options, true)
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return 0, errs.Wrap(err)
	}
	return count, nil
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
	// hidden campaigns a program manages must never surface in a campaign listing
	if !options.IncludeProgramManaged {
		db = db.Where(clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: nil})
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
	// WithWebhooks is handled post-query via GetCampaignWebhooks
	// because the junction table stores extra columns (include_data, events)
	// that GORM many2many preload cannot populate into model.CampaignWebhook
	return db
}

// preloadEventRecipient preloads the event user
func (r *Campaign) preloadEventRecipient(db *gorm.DB, options *CampaignEventOption) *gorm.DB {
	if options.WithUser {
		db = db.Preload("Recipient", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "first_name", "last_name", "email")
		})
	}
	if options.WithCampaign {
		db = db.Preload("Campaign", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "name", "company_id").Preload("Company")
		})
	}
	return db
}

// preloadAllEventRecipient preloads the event user for all events queries
func (r *Campaign) preloadAllEventRecipient(db *gorm.DB, options *CampaignEventAllOption) *gorm.DB {
	if options.WithUser {
		db = db.Preload("Recipient", func(db *gorm.DB) *gorm.DB {
			return db
		})
	}
	if options.WithCampaign {
		db = db.Preload("Campaign.Company")
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

	// the campaign row and its template pool are written together
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.Campaign{}).Create(row).Error; err != nil {
			return err
		}
		if routes, err := campaign.LanguageRoutes.Get(); err == nil && len(routes) > 0 {
			if err := replaceCampaignLanguageRoutesTx(tx, &id, routes); err != nil {
				return err
			}
		}
		if pool := campaign.TemplatePool(); len(pool) > 0 {
			return setTemplatePoolTx(tx, &id, pool)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = r.AddRecipientGroups(ctx, &id, campaign.RecipientGroupIDs.MustGet())
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

// AddWebhooks adds webhooks to campaign with per-webhook configuration
func (r *Campaign) AddWebhooks(
	ctx context.Context,
	campaignID *uuid.UUID,
	webhooks []*model.CampaignWebhook,
) error {
	batch := []database.CampaignWebhook{}
	// deduplicate by webhook id to prevent unique constraint violations
	seen := map[string]struct{}{}
	for _, wh := range webhooks {
		webhookID := wh.WebhookID.MustGet()
		key := webhookID.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		batch = append(batch, database.CampaignWebhook{
			CampaignID:         campaignID,
			WebhookID:          &webhookID,
			WebhookIncludeData: wh.GetWebhookIncludeDataOrDefault(),
			WebhookEvents:      wh.GetWebhookEventsOrDefault(),
		})
	}
	if len(batch) == 0 {
		return nil
	}
	res := r.DB.Create(&batch)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveWebhooksByCampaignID removes all webhooks from a campaign
func (r *Campaign) RemoveWebhooksByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where("campaign_id = ?", campaignID).
		Delete(&database.CampaignWebhook{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetCampaignWebhooks fetches webhook configurations for a campaign from junction table
func (r *Campaign) GetCampaignWebhooks(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]*model.CampaignWebhook, error) {
	var rows []database.CampaignWebhook
	res := r.DB.
		Where("campaign_id = ?", campaignID.String()).
		Find(&rows)

	if res.Error != nil {
		return nil, res.Error
	}

	webhooks := []*model.CampaignWebhook{}
	for _, row := range rows {
		if row.WebhookID == nil {
			continue
		}
		wh := &model.CampaignWebhook{
			WebhookID:          nullable.NewNullableWithValue(*row.WebhookID),
			WebhookIncludeData: nullable.NewNullableWithValue(row.WebhookIncludeData),
			WebhookEvents:      nullable.NewNullableWithValue(row.WebhookEvents),
		}
		webhooks = append(webhooks, wh)
	}

	return webhooks, nil
}

// GetByWebhookID gets campaigns by webhook ID
// not paginated
func (r *Campaign) GetByWebhookID(
	ctx context.Context,
	webhookID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}

	// use a map to deduplicate campaign IDs collected from both the junction
	// table and the legacy webhook_id column - after migration a campaign will
	// appear in both sources, so deduplication is required before the final query
	seen := map[string]struct{}{}

	// get from junction table
	var junctionRows []database.CampaignWebhook
	r.DB.Where("webhook_id = ?", webhookID.String()).Find(&junctionRows)
	for _, row := range junctionRows {
		if row.CampaignID != nil {
			seen[row.CampaignID.String()] = struct{}{}
		}
	}

	// get from old webhook_id field (backward compatibility)
	var oldRows []*database.Campaign
	r.DB.Where("webhook_id = ?", webhookID.String()).Find(&oldRows)
	for _, row := range oldRows {
		if row.ID != nil {
			seen[row.ID.String()] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return models, nil
	}

	campaignIDs := make([]string, 0, len(seen))
	for id := range seen {
		campaignIDs = append(campaignIDs, id)
	}

	res := r.DB.
		Where("id IN ?", campaignIDs).
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

// GetByDenyPageID gets the campaigns that use a page as their deny page
// not paginated
func (r *Campaign) GetByDenyPageID(
	ctx context.Context,
	pageID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	res := r.DB.
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_TABLE, "deny_page_id")),
			pageID.String(),
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

// GetByEvasionPageID gets the campaigns that use a page as their evasion page
// not paginated
func (r *Campaign) GetByEvasionPageID(
	ctx context.Context,
	pageID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TABLE, Name: "evasion_page_id"}, Value: pageID}).
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

// GetByScriptID gets the campaigns a script is attached to
// not paginated
func (r *Campaign) GetByScriptID(
	ctx context.Context,
	scriptID *uuid.UUID,
) ([]*model.Campaign, error) {
	rows := []*database.Campaign{}
	models := []*model.Campaign{}
	res := r.DB.
		InnerJoins(
			LeftJoinOn(
				database.CAMPAIGN_TABLE,
				"id",
				database.CAMPAIGN_SCRIPT_TABLE,
				"campaign_id",
			),
		).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_SCRIPT_TABLE, "script_id")),
			scriptID.String(),
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
	if len(templateIDs) == 0 {
		return models, nil
	}
	res := r.whereUsesTemplates(r.DB, UUIDsToStrings(templateIDs)).
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

// RemoveWebhookFromJunctionByWebhookID removes all campaign_webhooks rows for a given webhook id
// must be called before deleting a webhook to avoid orphaned junction rows
func (r *Campaign) RemoveWebhookFromJunctionByWebhookID(
	ctx context.Context,
	webhookID *uuid.UUID,
) error {
	res := r.DB.
		Where("webhook_id = ?", webhookID.String()).
		Delete(&database.CampaignWebhook{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// AddScripts adds scripts to a campaign with per script configuration
func (r *Campaign) AddScripts(
	ctx context.Context,
	campaignID *uuid.UUID,
	scripts []*model.CampaignScript,
) error {
	batch := []database.CampaignScript{}
	// deduplicate by script id to prevent unique constraint violations
	seen := map[string]struct{}{}
	for _, ca := range scripts {
		scriptID := ca.ScriptID.MustGet()
		key := scriptID.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		batch = append(batch, database.CampaignScript{
			CampaignID:        campaignID,
			ScriptID:          &scriptID,
			ScriptIncludeData: ca.GetScriptIncludeDataOrDefault(),
			ScriptEvents:      ca.GetScriptEventsOrDefault(),
		})
	}
	if len(batch) == 0 {
		return nil
	}
	res := r.DB.Create(&batch)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveScriptsByCampaignID removes all scripts from a campaign
func (r *Campaign) RemoveScriptsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.
		Where("campaign_id = ?", campaignID).
		Delete(&database.CampaignScript{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetCampaignScripts fetches script configurations for a campaign from the junction table
func (r *Campaign) GetCampaignScripts(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]*model.CampaignScript, error) {
	var rows []database.CampaignScript
	res := r.DB.
		Where("campaign_id = ?", campaignID.String()).
		Find(&rows)

	if res.Error != nil {
		return nil, res.Error
	}

	scripts := []*model.CampaignScript{}
	for _, row := range rows {
		if row.ScriptID == nil {
			continue
		}
		ca := &model.CampaignScript{
			ScriptID:          nullable.NewNullableWithValue(*row.ScriptID),
			ScriptIncludeData: nullable.NewNullableWithValue(row.ScriptIncludeData),
			ScriptEvents:      nullable.NewNullableWithValue(row.ScriptEvents),
		}
		scripts = append(scripts, ca)
	}

	return scripts, nil
}

// RemoveScriptFromJunctionByScriptID removes all campaign_scripts rows for a given script id
// must be called before deleting a script to avoid orphaned junction rows
func (r *Campaign) RemoveScriptFromJunctionByScriptID(
	ctx context.Context,
	scriptID *uuid.UUID,
) error {
	res := r.DB.
		Where("script_id = ?", scriptID.String()).
		Delete(&database.CampaignScript{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveWebhookByCampaignIDs removes the webhook from campaigns by ids
func (r *Campaign) RemoveWebhookByCampaignIDs(
	ctx context.Context,
	campaignIDs []*uuid.UUID,
) error {
	if len(campaignIDs) == 0 {
		return nil
	}
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

// GetOpenNamesByFilterPage returns the names of at most limit campaigns that
// are not closed and use a page as deny or evasion page, sorted by name, and
// how many such campaigns there are. The hidden campaigns of a program are
// left out, the program is named instead.
func (r *Campaign) GetOpenNamesByFilterPage(
	ctx context.Context,
	pageID *uuid.UUID,
	limit int,
) ([]string, int64, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	q := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: col("closed_at"), Value: nil}).
		Where(clause.Eq{Column: col("program_id"), Value: nil}).
		Where(clause.Or(
			clause.Eq{Column: col("deny_page_id"), Value: pageID},
			clause.Eq{Column: col("evasion_page_id"), Value: pageID},
		))
	var count int64
	if err := q.Session(&gorm.Session{}).Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	names := []string{}
	if count == 0 {
		return names, 0, nil
	}
	if err := q.Session(&gorm.Session{}).
		Order(clause.OrderByColumn{Column: col("name")}).
		Limit(limit).
		Pluck("name", &names).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return names, count, nil
}

// RemovePageFromFilters clears a page that is being deleted from every closed
// campaign. A campaign that used it as deny page also loses its evasion page
// and its allow or deny lists, as both need a deny page. A campaign that used
// it as evasion page loses only the evasion page. A campaign that is not
// closed keeps the page, the delete is refused while one uses it, see
// GetOpenNamesByFilterPage. The hidden campaigns of a program are cleared as
// closed ones.
func (r *Campaign) RemovePageFromFilters(
	ctx context.Context,
	pageID *uuid.UUID,
) error {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	// a hidden campaign follows its program, which refuses the delete while
	// it is open and uses the page
	closed := clause.Or(
		clause.Neq{Column: col("closed_at"), Value: nil},
		clause.Neq{Column: col("program_id"), Value: nil},
	)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		usingAsDenyPage := tx.Session(&gorm.Session{NewDB: true}).
			Table(database.CAMPAIGN_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{col("id")}}).
			Where(clause.Eq{Column: col("deny_page_id"), Value: pageID}).
			Where(closed)
		if err := tx.
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{
				clause.Column{Table: database.CAMPAIGN_ALLOW_DENY_TABLE, Name: "campaign_id"},
				usingAsDenyPage,
			}}).
			Delete(&database.CampaignAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		row := map[string]any{"deny_page_id": nil, "evasion_page_id": nil}
		AddUpdatedAt(row)
		if err := tx.Model(&database.Campaign{}).
			Where(clause.Eq{Column: col("deny_page_id"), Value: pageID}).
			Where(closed).
			Updates(row).Error; err != nil {
			return errs.Wrap(err)
		}
		row = map[string]any{"evasion_page_id": nil}
		AddUpdatedAt(row)
		if err := tx.Model(&database.Campaign{}).
			Where(clause.Eq{Column: col("evasion_page_id"), Value: pageID}).
			Where(closed).
			Updates(row).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
}

// RemoveAllowDenyFromFilters removes an allow or deny list that is being
// deleted from every campaign. A campaign left with no list and no evasion
// page also loses its deny page, as nothing uses it anymore. A campaign that
// keeps another list or an evasion page keeps its deny page, both need it.
func (r *Campaign) RemoveAllowDenyFromFilters(
	ctx context.Context,
	allowDenyID *uuid.UUID,
) error {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	listCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_ALLOW_DENY_TABLE, Name: name}
	}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		newDB := tx.Session(&gorm.Session{NewDB: true})
		holders := newDB.Table(database.CAMPAIGN_ALLOW_DENY_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{listCol("campaign_id")}}).
			Where(clause.Eq{Column: listCol("allow_deny_id"), Value: allowDenyID})
		otherHolders := newDB.Table(database.CAMPAIGN_ALLOW_DENY_TABLE).
			Clauses(clause.Select{Columns: []clause.Column{listCol("campaign_id")}}).
			Where(clause.Neq{Column: listCol("allow_deny_id"), Value: allowDenyID})
		row := map[string]any{"deny_page_id": nil}
		AddUpdatedAt(row)
		if err := tx.Model(&database.Campaign{}).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{col("id"), holders}}).
			Where(clause.Expr{SQL: "? NOT IN (?)", Vars: []any{col("id"), otherHolders}}).
			Where(clause.Eq{Column: col("evasion_page_id"), Value: nil}).
			Updates(row).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.
			Where(clause.Eq{Column: listCol("allow_deny_id"), Value: allowDenyID}).
			Delete(&database.CampaignAllowDeny{}).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
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

	// open campaigns that started, are self managed or are paused. A campaign
	// paused before its delivery started is listed here as Paused, not as
	// scheduled, the same bucket as the dashboard Paused tile.
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	var dbCampaigns []database.Campaign
	res := withPeek(db.
		Where(clause.Eq{Column: col("closed_at"), Value: nil}).
		Where(clause.Or(
			clause.Lte{Column: col("send_start_at"), Value: utils.NowRFC3339UTC()},
			clause.Eq{Column: col("send_start_at"), Value: nil},
			clause.Neq{Column: col("paused_at"), Value: nil},
		)), options.QueryArgs).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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

	// a paused campaign is listed with the active campaigns as Paused
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	var dbCampaigns []database.Campaign
	res := withPeek(db.
		Where(clause.Gt{Column: col("send_start_at"), Value: utils.NowRFC3339UTC()}).
		Where(clause.Eq{Column: col("closed_at"), Value: nil}).
		Where(clause.Eq{Column: col("paused_at"), Value: nil}), options.QueryArgs).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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
	res := withPeek(db.
		Where("closed_at IS NOT NULL"), options.QueryArgs).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetAllEvents gets one page of campaign events in the dashboard scope. A
// page sorted by time is read the cheaper of two ways, see
// dashboardEventsSortLimit, and both give the same rows in the same order.
func (r *Campaign) GetAllEvents(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignEventAllOption,
) (*model.Result[model.CampaignEvent], error) {
	result := model.NewEmptyResult[model.CampaignEvent]()
	sortedByTime := eventsSortedByCreatedAt(options.QueryArgs)
	// walk the created_at index newest first and stop after one page, unless
	// a company or an event type keeps few enough events to read them all
	// campaign by campaign and sort them, since the walk would pass every
	// event of everything else to fill the page
	walk := sortedByTime
	if sortedByTime && (companyID != nil || len(options.EventTypeIDs) > 0) {
		few, err := r.dashboardHasFewEvents(ctx, companyID, options)
		if err != nil {
			return result, err
		}
		walk = !few
	}
	db := r.preloadAllEventRecipient(r.DB.WithContext(ctx), options)
	db = r.joinEvent(db)
	queryArgs := options.QueryArgs
	if sortedByTime {
		// sorted without case folding, so a page walks the created_at index
		// in order and stops, see getEventsInScope
		q := *queryArgs
		q.OrderBy = ""
		queryArgs = &q
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, queryArgs, allowedCampaginEventViewColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if sortedByTime {
		db = db.Clauses(eventTimeOrder(options.QueryArgs.Desc, walk))
		if !walk {
			db = db.Where(r.dashboardCampaigns(companyID))
		}
	}
	var dbCampaignEvents []database.CampaignEvent
	db = db.
		Joins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"recipient_id",
			database.RECIPIENT_TABLE,
			"id",
		)).
		Joins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"campaign_id",
			database.CAMPAIGN_TABLE,
			"id",
		))

	db = r.whereDashboardEvents(db, companyID, options, !walk)

	res := withPeek(db, options.QueryArgs).Find(&dbCampaignEvents)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaignEvents, options.QueryArgs)

	for _, dbCampaignEvent := range dbCampaignEvents {
		c, err := ToCampaignEvent(&dbCampaignEvent)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, c)
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
	scope := clause.Eq{
		Column: clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
		Value:  campaignID,
	}
	return r.getEventsInScope(ctx, scope, options, since, true)
}

// eventTimeOrder sorts campaign events by time and then by row id, so events
// of the same time keep the order a walk of the created_at index gives. With
// indexed false the time is not read through its index, so SQLite sorts the
// rows the other predicates found.
func eventTimeOrder(desc, indexed bool) clause.OrderBy {
	createdAt := clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "created_at"}
	rowID := clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "rowid"}
	if indexed {
		return clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: createdAt, Desc: desc},
			{Column: rowID, Desc: desc},
		}}
	}
	if desc {
		return clause.OrderBy{Expression: clause.Expr{SQL: "+? DESC, ? DESC", Vars: []any{createdAt, rowID}}}
	}
	return clause.OrderBy{Expression: clause.Expr{SQL: "+? ASC, ? ASC", Vars: []any{createdAt, rowID}}}
}

// campaignEventJoins joins the event type and the recipient to a campaign
// event, the columns the event view sorts and searches on
func campaignEventJoins() clause.From {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	return clause.From{Joins: []clause.Join{
		{
			Type:  clause.LeftJoin,
			Table: clause.Table{Name: database.EVENT_TABLE},
			ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
				Column: eventCol("event_id"),
				Value:  clause.Column{Table: database.EVENT_TABLE, Name: "id"},
			}}},
		},
		{
			Type:  clause.LeftJoin,
			Table: clause.Table{Name: database.RECIPIENT_TABLE},
			ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
				Column: eventCol("recipient_id"),
				Value:  clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
			}}},
		},
	}}
}

// getEventsInScope reads one page of campaign events limited by scope, sorted
// and searched through the campaign event view allowlist. since keeps only
// events created after it, EventTypeIDs keeps only those event types. Events
// sorted by time are sorted by row id next, the order a walk of the
// created_at index gives. With timeIndexed false the created_at column is not
// read through its index, neither for the sort nor for the time bounds, so
// the scope picks the index.
func (r *Campaign) getEventsInScope(
	ctx context.Context,
	scope clause.Expression,
	options *CampaignEventOption,
	since *time.Time,
	timeIndexed bool,
) (*model.Result[model.CampaignEvent], error) {
	result := model.NewEmptyResult[model.CampaignEvent]()
	db := r.preloadEventRecipient(r.DB.WithContext(ctx), options)
	db = db.Model(&database.CampaignEvent{}).Clauses(campaignEventJoins())
	queryArgs := options.QueryArgs
	if eventsSortedByCreatedAt(queryArgs) {
		// a time sorts the same with or without case folding, and without it
		// SQLite can read the created_at index in order instead of sorting
		q := *queryArgs
		q.OrderBy = ""
		queryArgs = &q
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, queryArgs, allowedCampaginEventViewColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	createdAt := clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "created_at"}
	if queryArgs != options.QueryArgs {
		db = db.Clauses(eventTimeOrder(options.QueryArgs.Desc, timeIndexed))
	}
	// the time bounds as the sort reads the created_at column
	var created any = createdAt
	if !timeIndexed {
		created = notIndexed(createdAt)
	}
	db = db.Where(scope)
	if since != nil {
		db = db.Where(clause.Expr{SQL: "? > ?", Vars: []any{created, utils.RFC3339UTC(*since)}})
	}
	if options.CreatedFrom != nil {
		db = db.Where(clause.Expr{SQL: "? >= ?", Vars: []any{created, utils.RFC3339UTC(*options.CreatedFrom)}})
	}
	if options.CreatedTo != nil {
		db = db.Where(clause.Expr{SQL: "? <= ?", Vars: []any{created, utils.RFC3339UTC(*options.CreatedTo)}})
	}
	if options.RecipientID != nil {
		db = db.Where(clause.Eq{
			Column: clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "recipient_id"},
			Value:  options.RecipientID,
		})
	}
	if len(options.EventTypeIDs) > 0 {
		values := make([]any, 0, len(options.EventTypeIDs))
		for _, id := range options.EventTypeIDs {
			values = append(values, id)
		}
		db = db.Where(clause.IN{
			Column: clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "event_id"},
			Values: values,
		})
	}
	var dbCampaignEvents []database.CampaignEvent
	if err := withPeek(db, options.QueryArgs).Find(&dbCampaignEvents).Error; err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = trimPeek(&dbCampaignEvents, options.QueryArgs)
	for _, dbCampaignEvent := range dbCampaignEvents {
		c, err := ToCampaignEvent(&dbCampaignEvent)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, c)
	}
	return result, nil
}

// CampaignEventExportRow is one event of a campaign as the company export
// writes it. The text fields have their outer spaces removed, as the event
// view gives them. The recipient fields are empty when the event names nobody
// or the recipient is gone, HasRecipient tells which.
type CampaignEventExportRow struct {
	CreatedAt    time.Time
	EventID      uuid.UUID
	Data         string
	UserAgent    string
	IP           string
	HasRecipient bool
	FirstName    string
	LastName     string
	Email        string
}

// EachCampaignEventForExport calls fn with every event of a campaign with its
// recipient, newest first when desc is set and oldest first otherwise, read
// one row at a time. The events are read through the campaign index and
// sorted, so only the events of the campaign are read. Events of the same
// time are sorted by row id in the same direction, the order a walk of the
// created_at index gives. The row passed to fn is reused, fn must copy what
// it keeps.
func (r *Campaign) EachCampaignEventForExport(
	ctx context.Context,
	campaignID *uuid.UUID,
	desc bool,
	fn func(*CampaignEventExportRow) error,
) error {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	recipientCol := func(name string) clause.Column {
		return clause.Column{Table: database.RECIPIENT_TABLE, Name: name}
	}
	rows, err := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Clauses(
			clause.From{Joins: []clause.Join{{
				Type:  clause.LeftJoin,
				Table: clause.Table{Name: database.RECIPIENT_TABLE},
				ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
					Column: eventCol("recipient_id"),
					Value:  recipientCol("id"),
				}}},
			}}},
			clause.Select{Columns: []clause.Column{
				eventCol("created_at"),
				eventCol("event_id"),
				eventCol("data"),
				eventCol("user_agent"),
				eventCol("ip_address"),
				recipientCol("id"),
				recipientCol("first_name"),
				recipientCol("last_name"),
				recipientCol("email"),
			}},
			eventTimeOrder(desc, false),
		).
		Where(clause.Eq{Column: eventCol("campaign_id"), Value: campaignID}).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	defer rows.Close()
	var row CampaignEventExportRow
	for rows.Next() {
		var data, userAgent, ip, recipientID, firstName, lastName, email sql.NullString
		if err := rows.Scan(
			&row.CreatedAt, &row.EventID, &data, &userAgent, &ip,
			&recipientID, &firstName, &lastName, &email,
		); err != nil {
			return errs.Wrap(err)
		}
		row.Data = strings.TrimSpace(data.String)
		row.UserAgent = strings.TrimSpace(userAgent.String)
		row.IP = strings.TrimSpace(ip.String)
		row.HasRecipient = recipientID.Valid
		row.FirstName = strings.TrimSpace(firstName.String)
		row.LastName = strings.TrimSpace(lastName.String)
		row.Email = email.String
		if err := fn(&row); err != nil {
			return err
		}
	}
	return errs.Wrap(rows.Err())
}

// GetCampaignCountByTemplateID gets the count of campaigns by template id
func (r *Campaign) GetCampaignCountByTemplateID(
	ctx context.Context,
	templateID *uuid.UUID,
) (int, error) {
	var count int64
	res := r.whereUsesTemplates(
		r.DB.Model(&database.Campaign{}),
		[]string{templateID.String()},
	).
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

	// get recipients count for campaign. each campaign_recipients row is one person
	// and carries a recipient_id, an anonymized_id, or both (an anonymous campaign
	// holds both while live so it can still send). counting rows avoids the double
	// count a UNION of the two id columns would produce for a live anonymous campaign.
	res := r.DB.Raw(`
    SELECT COUNT(*)
    FROM campaign_recipients
    WHERE campaign_id = ?
    AND (recipient_id IS NOT NULL OR anonymized_id IS NOT NULL)
    `, campaignID).Scan(&stats.Recipients)

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

	// the training funnel exists for training campaigns only, a phishing campaign
	// emits no training events so the counts below would scan for rows that
	// cannot be there
	var isTraining bool
	res = r.DB.Raw(`
    SELECT is_training
    FROM campaigns
    WHERE id = ?
`,
		campaignID,
	).Scan(&isTraining)

	if res.Error != nil {
		return nil, res.Error
	}
	if !isTraining {
		return stats, nil
	}

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
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
	).Scan(&stats.TrainingStarted)

	if res.Error != nil {
		return nil, res.Error
	}

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
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
	).Scan(&stats.TrainingCompleted)

	if res.Error != nil {
		return nil, res.Error
	}

	return stats, nil
}

// programEventsSortLimit is the most events a program may have for a page of
// its events sorted by time to be read through the campaign index and
// sorted. Sorting this many rows takes a few milliseconds. A program with more
// events walks the created_at index instead, which stops after one page and is
// fast when the program has many of the events of its time.
var programEventsSortLimit = 20000

// GetEventsByProgramID gets one page of the events of the hidden campaigns a
// program owns, sorted and searched through the campaign event allowlist. The
// campaigns are selected with a subquery, so their number never bumps into the
// SQLite bind parameter ceiling. A page sorted by time is read the cheaper of
// two ways, see programEventsSortLimit, and both give the same rows in the
// same order.
func (r *Campaign) GetEventsByProgramID(
	ctx context.Context,
	programID *uuid.UUID,
	options *CampaignEventOption,
) (*model.Result[model.CampaignEvent], error) {
	var campaign any = clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"}
	timeIndexed := true
	switch {
	case options.RecipientID != nil:
		// one recipient is read through the recipient index, the unary plus
		// keeps the campaign predicate off its index
		campaign = clause.Expr{SQL: "+?", Vars: []any{campaign}}
	case eventsSortedByCreatedAt(options.QueryArgs):
		few, err := r.programHasFewEvents(ctx, programID, options.EventTypeIDs)
		if err != nil {
			return model.NewEmptyResult[model.CampaignEvent](), err
		}
		if few {
			// every event of the program is read through the campaign index
			// and sorted, the walk would pass the events of everything else
			timeIndexed = false
		} else {
			// newest first walks the created_at index back and stops after
			// one page, which SQLite does not pick without statistics
			campaign = clause.Expr{SQL: "+?", Vars: []any{campaign}}
			res, ok, err := r.programEventsCappedWalk(ctx, programID, campaign, options)
			if err != nil || ok {
				return res, err
			}
			// the program has no full page among the newest events of the
			// install, such as a large program that is quiet now, so its
			// events are read through the campaign index and sorted, which
			// costs the size of the program and not of the install
			campaign = clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"}
			timeIndexed = false
		}
	}
	scope := clause.Expr{
		SQL:  "? IN (?)",
		Vars: []any{campaign, r.programCampaignIDs(programID)},
	}
	return r.getEventsInScope(ctx, scope, options, nil, timeIndexed)
}

// programEventsWalkLimit is how many events of the install a walk of the
// created_at index for a page of program events may pass. Finding where the
// walk stops reads that many entries of the index only, which takes a few
// milliseconds.
var programEventsWalkLimit = 50000

// programEventsCappedWalk reads a page of program events sorted by time with a
// walk of the created_at index that passes at most programEventsWalkLimit
// events of the install. ok is false when the walk found no full page with a
// row past it, which the caller then reads another way. Every event the walk
// leaves out sorts after every event it reads, so a page with a row past it
// is the same page an unbounded walk gives.
func (r *Campaign) programEventsCappedWalk(
	ctx context.Context,
	programID *uuid.UUID,
	campaign any,
	options *CampaignEventOption,
) (*model.Result[model.CampaignEvent], bool, error) {
	q := options.QueryArgs
	if q == nil || q.Limit <= 0 {
		return nil, false, nil
	}
	createdAt := clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "created_at"}
	// the time of the event programEventsWalkLimit steps into the walk, read
	// within the same time window as the page
	// the unary plus drops the declared column type, so the driver hands over
	// the stored text as it is
	edge := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select("+? AS edge", createdAt)
	if options.CreatedFrom != nil {
		edge = edge.Where(clause.Expr{SQL: "? >= ?", Vars: []any{createdAt, utils.RFC3339UTC(*options.CreatedFrom)}})
	}
	if options.CreatedTo != nil {
		edge = edge.Where(clause.Expr{SQL: "? <= ?", Vars: []any{createdAt, utils.RFC3339UTC(*options.CreatedTo)}})
	}
	var edges []struct{ Edge string }
	err := edge.Order(clause.OrderByColumn{Column: createdAt, Desc: q.Desc}).
		Offset(programEventsWalkLimit).
		Limit(1).
		Scan(&edges).Error
	if err != nil {
		return nil, false, errs.Wrap(err)
	}
	if len(edges) == 0 {
		// the window holds fewer events than the limit, the walk is bounded
		scope := clause.Expr{SQL: "? IN (?)", Vars: []any{campaign, r.programCampaignIDs(programID)}}
		res, err := r.getEventsInScope(ctx, scope, options, nil, true)
		return res, err == nil, err
	}
	// the stored text is compared as stored, so events that share the edge
	// time are all read or all left out
	bound := clause.Expr{SQL: "? >= ?", Vars: []any{createdAt, edges[0].Edge}}
	if !q.Desc {
		bound = clause.Expr{SQL: "? <= ?", Vars: []any{createdAt, edges[0].Edge}}
	}
	scope := clause.And(
		clause.Expr{SQL: "? IN (?)", Vars: []any{campaign, r.programCampaignIDs(programID)}},
		bound,
	)
	res, err := r.getEventsInScope(ctx, scope, options, nil, true)
	if err != nil {
		return nil, false, err
	}
	if !res.HasNextPage {
		return nil, false, nil
	}
	return res, true, nil
}

// programCampaignIDs selects the ids of the hidden campaigns of a program
func (r *Campaign) programCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.CAMPAIGN_TABLE, Name: "id"}}}).
		Where(clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: programID})
}

// programHasFewEvents reports whether a program has at most
// programEventsSortLimit events of the given types, or of any type when none
// are given. The count reads the campaign and event index only and stops
// after the limit, so it stays cheap for a large program.
func (r *Campaign) programHasFewEvents(ctx context.Context, programID *uuid.UUID, eventTypeIDs []string) (bool, error) {
	capped := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignEvent{}).
		Select("1").
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
			r.programCampaignIDs(programID),
		}}).
		Limit(programEventsSortLimit + 1)
	if len(eventTypeIDs) > 0 {
		capped = capped.Where(clause.IN{
			Column: clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "event_id"},
			Values: eventIDValues(eventTypeIDs),
		})
	}
	var count int64
	if err := r.DB.WithContext(ctx).Table("(?) AS capped", capped).Count(&count).Error; err != nil {
		return false, errs.Wrap(err)
	}
	return count <= int64(programEventsSortLimit), nil
}

// eventsSortedByCreatedAt reports whether a page of events is sorted by when
// the event was created
func eventsSortedByCreatedAt(q *vo.QueryArgs) bool {
	return q != nil && q.OrderBy == TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at")
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
	// apply test campaign filter based on options
	db = r.applyTestCampaignFilter(db, options)

	var dbCampaigns []database.Campaign
	res := withPeek(db, options.QueryArgs).Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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
	res := withPeek(db.Where(
		"(send_start_at IS NULL) OR "+ // self managed
			"(send_start_at BETWEEN ? AND ?)", // is within time
		utils.RFC3339UTC(startDate),
		utils.RFC3339UTC(endDate),
	), options.QueryArgs).Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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
	if !options.IncludeProgramManaged {
		db = db.Where(clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: nil})
	}
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs, allowedCampaignColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbCampaigns []database.Campaign
	res := withPeek(db, options.QueryArgs).Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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
	db := r.load(r.DB.WithContext(ctx), options)
	var dbCampaign database.Campaign
	res := db.
		Where("campaigns.id = ?", id.String()).
		First(&dbCampaign)

	if res.Error != nil {
		return nil, res.Error
	}
	campaign, err := ToCampaign(&dbCampaign)
	if err != nil {
		return nil, err
	}
	if options.WithWebhooks {
		webhooks, err := r.GetCampaignWebhooks(ctx, id)
		if err != nil {
			return nil, err
		}
		campaign.Webhooks = nullable.NewNullableWithValue(webhooks)
	}
	if options.WithScripts {
		scripts, err := r.GetCampaignScripts(ctx, id)
		if err != nil {
			return nil, err
		}
		campaign.Scripts = nullable.NewNullableWithValue(scripts)
	}
	if options.WithLanguageRoutes {
		routes, err := r.GetLanguageRoutesByCampaignID(ctx, id)
		if err != nil {
			return nil, err
		}
		campaign.LanguageRoutes = nullable.NewNullableWithValue(routes)
	}
	return campaign, nil
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

// CampaignDisplayName is the name users and integrations see for a campaign.
// The hidden campaigns a program sends with are named after their program.
type CampaignDisplayName struct {
	// Name is the campaign name, or the program name for a program send
	Name string
	// ProgramID and ProgramName are set for a program send
	ProgramID   *uuid.UUID
	ProgramName string
}

// GetDisplayNameByID gets the name users and integrations see for a campaign
func (r *Campaign) GetDisplayNameByID(
	ctx context.Context,
	id *uuid.UUID,
) (*CampaignDisplayName, error) {
	var row database.Campaign
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("name", "program_id").
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TABLE, Name: "id"}, Value: id}).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if row.ProgramID == nil {
		return &CampaignDisplayName{Name: row.Name}, nil
	}
	names, err := r.GetProgramNamesByIDs(ctx, []*uuid.UUID{row.ProgramID})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	programName := names[*row.ProgramID]
	return &CampaignDisplayName{
		Name:        programName,
		ProgramID:   row.ProgramID,
		ProgramName: programName,
	}, nil
}

// GetProgramNamesByIDs maps program ids to program names. An unknown id is
// left out of the map.
func (r *Campaign) GetProgramNamesByIDs(
	ctx context.Context,
	ids []*uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		if id != nil {
			values = append(values, id)
		}
	}
	var rows []database.Program
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Select("id", "name").
		Where(clause.IN{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "id"}, Values: values}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	for _, row := range rows {
		if row.ID != nil {
			out[*row.ID] = row.Name
		}
	}
	return out, nil
}

// ProgramExportSource is a program whose sends a company export lists
type ProgramExportSource struct {
	ID          *uuid.UUID
	Name        string
	IsAnonymous bool
	// Anonymizing is set while an Anonymize All of the program runs, the
	// campaigns it has not reached then still name people
	Anonymizing bool
}

// GetProgramsForExport returns the programs of exactly the company, or the
// global programs when companyID is nil, sorted by name
func (r *Campaign) GetProgramsForExport(
	ctx context.Context,
	companyID *uuid.UUID,
) ([]ProgramExportSource, error) {
	var rows []database.Program
	db := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Select("id", "name", "is_anonymous", "anonymizing_at", "anonymized_at")
	db = whereCompany(db, database.PROGRAM_TABLE, companyID)
	res := db.
		Order(clause.OrderByColumn{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "name"}}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]ProgramExportSource, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProgramExportSource{
			ID:          row.ID,
			Name:        row.Name,
			IsAnonymous: row.IsAnonymous,
			Anonymizing: row.AnonymizingAt != nil && row.AnonymizedAt == nil,
		})
	}
	return out, nil
}

// ProgramEventExportRow is one event of a program as the company export
// writes it. The recipient fields are empty when the event names nobody or
// the recipient is gone, HasRecipient tells which.
type ProgramEventExportRow struct {
	CampaignID   uuid.UUID
	CreatedAt    time.Time
	EventID      uuid.UUID
	Data         string
	UserAgent    string
	IP           string
	HasRecipient bool
	FirstName    string
	LastName     string
	Email        string
}

// EachProgramEventForExport calls fn with every event of the hidden campaigns
// of a program with its recipient, newest first when desc is set and oldest
// first otherwise, read one row at a time. The events are read through the
// campaign index and sorted, so only the events of the program are read.
// Events of the same time are sorted by row id in the same direction, the
// order a walk of the created_at index gives. The row passed to fn is reused,
// fn must copy what it keeps.
func (r *Campaign) EachProgramEventForExport(
	ctx context.Context,
	programID *uuid.UUID,
	desc bool,
	fn func(*ProgramEventExportRow) error,
) error {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	recipientCol := func(name string) clause.Column {
		return clause.Column{Table: database.RECIPIENT_TABLE, Name: name}
	}
	rows, err := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Clauses(
			clause.From{Joins: []clause.Join{{
				Type:  clause.LeftJoin,
				Table: clause.Table{Name: database.RECIPIENT_TABLE},
				ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
					Column: eventCol("recipient_id"),
					Value:  recipientCol("id"),
				}}},
			}}},
			clause.Select{Columns: []clause.Column{
				eventCol("campaign_id"),
				eventCol("created_at"),
				eventCol("event_id"),
				eventCol("data"),
				eventCol("user_agent"),
				eventCol("ip_address"),
				recipientCol("id"),
				recipientCol("first_name"),
				recipientCol("last_name"),
				recipientCol("email"),
			}},
			clause.OrderBy{Columns: []clause.OrderByColumn{
				{Column: eventCol("created_at"), Desc: desc},
				{Column: eventCol("rowid"), Desc: desc},
			}},
		).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{eventCol("campaign_id"), r.programCampaignIDs(programID)}}).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	defer rows.Close()
	var row ProgramEventExportRow
	for rows.Next() {
		var userAgent, ip, recipientID, firstName, lastName, email sql.NullString
		if err := rows.Scan(
			&row.CampaignID, &row.CreatedAt, &row.EventID, &row.Data, &userAgent, &ip,
			&recipientID, &firstName, &lastName, &email,
		); err != nil {
			return errs.Wrap(err)
		}
		row.UserAgent, row.IP = userAgent.String, ip.String
		row.HasRecipient = recipientID.Valid
		row.FirstName, row.LastName, row.Email = firstName.String, lastName.String, email.String
		if err := fn(&row); err != nil {
			return err
		}
	}
	return errs.Wrap(rows.Err())
}

// GetProgramTemplateNames maps the hidden campaigns of a program to the name
// of the template each sends
func (r *Campaign) GetProgramTemplateNames(
	ctx context.Context,
	programID *uuid.UUID,
) (map[uuid.UUID]string, error) {
	type row struct {
		CampaignID   *uuid.UUID
		TemplateName string
	}
	campaignCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	rows := []row{}
	res := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_TABLE).
		Clauses(
			clause.From{Joins: []clause.Join{{
				Type:  clause.LeftJoin,
				Table: clause.Table{Name: database.CAMPAIGN_TEMPLATE_TABLE},
				ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
					Column: campaignCol("campaign_template_id"),
					Value:  clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"},
				}}},
			}}},
			clause.Select{Columns: []clause.Column{
				{Table: database.CAMPAIGN_TABLE, Name: "id", Alias: "campaign_id"},
				{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "name", Alias: "template_name"},
			}},
		).
		Where(clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: programID}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		if r.CampaignID != nil {
			out[*r.CampaignID] = r.TemplateName
		}
	}
	return out, nil
}

// IsAnonymousByID reads only the anonymity flag for a campaign.
func (r *Campaign) IsAnonymousByID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	var dbCampaign database.Campaign
	res := r.DB.
		Model(&database.Campaign{}).
		Select("is_anonymous").
		Where("id = ?", id).
		First(&dbCampaign)

	if res.Error != nil {
		return false, res.Error
	}
	return dbCampaign.IsAnonymous, nil
}

// IsTrainingByID reports whether the campaign is an awareness training campaign.
func (r *Campaign) IsTrainingByID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	var dbCampaign database.Campaign
	res := r.DB.
		Model(&database.Campaign{}).
		Select("is_training").
		Where("id = ?", id).
		First(&dbCampaign)

	if res.Error != nil {
		return false, res.Error
	}
	return dbCampaign.IsTraining, nil
}

// IsAnonymizedByID reports whether the campaign has been anonymized after the fact,
// i.e. its AnonymizedAt timestamp is set.
func (r *Campaign) IsAnonymizedByID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	var dbCampaign database.Campaign
	res := r.DB.
		Model(&database.Campaign{}).
		Select("anonymized_at").
		Where("id = ?", id).
		First(&dbCampaign)

	if res.Error != nil {
		return false, res.Error
	}
	return dbCampaign.AnonymizedAt != nil, nil
}

// IsAnonymousByCampaignRecipientID reports whether the campaign owning a campaign
// recipient is anonymous. found is false when the id matches no campaign recipient.
func (r *Campaign) IsAnonymousByCampaignRecipientID(
	ctx context.Context,
	campaignRecipientID *uuid.UUID,
) (isAnon bool, found bool, err error) {
	var out []bool
	res := r.DB.WithContext(ctx).Raw(`
		SELECT c.is_anonymous
		FROM campaign_recipients cr
		JOIN campaigns c ON c.id = cr.campaign_id
		WHERE cr.id = ?
		LIMIT 1`, campaignRecipientID).Scan(&out)
	if res.Error != nil {
		return false, false, res.Error
	}
	if len(out) == 0 {
		return false, false, nil
	}
	return out[0], true, nil
}

// HasGroupDataByCampaignID reports whether any recipient carries a position or
// department, i.e. whether a grouped breakdown has anything to show.
func (r *Campaign) HasGroupDataByCampaignID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	// check the snapshot columns and, since they are set only for anonymous
	// campaigns, the recipient's own position/department too
	var count int64
	res := r.DB.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM campaign_recipients cr
			LEFT JOIN recipients r ON r.id = cr.recipient_id
			WHERE cr.campaign_id = ? AND (
				cr.position <> '' OR cr.department <> ''
				OR COALESCE(r.position, '') <> '' OR COALESCE(r.department, '') <> ''
			)
			LIMIT 1
		) AS x`, id).Scan(&count)
	if res.Error != nil {
		return false, res.Error
	}
	return count > 0, nil
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
	res := withPeek(db.
		Where("close_at <= ? AND closed_at IS NULL", utils.NowRFC3339UTC()), options.QueryArgs).
		Find(&dbCampaigns)

	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

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
	// a campaign is ready to anonymize when its scheduled anonymize_at has passed,
	// or when it is anonymous and closed: an anonymous campaign always severs its
	// recipient relation at close, and this is the fallback that guarantees it even
	// if the inline anonymize at close time failed. either way anonymized_at must be
	// unset so a campaign that is already anonymized is never reprocessed.
	var dbCampaigns []database.Campaign
	now := utils.NowRFC3339UTC()
	res := withPeek(db.
		Where("anonymized_at IS NULL").
		Where("(anonymize_at <= ?) OR (is_anonymous = ? AND closed_at IS NOT NULL)", now, true), options.QueryArgs).
		Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetReadyToDataAnonymize gets all campaigns that are ready to have their
// submitted data anonymized while keeping the recipient relation intact. A
// campaign is ready when its data anonymize date has passed and its data was
// not anonymized since that date, so a run before the date, by hand or while
// the campaign was open, does not stand in for it. A closed campaign whose
// data was anonymized at or after the close has no newer data and is left
// out. Fully anonymized campaigns are excluded as their data is already
// scrubbed.
func (r *Campaign) GetReadyToDataAnonymize(
	ctx context.Context,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	at := clause.Column{Table: database.CAMPAIGN_TABLE, Name: "data_anonymize_at"}
	ran := clause.Column{Table: database.CAMPAIGN_TABLE, Name: "data_anonymized_at"}
	closed := clause.Column{Table: database.CAMPAIGN_TABLE, Name: "closed_at"}
	var dbCampaigns []database.Campaign
	res := withPeek(db.
		Where(clause.Neq{Column: at, Value: nil}).
		Where(clause.Lte{Column: at, Value: utils.NowRFC3339UTC()}).
		Where(clause.Or(
			clause.Eq{Column: ran, Value: nil},
			clause.Expr{SQL: "julianday(?) < julianday(?)", Vars: []any{ran, at}},
		)).
		Where(clause.Or(
			clause.Eq{Column: closed, Value: nil},
			clause.Eq{Column: ran, Value: nil},
			clause.Expr{SQL: "julianday(?) < julianday(?)", Vars: []any{ran, closed}},
		)).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TABLE, Name: "anonymized_at"}, Value: nil}), options.QueryArgs).
		Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}

	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)

	for _, dbCampaign := range dbCampaigns {
		campaign, err := ToCampaign(&dbCampaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, campaign)
	}
	return result, nil
}

// GetReadyToLateSchedule returns campaigns where schedule_at <= now and
// the campaign has not yet been scheduled (notable event is still pending_schedule).
func (r *Campaign) GetReadyToLateSchedule(
	ctx context.Context,
	options *CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	db := r.load(r.DB, options)
	db, err := useQuery(db, database.CAMPAIGN_TABLE, options.QueryArgs)
	if err != nil {
		return result, errs.Wrap(err)
	}
	pendingEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_PENDING_SCHEDULE]
	var dbCampaigns []database.Campaign
	res := withPeek(db.
		Where("schedule_at <= ? AND notable_event_id = ?", utils.NowRFC3339UTC(), pendingEventID), options.QueryArgs).
		Find(&dbCampaigns)
	if res.Error != nil {
		return result, res.Error
	}
	result.HasNextPage = trimPeek(&dbCampaigns, options.QueryArgs)
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
		"metadata": func() string {
			if campaignEvent.Metadata == nil {
				return ""
			}
			return campaignEvent.Metadata.String()
		}(),
	}
	if campaignEvent.RecipientID != nil {
		row["recipient_id"] = campaignEvent.RecipientID.String()
	}
	if campaignEvent.AnonymizedID != nil {
		row["anonymized_id"] = campaignEvent.AnonymizedID.String()
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
	return r.HasEvent(ctx, campaignID, recipientID, messageReadEventID)
}

// HasEvent reports whether an event of the given type exists for the campaign,
// optionally scoped to a single recipient.
func (r *Campaign) HasEvent(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	eventID *uuid.UUID,
) (bool, error) {
	if recipientID == nil {
		var count int64
		res := r.DB.WithContext(ctx).Model(&database.CampaignEvent{}).
			Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
			Where(clause.Eq{Column: clause.Column{Name: "event_id"}, Value: eventID}).
			Count(&count)
		if res.Error != nil {
			return false, res.Error
		}
		return count > 0, nil
	}
	return r.hasEventOfPerson(ctx, campaignID, eventID,
		clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID})
}

// HasEventByAnonymizedID reports whether an event of the given type already exists
// for a pseudonym in a campaign. Used to deduplicate events on anonymous campaigns,
// whose events carry no recipient id.
func (r *Campaign) HasEventByAnonymizedID(
	ctx context.Context,
	campaignID *uuid.UUID,
	anonymizedID *uuid.UUID,
	eventID *uuid.UUID,
) (bool, error) {
	return r.hasEventOfPerson(ctx, campaignID, eventID,
		clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: anonymizedID})
}

// hasEventOfPerson reports whether an event of the given type exists in the
// campaign for the person that person matches. The campaign and event columns
// are not read through their index: without table statistics sqlite picks the
// campaign and event index and checks every event of that type in the
// campaign, while the person index finds the few events of the person.
func (r *Campaign) hasEventOfPerson(
	ctx context.Context,
	campaignID *uuid.UUID,
	eventID *uuid.UUID,
	person clause.Expression,
) (bool, error) {
	var count int64
	res := r.DB.WithContext(ctx).Model(&database.CampaignEvent{}).
		Where(person).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), campaignID}}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "event_id"}), eventID}}).
		Count(&count)
	if res.Error != nil {
		return false, res.Error
	}
	return count > 0, nil
}

// SetLureSettingsByID writes the lure URL settings taken from the campaign
// template. The only path that writes them, because a create or update request
// carries whatever a caller put in those fields and they must come from the
// template instead.
func (r *Campaign) SetLureSettingsByID(
	ctx context.Context,
	id *uuid.UUID,
	mode string,
	algorithm string,
	length int,
	isTraining bool,
) error {
	row := map[string]any{
		"lure_url_mode":    mode,
		"lure_code_algo":   algorithm,
		"lure_code_length": length,
		"is_training":      isTraining,
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row)

	return res.Error
}

// UpdateByID updates a campaign by id
// does not update the campaign recipient groups and campaign recipients
func (r *Campaign) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
) error {
	return r.UpdateByIDWith(ctx, id, campaign, nil)
}

// UpdateByIDWith updates a campaign like UpdateByID. When then is set, it
// runs in the same transaction as the campaign row and its language routes,
// so either all of them are written or none.
func (r *Campaign) UpdateByIDWith(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
	then func(tx *gorm.DB) error,
) error {
	row := campaign.ToDBMap()
	AddUpdatedAt(row)
	var writeErr error
	if campaign.LanguageRoutes.IsSpecified() || then != nil {
		// the campaign row and its language routes are written together
		writeErr = r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.
				Model(&database.Campaign{}).
				Where("id = ?", id).
				Updates(row).Error; err != nil {
				return err
			}
			if campaign.LanguageRoutes.IsSpecified() {
				routes := map[string]string{}
				if v, err := campaign.LanguageRoutes.Get(); err == nil {
					routes = v
				}
				if err := replaceCampaignLanguageRoutesTx(tx, id, routes); err != nil {
					return err
				}
			}
			if then != nil {
				return then(tx)
			}
			return nil
		})
	} else {
		writeErr = r.DB.
			Model(&database.Campaign{}).
			Where("id = ?", id).
			Updates(row).Error
	}
	// a failed campaign write leaves the allow and deny lists as they are
	if writeErr != nil {
		return writeErr
	}

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

	return nil
}

// SetPausedByID marks a campaign as paused by setting its paused at timestamp.
// It is a targeted write so it never touches any other campaign column.
func (r *Campaign) SetPausedByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]any{"paused_at": utils.NowRFC3339UTC()}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row)
	return res.Error
}

// PauseOpenByID marks a campaign as paused when it is open and not paused
// yet. It is one write by the primary key that touches no other column, so a
// campaign closed in between is left as it is.
func (r *Campaign) PauseOpenByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	now := utils.NowRFC3339UTC()
	return r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Name: "paused_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Updates(map[string]any{"paused_at": now, "updated_at": now}).Error
}

// SetClosedAtByID writes only the closed at time, so a campaign row loaded
// earlier can not write back other columns that changed since
func (r *Campaign) SetClosedAtByID(
	ctx context.Context,
	id *uuid.UUID,
	closedAt time.Time,
) error {
	row := map[string]any{"closed_at": utils.RFC3339UTC(closedAt)}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row).Error
}

// SetCloseAtByID writes only the time the campaign is set to close, so a
// campaign row loaded earlier can not write back other columns that changed
// since
func (r *Campaign) SetCloseAtByID(
	ctx context.Context,
	id *uuid.UUID,
	closeAt time.Time,
) error {
	row := map[string]any{"close_at": utils.RFC3339UTC(closeAt)}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row).Error
}

// SetAnonymizeAtByID writes only the time a closed campaign is anonymized,
// or clears it when at is nil. It writes nothing when the campaign is open or
// already anonymized and reports whether a row was written.
func (r *Campaign) SetAnonymizeAtByID(ctx context.Context, id *uuid.UUID, at *time.Time) (bool, error) {
	return r.setClosedCampaignDate(ctx, id, map[string]any{"anonymize_at": optionalRFC3339UTC(at)})
}

// SetDataAnonymizeAtByID writes only the time the data of a closed campaign
// is anonymized, or clears it when at is nil, see SetAnonymizeAtByID
func (r *Campaign) SetDataAnonymizeAtByID(ctx context.Context, id *uuid.UUID, at *time.Time) (bool, error) {
	return r.setClosedCampaignDate(ctx, id, map[string]any{"data_anonymize_at": optionalRFC3339UTC(at)})
}

// setClosedCampaignDate writes row on a campaign that is closed and not
// anonymized and reports whether a row was written
func (r *Campaign) setClosedCampaignDate(ctx context.Context, id *uuid.UUID, row map[string]any) (bool, error) {
	if id == nil {
		return false, errs.Wrap(errNilCampaignID)
	}
	AddUpdatedAt(row)
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Neq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_at"}, Value: nil}).
		Updates(row)
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	return res.RowsAffected == 1, nil
}

// optionalRFC3339UTC is at as stored, or nil
func optionalRFC3339UTC(at *time.Time) any {
	if at == nil {
		return nil
	}
	return utils.RFC3339UTC(*at)
}

// SetNotableEventByID writes only the most notable event, so a campaign row
// loaded earlier can not write back other columns that changed since
func (r *Campaign) SetNotableEventByID(
	ctx context.Context,
	id *uuid.UUID,
	eventID *uuid.UUID,
) error {
	row := map[string]any{"notable_event_id": eventID.String()}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row).Error
}

// ClearPausedByID clears the paused at timestamp. Used on resume and on close so
// a closed campaign is never left paused.
func (r *Campaign) ClearPausedByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]any{"paused_at": nil}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Campaign{}).
		Where("id = ?", id).
		Updates(row)
	return res.Error
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

// DeleteByID deletes a campaign by id including its stats
func (r *Campaign) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	// the pool rows and the campaign row go together
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("campaign_id = ?", id.String()).
			Delete(&database.CampaignTemplatePool{}).Error; err != nil {
			return err
		}
		if err := deleteCampaignLanguageRoutesTx(tx, id); err != nil {
			return err
		}
		// a tracking event saved after the events were deleted would make the
		// campaign row fail its foreign key, so the events are deleted again in
		// the same transaction as the row. There are none or a few.
		if err := tx.
			Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: id}).
			Delete(&database.CampaignEvent{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&database.Campaign{}).Error
	})
	if err != nil {
		return err
	}
	return r.DeleteCampaignStats(ctx, id)
}

// DeleteEventsByCampaignID deletes all events by campaign id, in batches with
// a pause between them, see deleteInBatches
func (r *Campaign) DeleteEventsByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	// a delete that began finishes also when the request ends, so the events
	// of a campaign are not left half deleted
	return deleteInBatches(context.WithoutCancel(ctx), r.DB, &database.CampaignEvent{},
		clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}, deleteBatch)
}

// GetEventByID gets a campaign event by its id
func (r *Campaign) GetEventByID(
	ctx context.Context,
	eventID *uuid.UUID,
) (*database.CampaignEvent, error) {
	var event database.CampaignEvent
	res := r.DB.WithContext(ctx).
		Where("id = ?", eventID).
		First(&event)
	if res.Error != nil {
		return nil, res.Error
	}
	return &event, nil
}

// DeleteEventByID deletes a campaign event by its id
func (r *Campaign) DeleteEventByID(
	ctx context.Context,
	eventID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where("id = ?", eventID).
		Delete(&database.CampaignEvent{})
	return res.Error
}

// GetMostNotableEventForRecipient finds the most notable event for a recipient in a campaign
// excludes the specified event id (the one being deleted)
func (r *Campaign) GetMostNotableEventForRecipient(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	excludeEventID *uuid.UUID,
) (*uuid.UUID, error) {
	var events []database.CampaignEvent
	query := r.DB.WithContext(ctx).
		Where("campaign_id = ?", campaignID).
		Where("id != ?", excludeEventID)

	// handle both regular and anonymized recipients
	if recipientID != nil {
		query = query.Where("recipient_id = ?", recipientID)
	}

	res := query.Find(&events)
	if res.Error != nil {
		return nil, res.Error
	}

	if len(events) == 0 {
		return nil, nil
	}

	// find the most notable event using priority
	var mostNotableEventID *uuid.UUID
	highestPriority := -1

	for _, event := range events {
		if event.EventID == nil {
			continue
		}
		priority := cache.GetEventPriority(event.EventID)
		if priority > highestPriority {
			highestPriority = priority
			mostNotableEventID = event.EventID
		}
	}

	return mostNotableEventID, nil
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

// AddDataAnonymizedAt adds a data anonymized at time to a campaign
func (r *Campaign) AddDataAnonymizedAt(
	ctx context.Context,
	id *uuid.UUID,
) error {
	row := map[string]interface{}{
		"data_anonymized_at": utils.NowRFC3339UTC(),
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

// AnonymizeCampaignEventData anonymizes the submitted data, user agent, ip
// address and browser metadata of all events in a campaign while keeping the
// recipient relation intact
func (r *Campaign) AnonymizeCampaignEventData(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	row := map[string]any{
		"user_agent": "anonymized",
		"ip_address": nil,
		"data":       "anonymized",
		"metadata":   "",
	}
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignEvent{}).
		Where("campaign_id = ?", campaignID).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// errNilCampaignID and errNilRecipientID refuse a write keyed by a nil id. A
// nil id in a clause.Eq becomes IS NULL and a skipped filter matches every
// row, both of which would reach rows outside the campaign or recipient.
var (
	errNilCampaignID  = errors.New("campaign id is nil")
	errNilRecipientID = errors.New("recipient id is nil")
)

// AnonymizeCampaignEventsByRecipientID moves the events of a recipient onto a
// pseudonym and removes their submitted data, IP address, user agent and
// browser metadata. Each event takes the pseudonym of the recipient's send in
// its own campaign when that send has one, so the event stays with its send,
// else anonymizedID. The sends must still name the recipient.
func (r *Campaign) AnonymizeCampaignEventsByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) error {
	// a nil id would match recipient_id IS NULL, which is every campaign
	// event and every already anonymized event of every campaign
	if recipientID == nil {
		return errs.Wrap(errNilRecipientID)
	}
	if anonymizedID == nil {
		return errs.Wrap(errors.New("anonymized id is nil"))
	}
	sendPseudonym := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignRecipient{}).
		Select("?", clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "anonymized_id"}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
			clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
		}}).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Neq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "anonymized_id"}, Value: nil}).
		Limit(1)
	row := scrubbedEventFields()
	row["recipient_id"] = nil
	row["anonymized_id"] = clause.Expr{SQL: "COALESCE((?), ?)", Vars: []any{sendPseudonym, anonymizedID.String()}}
	res := r.DB.
		WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// ActiveCounts are the open campaigns that started, are self managed or are paused
type ActiveCounts struct {
	// Active is sending now, paused campaigns left out
	Active int64
	// SelfManaged is the part of Active without a delivery window
	SelfManaged int64
	// Paused is open with sending paused, also when paused before the delivery
	// started. Paused wins over scheduled, like the campaign list badge.
	Paused int64
}

// GetActiveCounts counts the open campaigns that started, are self managed or
// are paused. If no company ID is selected it counts all companies. The hidden
// campaigns of a program are not counted.
func (r *Campaign) GetActiveCounts(ctx context.Context, companyID *uuid.UUID, includeTestCampaigns bool) (*ActiveCounts, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	open := func() *gorm.DB {
		db := r.DB.WithContext(ctx).Model(&database.Campaign{})
		if companyID != nil {
			db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
		}
		if !includeTestCampaigns {
			db = db.Where(clause.Eq{Column: col("is_test"), Value: false})
		}
		return db.
			Where(clause.Eq{Column: col("closed_at"), Value: nil}).
			Where(notProgramCampaign)
	}
	started := func() *gorm.DB {
		return open().
			Where(clause.Or(
				clause.Lte{Column: col("send_start_at"), Value: utils.NowRFC3339UTC()},
				clause.Eq{Column: col("send_start_at"), Value: nil},
			)).
			Where(clause.Eq{Column: col("paused_at"), Value: nil})
	}
	out := &ActiveCounts{}
	if err := started().Count(&out.Active).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	if err := started().
		Where(clause.Eq{Column: col("send_start_at"), Value: nil}).
		Count(&out.SelfManaged).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	if err := open().Where(clause.Neq{Column: col("paused_at"), Value: nil}).Count(&out.Paused).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	return out, nil
}

// GetUpcomingCount get the upcoming campaign count
// if no company ID is selected it gets the global count including all companies.
// The hidden campaigns of a program are not counted. A paused campaign counts
// as paused, not as upcoming.
func (r *Campaign) GetUpcomingCount(ctx context.Context, companyID *uuid.UUID, includeTestCampaigns bool) (int64, error) {
	col := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	var c int64
	db := r.DB.WithContext(ctx)
	if companyID != nil {
		db = whereCompany(db, database.CAMPAIGN_TABLE, companyID)
	}
	if !includeTestCampaigns {
		db = db.Where(clause.Eq{Column: col("is_test"), Value: false})
	}
	res := db.
		Model(&database.Campaign{}).
		Where(clause.Gt{Column: col("send_start_at"), Value: utils.NowRFC3339UTC()}).
		Where(clause.Eq{Column: col("closed_at"), Value: nil}).
		Where(clause.Eq{Column: col("paused_at"), Value: nil}).
		Where(notProgramCampaign).
		Count(&c)

	return c, res.Error
}

// GetFinishedCount get the finished campaign count
// if no company ID is selected it gets the global count including all companies.
// The hidden campaigns of a program are not counted.
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
		Where(notProgramCampaign).
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
	var pausedAt nullable.Nullable[time.Time]
	pausedAt.SetNull()
	if row.PausedAt != nil {
		pausedAt = nullable.NewNullableWithValue(*row.PausedAt)
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
	var scheduleAt nullable.Nullable[time.Time]
	if row.ScheduleAt != nil {
		scheduleAt = nullable.NewNullableWithValue(*row.ScheduleAt)
	} else {
		scheduleAt.SetNull()
	}
	var jitterMin nullable.Nullable[int]
	if row.JitterMin != nil {
		jitterMin = nullable.NewNullableWithValue(*row.JitterMin)
	} else {
		jitterMin.SetNull()
	}
	var jitterMax nullable.Nullable[int]
	if row.JitterMax != nil {
		jitterMax = nullable.NewNullableWithValue(*row.JitterMax)
	} else {
		jitterMax.SetNull()
	}
	saveSubmittedData := nullable.NewNullableWithValue(row.SaveSubmittedData)
	saveBrowserMetadata := nullable.NewNullableWithValue(row.SaveBrowserMetadata)
	isAnonymous := nullable.NewNullableWithValue(row.IsAnonymous)
	isTest := nullable.NewNullableWithValue(row.IsTest)
	isTraining := nullable.NewNullableWithValue(row.IsTraining)
	obfuscate := nullable.NewNullableWithValue(row.Obfuscate)
	templateMixBy := nullable.NewNullableWithValue(row.TemplateMixBy)
	languageRouteBy := nullable.NewNullableWithValue(row.LanguageRouteBy)
	languageDefault := nullable.NewNullableWithValue(row.LanguageDefault)
	// deprecated fields - kept for backward compatibility
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
	programID := nullable.NewNullNullable[uuid.UUID]()
	if row.ProgramID != nil {
		programID.Set(*row.ProgramID)
	} else {
		programID.SetNull()
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
	// AllowDenyIDs stays unspecified unless the allow/deny association was
	// preloaded. A scalar-only UpdateByID (notable event, close) reads a campaign
	// without that preload and must leave the filter lists alone; a specified but
	// empty value makes UpdateByID delete every join row.
	var allowDenyIDs nullable.Nullable[[]*uuid.UUID]
	if row.AllowDeny != nil {
		ids := []*uuid.UUID{}
		for _, ad := range row.AllowDeny {
			allowDeny = append(allowDeny, ToAllowDeny(ad))
			ids = append(ids, ad.ID)
		}
		allowDenyIDs.Set(ids)
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
	constraintTimeZone := nullable.NewNullNullable[vo.CampaignTimeZone]()
	if row.ConstraintTimeZone != nil {
		tz, err := vo.NewCampaignTimeZone(*row.ConstraintTimeZone)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		constraintTimeZone.Set(*tz)
	}
	// handle old single webhook (backward compatibility)
	webhookID := nullable.NewNullNullable[uuid.UUID]()
	if row.WebhookID != nil {
		webhookID.Set(*row.WebhookID)
	}

	// handle new multiple webhooks - will be populated separately if needed
	webhooks := nullable.NewNullNullable[[]*model.CampaignWebhook]()
	anonymizeAt := nullable.NewNullNullable[time.Time]()
	if row.AnonymizeAt != nil {
		anonymizeAt.Set(*row.AnonymizeAt)
	}
	anonymizedAt := nullable.NewNullNullable[time.Time]()
	if row.AnonymizedAt != nil {
		anonymizedAt.Set(*row.AnonymizedAt)
	}
	dataAnonymizeAt := nullable.NewNullNullable[time.Time]()
	if row.DataAnonymizeAt != nil {
		dataAnonymizeAt.Set(*row.DataAnonymizeAt)
	}
	dataAnonymizedAt := nullable.NewNullNullable[time.Time]()
	if row.DataAnonymizedAt != nil {
		dataAnonymizedAt.Set(*row.DataAnonymizedAt)
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
		PausedAt:            pausedAt,
		AnonymizeAt:         anonymizeAt,
		AnonymizedAt:        anonymizedAt,
		DataAnonymizeAt:     dataAnonymizeAt,
		DataAnonymizedAt:    dataAnonymizedAt,
		SortField:           sortField,
		SortOrder:           sortOrder,
		SendStartAt:         sendStartAt,
		SendEndAt:           sendEndAt,
		ScheduleAt:          scheduleAt,
		JitterMin:           jitterMin,
		JitterMax:           jitterMax,
		ConstraintWeekDays:  constraintWeekDays,
		ConstraintStartTime: constraintStartTime,
		ConstraintEndTime:   constraintEndTime,
		ConstraintTimeZone:  constraintTimeZone,
		SaveSubmittedData:   saveSubmittedData,
		SaveBrowserMetadata: saveBrowserMetadata,
		IsAnonymous:         isAnonymous,
		IsTraining:          isTraining,
		IsTest:              isTest,
		Obfuscate:           obfuscate,
		TemplateMixBy:       templateMixBy,
		LanguageRouteBy:     languageRouteBy,
		LanguageDefault:     languageDefault,
		WebhookIncludeData:  webhookIncludeData,
		WebhookEvents:       webhookEvents,
		TemplateID:          templateID,
		Template:            template,
		ProgramID:           programID,
		RecipientGroups:     recipientGroups,
		RecipientGroupIDs:   nullable.NewNullableWithValue(recipientGroupIDs),
		AllowDeny:           allowDeny,
		AllowDenyIDs:        allowDenyIDs,
		DenyPage:            denyPage,
		DenyPageID:          denyPageID,
		EvasionPage:         evasionPage,
		EvasionPageID:       evasionPageID,
		WebhookID:           webhookID,
		Webhooks:            webhooks,
		NotableEventID:      notableEventID,
		NotableEventName:    notableEventName,
		LureURLMode:         nullable.NewNullableWithValue(row.LureURLMode),
		LureCodeAlgo:        nullable.NewNullableWithValue(row.LureCodeAlgo),
		LureCodeLength:      nullable.NewNullableWithValue(row.LureCodeLength),
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
	var campaign *model.Campaign
	if row.Campaign != nil {
		c, err := ToCampaign(row.Campaign)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		campaign = c
	}
	ip := vo.NewOptionalString64Must(row.IPAddress)
	userAgent := vo.NewOptionalString255Must(row.UserAgent)
	data := vo.NewOptionalString1MBMust(row.Data)
	metadata := vo.NewOptionalString1MBMust(row.Metadata)

	return &model.CampaignEvent{
		ID:           row.ID,
		CreatedAt:    row.CreatedAt,
		CampaignID:   row.CampaignID,
		Campaign:     campaign,
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

// GetAllCampaignStats retrieves all campaign statistics. When since is set
// only the campaigns that started, closed or were created at or after it are
// returned. A campaign without a start date is dated by when it was created.
func (r *Campaign) GetAllCampaignStats(ctx context.Context, companyID *uuid.UUID, since *time.Time) ([]database.CampaignStats, error) {
	var stats []database.CampaignStats

	db := r.DB.WithContext(ctx).Order("campaign_start_date DESC NULLS LAST, created_at DESC")

	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}
	if since != nil {
		col := func(name string) clause.Column {
			return clause.Column{Table: database.CAMPAIGN_STATS_TABLE, Name: name}
		}
		at := since.UTC()
		db = db.Where(clause.Or(
			clause.Gte{Column: col("campaign_start_date"), Value: at},
			clause.Gte{Column: col("campaign_closed_at"), Value: at},
			clause.And(
				clause.Eq{Column: col("campaign_start_date"), Value: nil},
				clause.Gte{Column: col("created_at"), Value: at},
			),
		))
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

	db := r.DB.WithContext(ctx).Where("campaign_id IS NULL").Order("campaign_start_date DESC NULLS LAST, created_at DESC")

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

// DeleteManualStatsByCompanyID deletes the manual campaign stats of a
// company, the ones that have no campaign
func (r *Campaign) DeleteManualStatsByCompanyID(ctx context.Context, companyID *uuid.UUID) error {
	err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "company_id"}, Value: companyID}).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: nil}).
		Delete(&database.CampaignStats{}).Error
	return errs.Wrap(err)
}

// DeleteCampaignStatsByID deletes campaign statistics by stats ID
func (r *Campaign) DeleteCampaignStatsByID(ctx context.Context, statsID *uuid.UUID) error {
	res := r.DB.WithContext(ctx).Where("id = ?", statsID).Delete(&database.CampaignStats{})
	return res.Error
}

// reportRecipientRow is the scan target for GetReportRecipients
type reportRecipientRow struct {
	FirstName         string `gorm:"column:first_name"`
	LastName          string `gorm:"column:last_name"`
	Email             string `gorm:"column:email"`
	Department        string `gorm:"column:department"`
	Position          string `gorm:"column:position"`
	ClickedLink       bool   `gorm:"column:clicked_link"`
	SubmittedData     bool   `gorm:"column:submitted_data"`
	Reported          bool   `gorm:"column:reported"`
	TrainingStarted   bool   `gorm:"column:training_started"`
	TrainingCompleted bool   `gorm:"column:training_completed"`
}

// GetReportRecipients returns per recipient click/submit/reported results for a campaign.
// Only non-anonymized recipients (recipient_id IS NOT NULL) are included.
func (r *Campaign) GetReportRecipients(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]model.ReportRecipient, error) {
	var rows []reportRecipientRow

	res := r.DB.WithContext(ctx).Raw(`
		SELECT
			rec.first_name,
			rec.last_name,
			rec.email,
			rec.department,
			rec.position,
			CASE WHEN clicked.recipient_id IS NOT NULL THEN 1 ELSE 0 END AS clicked_link,
			CASE WHEN submitted.recipient_id IS NOT NULL THEN 1 ELSE 0 END AS submitted_data,
			CASE WHEN reported.recipient_id IS NOT NULL THEN 1 ELSE 0 END AS reported,
			CASE WHEN started.recipient_id IS NOT NULL THEN 1 ELSE 0 END AS training_started,
			CASE WHEN completed.recipient_id IS NOT NULL THEN 1 ELSE 0 END AS training_completed
		FROM campaign_recipients cr
		JOIN recipients rec ON rec.id = cr.recipient_id
		LEFT JOIN (
			SELECT DISTINCT recipient_id
			FROM campaign_events
			WHERE campaign_id = ? AND recipient_id IS NOT NULL
			AND event_id IN (?, ?, ?)
		) AS clicked ON clicked.recipient_id = cr.recipient_id
		LEFT JOIN (
			SELECT DISTINCT recipient_id
			FROM campaign_events
			WHERE campaign_id = ? AND recipient_id IS NOT NULL
			AND event_id = ?
		) AS submitted ON submitted.recipient_id = cr.recipient_id
		LEFT JOIN (
			SELECT DISTINCT recipient_id
			FROM campaign_events
			WHERE campaign_id = ? AND recipient_id IS NOT NULL
			AND event_id = ?
		) AS reported ON reported.recipient_id = cr.recipient_id
		LEFT JOIN (
			SELECT DISTINCT recipient_id
			FROM campaign_events
			WHERE campaign_id = ? AND recipient_id IS NOT NULL
			AND event_id = ?
		) AS started ON started.recipient_id = cr.recipient_id
		LEFT JOIN (
			SELECT DISTINCT recipient_id
			FROM campaign_events
			WHERE campaign_id = ? AND recipient_id IS NOT NULL
			AND event_id = ?
		) AS completed ON completed.recipient_id = cr.recipient_id
		WHERE cr.campaign_id = ? AND cr.recipient_id IS NOT NULL
		ORDER BY rec.last_name, rec.first_name
	`,
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED],
		campaignID,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
		campaignID,
	).Scan(&rows)

	if res.Error != nil {
		return nil, res.Error
	}

	result := make([]model.ReportRecipient, 0, len(rows))
	for _, row := range rows {
		result = append(result, model.ReportRecipient{
			FirstName:         row.FirstName,
			LastName:          row.LastName,
			Email:             row.Email,
			Department:        row.Department,
			Position:          row.Position,
			ClickedLink:       row.ClickedLink,
			SubmittedData:     row.SubmittedData,
			Reported:          row.Reported,
			TrainingStarted:   row.TrainingStarted,
			TrainingCompleted: row.TrainingCompleted,
		})
	}
	return result, nil
}

// groupedStatColumns is the fixed allowlist of snapshot columns a grouped stats
// query may group on. Never group on a raw column taken from user input.
var groupedStatColumns = map[string]bool{
	"position":   true,
	"department": true,
}

// GroupedStatByTemplate groups results by each recipient's template
const GroupedStatByTemplate = "template"

// GroupedStatByLanguage groups results by each recipient's language
const GroupedStatByLanguage = "language"

// UnspecifiedLanguageGroup is the group of recipients whose template has no
// language set
const UnspecifiedLanguageGroup = "(unspecified)"

// GetGroupedResultStats returns outcome counts grouped by a recipient attribute
// snapshotted on the campaign recipient, joining events to recipients by the
// stable pseudonym so it works for an anonymous campaign both while live and after
// the recipient relation is severed. Each unique pseudonym is counted once per
// outcome. Only anonymous campaigns snapshot the grouping columns, so this returns
// empty for a campaign that has none.
func (r *Campaign) GetGroupedResultStats(
	ctx context.Context,
	campaignID *uuid.UUID,
	groupColumn string,
) ([]model.CampaignGroupStat, error) {
	if groupColumn == GroupedStatByTemplate {
		return r.getGroupedResultStatsByTemplate(ctx, campaignID)
	}
	if groupColumn == GroupedStatByLanguage {
		return r.getGroupedResultStatsByLanguage(ctx, campaignID)
	}
	if !groupedStatColumns[groupColumn] {
		return nil, fmt.Errorf("invalid group column: %s", groupColumn)
	}
	beforeID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED]
	pageID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	afterID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED]
	submitID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	reportID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	startedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED]
	completedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED]

	type groupRow struct {
		Grp               string
		Total             int
		Clicked           int
		Submitted         int
		Reported          int
		TrainingStarted   int
		TrainingCompleted int
	}
	var rows []groupRow
	// the person key is the pseudonym when present, else the recipient id, so counts
	// work for normal, anonymous and anonymized campaigns; the group value is the
	// snapshot, falling back to the recipient's attribute. Events are aggregated to
	// one row per person first, then joined once to the recipients so the join key is
	// a plain column the database can index; joining the raw event rows on the
	// COALESCE expression scans every recipient against every event and is quadratic.
	// groupColumn is allowlisted above, so its interpolation is safe.
	query := fmt.Sprintf(`
	SELECT
		p.grp AS grp,
		COUNT(DISTINCT p.person) AS total,
		COUNT(DISTINCT CASE WHEN ev.clicked = 1 THEN p.person END) AS clicked,
		COUNT(DISTINCT CASE WHEN ev.submitted = 1 THEN p.person END) AS submitted,
		COUNT(DISTINCT CASE WHEN ev.reported = 1 THEN p.person END) AS reported,
		COUNT(DISTINCT CASE WHEN ev.training_started = 1 THEN p.person END) AS training_started,
		COUNT(DISTINCT CASE WHEN ev.training_completed = 1 THEN p.person END) AS training_completed
	FROM (
		SELECT
			COALESCE(cr.anonymized_id, cr.recipient_id) AS person,
			COALESCE(NULLIF(cr.%[1]s, ''), r.%[1]s) AS grp
		FROM campaign_recipients cr
		LEFT JOIN recipients r ON r.id = cr.recipient_id
		WHERE cr.campaign_id = ? AND COALESCE(cr.anonymized_id, cr.recipient_id) IS NOT NULL
	) AS p
	LEFT JOIN (
		SELECT
			COALESCE(anonymized_id, recipient_id) AS person,
			MAX(CASE WHEN event_id IN (?, ?, ?) THEN 1 ELSE 0 END) AS clicked,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS submitted,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS reported,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_started,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_completed
		FROM campaign_events
		WHERE campaign_id = ? AND COALESCE(anonymized_id, recipient_id) IS NOT NULL
		GROUP BY COALESCE(anonymized_id, recipient_id)
	) AS ev ON ev.person = p.person
	GROUP BY p.grp
	ORDER BY total DESC
	`, groupColumn)

	res := r.DB.WithContext(ctx).Raw(query,
		campaignID,
		beforeID, pageID, afterID,
		submitID,
		reportID,
		startedID,
		completedID,
		campaignID,
	).Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	result := make([]model.CampaignGroupStat, 0, len(rows))
	for _, row := range rows {
		grp := row.Grp
		if grp == "" {
			grp = "(unspecified)"
		}
		result = append(result, model.CampaignGroupStat{
			Group:             grp,
			Total:             row.Total,
			Clicked:           row.Clicked,
			Submitted:         row.Submitted,
			Reported:          row.Reported,
			TrainingStarted:   row.TrainingStarted,
			TrainingCompleted: row.TrainingCompleted,
		})
	}
	return result, nil
}

// getGroupedResultStatsByTemplate returns outcome counts grouped by each
// recipient's template, using the campaign's first template for recipients that
// have none. It counts people the same way as GetGroupedResultStats.
func (r *Campaign) getGroupedResultStatsByTemplate(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]model.CampaignGroupStat, error) {
	beforeID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED]
	pageID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	afterID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED]
	submitID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	reportID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	startedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED]
	completedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED]

	type groupRow struct {
		TemplateID        *string
		TemplateName      *string
		Total             int
		Clicked           int
		Submitted         int
		Reported          int
		TrainingStarted   int
		TrainingCompleted int
	}
	var rows []groupRow
	res := r.DB.WithContext(ctx).Raw(`
	SELECT
		p.template_id AS template_id,
		t.name AS template_name,
		COUNT(DISTINCT p.person) AS total,
		COUNT(DISTINCT CASE WHEN ev.clicked = 1 THEN p.person END) AS clicked,
		COUNT(DISTINCT CASE WHEN ev.submitted = 1 THEN p.person END) AS submitted,
		COUNT(DISTINCT CASE WHEN ev.reported = 1 THEN p.person END) AS reported,
		COUNT(DISTINCT CASE WHEN ev.training_started = 1 THEN p.person END) AS training_started,
		COUNT(DISTINCT CASE WHEN ev.training_completed = 1 THEN p.person END) AS training_completed
	FROM (
		SELECT
			COALESCE(cr.anonymized_id, cr.recipient_id) AS person,
			COALESCE(cr.campaign_template_id, c.campaign_template_id) AS template_id
		FROM campaign_recipients cr
		JOIN campaigns c ON c.id = cr.campaign_id
		WHERE cr.campaign_id = ? AND COALESCE(cr.anonymized_id, cr.recipient_id) IS NOT NULL
	) AS p
	LEFT JOIN (
		SELECT
			COALESCE(anonymized_id, recipient_id) AS person,
			MAX(CASE WHEN event_id IN (?, ?, ?) THEN 1 ELSE 0 END) AS clicked,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS submitted,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS reported,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_started,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_completed
		FROM campaign_events
		WHERE campaign_id = ? AND COALESCE(anonymized_id, recipient_id) IS NOT NULL
		GROUP BY COALESCE(anonymized_id, recipient_id)
	) AS ev ON ev.person = p.person
	LEFT JOIN campaign_templates t ON t.id = p.template_id
	GROUP BY p.template_id, t.name
	ORDER BY total DESC
	`,
		campaignID,
		beforeID, pageID, afterID,
		submitID,
		reportID,
		startedID,
		completedID,
		campaignID,
	).Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	result := make([]model.CampaignGroupStat, 0, len(rows))
	for _, row := range rows {
		group := "(deleted template)"
		if row.TemplateName != nil && *row.TemplateName != "" {
			group = *row.TemplateName
		}
		groupID := ""
		if row.TemplateID != nil {
			groupID = *row.TemplateID
		}
		result = append(result, model.CampaignGroupStat{
			Group:             group,
			GroupID:           groupID,
			Total:             row.Total,
			Clicked:           row.Clicked,
			Submitted:         row.Submitted,
			Reported:          row.Reported,
			TrainingStarted:   row.TrainingStarted,
			TrainingCompleted: row.TrainingCompleted,
		})
	}
	return result, nil
}

// getGroupedResultStatsByLanguage returns outcome counts grouped by each
// recipient's language. It counts people the same way as
// GetGroupedResultStats, grouped by template and stamp, and a row with no
// stamp is put in its template's language in Go. A person has one row per
// campaign, so the sums are exact. The group is the language name and the
// group id its code; a template with no language gives UnspecifiedLanguageGroup.
func (r *Campaign) getGroupedResultStatsByLanguage(
	ctx context.Context,
	campaignID *uuid.UUID,
) ([]model.CampaignGroupStat, error) {
	beforeID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED]
	pageID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED]
	afterID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED]
	submitID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	reportID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]
	startedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_STARTED]
	completedID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED]

	var rows []languageStatRow
	res := r.DB.WithContext(ctx).Raw(`
	SELECT
		p.language AS language,
		t.language AS template_language,
		COUNT(DISTINCT p.person) AS total,
		COUNT(DISTINCT CASE WHEN ev.clicked = 1 THEN p.person END) AS clicked,
		COUNT(DISTINCT CASE WHEN ev.submitted = 1 THEN p.person END) AS submitted,
		COUNT(DISTINCT CASE WHEN ev.reported = 1 THEN p.person END) AS reported,
		COUNT(DISTINCT CASE WHEN ev.training_started = 1 THEN p.person END) AS training_started,
		COUNT(DISTINCT CASE WHEN ev.training_completed = 1 THEN p.person END) AS training_completed
	FROM (
		SELECT
			COALESCE(cr.anonymized_id, cr.recipient_id) AS person,
			COALESCE(cr.campaign_template_id, c.campaign_template_id) AS template_id,
			cr.language AS language
		FROM campaign_recipients cr
		JOIN campaigns c ON c.id = cr.campaign_id
		WHERE cr.campaign_id = ? AND COALESCE(cr.anonymized_id, cr.recipient_id) IS NOT NULL
	) AS p
	LEFT JOIN (
		SELECT
			COALESCE(anonymized_id, recipient_id) AS person,
			MAX(CASE WHEN event_id IN (?, ?, ?) THEN 1 ELSE 0 END) AS clicked,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS submitted,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS reported,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_started,
			MAX(CASE WHEN event_id = ? THEN 1 ELSE 0 END) AS training_completed
		FROM campaign_events
		WHERE campaign_id = ? AND COALESCE(anonymized_id, recipient_id) IS NOT NULL
		GROUP BY COALESCE(anonymized_id, recipient_id)
	) AS ev ON ev.person = p.person
	LEFT JOIN campaign_templates t ON t.id = p.template_id
	GROUP BY p.template_id, p.language
	`,
		campaignID,
		beforeID, pageID, afterID,
		submitID,
		reportID,
		startedID,
		completedID,
		campaignID,
	).Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	return mergeLanguageStatRows(rows), nil
}

// languageStatRow is one template and stamp of a grouped stats by language
// query
type languageStatRow struct {
	Language          *string
	TemplateLanguage  *string
	Total             int
	Clicked           int
	Submitted         int
	Reported          int
	TrainingStarted   int
	TrainingCompleted int
}

// mergeLanguageStatRows puts every row in its language, a row with no stamp
// in its template language, sums the rows of each language and orders the
// groups by language name, the group with no language last
func mergeLanguageStatRows(rows []languageStatRow) []model.CampaignGroupStat {
	byCode := map[string]*model.CampaignGroupStat{}
	order := []string{}
	for _, row := range rows {
		code := ""
		if row.Language != nil && *row.Language != "" {
			code = *row.Language
		} else if row.TemplateLanguage != nil {
			code = *row.TemplateLanguage
		}
		stat, ok := byCode[code]
		if !ok {
			group := UnspecifiedLanguageGroup
			if code != "" {
				group = data.LanguageName(code)
			}
			stat = &model.CampaignGroupStat{Group: group, GroupID: code}
			byCode[code] = stat
			order = append(order, code)
		}
		stat.Total += row.Total
		stat.Clicked += row.Clicked
		stat.Submitted += row.Submitted
		stat.Reported += row.Reported
		stat.TrainingStarted += row.TrainingStarted
		stat.TrainingCompleted += row.TrainingCompleted
	}
	result := make([]model.CampaignGroupStat, 0, len(order))
	for _, code := range order {
		result = append(result, *byCode[code])
	}
	// by language name, so the order tells nothing of the group sizes, and
	// the unspecified group last
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i].GroupID == "", result[j].GroupID == ""
		if a != b {
			return b
		}
		if result[i].Group != result[j].Group {
			return result[i].Group < result[j].Group
		}
		return result[i].GroupID < result[j].GroupID
	})
	return result
}

// HasLanguageGroupDataByCampaignID reports whether the campaign's recipients
// are in more than one language, a row with no stamp counting as its template
// language, or its language routes and default pick more than one language.
// Routing by the Language field counts as more than one language, as each
// recipient brings their own.
func (r *Campaign) HasLanguageGroupDataByCampaignID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	var direct int64
	res := r.DB.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM campaigns c WHERE c.id = ? AND c.language_route_by = 'language'`,
		id.String(),
	).Scan(&direct)
	if res.Error != nil {
		return false, res.Error
	}
	if direct > 0 {
		return true, nil
	}
	var routed int64
	res = r.DB.WithContext(ctx).Raw(`SELECT COUNT(*) FROM (
		SELECT lr.language AS language FROM campaign_language_routes lr
		WHERE lr.campaign_id = ?
		UNION
		SELECT c.language_default AS language FROM campaigns c
		WHERE c.id = ? AND c.language_route_by <> '' AND c.language_default <> ''
	)`, id.String(), id.String()).Scan(&routed)
	if res.Error != nil {
		return false, res.Error
	}
	if routed > 1 {
		return true, nil
	}
	// the read stops once a second language is found. With one language every
	// row of the campaign is read, which is fine at the recipient cap
	var used int64
	res = r.DB.WithContext(ctx).Raw(`SELECT COUNT(*) FROM (
		SELECT DISTINCT COALESCE(cr.language, t.language, '') AS language
		FROM campaign_recipients cr
		JOIN campaigns c ON c.id = cr.campaign_id
		LEFT JOIN campaign_templates t ON t.id = COALESCE(cr.campaign_template_id, c.campaign_template_id)
		WHERE cr.campaign_id = ?
		LIMIT 2
	)`, id.String()).Scan(&used)
	if res.Error != nil {
		return false, res.Error
	}
	return used > 1, nil
}

// HasTemplateGroupDataByCampaignID reports whether the campaign's recipients use
// more than one template, or its pool holds more than one
func (r *Campaign) HasTemplateGroupDataByCampaignID(
	ctx context.Context,
	id *uuid.UUID,
) (bool, error) {
	var poolCount int64
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignTemplatePool{}).
		Where("campaign_id = ?", id.String()).
		Count(&poolCount)
	if res.Error != nil {
		return false, res.Error
	}
	if poolCount > 1 {
		return true, nil
	}
	// at most two distinct templates are read, which is enough to answer
	used := r.DB.
		Table("campaign_recipients AS cr").
		Select("DISTINCT COALESCE(cr.campaign_template_id, c.campaign_template_id) AS template_id").
		Joins("JOIN campaigns c ON c.id = cr.campaign_id").
		Where("cr.campaign_id = ?", id.String()).
		Where("COALESCE(cr.campaign_template_id, c.campaign_template_id) IS NOT NULL").
		Limit(2)
	var count int64
	res = r.DB.WithContext(ctx).
		Table("(?) AS used", used).
		Count(&count)
	if res.Error != nil {
		return false, res.Error
	}
	return count > 1, nil
}
