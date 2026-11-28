package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var allowdCampaignTemplatesColumns = []string{
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "created_at"),
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "updated_at"),
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "name"),
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "after_landing_page_redirect_url"),
	TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "is_usable"),
	TableColumn(database.DOMAIN_TABLE, "name"),
	TableColumn("before_landing_page", "name"),
	TableColumn("landing_page", "name"),
	TableColumn("after_landing_page", "name"),
	TableColumn(database.EMAIL_TABLE, "name"),
	TableColumn(database.SMTP_CONFIGURATION_TABLE, "name"),
	TableColumn(database.API_SENDER_TABLE, "name"),
}

type CampaignTemplateOption struct {
	*vo.QueryArgs
	Columns []string

	UsableOnly bool

	WithCompany            bool
	WithDomain             bool
	WithLandingPage        bool
	WithBeforeLandingPage  bool
	WithAfterLandingPage   bool
	WithLandingProxy       bool
	WithBeforeLandingProxy bool
	WithAfterLandingProxy  bool
	WithEmail              bool
	WithSMTPConfiguration  bool
	WithAPISender          bool
	// url and cookie keys
	WithIdentifier bool
	// WithLanguages loads the language rows. GetByID also sets the names of
	// their email and pages, GetAll leaves the names out.
	WithLanguages bool
}

// CampaignTemplate is a campaign template repository
type CampaignTemplate struct {
	DB *gorm.DB
}

// load applies the preloading options
func (r CampaignTemplate) load(o *CampaignTemplateOption, db *gorm.DB) *gorm.DB {
	if o == nil {
		return db
	}
	if o.WithCompany {
		db = db.Preload("Company")
	}
	if o.WithDomain {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOn(database.CAMPAIGN_TEMPLATE_TABLE, "domain_id", database.DOMAIN_TABLE, "id"))
		} else {
			db = db.Joins("Domain")
		}
	}
	if o.WithLandingPage {

		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"landing_page_id",
				database.PAGE_TABLE,
				"id",
				"landing_page",
			))
		} else {
			db = db.Preload("LandingPage")
		}
	}
	if o.WithBeforeLandingPage {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"before_landing_page_id",
				database.PAGE_TABLE,
				"id",
				"before_landing_page",
			))
		} else {
			db = db.Preload("BeforeLandingPage")
		}
	}
	if o.WithAfterLandingPage {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"after_landing_page_id",
				database.PAGE_TABLE,
				"id",
				"after_landing_page",
			))
		} else {
			db = db.Preload("AfterLandingPage")
		}
	}
	if o.WithLandingProxy {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"landing_proxy_id",
				database.PROXY_TABLE,
				"id",
				"landing_proxy",
			))
		} else {
			db = db.Preload("LandingProxy")
		}
	}
	if o.WithBeforeLandingProxy {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"before_landing_proxy_id",
				database.PROXY_TABLE,
				"id",
				"before_landing_proxy",
			))
		} else {
			db = db.Preload("BeforeLandingProxy")
		}
	}
	if o.WithAfterLandingProxy {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOnWithAlias(
				database.CAMPAIGN_TEMPLATE_TABLE,
				"after_landing_proxy_id",
				database.PROXY_TABLE,
				"id",
				"after_landing_proxy",
			))
		} else {
			db = db.Preload("AfterLandingProxy")
		}
	}
	if o.WithEmail {
		if len(o.Columns) > 0 {

			db = db.Joins(LeftJoinOn(database.CAMPAIGN_TEMPLATE_TABLE, "email_id", database.EMAIL_TABLE, "id"))
		} else {
			db = db.Preload("Email")

		}
	}
	if o.WithSMTPConfiguration {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOn(database.CAMPAIGN_TEMPLATE_TABLE, "smtp_configuration_id", database.SMTP_CONFIGURATION_TABLE, "id"))
		} else {
			db = db.Preload("SMTPConfiguration")

		}
	}
	if o.WithAPISender {
		if len(o.Columns) > 0 {
			db = db.Joins(LeftJoinOn(database.CAMPAIGN_TEMPLATE_TABLE, "api_sender_id", database.API_SENDER_TABLE, "id"))
		} else {
			db = db.Preload("APISender")
		}
	}
	if o.WithIdentifier {
		db = db.Preload("URLIdentifier")
		db = db.Preload("StateIdentifier")
	}
	return db
}

