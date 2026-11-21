package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
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

var allowdCols = []string{
	"created_at",
	"updated_at",
	"first_name",
	"last_name",
	"email",
	"phone",
	"extra_identifier",
	"position",
	"department",
	"city",
	"country",
	"misc",
}

// base columns with table prefix
var allowdRecipientColumns = assignTableToColumns(database.RECIPIENT_TABLE, allowdCols)

// special columns that don't need table prefix
var allowdGetAllColumns = append(
	allowdRecipientColumns,
	"is_repeat_offender",
)

var allowdRecipientCampaignEventColumns = utils.MergeStringSlices(
	allowedCampaginEventColumns,
	[]string{
		TableColumn(database.EVENT_TABLE, "name"),
		TableColumn(database.CAMPAIGN_TABLE, "name"),
	},
)

// RecipientOption is options for preloading
type RecipientOption struct {
	Fields []string
	*vo.QueryArgs

	WithCompany bool
	WithGroups  bool
	// ExcludeSoftDeleted filters out recipients that are SCIM soft-deleted
	// (pending prune). Used when resolving campaign targets.
	ExcludeSoftDeleted bool
}

// Recipient
type Recipient struct {
	DB               *gorm.DB
	OptionRepository *Option
	// ExportPageSize is how many recipients one page of the export reads,
	// zero uses recipientExportPageSize
	ExportPageSize int
}

func (r *Recipient) load(db *gorm.DB, options *RecipientOption) *gorm.DB {
	if options.WithCompany {
		db = db.Preload("Company")
	}
	if options.WithGroups {
		db = db.Preload("Groups", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "name").Order("name")
		})
	}
	return db
}

// RepeatOffenderMonths returns how many months back a click or submit counts
// toward a repeat offender, from the repeat_offender_months option
func (r *Recipient) RepeatOffenderMonths(ctx context.Context) (int, error) {
	opt, err := r.OptionRepository.GetByKey(ctx, data.OptionKeyRepeatOffenderMonths)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	months, err := strconv.Atoi(opt.Value.String())
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return months, nil
}

// repeatOffenderCampaigns limits campaign events to the campaigns that count
// toward a repeat offender: test and training campaigns are left out, program
// sends count like campaigns unless the program is anonymous
func (r *Recipient) repeatOffenderCampaigns() clause.Expression {
	newDB := r.DB.Session(&gorm.Session{NewDB: true})
	campaignCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	notAnonymousPrograms := newDB.
		Table(database.PROGRAM_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.PROGRAM_TABLE, Name: "id"}}}).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_TABLE, Name: "is_anonymous"}, Value: false})
	countedCampaigns := newDB.
		Table(database.CAMPAIGN_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{campaignCol("id")}}).
		Where(clause.Eq{Column: campaignCol("is_test"), Value: false}).
		Where(clause.Eq{Column: campaignCol("is_training"), Value: false}).
		Where(clause.Or(
			clause.Eq{Column: campaignCol("program_id"), Value: nil},
			clause.Expr{SQL: "? IN (?)", Vars: []interface{}{campaignCol("program_id"), notAnonymousPrograms}},
		))
	return clause.Expr{SQL: "? IN (?)", Vars: []interface{}{
		clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
		countedCampaigns,
	}}
}

// repeatOffenderIDs is a subquery of the ids of the recipients who clicked or
// submitted data in more than one counted campaign since the given time. It is
// read once for every recipient, not once per recipient row.
func (r *Recipient) repeatOffenderIDs(since time.Time) *gorm.DB {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{eventCol("recipient_id")}}).
		// the click and submit events are a small part of all events, read
		// through the campaign and event index. The unary plus keeps the time
		// window, which holds every kind of event, off the created_at index.
		// created_at is stored as RFC3339 UTC text, so the time is compared as such
		Where(clause.Expr{SQL: "+? >= ?", Vars: []interface{}{eventCol("created_at"), utils.RFC3339UTC(since)}}).
		Where(clause.IN{Column: eventCol("event_id"), Values: repeatOffenderEventIDs()}).
		Where(r.repeatOffenderCampaigns()).
		// grouped on the value with a unary plus, so SQLite does not walk the
		// whole recipient index to get the groups in order and reads only the
		// events of the window
		Clauses(clause.GroupBy{
			Columns: []clause.Column{{Name: "+campaign_events.recipient_id", Raw: true}},
			Having: []clause.Expression{clause.Expr{
				SQL:  "COUNT(DISTINCT ?) > 1",
				Vars: []interface{}{eventCol("campaign_id")},
			}},
		})
}

