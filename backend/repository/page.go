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

var pageAllowedColumns = assignTableToColumns(database.PAGE_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

// PageOption is for eager loading
type PageOption struct {
	Fields []string
	*vo.QueryArgs
	WithCompany bool
}

// Page is a Page repository
type Page struct {
	DB *gorm.DB
}

// load preloads the table relations
func (pa *Page) load(
	options *PageOption,
	db *gorm.DB,
) *gorm.DB {
	if options.WithCompany {
		db = db.Joins("Company")
	}
	return db
}

// Insert inserts a page
func (pa *Page) Insert(
	ctx context.Context,
	page *model.Page,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := page.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := pa.DB.
		Model(&database.Page{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets pages
func (pa *Page) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	var dbPages []database.Page
	db := pa.load(options, pa.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.PAGE_TABLE)
	db, err := useQuery(db, database.PAGE_TABLE, options.QueryArgs, pageAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.Fields != nil {
		// TODO potential issue with inner join selects
		fields := assignTableToColumns(database.PAGE_TABLE, options.Fields)
		db = db.Select(strings.Join(fields, ","))
	}
	dbRes := db.
		Find(&dbPages)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.PAGE_TABLE, options.QueryArgs, pageAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbPage := range dbPages {
		page, err := ToPage(&dbPage)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, page)
	}
	return result, nil
}

// GetAllByCompanyID gets pages by company id
func (pa *Page) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *PageOption,
) (*model.Result[model.Page], error) {
	result := model.NewEmptyResult[model.Page]()
	var dbPages []database.Page
	db := pa.load(options, pa.DB)
	db = whereCompany(db, database.PAGE_TABLE, companyID)
	db, err := useQuery(db, database.PAGE_TABLE, options.QueryArgs, pageAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.Fields != nil {
		// TODO potential issue with inner join selects
		fields := assignTableToColumns(database.PAGE_TABLE, options.Fields)
		db = db.Select(strings.Join(fields, ","))
	}
	dbRes := db.
		Find(&dbPages)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.PAGE_TABLE, options.QueryArgs, pageAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbPage := range dbPages {
		page, err := ToPage(&dbPage)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, page)
	}
	return result, nil
}

// GetByID gets pages by id
func (pa *Page) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *PageOption,
) (*model.Page, error) {
	dbPage := database.Page{}
	db := pa.load(options, pa.DB)
	result := db.
		Where(TableColumnID(database.PAGE_TABLE)+" = ?", id).
		First(&dbPage)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToPage(&dbPage)
}

// GetByCompanyID gets pages by company id
func (pa *Page) GetByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *PageOption,
) (*model.Page, error) {
	dbPage := database.Page{}
	db := pa.load(options, pa.DB)
	result := db.
		Where(TableColumn(database.PAGE_TABLE, "company_id")+" = ?", companyID).
		First(&dbPage)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToPage(&dbPage)
}

// GetByNameAndCompanyID gets pages by name
func (pa *Page) GetByNameAndCompanyID(
	ctx context.Context,
	name *vo.String64,
	companyID *uuid.UUID, // can be null
	options *PageOption,
) (*model.Page, error) {
	page := database.Page{}
	db := pa.load(options, pa.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.PAGE_TABLE)
	result := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.PAGE_TABLE, "name"),
			),
			name.String(),
		).
		First(&page)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToPage(&page)
}

// UpdateByID updates a page by id
func (pa *Page) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	page *model.Page,
) error {
	row := page.ToDBMap()
	AddUpdatedAt(row)
	res := pa.DB.
		Model(&database.Page{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a page by id
func (l *Page) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := l.DB.Delete(&database.Page{}, id)

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func ToPage(row *database.Page) (*model.Page, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	c, err := vo.NewOptionalString1MB(row.Content)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	content := nullable.NewNullableWithValue(*c)

	// Handle proxy fields
	typeValue := row.Type
	if typeValue == "" {
		typeValue = "regular"
	}
	pageType := nullable.NewNullableWithValue(*vo.NewString32Must(typeValue))

	targetURL, err := vo.NewOptionalString1024(row.TargetURL)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	targetURLNullable := nullable.NewNullableWithValue(*targetURL)

	proxyConfig, err := vo.NewOptionalString1MB(row.ProxyConfig)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	proxyConfigNullable := nullable.NewNullableWithValue(*proxyConfig)

	return &model.Page{
		ID:          id,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		CompanyID:   companyID,
		Name:        name,
		Content:     content,
		Type:        pageType,
		TargetURL:   targetURLNullable,
		ProxyConfig: proxyConfigNullable,
	}, nil
}