// Insert inserts a new campaign template
func (r *CampaignTemplate) Insert(
	ctx context.Context,
	campaignTemplate *model.CampaignTemplate,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := campaignTemplate.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	// the template and its language rows are written together
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.CampaignTemplate{}).Create(row).Error; err != nil {
			return err
		}
		if languages, err := campaignTemplate.Languages.Get(); err == nil && len(languages) > 0 {
			return replaceTemplateLanguagesTx(tx, &id, model.StagesOf(campaignTemplate), languages)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// GetByID gets a campaign template by id
func (r *CampaignTemplate) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *CampaignTemplateOption,
) (*model.CampaignTemplate, error) {
	db := r.load(options, r.DB)
	var tmpl database.CampaignTemplate
	res := db.
		Where(
			TableColumnID(database.CAMPAIGN_TEMPLATE_TABLE)+" = ?",
			id.String(),
		).
		First(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	out, err := ToCampaignTemplate(&tmpl)
	if err != nil {
		return nil, err
	}
	if options != nil && options.WithLanguages {
		languages, err := r.GetLanguagesByTemplateID(ctx, id, true)
		if err != nil {
			return nil, err
		}
		out.Languages = nullable.NewNullableWithValue(languages)
	}
	return out, nil
}

// GetByIDs gets the campaign templates with the given ids in one query. A
// template that does not exist is left out, the order is not kept.
func (r *CampaignTemplate) GetByIDs(
	ctx context.Context,
	ids []uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	out := []*model.CampaignTemplate{}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	db := r.load(options, r.DB.WithContext(ctx))
	var rows []database.CampaignTemplate
	res := db.
		Where(clause.IN{
			Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"},
			Values: values,
		}).
		Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	for i := range rows {
		tmpl, err := ToCampaignTemplate(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, tmpl)
	}
	return out, nil
}

// GetByNameAndCompanyID gets a campaign template by name and company ID
func (r *CampaignTemplate) GetByNameAndCompanyID(
	ctx context.Context,
	name string,
	companyID *uuid.UUID,
	options *CampaignTemplateOption,
) (*model.CampaignTemplate, error) {
	db := r.load(options, r.DB)
	var tmpl database.CampaignTemplate
	db = withCompanyIncludingNullContext(db, companyID, database.CAMPAIGN_TEMPLATE_TABLE)
	res := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "name"),
			),
			name,
		).
		First(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCampaignTemplate(&tmpl)
}