// repeatOffenderCampaignCount is a subquery that counts the campaigns in which
// the outer recipient row clicked or submitted since the given time. It is
// cheap for a page of rows, repeatOffenderIDs for every row.
func (r *Recipient) repeatOffenderCampaignCount(since time.Time) *gorm.DB {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Select("COUNT(DISTINCT ?)", eventCol("campaign_id")).
		Where(clause.Eq{Column: eventCol("recipient_id"), Value: clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}}).
		Where(clause.Gte{Column: eventCol("created_at"), Value: utils.RFC3339UTC(since)}).
		Where(clause.IN{Column: eventCol("event_id"), Values: repeatOffenderEventIDs()}).
		Where(r.repeatOffenderCampaigns())
}

// repeatOffenderEventIDs are the events that count toward a repeat offender
func repeatOffenderEventIDs() []interface{} {
	return []interface{}{
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
	}
}

// isRepeatOffender is true for a recipient row that is a repeat offender
func (r *Recipient) isRepeatOffender(since time.Time) clause.Expr {
	return clause.Expr{SQL: "? IN (?)", Vars: []interface{}{
		clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
		r.repeatOffenderIDs(since),
	}}
}

// GetRepeatOffenderCount counts the repeat offenders of one company, or of
// every company and the unowned recipients when companyID is nil, the scope of
// the dashboard
func (r *Recipient) GetRepeatOffenderCount(
	ctx context.Context,
	companyID *uuid.UUID,
) (int64, error) {
	months, err := r.RepeatOffenderMonths(ctx)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	since := time.Now().AddDate(0, -months, 0)

	db := r.DB.WithContext(ctx).
		Table(database.RECIPIENT_TABLE).
		Where(r.isRepeatOffender(since)).
		// SCIM disabled (deprovisioned) recipients are left out: repeat offender
		// flags current at risk people for training, not those who have left
		Where(clause.Eq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "scim_soft_deleted_at"}, Value: nil})
	if companyID != nil {
		db = whereCompany(db, database.RECIPIENT_TABLE, companyID)
	}
	var count int64
	if err := db.Count(&count).Error; err != nil {
		return 0, errs.Wrap(err)
	}
	return count, nil
}

// GetAll gets all recipients
func (r *Recipient) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientOption,
) (*model.Result[model.RecipientView], error) {
	result := model.NewEmptyResult[model.RecipientView]()
	db := r.load(r.DB, options)

	months, err := r.RepeatOffenderMonths(ctx)
	if err != nil {
		return result, errs.Wrap(err)
	}
	repeatOffenderTimeThreshold := time.Now().AddDate(0, -months, 0)

	// every recipient column plus the computed repeat offender flag. Sorted on
	// the flag every row needs it, so the offenders are read once. Otherwise
	// only the rows of the page need it, counted per row.
	var flag interface{} = clause.Expr{SQL: "(?) > 1", Vars: []interface{}{r.repeatOffenderCampaignCount(repeatOffenderTimeThreshold)}}
	if options.QueryArgs != nil && options.QueryArgs.OrderBy == "is_repeat_offender" {
		flag = r.isRepeatOffender(repeatOffenderTimeThreshold)
	}
	baseDb := db.Table(database.RECIPIENT_TABLE).
		Select(
			"?, ? AS is_repeat_offender",
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "*", Raw: true},
			flag,
		)

	// Apply company filter
	baseDb = withCompanyIncludingNullContext(baseDb, companyID, database.RECIPIENT_TABLE)

	// Clone the base query for the actual results
	db = baseDb.Session(&gorm.Session{})

	// Apply sorting and pagination
	if options.QueryArgs != nil {
		if options.QueryArgs.OrderBy == "is_repeat_offender" {
			// is_repeat_offender is a computed column, so we need to handle it specially
			// first apply search/filters via useQuery with OrderBy/Limit/Offset cleared
			// (pagination is applied separately below)
			queryArgsNoSort := *options.QueryArgs
			queryArgsNoSort.OrderBy = ""
			queryArgsNoSort.Limit = 0
			queryArgsNoSort.Offset = 0
			var err error
			db, err = useQuery(db, database.RECIPIENT_TABLE, &queryArgsNoSort, allowdRecipientColumns...)
			if err != nil {
				return result, errs.Wrap(err)
			}
			baseDb, err = useQuery(baseDb, database.RECIPIENT_TABLE, &queryArgsNoSort, allowdRecipientColumns...)
			if err != nil {
				return result, errs.Wrap(err)
			}
			// then manually apply ORDER BY for the computed column
			if options.QueryArgs.Desc {
				db = db.Order("is_repeat_offender DESC")
				baseDb = baseDb.Order("is_repeat_offender DESC")
			} else {
				db = db.Order("is_repeat_offender ASC")
				baseDb = baseDb.Order("is_repeat_offender ASC")
			}
		} else {
			// use standard query handling for other columns
			var err error
			db, err = useQuery(db, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...)
			if err != nil {
				return result, errs.Wrap(err)
			}
			baseDb, err = useQuery(baseDb, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...)
			if err != nil {
				return result, errs.Wrap(err)
			}
		}

		// Apply pagination to main query only
		if options.QueryArgs.Limit > 0 {
			db = db.Limit(options.QueryArgs.Limit).Offset(options.QueryArgs.Offset)
		}
	}

	// Execute main query
	var dbResults []struct {
		database.Recipient
		IsRepeatOffender bool `gorm:"column:is_repeat_offender"`
	}

	if err := db.Find(&dbResults).Error; err != nil {
		return result, errs.Wrap(err)
	}

	// check for next page using a fresh count query with only filters, no limit/offset
	if options.QueryArgs != nil && options.QueryArgs.Limit > 0 {
		countDb := r.DB.Table(database.RECIPIENT_TABLE)
		countDb = r.load(countDb, options)
		countDb = withCompanyIncludingNullContext(countDb, companyID, database.RECIPIENT_TABLE)
		if options.QueryArgs != nil {
			// create a copy of QueryArgs with Limit/Offset set to zero
			queryArgsNoPagination := *options.QueryArgs
			queryArgsNoPagination.Limit = 0
			queryArgsNoPagination.Offset = 0
			// is_repeat_offender is a computed column in the SELECT query and doesn't exist
			// in the base recipients table, so we can't use it for ordering in the count query
			if queryArgsNoPagination.OrderBy == "is_repeat_offender" {
				queryArgsNoPagination.OrderBy = ""
			}
			var err error
			countDb, err = useQuery(countDb, database.RECIPIENT_TABLE, &queryArgsNoPagination, allowdRecipientColumns...)
			if err != nil {
				return result, errs.Wrap(err)
			}
		}
		var total int64
		if err := countDb.Count(&total).Error; err != nil {
			return result, errs.Wrap(err)
		}
		offset64 := int64(options.QueryArgs.Offset)
		limit64 := int64(options.QueryArgs.Limit)
		result.HasNextPage = total > (offset64 + limit64)
	}

	// Convert to view models
	for _, dbResult := range dbResults {
		recipient, err := ToRecipient(&dbResult.Recipient)
		if err != nil {
			return result, errs.Wrap(err)
		}

		recipientView := model.NewRecipientView(recipient)
		recipientView.IsRepeatOffender = dbResult.IsRepeatOffender

		result.Rows = append(result.Rows, recipientView)
	}

	return result, nil
}

