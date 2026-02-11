package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var allowDenyAllowColumns = assignTableToColumns(database.ALLOW_DENY_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"cidr",
	"allowed",
})

type AllowDenyOption struct {
	Fields []string
	*vo.QueryArgs
}

// AllowDeny is a repository for allow deny lists
type AllowDeny struct {
	DB *gorm.DB
}

// Insert inserts a new allow deny list
func (r *AllowDeny) Insert(
	ctx context.Context,
	conf *model.AllowDeny,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := conf.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.Model(&database.AllowDeny{}).Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets all allow deny lists
func (r *AllowDeny) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *AllowDenyOption,
) (*model.Result[model.AllowDeny], error) {
	result := model.NewEmptyResult[model.AllowDeny]()
	db := withCompanyIncludingNullContext(r.DB, companyID, "allow_denies")
	db, err := useQuery(db, database.ALLOW_DENY_TABLE, options.QueryArgs, allowDenyAllowColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var rows []*database.AllowDeny
	if options.Fields != nil {
		db = db.Select(strings.Join(options.Fields, ","))
	}
	res := db.
		Find(&rows)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.ALLOW_DENY_TABLE,
		options.QueryArgs,
		allowDenyAllowColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, ad := range rows {
		row := ToAllowDeny(ad)
		result.Rows = append(result.Rows, row)
	}

	return result, nil
}

// GetAllByCompanyID gets all allow deny lists by company id
func (r *AllowDeny) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *AllowDenyOption,
) (*model.Result[model.AllowDeny], error) {
	results := model.NewEmptyResult[model.AllowDeny]()
	db := whereCompany(r.DB, database.ALLOW_DENY_TABLE, companyID)
	db, err := useQuery(db, database.ALLOW_DENY_TABLE, options.QueryArgs, allowDenyAllowColumns...)
	if err != nil {
		return results, errs.Wrap(err)
	}
	var rows []*database.AllowDeny
	res := db.
		Find(&rows)

	if res.Error != nil {
		return results, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.ALLOW_DENY_TABLE, options.QueryArgs, allowDenyAllowColumns...)
	if err != nil {
		return results, errs.Wrap(err)
	}
	results.HasNextPage = hasNextPage

	for _, ad := range rows {
		results.Rows = append(results.Rows, ToAllowDeny(ad))
	}

	return results, nil
}

// GetByID gets an existing allow deny list
func (r *AllowDeny) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	option *AllowDenyOption,
) (*model.AllowDeny, error) {

	var row database.AllowDeny
	res := r.DB.Where("id = ?", id).First(&row)

	if res.Error != nil {
		return nil, res.Error
	}

	return ToAllowDeny(&row), nil
}

// Update updates an existing allow deny list
func (r *AllowDeny) Update(
	ctx context.Context,
	id uuid.UUID,
	conf *model.AllowDeny,
) error {
	row := conf.ToDBMap()
	AddUpdatedAt(row)

	res := r.DB.Model(&database.AllowDeny{}).
		Where("id = ?", id).
		Updates(row)

	return res.Error
}

// Delete deletes an existing allow deny list
func (r *AllowDeny) Delete(
	ctx context.Context,
	id uuid.UUID,
) error {
	res := r.DB.Model(&database.AllowDeny{}).
		Where("id = ?", id).
		Delete(&database.AllowDeny{})

	return res.Error
}

func ToAllowDeny(row *database.AllowDeny) *model.AllowDeny {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	cidrs := vo.IPNetSlice{}
	for _, cidr := range strings.Split(row.Cidrs, "\n") {
		if len(cidr) == 0 {
			continue
		}
		cidr := *vo.NewIPNetMust(cidr)
		cidrs = append(cidrs, cidr)
	}
	cidrsNullable := nullable.NewNullableWithValue(cidrs)

	ja4Fingerprints := nullable.NewNullableWithValue(row.JA4Fingerprints)
	countryCodes := nullable.NewNullableWithValue(row.CountryCodes)
	headers := nullable.NewNullableWithValue(row.Headers)

	return &model.AllowDeny{
		ID:              id,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Name:            name,
		Cidrs:           cidrsNullable,
		JA4Fingerprints: ja4Fingerprints,
		CountryCodes:    countryCodes,
		Headers:         headers,
		Allowed:         nullable.NewNullableWithValue(row.Allowed),
		CompanyID:       companyID,
	}
}