// GetAll gets all campaign templates
func (r *CampaignTemplate) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignTemplateOption,
) (*model.Result[model.CampaignTemplate], error) {
	result := model.NewEmptyResult[model.CampaignTemplate]()
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db = withCompanyIncludingNullContext(db, companyID, database.CAMPAIGN_TEMPLATE_TABLE)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.UsableOnly {
		db.Where(
			fmt.Sprintf("%s = ?",
				TableColumn(
					database.CAMPAIGN_TEMPLATE_TABLE,
					"is_usable",
				),
			),
			true,
		)
	}
	var tmpl []database.CampaignTemplate
	res := withPeek(db, options.QueryArgs).Find(&tmpl)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage := trimPeek(&tmpl, options.QueryArgs)
	result.HasNextPage = hasNextPage

	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, tmpl)
	}
	if options.WithLanguages {
		if err := r.fillListLanguages(ctx, result.Rows); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// fillListLanguages sets the language rows, without email and page names, on
// every template of a list page. It reads the rows of the whole page in one
// query.
func (r *CampaignTemplate) fillListLanguages(ctx context.Context, templates []*model.CampaignTemplate) error {
	ids := make([]uuid.UUID, 0, len(templates))
	for _, t := range templates {
		if id, err := t.ID.Get(); err == nil {
			ids = append(ids, id)
		}
	}
	rows, err := r.GetLanguageRowsByTemplateIDs(ctx, ids)
	if err != nil {
		return err
	}
	for _, t := range templates {
		id, err := t.ID.Get()
		if err != nil {
			continue
		}
		languages := rows[id]
		if languages == nil {
			languages = []*model.CampaignTemplateLanguage{}
		}
		t.Languages = nullable.NewNullableWithValue(languages)
	}
	return nil
}

// GetAllByCompanyID gets all campaign templates by company id
func (r *CampaignTemplate) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *CampaignTemplateOption,
) (*model.Result[model.CampaignTemplate], error) {
	result := model.NewEmptyResult[model.CampaignTemplate]()
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db = whereCompany(db, database.CAMPAIGN_TEMPLATE_TABLE, companyID)
	db, err := useQuery(
		db,
		database.CAMPAIGN_TEMPLATE_TABLE,
		options.QueryArgs,
		allowdCampaignTemplatesColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.UsableOnly {
		db.Where(
			fmt.Sprintf("%s = ?",
				TableColumn(
					database.CAMPAIGN_TEMPLATE_TABLE,
					"is_usable",
				),
			),
			true,
		)
	}
	var tmpl []database.CampaignTemplate
	res := withPeek(db, options.QueryArgs).Find(&tmpl)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage := trimPeek(&tmpl, options.QueryArgs)
	result.HasNextPage = hasNextPage

	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, tmpl)
	}
	return result, nil
}