// notProgramCampaign leaves out the hidden campaigns a program owns, which are
// reported through the program and may be anonymous
var notProgramCampaign = clause.Eq{Column: programColumn(database.CAMPAIGN_TABLE), Value: nil}

// GetAllCampaignEvents gets events by a recipient id
// if campaignID is nil, it retrieves all events
func (r *Recipient) GetAllCampaignEvents(
	ctx context.Context,
	recipientID *uuid.UUID,
	campaignID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.RecipientCampaignEvent], error) {
	result := model.NewEmptyResult[model.RecipientCampaignEvent]()
	db, err := useQuery(
		r.DB,
		database.CAMPAIGN_EVENT_TABLE,
		queryArgs,
		allowdRecipientCampaignEventColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbEvents []*database.RecipientCampaignEventView
	db = db.
		Table(database.CAMPAIGN_EVENT_TABLE).
		Select(
			TableSelect(
				TableColumnAll(database.CAMPAIGN_EVENT_TABLE),
				TableColumn(database.EVENT_TABLE, "name"),
				TableColumnAlias(database.CAMPAIGN_TABLE, "name", "campaign_name"),
			),
		).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_EVENT_TABLE, "recipient_id")),
			recipientID.String(),
		)
	if campaignID != nil {
		db = db.Where(
			fmt.Sprintf("%s = ?", TableColumn(database.CAMPAIGN_EVENT_TABLE, "campaign_id")),
			campaignID.String(),
		)
	}
	// a program's hidden campaigns are reported through the program
	db = withPeek(db.Where(notProgramCampaign), queryArgs)
	res := db.InnerJoins(LeftJoinOn(
		database.CAMPAIGN_EVENT_TABLE,
		"event_id",
		database.EVENT_TABLE,
		"id",
	)).
		InnerJoins(LeftJoinOn(
			database.CAMPAIGN_EVENT_TABLE,
			"campaign_id",
			database.CAMPAIGN_TABLE,
			"id",
		)).
		Find(&dbEvents)
	if res.Error != nil {
		return result, res.Error
	}
	result.HasNextPage = trimPeek(&dbEvents, queryArgs)

	for _, event := range dbEvents {
		evt, err := ToRecipientCampaignEvent(event)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, evt)
	}

	return result, nil
}

