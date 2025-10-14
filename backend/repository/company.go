package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var companyAllowedColumns = assignTableToColumns(database.COMPANY_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"comment",
})

// Company is a Company repository
type Company struct {
	DB *gorm.DB
}

// Insert inserts a new company
func (r *Company) Insert(
	ctx context.Context,
	company *model.Company,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := company.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.Company{}).
		Create(&row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetByName gets a company by name
func (r *Company) GetByName(
	ctx context.Context,
	name string,
) (*model.Company, error) {
	var dbCompany database.Company
	res := r.DB.
		Where("name = ?", name).
		First(&dbCompany)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToCompany(&dbCompany), nil
}

// GetAll gets all companies with pagination
func (r *Company) GetAll(
	ctx context.Context,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Company], error) {
	result := model.NewEmptyResult[model.Company]()
	var dbCompanies []database.Company
	db, err := useQuery(r.DB, database.COMPANY_TABLE, queryArgs, companyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.
		Find(&dbCompanies)

	if dbRes.Error != nil {
		return nil, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.COMPANY_TABLE, queryArgs, companyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbCompany := range dbCompanies {
		result.Rows = append(result.Rows, ToCompany(&dbCompany))
	}
	return result, nil
}

// GetByID gets a company by id
func (r *Company) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Company, error) {
	var dbCompany database.Company
	result := r.DB.
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.COMPANY_TABLE)),
			id.String(),
		).
		First(&dbCompany)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToCompany(&dbCompany), nil
}

// UpdateByID updates a company by id
func (r *Company) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	company *model.Company,
) error {
	row := company.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Company{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumnID(database.COMPANY_TABLE)),
			id.String(),
		).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a company
// returns the number of rows affected and an error
func (r *Company) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) (int, error) {
	result := r.DB.Delete(&database.Company{ID: *id})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func ToCompany(row *database.Company) *model.Company {
	id := nullable.NewNullableWithValue(row.ID)
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	var comment nullable.Nullable[vo.OptionalString1MB]
	if row.Comment != nil {
		comment = nullable.NewNullableWithValue(*vo.NewUnsafeOptionalString1MB(*row.Comment))
	}
	return &model.Company{
		ID:        id,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		Name:      name,
		Comment:   comment,
	}
}