// GetBySmtpID gets all campaign templates by smtp configuration ID
// does not support Result based return
func (r *CampaignTemplate) GetBySmtpID(
	ctx context.Context,
	smtpID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "smtp_configuration_id"),
		),
		smtpID.String(),
	)
	if options.UsableOnly {
		db.Where(
			fmt.Sprintf("%s = ?",
				TableColumn(
					database.CAMPAIGN_TEMPLATE_TABLE,
					"is_usable",
				),
			),
			true,
		)
	}
	var tmpl []database.CampaignTemplate
	res := db.Find(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// GetByAPISenderID gets all campaign templates by API sender ID
// does not support Result based return
func (r *CampaignTemplate) GetByAPISenderID(
	ctx context.Context,
	apiSenderID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "api_sender_id"),
		),
		apiSenderID.String(),
	)
	if options.UsableOnly {
		db.Where(
			fmt.Sprintf("%s = ?",
				TableColumn(
					database.CAMPAIGN_TEMPLATE_TABLE,
					"is_usable",
				),
			),
			true,
		)
	}
	var tmpl []database.CampaignTemplate
	res := db.Find(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// GetByDomainID gets all campaign templates by domain ID
func (r *CampaignTemplate) GetByDomainID(
	ctx context.Context,
	domainID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "domain_id"),
		),
		domainID.String(),
	)
	if options.UsableOnly {
		db.Where(
			fmt.Sprintf("%s = ?",
				TableColumn(
					database.CAMPAIGN_TEMPLATE_TABLE,
					"is_usable",
				),
			),
			true,
		)
	}
	var tmpl []database.CampaignTemplate
	res := db.Find(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// GetByPageID gets all campaign templates that use a page ID in before,
// landing or after page, on the template or on one of its language rows.
func (r *CampaignTemplate) GetByPageID(
	ctx context.Context,
	pageID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(r.templatesUsingContent(
		pageID,
		[]string{"before_landing_page_id", "landing_page_id", "after_landing_page_id"},
	))
	if options.UsableOnly {
		db = db.Where(clause.Eq{
			Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "is_usable"},
			Value:  true,
		})
	}
	var tmpl []database.CampaignTemplate
	res := db.Find(&tmpl)

	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// RemoveDomainIDFromAll removes the domain ID from all templates by domain ID
func (r *CampaignTemplate) RemoveDomainIDFromAll(
	ctx context.Context,
	domainID *uuid.UUID,
) error {
	row := map[string]any{}
	AddUpdatedAt(row)
	row["domain_id"] = nil
	row["is_usable"] = false
	res := r.DB.
		Model(&database.CampaignTemplate{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "domain_id"),
			),
			domainID.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveAPISenderIDFromAll removes the smtp configuration ID from all templates by smtp configuration ID
func (r *CampaignTemplate) RemoveAPISenderIDFromAll(
	ctx context.Context,
	domainID *uuid.UUID,
) error {
	row := map[string]any{}
	AddUpdatedAt(row)
	row["api_sender_id"] = nil
	row["is_usable"] = false
	res := r.DB.
		Model(&database.CampaignTemplate{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "api_sender_id"),
			),
			domainID.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// RemoveSmtpIDFromAll removes the smtp configuration ID from all templates by smtp configuration ID
func (r *CampaignTemplate) RemoveSmtpIDFromAll(
	ctx context.Context,
	domainID *uuid.UUID,
) error {
	row := map[string]any{}
	AddUpdatedAt(row)
	row["smtp_configuration_id"] = nil
	row["is_usable"] = false
	res := r.DB.
		Model(&database.CampaignTemplate{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "smtp_configuration_id"),
			),
			domainID.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// GetByEmailID gets all campaign templates that use an email ID, on the
// template or on one of its language rows
func (r *CampaignTemplate) GetByEmailID(
	ctx context.Context,
	emailID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(r.templatesUsingContent(emailID, []string{"email_id"}))
	var tmpl []database.CampaignTemplate
	res := db.Find(&tmpl)
	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, t := range tmpl {
		tmpl, err := ToCampaignTemplate(&t)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// RemoveEmailIDFromAll removes the email ID from all campaign templates and
// language rows that reference it and marks them as not usable, in one
// transaction
func (r *CampaignTemplate) RemoveEmailIDFromAll(
	ctx context.Context,
	emailID *uuid.UUID,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := map[string]any{}
		AddUpdatedAt(row)
		row["email_id"] = nil
		row["is_usable"] = false
		res := tx.
			Model(&database.CampaignTemplate{}).
			Where(clause.Eq{
				Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "email_id"},
				Value:  emailID.String(),
			}).
			Updates(row)
		if res.Error != nil {
			return res.Error
		}
		return clearLanguageContentTx(tx, emailID, []string{"email_id"})
	})
}

// RemovePageIDFromAll removes the page ID from any matching columns
// landing_page_id, before_landing_page_id and after_landing_page_id. A template
// is marked as not usable only when it loses its landing page and has no
// landing proxy, see model.CampaignTemplate.ToDBMap. A before or after page is
// optional and leaves the usable flag as it is. A language row that holds the
// page loses it and is marked as not usable when the template still has that
// stage as a page. The other language rows of a
// template that lost its own page keep their pages, the stage is only missing
// its main content. All writes are one transaction.
func (r *CampaignTemplate) RemovePageIDFromAll(
	ctx context.Context,
	pageID *uuid.UUID,
) error {
	pageColumns := []string{"before_landing_page_id", "landing_page_id", "after_landing_page_id"}
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := removeLandingFromAllTx(tx, pageID,
			[]string{"before_landing_page_id", "after_landing_page_id"},
			"landing_page_id", "landing_proxy_id",
		); err != nil {
			return err
		}
		// a template that loses its own page keeps the stage on its language
		// rows, only the rows holding the page lose it
		return clearLanguageContentTx(tx, pageID, pageColumns)
	})
}

// removeLandingFromAll sets the given optional columns and the landing column
// to null where they hold id. A template that loses its landing column and has
// nothing in the other landing column is marked as not usable. The writes are
// one transaction, so a template is never left without its landing and still
// marked as usable.
func (r *CampaignTemplate) removeLandingFromAll(
	ctx context.Context,
	id *uuid.UUID,
	optional []string,
	landing string,
	otherLanding string,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return removeLandingFromAllTx(tx, id, optional, landing, otherLanding)
	})
}