// GetOrphaned gets all recipients that are not in any group
func (r *Recipient) GetOrphaned(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientOption,
) (*model.Result[model.Recipient], error) {
	result := model.NewEmptyResult[model.Recipient]()

	// build company scope filter applied to both the recipient table and the
	// dynamic group subquery so they stay in the same scope.
	var companyFilter string
	var dynamicGroupCompanyFilter string
	var args []interface{}
	var countArgs []interface{}

	if companyID != nil {
		companyFilter = fmt.Sprintf("AND %s.company_id = ?", database.RECIPIENT_TABLE)
		dynamicGroupCompanyFilter = "AND rg.company_id = ?"
		args = append(args, companyID, companyID)
		countArgs = append(countArgs, companyID, companyID)
	} else {
		companyFilter = fmt.Sprintf("AND %s.company_id IS NULL", database.RECIPIENT_TABLE)
		dynamicGroupCompanyFilter = "AND rg.company_id IS NULL"
	}

	// A recipient is orphaned when:
	//   1. it has no row in the static many-to-many join table, AND
	//   2. it does not match any dynamic group filter in the same company scope.
	//
	// The dynamic group exclusion subquery joins recipient_groups on the
	// field/value columns so the filtering happens entirely in SQL, keeping
	// pagination and counts accurate.
	orphanCondition := fmt.Sprintf(`
		LEFT JOIN %s rgr ON %s.id = rgr.recipient_id
		WHERE rgr.recipient_id IS NULL
		AND recipients.scim_soft_deleted_at IS NULL %s
		AND %s.id NOT IN (
			SELECT DISTINCT %s.id FROM %s
			INNER JOIN %s rg
				ON rg.is_dynamic = true
				AND rg.filter_field != ''
				AND rg.filter_value != ''
				%s
				AND (
					(rg.filter_field = 'position'   AND %s.position   = rg.filter_value) OR
					(rg.filter_field = 'department' AND %s.department = rg.filter_value) OR
					(rg.filter_field = 'city'       AND %s.city       = rg.filter_value) OR
					(rg.filter_field = 'country'    AND %s.country    = rg.filter_value) OR
					(rg.filter_field = 'misc'       AND %s.misc       = rg.filter_value)
				)
		)`,
		database.RECIPIENT_GROUP_RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		companyFilter,
		database.RECIPIENT_TABLE,
		// subquery
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		database.RECIPIENT_GROUP_TABLE,
		dynamicGroupCompanyFilter,
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
	)

	query := fmt.Sprintf(
		"SELECT %s.* FROM %s %s",
		database.RECIPIENT_TABLE,
		database.RECIPIENT_TABLE,
		orphanCondition,
	)

	// apply query args for sorting/pagination if provided
	if options.QueryArgs != nil {
		if options.QueryArgs.OrderBy != "" {
			// validate orderBy against allowlist
			if !slices.Contains(allowdRecipientColumns, options.QueryArgs.OrderBy) {
				return result, fmt.Errorf("invalid order by column: %s", options.QueryArgs.OrderBy)
			}
			direction := "ASC"
			if options.QueryArgs.Desc {
				direction = "DESC"
			}
			query += fmt.Sprintf(" ORDER BY %s %s", options.QueryArgs.OrderBy, direction)
		}
		if options.QueryArgs.Limit > 0 {
			query += fmt.Sprintf(" LIMIT %d OFFSET %d", options.QueryArgs.Limit, options.QueryArgs.Offset)
		}
	}

	var rows []database.Recipient
	dbRes := r.DB.Raw(query, args...).Find(&rows)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	// check for next page using the same orphan condition so the count
	// reflects the dynamic-group exclusion too.
	if options.QueryArgs != nil && options.QueryArgs.Limit > 0 {
		countQuery := fmt.Sprintf(
			"SELECT COUNT(*) FROM %s %s",
			database.RECIPIENT_TABLE,
			orphanCondition,
		)

		var totalCount int64
		if err := r.DB.Raw(countQuery, countArgs...).Count(&totalCount).Error; err != nil {
			return result, errs.Wrap(err)
		}

		offset64 := int64(options.QueryArgs.Offset)
		limit64 := int64(options.QueryArgs.Limit)
		result.HasNextPage = totalCount > (offset64 + limit64)
	}

	for _, recipient := range rows {
		r, err := ToRecipient(&recipient)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, r)
	}

	return result, nil
}

// GetByID gets a recipient by id
func (r *Recipient) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *RecipientOption,
) (*model.Recipient, error) {
	db := r.load(r.DB, options)
	var dbRecipient database.Recipient
	res := db.
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.RECIPIENT_TABLE)),
			id,
		).
		First(&dbRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToRecipient(&dbRecipient)
}

