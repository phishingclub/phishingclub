package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var assetAllowedColumns = assignTableToColumns(database.ASSET_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"description",
	"path",
})

// Asset is a asset repository
type Asset struct {
	DB *gorm.DB
}

// Insert inserts a new asset
func (r *Asset) Insert(
	ctx context.Context,
	asset *model.Asset,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := asset.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.Model(&database.Asset{}).Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

func (r *Asset) GetAllByDomainAndContext(
	ctx context.Context,
	domainID *uuid.UUID,
	companyID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Asset], error) {
	result := model.NewEmptyResult[model.Asset]()
	db := r.DB
	// domain specific context
	// TODO this might need to be refactored such that both domain id and company is
	// indivuadually checked, this is important to check if roles are implemented
	if domainID != nil {
		db = db.
			Joins("left join domains on domains.id = assets.domain_id").
			Select(r.joinSelectString()).
			Where("(assets.company_id = ? OR assets.company_id IS NULL) AND domain_id = ?", companyID, domainID)
	} else {
		db.Where("assets.company_id = ?", companyID)
	}
	db, err := useQuery(db, database.ASSET_TABLE, queryArgs, assetAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}

	var dbModels []*database.Asset
	dbRes := db.
		Find(&dbModels)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.ASSET_TABLE, queryArgs, assetAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbModel := range dbModels {
		result.Rows = append(result.Rows, ToAsset(dbModel))
	}
	return result, nil
}

func (r *Asset) joinSelectString() string {
	return "assets.id AS id, assets.created_at AS created_at, assets.updated_at AS updated_at, assets.company_id AS company_id, assets.name AS name, assets.description AS description, assets.path AS path, domains.id AS domain_id, domains.name AS domain_name"
}

// GetAllByGlobalContext gets all global assets
func (r *Asset) GetAllByGlobalContext(
	ctx context.Context,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Asset], error) {
	result := model.NewEmptyResult[model.Asset]()
	var db *gorm.DB
	db, err := useQuery(r.DB, database.ASSET_TABLE, queryArgs, assetAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbModels []*database.Asset
	dbRes := db.
		Where("company_id IS NULL AND domain_id IS NULL").
		Find(&dbModels)

	if dbRes.Error != nil {
		return nil, dbRes.Error
	}
	for _, dbModel := range dbModels {
		result.Rows = append(result.Rows, ToAsset(dbModel))
	}
	return result, nil
}

// GetByPath gets an asset by file path
func (r *Asset) GetByPath(
	ctx context.Context,
	path string,
) (*model.Asset, error) {
	var dbModel database.Asset
	res := r.DB.Joins("left join domains on domains.id = assets.domain_id").
		Select("assets.*, domains.name AS domain_name").
		Where("assets.path = ?", path).
		First(&dbModel)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToAsset(&dbModel), nil
}

// GetByID gets an asset by id
func (r *Asset) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Asset, error) {
	var dbModel database.Asset
	res := r.DB.Joins("left join domains on domains.id = assets.domain_id").
		Select("assets.*, domains.name AS domain_name").
		Where("assets.id = ?", id).
		First(&dbModel)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToAsset(&dbModel), nil
}

// GetAllByCompanyID gets all assets by company id
func (r *Asset) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) ([]*model.Asset, error) {
	models := []*model.Asset{}
	dbModels := []*database.Asset{}
	res := r.DB.Model(&database.Asset{}).
		Where("company_id = ?", companyID.String()).
		Find(&dbModels)

	if res.Error != nil {
		return models, res.Error
	}
	for _, dbModel := range dbModels {
		models = append(models, ToAsset(dbModel))
	}
	return models, nil
}

// GetAllByDomainID  gets all assets by company id
func (r *Asset) GetAllByDomainID(
	ctx context.Context,
	companyID *uuid.UUID,
) ([]*model.Asset, error) {
	models := []*model.Asset{}
	dbModels := []*database.Asset{}
	res := r.DB.Model(&database.Asset{}).
		Where("domain_id = ?", companyID.String()).
		Find(&dbModels)

	if res.Error != nil {
		return models, res.Error
	}
	for _, dbModel := range dbModels {
		models = append(models, ToAsset(dbModel))
	}
	return models, nil
}

// UpdateByID updates an asset by id
func (r *Asset) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	asset *model.Asset,
) error {
	row := asset.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.Model(&database.Asset{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes an asset by id
func (r *Asset) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := r.DB.Where("id = ?", id).Delete(&database.Asset{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func ToAsset(row *database.Asset) *model.Asset {
	id := nullable.NewNullableWithValue(*row.ID)
	name := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Name),
	)
	description := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.Description),
	)
	path := nullable.NewNullableWithValue(
		*vo.NewRelativeFilePathMust(row.Path),
	)
	asset := &model.Asset{
		ID:          id,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		Name:        name,
		Description: description,
		Path:        path,
	}
	asset.CompanyID = nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		asset.CompanyID.Set(*row.CompanyID)
	}
	asset.DomainID = nullable.NewNullNullable[uuid.UUID]()
	if row.DomainID != nil {
		asset.DomainID.Set(*row.DomainID)
	}
	asset.DomainName = nullable.NewNullNullable[vo.String255]()
	if row.DomainName != "" {
		asset.DomainName.Set(*vo.NewString255Must(row.DomainName))
	}
	return asset
}