// removeLandingFromAllTx is removeLandingFromAll inside the caller's
// transaction
func removeLandingFromAllTx(
	tx *gorm.DB,
	id *uuid.UUID,
	optional []string,
	landing string,
	otherLanding string,
) error {
	res := tx.
		Model(&database.CampaignTemplate{}).
		Where(clause.Eq{Column: clause.Column{Name: landing}, Value: id.String()}).
		Where(clause.Eq{Column: clause.Column{Name: otherLanding}, Value: nil}).
		Updates(map[string]any{"is_usable": false})
	if res.Error != nil {
		return res.Error
	}
	for _, column := range append(optional, landing) {
		row := map[string]any{column: nil}
		AddUpdatedAt(row)
		res := tx.
			Model(&database.CampaignTemplate{}).
			Where(clause.Eq{Column: clause.Column{Name: column}, Value: id.String()}).
			Updates(row)
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

// UpdateByID updates a campaign template by id
func (r *CampaignTemplate) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	campaignTemplate *model.CampaignTemplate,
) error {
	_, err := r.UpdateByIDStampingNull(ctx, id, campaignTemplate, "")
	return err
}

// UpdateByIDStampingNull updates a campaign template like UpdateByID. When
// nullStamp is not empty, every recipient on this template that has no
// language, in any campaign and program, is stamped with it in the same
// transaction. It returns the number of stamped recipients.
func (r *CampaignTemplate) UpdateByIDStampingNull(
	ctx context.Context,
	id *uuid.UUID,
	campaignTemplate *model.CampaignTemplate,
	nullStamp string,
) (int64, error) {
	row := campaignTemplate.ToDBMap()
	AddUpdatedAt(row)
	stages := model.StagesOf(campaignTemplate)
	var stamped int64
	// the template row and its language rows are written together. Given
	// languages replace the stored rows, otherwise the stored rows are kept in
	// the shape of the page flow.
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored database.CampaignTemplate
		if err := tx.
			Select("before_landing_page_id", "landing_page_id", "after_landing_page_id").
			Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, Value: id.String()}).
			First(&stored).Error; err != nil {
			return err
		}
		if err := tx.
			Model(&database.CampaignTemplate{}).
			Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, Value: id.String()}).
			Updates(row).Error; err != nil {
			return err
		}
		var err error
		if languages, getErr := campaignTemplate.Languages.Get(); getErr == nil {
			err = replaceTemplateLanguagesTx(tx, id, stages, languages)
		} else if campaignTemplate.Languages.IsNull() {
			err = deleteTemplateLanguagesTx(tx, id)
		} else {
			err = syncTemplateLanguageStagesTx(tx, id, stages, removedStages(&stored, campaignTemplate))
		}
		if err != nil || nullStamp == "" {
			return err
		}
		stamped, err = stampTemplateNullLanguagesTx(tx, id, nullStamp)
		return err
	})
	return stamped, err
}

// removedStages returns the stages the update takes away as a page: the
// stage was a page before and is not one now, or it becomes a proxy. A stage
// that was empty before and stays empty keeps the pages of the language rows,
// as its main page was deleted and only the main content is missing.
func removedStages(stored *database.CampaignTemplate, updated *model.CampaignTemplate) model.TemplateStages {
	after := model.StagesOf(updated)
	removed := func(storedPage *uuid.UUID, isPage bool, proxy nullable.Nullable[uuid.UUID]) bool {
		if isPage {
			return false
		}
		_, err := proxy.Get()
		return storedPage != nil || err == nil
	}
	return model.TemplateStages{
		BeforeLandingPage: removed(stored.BeforeLandingPageID, after.BeforeLandingPage, updated.BeforeLandingProxyID),
		LandingPage:       removed(stored.LandingPageID, after.LandingPage, updated.LandingProxyID),
		AfterLandingPage:  removed(stored.AfterLandingPageID, after.AfterLandingPage, updated.AfterLandingProxyID),
	}
}