func (r *Recipient) GetStatsByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.RecipientCampaignStatsView, error) {
	stats := &model.RecipientCampaignStatsView{}

	months, err := r.RepeatOffenderMonths(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// created_at is stored as RFC3339 UTC text, so the time is compared as such
	repeatOffenderTimeThreshold := utils.RFC3339UTC(time.Now().AddDate(0, -months, 0))

	// get phishing campaign count, training campaigns are counted separately below
	r.DB.Model(&database.CampaignRecipient{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_recipients.campaign_id").
		Where(notProgramCampaign).
		Where("campaign_recipients.recipient_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?", id, false, false).
		Distinct("campaign_recipients.campaign_id").
		Count(&stats.CampaignsParticiated)

	// get training campaigns the recipient was scheduled in
	r.DB.Model(&database.CampaignRecipient{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_recipients.campaign_id").
		Where(notProgramCampaign).
		Where("campaign_recipients.recipient_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?", id, false, true).
		Distinct("campaign_recipients.campaign_id").
		Count(&stats.TrainingsAssigned)

	// get training campaigns the recipient completed
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_TRAINING_COMPLETED],
			false,
			true,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.TrainingsCompleted)

	// get unique tracking pixels loaded
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
			false,
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsTrackingPixelLoaded)

	// get any phishing page loaded distinct by recipient and campaign
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id IN (?,?,?) AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			false,
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsPhishingPageLoaded)

	// get unique submits
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
			false,
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsDataSubmitted)

	// get unique reported campaigns
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED],
			false,
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsReported)

	// Get repeat link clicks in last selected threshold months. The recipient
	// page tiles count campaigns only, program sends show in its Programs table
	var linkClickCount int64
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Select("COUNT(DISTINCT campaign_events.campaign_id)").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id IN (?,?,?) AND campaign_events.created_at >= ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			repeatOffenderTimeThreshold,
			false,
			false,
		).
		Scan(&linkClickCount)

	// If they clicked in more than one campaign in the last x months, they're a repeat offender
	if linkClickCount > 1 {
		stats.RepeatLinkClicks = linkClickCount - 1 // Subtract 1 since we only count repeats
	} else {
		stats.RepeatLinkClicks = 0
	}

	// Get repeat submissions in last x months
	var submitCount int64
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(notProgramCampaign).
		Select("COUNT(DISTINCT campaign_events.campaign_id)").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaign_events.created_at >= ? AND campaigns.is_test = ? AND campaigns.is_training = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
			repeatOffenderTimeThreshold,
			false,
			false,
		).
		Scan(&submitCount)

	// If they submitted in more than one campaign in the last x months, they're a repeat offender
	if submitCount > 1 {
		stats.RepeatSubmissions = submitCount - 1 // Subtract 1 since we only count repeats
	} else {
		stats.RepeatSubmissions = 0
	}

	return stats, nil
}

// GetEmailByID gets a recipient by id
func (r *Recipient) GetEmailByID(
	ctx context.Context,
	id *uuid.UUID,
) (*vo.Email, error) {
	var recipient database.Recipient
	res := r.DB.
		Select(
			TableColumn(database.RECIPIENT_TABLE, "email"),
		).
		Where("id = ?", id).
		First(&recipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return vo.NewEmailMust(*recipient.Email), nil
}

// GetAllByCompanyID gets all recipients by company id
func (r *Recipient) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientOption,
) (*model.Result[model.Recipient], error) {
	result := model.NewEmptyResult[model.Recipient]()
	db := r.load(r.DB, options)
	var dbRecipients []database.Recipient
	db = whereCompany(db, database.RECIPIENT_TABLE, companyID)
	db, err := useQuery(db, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	res := withPeek(db, options.QueryArgs).Find(&dbRecipients)

	if res.Error != nil {
		return result, res.Error
	}
	result.HasNextPage = trimPeek(&dbRecipients, options.QueryArgs)

	for _, dbRecipient := range dbRecipients {
		r, err := ToRecipient(&dbRecipient)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, r)
	}
	return result, nil
}

// recipientExportPageSize is how many recipients one page of the export
// reads. The groups of a page are read with one variable per recipient, so a
// page stays far below the SQLite limit of 32766 variables.
const recipientExportPageSize = 500

// recipientPageKey is the row id and id of a recipient
type recipientPageKey struct {
	RowID int64
	ID    *uuid.UUID
}

