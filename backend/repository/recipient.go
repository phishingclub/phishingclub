package repository

import (
	"context"
	"fmt"
	"strconv"
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
}

// Recipient
type Recipient struct {
	DB               *gorm.DB
	OptionRepository *Option
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

// GetRepeatOffenderCount gets the repeat offender count
func (r *Recipient) GetRepeatOffenderCount(
	ctx context.Context,
	companyID *uuid.UUID,
) (int64, error) {
	// get configured months from options
	opt, err := r.OptionRepository.GetByKey(ctx, data.OptionKeyRepeatOffenderMonths)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	months, err := strconv.Atoi(opt.Value.String())
	if err != nil {
		return 0, errs.Wrap(err)
	}
	repeatOffenderTimeThreshold := time.Now().AddDate(0, -months, 0)

	query := fmt.Sprintf(`
        SELECT COUNT(*) FROM (
            SELECT %s.id
            FROM %s
            WHERE EXISTS (
                SELECT 1
                FROM campaign_events ce
                JOIN campaigns c ON ce.campaign_id = c.id
                WHERE ce.recipient_id = %s.id
                AND ce.created_at >= ?
                AND c.is_test = false
                GROUP BY ce.recipient_id
                HAVING COUNT(DISTINCT CASE
                    WHEN ce.event_id IN (?, ?, ?) THEN ce.campaign_id
                    WHEN ce.event_id = ? THEN ce.campaign_id
                    END) > 1
            )
    `, database.RECIPIENT_TABLE, database.RECIPIENT_TABLE, database.RECIPIENT_TABLE)

	if companyID != nil {
		query += fmt.Sprintf(" AND (%s.company_id = ? OR %s.company_id IS NULL)",
			database.RECIPIENT_TABLE, database.RECIPIENT_TABLE)
		query += ") as count_query"
		var count int64
		err := r.DB.Raw(query,
			repeatOffenderTimeThreshold,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
			companyID,
		).Scan(&count).Error
		return count, err
	}

	query += fmt.Sprintf(" AND %s.company_id IS NULL) as count_query", database.RECIPIENT_TABLE)
	var count int64
	err = r.DB.Raw(query,
		repeatOffenderTimeThreshold,
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
		cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
	).Scan(&count).Error
	return count, err
}

// GetAll gets all recipients
func (r *Recipient) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RecipientOption,
) (*model.Result[model.RecipientView], error) {
	result := model.NewEmptyResult[model.RecipientView]()
	db := r.load(r.DB, options)

	// get configured months from options
	opt, err := r.OptionRepository.GetByKey(ctx, data.OptionKeyRepeatOffenderMonths)
	if err != nil {
		return result, errs.Wrap(err)
	}
	months, err := strconv.Atoi(opt.Value.String())
	if err != nil {
		return result, errs.Wrap(err)
	}
	repeatOffenderTimeThreshold := time.Now().AddDate(0, -months, 0)

	// Create view query with repeat offender computation
	query := fmt.Sprintf(`
        %s.*,
        EXISTS (
            SELECT 1
            FROM campaign_events ce
            JOIN campaigns c ON ce.campaign_id = c.id
            WHERE ce.recipient_id = %s.id
            AND ce.created_at >= ?
            AND c.is_test = false
            GROUP BY ce.recipient_id
            HAVING COUNT(DISTINCT CASE
                WHEN ce.event_id IN (?, ?, ?) THEN ce.campaign_id
                WHEN ce.event_id = ? THEN ce.campaign_id
                END) > 1
        ) as is_repeat_offender
    `, database.RECIPIENT_TABLE, database.RECIPIENT_TABLE)

	baseDb := db.Table(database.RECIPIENT_TABLE).
		Select(query,
			repeatOffenderTimeThreshold,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
		)

	// Apply company filter
	baseDb = withCompanyIncludingNullContext(baseDb, companyID, database.RECIPIENT_TABLE)

	// Clone the base query for the actual results
	db = baseDb.Session(&gorm.Session{})

	// Apply sorting and pagination
	if options.QueryArgs != nil {
		if options.QueryArgs.OrderBy == "is_repeat_offender" {
			if options.QueryArgs.Desc {
				db = db.Order("is_repeat_offender DESC")
				baseDb = baseDb.Order("is_repeat_offender DESC")
			} else {
				db = db.Order("is_repeat_offender ASC")
				baseDb = baseDb.Order("is_repeat_offender ASC")
			}
		} else {
			// Use standard query handling for other columns
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

	// Check for next page
	if options.QueryArgs != nil && options.QueryArgs.Limit > 0 {
		var total int64
		if err := baseDb.Count(&total).Error; err != nil {
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

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_EVENT_TABLE,
		queryArgs,
		allowdRecipientCampaignEventColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, event := range dbEvents {
		evt, err := ToRecipientCampaignEvent(event)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, evt)
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

	// get configured months from options
	opt, err := r.OptionRepository.GetByKey(ctx, data.OptionKeyRepeatOffenderMonths)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	months, err := strconv.Atoi(opt.Value.String())
	if err != nil {
		return nil, errs.Wrap(err)
	}
	repeatOffenderTimeThreshold := time.Now().AddDate(0, -months, 0)

	// get campaign count
	r.DB.Model(&database.CampaignRecipient{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_recipients.campaign_id").
		Where("campaign_recipients.recipient_id = ? AND campaigns.is_test = ?", id, false).
		Distinct("campaign_recipients.campaign_id").
		Count(&stats.CampaignsParticiated)

	// get unique tracking pixels loaded
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsTrackingPixelLoaded)

	// get any phishing page loaded distinct by recipient and campaign
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id IN (?,?,?) AND campaigns.is_test = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsPhishingPageLoaded)

	// get unique submits
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaigns.is_test = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
			false,
		).
		Distinct("campaign_events.campaign_id").
		Count(&stats.CampaignsDataSubmitted)

	// Get repeat link clicks in last selected threshold months
	var linkClickCount int64
	r.DB.Model(&database.CampaignEvent{}).
		Joins("JOIN campaigns ON campaigns.id = campaign_events.campaign_id").
		Select("COUNT(DISTINCT campaign_events.campaign_id)").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id IN (?,?,?) AND campaign_events.created_at >= ? AND campaigns.is_test = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_BEFORE_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_PAGE_VISITED],
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_AFTER_PAGE_VISITED],
			repeatOffenderTimeThreshold,
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
		Select("COUNT(DISTINCT campaign_events.campaign_id)").
		Where(
			"campaign_events.recipient_id = ? AND campaign_events.event_id = ? AND campaign_events.created_at >= ? AND campaigns.is_test = ?",
			id,
			cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA],
			repeatOffenderTimeThreshold,
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
	res := db.Find(&dbRecipients)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.RECIPIENT_TABLE, options.QueryArgs, allowdRecipientColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbRecipient := range dbRecipients {
		r, err := ToRecipient(&dbRecipient)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, r)
	}
	return result, nil
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
		ID:              id,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		CompanyID:       companyID,
		FirstName:       firstName,
		LastName:        lastName,
		Email:           email,
		Phone:           phone,
		ExtraIdentifier: extraIdentifier,
		Position:        position,
		Department:      department,
		City:            city,
		Country:         country,
		Misc:            misc,
		Company:         company,
		Groups:          nullable.NewNullableWithValue(groups),
	}, nil
}