// nullStampBatch is how many campaign ids one null stamp statement binds,
// well under the SQLite bind limit
const nullStampBatch = 500

// stampTemplateNullLanguagesTx stamps code on every recipient that has no
// language and gets its content from the template id, the recipient's own
// template or else the campaign template. It returns the number of rows.
//
// The rows are reached by campaign id, so each statement seeks the campaign
// and template index and the update never walks the whole table. A row that
// holds the template itself can sit in any campaign, also one whose pool no
// longer has the template (a sent row keeps its template), so every campaign
// id is visited in pages. A row with no template of its own is only in a
// campaign whose campaign template is this one.
func stampTemplateNullLanguagesTx(tx *gorm.DB, id *uuid.UUID, code string) (int64, error) {
	now := utils.NowRFC3339UTC()
	var stamped int64
	idColumn := clause.Column{Table: database.CAMPAIGN_TABLE, Name: "id"}
	templateColumn := clause.Column{Table: database.CAMPAIGN_TABLE, Name: "campaign_template_id"}
	page := func(onlyTemplate bool, update func(ids []string) (int64, error)) error {
		last := ""
		for {
			ids := []string{}
			q := tx.Model(&database.Campaign{}).
				Where(clause.Gt{Column: idColumn, Value: last}).
				Order(clause.OrderByColumn{Column: idColumn}).
				Limit(nullStampBatch)
			if onlyTemplate {
				q = q.Where(clause.Eq{Column: templateColumn, Value: id.String()})
			}
			if err := q.Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			n, err := update(ids)
			if err != nil {
				return err
			}
			stamped += n
			if len(ids) < nullStampBatch {
				return nil
			}
			last = ids[len(ids)-1]
		}
	}
	// rows that hold the template themselves
	if err := page(false, func(ids []string) (int64, error) {
		res := tx.Exec(`UPDATE campaign_recipients
			SET language = ?, updated_at = ?
			WHERE campaign_id IN ? AND campaign_template_id = ? AND language IS NULL`,
			code, now, ids, id.String(),
		)
		return res.RowsAffected, res.Error
	}); err != nil {
		return 0, err
	}
	// rows with no template of their own in campaigns on this template
	if err := page(true, func(ids []string) (int64, error) {
		res := tx.Exec(`UPDATE campaign_recipients
			SET language = ?, updated_at = ?
			WHERE campaign_id IN ? AND campaign_template_id IS NULL AND language IS NULL`,
			code, now, ids,
		)
		return res.RowsAffected, res.Error
	}); err != nil {
		return 0, err
	}
	return stamped, nil
}

// DeleteByID deletes a campaign template by id
func (r *CampaignTemplate) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	// the language rows go with the template
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := deleteTemplateLanguagesTx(tx, id); err != nil {
			return err
		}
		return tx.
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id.String()}).
			Delete(&database.CampaignTemplate{}).Error
	})
}