// EachExportPageByCompanyID calls fn with the recipients of exactly the
// company, or the recipients with no company when companyID is nil, one page
// at a time in row id order. Each recipient has its groups sorted by name.
// The pages are read by row id, so a page never holds more than
// ExportPageSize recipients and the groups of a page never need more than
// that many variables. ExportPageSize zero uses recipientExportPageSize.
func (r *Recipient) EachExportPageByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	fn func([]*model.Recipient) error,
) error {
	pageSize := r.ExportPageSize
	if pageSize <= 0 {
		pageSize = recipientExportPageSize
	}
	rowID := clause.Column{Table: database.RECIPIENT_TABLE, Name: "rowid"}
	var after *int64
	for {
		keys := []recipientPageKey{}
		q := r.DB.WithContext(ctx).
			Session(&gorm.Session{NewDB: true}).
			Model(&database.Recipient{}).
			Clauses(clause.Select{Columns: []clause.Column{
				{Table: database.RECIPIENT_TABLE, Name: "rowid", Alias: "row_id"},
				{Table: database.RECIPIENT_TABLE, Name: "id"},
			}})
		q = whereCompany(q, database.RECIPIENT_TABLE, companyID)
		if after != nil {
			q = q.Where(clause.Gt{Column: rowID, Value: *after})
		}
		res := q.
			Order(clause.OrderByColumn{Column: rowID}).
			Limit(pageSize).
			Scan(&keys)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if len(keys) == 0 {
			return nil
		}
		ids := make([]any, 0, len(keys))
		for _, key := range keys {
			ids = append(ids, key.ID)
		}
		var rows []database.Recipient
		res = r.load(r.DB.WithContext(ctx), &RecipientOption{WithGroups: true}).
			Where(clause.IN{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}, Values: ids}).
			Find(&rows)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		byID := make(map[uuid.UUID]*database.Recipient, len(rows))
		for i := range rows {
			byID[*rows[i].ID] = &rows[i]
		}
		page := make([]*model.Recipient, 0, len(keys))
		for _, key := range keys {
			row, ok := byID[*key.ID]
			if !ok {
				// deleted between the two reads
				continue
			}
			recipient, err := ToRecipient(row)
			if err != nil {
				return errs.Wrap(err)
			}
			page = append(page, recipient)
		}
		if err := fn(page); err != nil {
			return err
		}
		if len(keys) < pageSize {
			return nil
		}
		last := keys[len(keys)-1].RowID
		after = &last
	}
}

// GetMaxGroupCountByCompanyID returns the most groups any one recipient of
// exactly the company is in, or of the recipients with no company when
// companyID is nil. Only groups that exist are counted.
func (r *Recipient) GetMaxGroupCountByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (int, error) {
	memberCol := func(name string) clause.Column {
		return clause.Column{Table: database.RECIPIENT_GROUP_RECIPIENT_TABLE, Name: name}
	}
	recipients := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.RECIPIENT_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.RECIPIENT_TABLE, Name: "id"}}})
	recipients = whereCompany(recipients, database.RECIPIENT_TABLE, companyID)
	groups := r.DB.Session(&gorm.Session{NewDB: true}).
		Table(database.RECIPIENT_GROUP_TABLE).
		Clauses(clause.Select{Columns: []clause.Column{{Table: database.RECIPIENT_GROUP_TABLE, Name: "id"}}})
	counts := []int{}
	res := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(database.RECIPIENT_GROUP_RECIPIENT_TABLE).
		Select("COUNT(*) AS n").
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{memberCol("recipient_id"), recipients}}).
		// the group check is a filter on the few rows of each recipient, not
		// an index probe per group
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(memberCol("recipient_group_id")), groups}}).
		Group("recipient_id").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "n"}, Desc: true}).
		Limit(1).
		Scan(&counts)
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	if len(counts) == 0 {
		return 0, nil
	}
	return counts[0], nil
}

// GetRandomByCompanyID gets a random recipient from a company, optionally excluding a specific recipient
func (r *Recipient) GetRandomByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	excludeRecipientID *uuid.UUID,
) (*model.Recipient, error) {
	var dbRecipient database.Recipient

	// build the company (and optional exclude) filter into a fresh query each call
	filtered := func() *gorm.DB {
		q := r.DB.Table(database.RECIPIENT_TABLE)
		q = whereCompany(q, database.RECIPIENT_TABLE, companyID)
		if excludeRecipientID != nil {
			q = q.Where(fmt.Sprintf("%s != ?", TableColumnID(database.RECIPIENT_TABLE)), excludeRecipientID)
		}
		return q
	}

	// pick a random rowid threshold and take the first matching row at or after it.
	// this rides the rowid index; ORDER BY RANDOM instead sorts the whole table on
	// every call, which gets slower as the recipients table grows. if the threshold
	// lands past the last match, wrap to the first matching row.
	//
	// the threshold is drawn from this company's own rowid range (min..max). a
	// threshold taken from the whole table landed past a company's last row for
	// every company except the one owning the highest rowids, so the fallback
	// kept returning that company's lowest rowid recipient far too often.
	table := database.RECIPIENT_TABLE
	companyPredicate := "company_id IS NULL"
	var predicateArgs []any
	if companyID != nil {
		companyPredicate = "company_id = ?"
		predicateArgs = []any{companyID, companyID, companyID}
	}
	thresholdExpr := fmt.Sprintf(
		"%[1]s.rowid >= ("+
			"(SELECT min(rowid) FROM %[1]s WHERE %[2]s) + "+
			"abs(random()) %% ("+
			"(SELECT max(rowid) FROM %[1]s WHERE %[2]s) - "+
			"(SELECT min(rowid) FROM %[1]s WHERE %[2]s) + 1))",
		table, companyPredicate,
	)
	res := filtered().
		Where(thresholdExpr, predicateArgs...).
		Order(table + ".rowid").
		Limit(1).
		First(&dbRecipient)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		res = filtered().Order(database.RECIPIENT_TABLE + ".rowid").Limit(1).First(&dbRecipient)
	}
	if res.Error != nil {
		return nil, res.Error
	}

	return ToRecipient(&dbRecipient)
}

