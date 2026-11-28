package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var reportTemplateAllowedColumns = assignTableToColumns(database.REPORT_TEMPLATE_TABLE, []string{
	"created_at",
	"updated_at",
})

// ReportTemplateOption is for query options
type ReportTemplateOption struct {
	*vo.QueryArgs
	WithCompany bool
}

// ReportTemplate is a report template repository
type ReportTemplate struct {
	DB *gorm.DB
}

func (r *ReportTemplate) load(options *ReportTemplateOption, db *gorm.DB) *gorm.DB {
	if options != nil && options.WithCompany {
		db = db.Joins("Company")
	}
	return db
}

// Insert inserts a report template
func (r *ReportTemplate) Insert(
	ctx context.Context,
	tmpl *model.ReportTemplate,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := tmpl.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.ReportTemplate{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets report templates
func (r *ReportTemplate) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ReportTemplateOption,
) (*model.Result[model.ReportTemplate], error) {
	result := model.NewEmptyResult[model.ReportTemplate]()
	var rows []database.ReportTemplate
	db := r.load(options, r.DB)
	db = whereCompany(db, database.REPORT_TEMPLATE_TABLE, companyID)
	db, err := useQuery(db, database.REPORT_TEMPLATE_TABLE, options.QueryArgs, reportTemplateAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if res := withPeek(db, options.QueryArgs).Find(&rows); res.Error != nil {
		return result, res.Error
	}
	hasNextPage := trimPeek(&rows, options.QueryArgs)
	result.HasNextPage = hasNextPage
	for _, row := range rows {
		tmpl, err := ToReportTemplate(&row)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, tmpl)
	}
	return result, nil
}

// GetByID gets a report template by id
func (r *ReportTemplate) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *ReportTemplateOption,
) (*model.ReportTemplate, error) {
	db := r.load(options, r.DB)
	var row database.ReportTemplate
	res := db.
		Where(TableColumnID(database.REPORT_TEMPLATE_TABLE)+" = ?", id).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToReportTemplate(&row)
}

// GetForCampaign resolves the campaign report template of a kind for a company:
// company specific first, then global (NULL company_id). Returns
// gorm.ErrRecordNotFound if neither exists.
func (r *ReportTemplate) GetForCampaign(
	ctx context.Context,
	companyID *uuid.UUID,
	isTraining bool,
) (*model.ReportTemplate, error) {
	return r.getForKind(ctx, companyID, isTraining, false)
}

// GetForProgram resolves the program report template for a company: company
// specific first, then global. Returns gorm.ErrRecordNotFound if neither exists.
func (r *ReportTemplate) GetForProgram(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.ReportTemplate, error) {
	return r.getForKind(ctx, companyID, false, true)
}

// getForKind reads the template of a kind, the company one before the global one
func (r *ReportTemplate) getForKind(
	ctx context.Context,
	companyID *uuid.UUID,
	isTraining bool,
	isProgram bool,
) (*model.ReportTemplate, error) {
	kind := func(db *gorm.DB) *gorm.DB {
		return db.
			Where(clause.Eq{Column: clause.Column{Table: database.REPORT_TEMPLATE_TABLE, Name: "is_training"}, Value: isTraining}).
			Where(clause.Eq{Column: clause.Column{Table: database.REPORT_TEMPLATE_TABLE, Name: "is_program"}, Value: isProgram})
	}
	var row database.ReportTemplate
	if companyID != nil {
		res := kind(r.DB.WithContext(ctx)).
			Where(clause.Eq{Column: clause.Column{Table: database.REPORT_TEMPLATE_TABLE, Name: "company_id"}, Value: companyID}).
			First(&row)
		if res.Error == nil {
			return ToReportTemplate(&row)
		}
		if !errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return nil, res.Error
		}
	}
	res := kind(whereCompanyIsNull(r.DB.WithContext(ctx), database.REPORT_TEMPLATE_TABLE)).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToReportTemplate(&row)
}

// UpdateByID updates a report template
func (r *ReportTemplate) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	tmpl *model.ReportTemplate,
) error {
	row := tmpl.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.ReportTemplate{}).
		Where(TableColumnID(database.REPORT_TEMPLATE_TABLE)+" = ?", id).
		Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a report template by id
func (r *ReportTemplate) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := r.DB.Delete(&database.ReportTemplate{}, id)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// ToReportTemplate converts a database row to a model
func ToReportTemplate(row *database.ReportTemplate) (*model.ReportTemplate, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	c, err := vo.NewOptionalString1MB(row.Content)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	content := nullable.NewNullableWithValue(*c)
	return &model.ReportTemplate{
		ID:         id,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		CompanyID:  companyID,
		Content:    content,
		IsTraining: nullable.NewNullableWithValue(row.IsTraining),
		IsProgram:  nullable.NewNullableWithValue(row.IsProgram),
	}, nil
}

// UpgradeGlobalProgramDefault replaces the content of each global program
// report template whose content hash is in earlier with content. An edited
// global template and every company template are left as they are.
func (r *ReportTemplate) UpgradeGlobalProgramDefault(
	ctx context.Context,
	earlier map[string]bool,
	hash func(string) string,
	content string,
) error {
	return r.upgradeGlobalDefault(ctx, true, false, earlier, hash, content)
}

// UpgradeGlobalCampaignDefault replaces the content of each global campaign
// report template of the kind, training or simulation, whose content hash is
// in earlier with content. An edited global template, every company
// template and the program templates are left as they are.
func (r *ReportTemplate) UpgradeGlobalCampaignDefault(
	ctx context.Context,
	isTraining bool,
	earlier map[string]bool,
	hash func(string) string,
	content string,
) error {
	return r.upgradeGlobalDefault(ctx, false, isTraining, earlier, hash, content)
}

func (r *ReportTemplate) upgradeGlobalDefault(
	ctx context.Context,
	isProgram bool,
	isTraining bool,
	earlier map[string]bool,
	hash func(string) string,
	content string,
) error {
	var rows []database.ReportTemplate
	db := r.DB.
		WithContext(ctx).
		Model(&database.ReportTemplate{}).
		Where(clause.Eq{Column: clause.Column{Name: "company_id"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "is_program"}, Value: isProgram})
	if !isProgram {
		db = db.Where(clause.Eq{Column: clause.Column{Name: "is_training"}, Value: isTraining})
	}
	res := db.Find(&rows)
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	for _, row := range rows {
		if row.ID == nil || !earlier[hash(row.Content)] {
			continue
		}
		res = r.DB.
			WithContext(ctx).
			Model(&database.ReportTemplate{}).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: row.ID}).
			Updates(map[string]any{"content": content, "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}