func ToCampaignTemplate(row *database.CampaignTemplate) (*model.CampaignTemplate, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	domainID := nullable.NewNullNullable[uuid.UUID]()
	if row.DomainID != nil {
		domainID.Set(*row.DomainID)
	}
	var domain *model.Domain
	if row.Domain != nil {
		domain = ToDomain(row.Domain)
	}
	var beforeLandingPage *model.Page
	if row.BeforeLandingPage != nil {
		p, err := ToPage(row.BeforeLandingPage)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		beforeLandingPage = p
	}
	beforeLandingPageID := nullable.NewNullNullable[uuid.UUID]()
	if row.BeforeLandingPageID != nil {
		beforeLandingPageID.Set(*row.BeforeLandingPageID)
	}
	var beforeLandingProxy *model.Proxy
	if row.BeforeLandingProxy != nil {
		m, err := ToProxy(row.BeforeLandingProxy)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		beforeLandingProxy = m
	}
	beforeLandingProxyID := nullable.NewNullNullable[uuid.UUID]()
	if row.BeforeLandingProxyID != nil {
		beforeLandingProxyID.Set(*row.BeforeLandingProxyID)
	}
	var landingPage *model.Page
	if row.LandingPage != nil {
		p, err := ToPage(row.LandingPage)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		landingPage = p
	}
	landingPageID := nullable.NewNullNullable[uuid.UUID]()
	if row.LandingPageID != nil {
		landingPageID.Set(*row.LandingPageID)
	}
	var landingProxy *model.Proxy
	if row.LandingProxy != nil {
		m, err := ToProxy(row.LandingProxy)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		landingProxy = m
	}
	landingProxyID := nullable.NewNullNullable[uuid.UUID]()
	if row.LandingProxyID != nil {
		landingProxyID.Set(*row.LandingProxyID)
	}
	var afterLandingPage *model.Page
	if row.AfterLandingPage != nil {
		p, err := ToPage(row.AfterLandingPage)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		afterLandingPage = p
	}
	afterLandingPageID := nullable.NewNullNullable[uuid.UUID]()
	if row.AfterLandingPageID != nil {
		afterLandingPageID.Set(*row.AfterLandingPageID)
	}
	var afterLandingProxy *model.Proxy
	if row.AfterLandingProxy != nil {
		m, err := ToProxy(row.AfterLandingProxy)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		afterLandingProxy = m
	}
	afterLandingProxyID := nullable.NewNullNullable[uuid.UUID]()
	if row.AfterLandingProxyID != nil {
		afterLandingProxyID.Set(*row.AfterLandingProxyID)
	}
	redirectURL := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(""))
	if row.AfterLandingPageRedirectURL != "" {
		redirectURL.Set(*vo.NewOptionalString255Must(row.AfterLandingPageRedirectURL))
	}
	emailID := nullable.NewNullNullable[uuid.UUID]()
	if row.EmailID != nil {
		emailID.Set(*row.EmailID)
	}
	var email *model.Email
	if row.Email != nil {
		email = ToEmail(row.Email)
	}
	smtpConfigurationID := nullable.NewNullNullable[uuid.UUID]()
	if row.SMTPConfigurationID != nil {
		smtpConfigurationID.Set(*row.SMTPConfigurationID)
	}
	var smtpConfiguration *model.SMTPConfiguration
	if row.SMTPConfiguration != nil {
		smtpConfiguration = ToSMTPConfiguration(row.SMTPConfiguration)
	}
	apiSenderID := nullable.NewNullNullable[uuid.UUID]()
	if row.APISenderID != nil {
		apiSenderID.Set(*row.APISenderID)
	}
	var apiSender *model.APISender
	if row.APISender != nil {
		var err error
		apiSender, err = ToAPISender(row.APISender)
		if err != nil {
			return nil, errs.Wrap(err)
		}
	}
	urlIdentifierID := nullable.NewNullableWithValue(row.URLIdentifierID)
	var urlIdentifier *model.Identifier
	if row.URLIdentifier != nil {
		urlIdentifier = ToIdentifier(row.URLIdentifier)
	}
	stateIdentifierID := nullable.NewNullableWithValue(row.StateIdentifierID)
	var stateIdentifier *model.Identifier
	if row.StateIdentifier != nil {
		stateIdentifier = ToIdentifier(row.StateIdentifier)
	}
	urlPath := nullable.NewNullableWithValue(*vo.NewURLPathMust(row.URLPath))

	isUsable := nullable.NewNullableWithValue(row.IsUsable)
	isTraining := nullable.NewNullableWithValue(row.IsTraining)
	language := nullable.NewNullableWithValue(row.Language)

	return &model.CampaignTemplate{
		ID:                          id,
		CompanyID:                   companyID,
		Name:                        name,
		DomainID:                    domainID,
		Domain:                      domain,
		BeforeLandingPageID:         beforeLandingPageID,
		BeforeLandingePage:          beforeLandingPage,
		BeforeLandingProxyID:        beforeLandingProxyID,
		BeforeLandingProxy:          beforeLandingProxy,
		LandingPageID:               landingPageID,
		LandingPage:                 landingPage,
		LandingProxyID:              landingProxyID,
		LandingProxy:                landingProxy,
		AfterLandingPageID:          afterLandingPageID,
		AfterLandingPage:            afterLandingPage,
		AfterLandingProxyID:         afterLandingProxyID,
		AfterLandingProxy:           afterLandingProxy,
		AfterLandingPageRedirectURL: redirectURL,
		EmailID:                     emailID,
		Email:                       email,
		SMTPConfigurationID:         smtpConfigurationID,
		SMTPConfiguration:           smtpConfiguration,
		APISenderID:                 apiSenderID,
		APISender:                   apiSender,
		URLIdentifierID:             urlIdentifierID,
		URLIdentifier:               urlIdentifier,
		StateIdentifierID:           stateIdentifierID,
		StateIdentifier:             stateIdentifier,
		URLPath:                     urlPath,
		LureURLMode:                 nullable.NewNullableWithValue(row.LureURLMode),
		LureCodeAlgo:                nullable.NewNullableWithValue(row.LureCodeAlgo),
		LureCodeLength:              nullable.NewNullableWithValue(row.LureCodeLength),
		IsUsable:                    isUsable,
		IsTraining:                  isTraining,
		Language:                    language,
	}, nil
}