func (r *Recipient) GetByEmail(
	ctx context.Context,
	email *vo.Email,
	fields ...string,
) (*model.Recipient, error) {
	var dbRecipient database.Recipient
	fields = assignTableToColumns(database.RECIPIENT_TABLE, fields)
	res := useSelect(r.DB, fields).
		Where(
			fmt.Sprintf("%s = ?", TableColumn(database.RECIPIENT_TABLE, "email")),
			email.String(),
		).
		First(&dbRecipient)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToRecipient(&dbRecipient)
}

// GetByEmailLower looks up a recipient by a case insensitive email match.
// the supplied email is compared with LOWER() on both sides so that the same
// address in a different casing (e.g. Foo@bar.tld vs foo@bar.tld) is treated as
// the same recipient. used to reject a new recipient that duplicates an
// existing one apart from casing. for resolving a specific recipient use the
// exact match GetByEmail instead.
func (r *Recipient) GetByEmailLower(
	ctx context.Context,
	email *vo.Email,
	fields ...string,
) (*model.Recipient, error) {
	var dbRecipient database.Recipient
	fields = assignTableToColumns(database.RECIPIENT_TABLE, fields)
	res := useSelect(r.DB, fields).
		Where(
			fmt.Sprintf("LOWER(%s) = LOWER(?)", TableColumn(database.RECIPIENT_TABLE, "email")),
			email.String(),
		).
		First(&dbRecipient)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToRecipient(&dbRecipient)
}

// GetByEmailLowerAndCompanyID looks up a recipient by a case-insensitive email
// match within a company. the supplied email is compared with LOWER() on both
// sides so that differently cased addresses (e.g. John@X.com vs john@x.com) are
// treated as the same recipient. used by SCIM provisioning to dedupe.
func (r *Recipient) GetByEmailLowerAndCompanyID(
	ctx context.Context,
	email *vo.Email,
	companyID *uuid.UUID,
	fields ...string,
) (*model.Recipient, error) {
	var dbRecipient database.Recipient
	emailCol := TableColumn(database.RECIPIENT_TABLE, "email")
	companyCol := TableColumn(database.RECIPIENT_TABLE, "company_id")
	q := r.DB.Where(
		fmt.Sprintf("LOWER(%s) = LOWER(?) AND %s = ?", emailCol, companyCol),
		email.String(),
		companyID,
	)
	fields = assignTableToColumns(database.RECIPIENT_TABLE, fields)
	q = useSelect(q, fields)
	res := q.First(&dbRecipient)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToRecipient(&dbRecipient)
}

func (r *Recipient) GetByEmailAndCompanyID(
	ctx context.Context,
	email *vo.Email,
	companyID *uuid.UUID,
	fields ...string,
) (*model.Recipient, error) {
	var dbRecipient database.Recipient
	q := r.DB
	if companyID == nil {
		q = q.Where(
			fmt.Sprintf(
				"%s = ? AND %s IS NULL",
				TableColumn(database.RECIPIENT_TABLE, "email"),
				TableColumn(database.RECIPIENT_TABLE, "company_id"),
			),
			email.String(),
		)
	} else {
		q = q.Where(
			fmt.Sprintf(
				"%s = ? AND %s = ?",
				TableColumn(database.RECIPIENT_TABLE, "email"),
				TableColumn(database.RECIPIENT_TABLE, "company_id"),
			),
			email.String(),
			companyID,
		)
	}
	fields = assignTableToColumns(database.RECIPIENT_TABLE, fields)
	q = useSelect(q, fields)
	res := q.
		First(&dbRecipient)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToRecipient(&dbRecipient)
}

