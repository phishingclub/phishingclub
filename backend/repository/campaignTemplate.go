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
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
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

	WithCompany           bool
	WithDomain            bool
	WithLandingPage       bool
	WithBeforeLandingPage bool
	WithAfterLandingPage  bool
	WithEmail             bool
	WithSMTPConfiguration bool
	WithAPISender         bool
	// url and cookie keys
	WithIdentifier bool
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

	res := r.DB.
		Model(&database.CampaignTemplate{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
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
	return ToCampaignTemplate(&tmpl)
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
	res := db.Find(&tmpl)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TEMPLATE_TABLE,
		options.QueryArgs,
		allowdCampaignTemplatesColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
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
	res := db.Find(&tmpl)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.CAMPAIGN_TEMPLATE_TABLE,
		options.QueryArgs,
		allowdCampaignTemplatesColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
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

// GetByPageID gets all campaign templates that uses a page ID
// in before, landing or after page.
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
	db = db.Where(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "landing_page_id"),
		),
		pageID.String(),
	).Or(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "before_landing_page_id"),
		),
		pageID.String(),
	).Or(
		fmt.Sprintf(
			"%s = ?",
			TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "after_landing_page_id"),
		),
		pageID.String(),
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

// RemovePageIDFromAll removes the page ID from any matching columns
// landing_page_id, before_landing_page_id and after_landing_page_id
func (r *CampaignTemplate) RemovePageIDFromAll(
	ctx context.Context,
	pageID *uuid.UUID,
) error {
	columns := []string{"before_landing_page_id", "after_landing_page_id", "landing_page_id"}
	for _, column := range columns {
		row := map[string]any{}
		AddUpdatedAt(row)
		row[column] = nil
		row["is_usable"] = false
		res := r.DB.
			Model(&database.CampaignTemplate{}).
			Where(
				fmt.Sprintf(
					"%s = ?",
					TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, column),
				),
				pageID.String(),
			).
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
	row := campaignTemplate.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.CampaignTemplate{}).
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.CAMPAIGN_TEMPLATE_TABLE),
			),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a campaign template by id
func (r *CampaignTemplate) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", id).
		Delete(&database.CampaignTemplate{})
	if res.Error != nil {
		return res.Error
	}
	return nil
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

	return &model.CampaignTemplate{
		ID:                          id,
		CompanyID:                   companyID,
		Name:                        name,
		DomainID:                    domainID,
		Domain:                      domain,
		BeforeLandingPageID:         beforeLandingPageID,
		BeforeLandingePage:          beforeLandingPage,
		LandingPageID:               landingPageID,
		LandingPage:                 landingPage,
		AfterLandingPageID:          afterLandingPageID,
		AfterLandingPage:            afterLandingPage,
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
		IsUsable:                    isUsable,
	}, nil
}