// RemoveProxyIDFromAll removes the proxy ID from any matching columns
// landing_proxy_id, before_landing_proxy_id and after_landing_proxy_id
// GetByProxyID gets campaign templates that use a specific proxy ID
func (r *CampaignTemplate) GetByProxyID(
	ctx context.Context,
	proxyID *uuid.UUID,
	options *CampaignTemplateOption,
) ([]*model.CampaignTemplate, error) {
	db := r.DB
	if options.Columns != nil && len(options.Columns) > 0 {
		db = db.Select(strings.Join(options.Columns, ","))
	}
	db = r.load(options, db)
	db, err := useQuery(db, database.CAMPAIGN_TEMPLATE_TABLE, options.QueryArgs, allowdCampaignTemplatesColumns...)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	db = db.Where(
		fmt.Sprintf(
			"(%s = ? OR %s = ? OR %s = ?)",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "before_landing_proxy_id"),
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "landing_proxy_id"),
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "after_landing_proxy_id"),
		),
		proxyID.String(),
		proxyID.String(),
		proxyID.String(),
	)
	if options.UsableOnly {
		db = db.Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "is_usable"),
			),
			true,
		)
	}
	var rows []*database.CampaignTemplate
	res := db.Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	templates := []*model.CampaignTemplate{}
	for _, row := range rows {
		tmpl, err := ToCampaignTemplate(row)
		if err != nil {
			return nil, err
		}
		templates = append(templates, tmpl)
	}
	return templates, nil
}

// RemoveProxyIDFromAll removes the proxy ID from any matching columns
// landing_proxy_id, before_landing_proxy_id and after_landing_proxy_id. A
// template is marked as not usable only when it loses its landing proxy and has
// no landing page, see RemovePageIDFromAll.
func (r *CampaignTemplate) RemoveProxyIDFromAll(
	ctx context.Context,
	proxyID *uuid.UUID,
) error {
	return r.removeLandingFromAll(ctx, proxyID,
		[]string{"before_landing_proxy_id", "after_landing_proxy_id"},
		"landing_proxy_id", "landing_page_id",
	)
}