// Insert inserts a new recipient
// there is a conflict, were if a user has email a@a.com and another has the phone number 1234
// if there is a user update by other identifier containing a@a.com and phone number, which one
// should it select? It matches two different identities. This is a conflict.
// a solution could be not allow updating if there is a conflict with two matching targets
// this solution is implemented
func (r *Recipient) Insert(
	ctx context.Context,
	recp *model.Recipient,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := recp.ToDBMap()
	// the email is the recipient identity and is stored lowercased so the same
	// address in a different casing is one recipient. only applied on create so
	// an existing stored casing is never rewritten on update
	if email, ok := row["email"].(string); ok {
		row["email"] = strings.ToLower(email)
	}
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.Model(&database.Recipient{}).Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// UpdateByID updates a recipient by id
func (r *Recipient) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	recp *model.Recipient,
) error {
	row := recp.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Recipient{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a recipient by id
func (r *Recipient) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", id).
		Delete(&database.Recipient{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// MarkScimSoftDeleted sets scim_soft_deleted_at, marking a SCIM-deprovisioned
// recipient as pending prune without deleting it. The scim_soft_deleted_at IS NULL
// guard preserves the original disable time when an IdP re-sends active=false for
// an already-disabled recipient, so the retention window is not reset each sync.
func (r *Recipient) MarkScimSoftDeleted(
	ctx context.Context,
	id *uuid.UUID,
	at time.Time,
) error {
	res := r.DB.
		Model(&database.Recipient{}).
		Where("id = ? AND scim_soft_deleted_at IS NULL", id).
		Updates(map[string]any{
			"scim_soft_deleted_at": at.UTC(),
			"updated_at":           time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// ClearScimSoftDeleted clears scim_soft_deleted_at, reviving a recipient that the
// IdP has re-provisioned.
func (r *Recipient) ClearScimSoftDeleted(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Model(&database.Recipient{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"scim_soft_deleted_at": nil,
			"updated_at":           time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetScimSoftDeletedBefore returns recipients whose scim_soft_deleted_at is older
// than the given cutoff. A nil companyID returns matches across all companies.
func (r *Recipient) GetScimSoftDeletedBefore(
	ctx context.Context,
	companyID *uuid.UUID,
	before time.Time,
) ([]*model.Recipient, error) {
	db := r.DB.
		Model(&database.Recipient{}).
		Where("scim_soft_deleted_at IS NOT NULL AND scim_soft_deleted_at < ?", before.UTC())
	if companyID != nil {
		db = db.Where("company_id = ?", companyID)
	}
	var rows []database.Recipient
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*model.Recipient, 0, len(rows))
	for i := range rows {
		m, err := ToRecipient(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, m)
	}
	return out, nil
}

func ToRecipient(row *database.Recipient) (*model.Recipient, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	firstName := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.FirstName),
	)
	lastName := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.LastName),
	)
	email := nullable.NewNullNullable[vo.Email]()
	if row.Email != nil && *row.Email != "" {
		email.Set(*vo.NewEmailMust(*row.Email))
	}
	phone := nullable.NewNullableWithValue(*vo.NewOptionalString127Must(""))
	if row.Phone != nil && *row.Phone != "" {
		phone.Set(*vo.NewOptionalString127Must(*row.Phone))
	}
	extraIdentifier := nullable.NewNullableWithValue(*vo.NewOptionalString127Must(""))
	if row.ExtraIdentifier != nil && *row.ExtraIdentifier != "" {
		extraIdentifier.Set(*vo.NewOptionalString127Must(*row.ExtraIdentifier))
	}
	position := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Position),
	)
	department := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Department),
	)
	city := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.City),
	)
	country := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Country),
	)
	misc := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Misc),
	)
	scimUserName := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.ScimUserName),
	)
	var company *model.Company
	if row.Company != nil {
		company = ToCompany(row.Company)
	}
	var groups []*model.RecipientGroup
	if row.Groups != nil && len(row.Groups) > 0 {
		for _, group := range row.Groups {
			g, err := ToRecipientGroup(&group)
			if err != nil {
				return nil, errs.Wrap(err)
			}
			groups = append(groups, g)
		}
	}

	return &model.Recipient{
		ID:                id,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		CompanyID:         companyID,
		FirstName:         firstName,
		LastName:          lastName,
		Email:             email,
		Phone:             phone,
		ExtraIdentifier:   extraIdentifier,
		Position:          position,
		Department:        department,
		City:              city,
		Country:           country,
		Misc:              misc,
		ScimUserName:      scimUserName,
		ScimSoftDeletedAt: row.ScimSoftDeletedAt,
		Company:           company,
		Groups:            nullable.NewNullableWithValue(groups),
	}, nil
}

// GetByIDs batch loads recipients by id, chunked to stay under the SQLite bind
// parameter ceiling. Used to enrich program enrollment rows with names.
func (r *Recipient) GetByIDs(
	ctx context.Context,
	ids []*uuid.UUID,
) ([]*model.Recipient, error) {
	out := []*model.Recipient{}
	if len(ids) == 0 {
		return out, nil
	}
	const chunkSize = 800
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		var rows []database.Recipient
		res := r.DB.WithContext(ctx).
			Where("id IN ?", ids[start:end]).
			Find(&rows)
		if res.Error != nil {
			return nil, errs.Wrap(res.Error)
		}
		for i := range rows {
			m, err := ToRecipient(&rows[i])
			if err != nil {
				return nil, errs.Wrap(err)
			}
			out = append(out, m)
		}
	}
	return out, nil
}
