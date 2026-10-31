package repository

import (
	"context"
	"mime"
	"path/filepath"
	"strings"

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
	return "assets.id AS id, assets.created_at AS created_at, assets.updated_at AS updated_at, assets.company_id AS company_id, assets.name AS name, assets.description AS description, assets.path AS path, assets.content_type AS content_type, domains.id AS domain_id, domains.name AS domain_name"
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

// GetAllByCompanyContext gets all assets in a company folder
// these are assets owned by a company but not attached to a domain
func (r *Asset) GetAllByCompanyContext(
	ctx context.Context,
	companyID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Asset], error) {
	result := model.NewEmptyResult[model.Asset]()
	db, err := useQuery(r.DB, database.ASSET_TABLE, queryArgs, assetAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var dbModels []*database.Asset
	dbRes := db.
		Where("company_id = ? AND domain_id IS NULL", companyID).
		Find(&dbModels)

	if dbRes.Error != nil {
		return nil, dbRes.Error
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

// GetGlobalByPath gets the global asset (no company and no domain) stored at
// the given path. It returns nil and no error when there is none.
func (r *Asset) GetGlobalByPath(
	ctx context.Context,
	path string,
) (*model.Asset, error) {
	var dbModels []database.Asset
	res := r.DB.Model(&database.Asset{}).
		Where("path = ? AND company_id IS NULL AND domain_id IS NULL", path).
		Limit(1).
		Find(&dbModels)
	if res.Error != nil {
		return nil, res.Error
	}
	if len(dbModels) == 0 {
		return nil, nil
	}
	return ToAsset(&dbModels[0]), nil
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

// DeleteByID deletes an asset by id together with its headers
func (r *Asset) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("asset_id = ?", id).Delete(&database.AssetHeader{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&database.Asset{}).Error
	})
}

// GetHeadersByAssetID gets the response headers of an asset in the order they
// were added
func (r *Asset) GetHeadersByAssetID(
	ctx context.Context,
	assetID *uuid.UUID,
) ([]*model.AssetHeader, error) {
	rows := []*database.AssetHeader{}
	res := r.DB.
		Where("asset_id = ?", assetID).
		Order("created_at, id").
		Find(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	headers := make([]*model.AssetHeader, 0, len(rows))
	for _, row := range rows {
		headers = append(headers, ToAssetHeader(row))
	}
	return headers, nil
}

// AddHeader adds a response header to an asset
func (r *Asset) AddHeader(
	ctx context.Context,
	header *model.AssetHeader,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := header.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.
		Model(&database.AssetHeader{}).
		Create(row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// RemoveHeader removes a response header from an asset. Only a header that
// belongs to the given asset is removed, it returns gorm.ErrRecordNotFound
// when there is none.
func (r *Asset) RemoveHeader(
	ctx context.Context,
	assetID *uuid.UUID,
	headerID *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ? AND asset_id = ?", headerID, assetID).
		Delete(&database.AssetHeader{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// assetServeRow is one row of the serve settings lookup, an asset joined with
// one of its headers
type assetServeRow struct {
	AssetID     *uuid.UUID
	CompanyID   *uuid.UUID
	ContentType string
	HeaderKey   *string
	HeaderValue *string
}

// GetServeSettingsForDomainAsset gets the serve settings of the domain asset
// stored at path for the domain. path is relative to the domain asset folder.
// It returns nil and no error when there is no such asset.
func (r *Asset) GetServeSettingsForDomainAsset(
	ctx context.Context,
	domainID *uuid.UUID,
	path string,
) (*model.AssetServeSettings, error) {
	rows := []assetServeRow{}
	res := r.DB.WithContext(ctx).
		Model(&database.Asset{}).
		Select("assets.id AS asset_id, assets.company_id AS company_id, assets.content_type AS content_type, asset_headers.key AS header_key, asset_headers.value AS header_value").
		Joins("LEFT JOIN asset_headers ON asset_headers.asset_id = assets.id").
		Where("assets.domain_id = ? AND assets.path = ?", domainID, path).
		Order("assets.created_at, assets.id, asset_headers.created_at, asset_headers.id").
		Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	return toAssetServeSettings(rows, false), nil
}

// GetServeSettingsForSharedAsset gets the serve settings of the asset stored at
// path in the shared folder. A file there belongs to either a global asset,
// stored with the full path, or a company folder asset, stored under the
// folder named by the company assets key with the path after that folder.
// When both exist for the same file the company asset is used. It returns nil
// and no error when there is no such asset.
func (r *Asset) GetServeSettingsForSharedAsset(
	ctx context.Context,
	path string,
) (*model.AssetServeSettings, error) {
	match := r.DB.Where("assets.company_id IS NULL AND assets.path = ?", path)
	if companyKey, companyPath, ok := SplitCompanyAssetPath(path); ok {
		match = match.Or(
			"assets.company_id IS NOT NULL AND companies.assets_key = ? AND assets.path = ?",
			companyKey,
			companyPath,
		)
	}
	rows := []assetServeRow{}
	res := r.DB.WithContext(ctx).
		Model(&database.Asset{}).
		Select("assets.id AS asset_id, assets.company_id AS company_id, assets.content_type AS content_type, asset_headers.key AS header_key, asset_headers.value AS header_value").
		Joins("LEFT JOIN companies ON companies.id = assets.company_id").
		Joins("LEFT JOIN asset_headers ON asset_headers.asset_id = assets.id").
		Where("assets.domain_id IS NULL").
		Where(match).
		Order("assets.created_at, assets.id, asset_headers.created_at, asset_headers.id").
		Scan(&rows)
	if res.Error != nil {
		return nil, res.Error
	}
	return toAssetServeSettings(rows, true), nil
}

// SplitCompanyAssetPath splits a path in the shared folder into the company
// folder name and the path inside that folder. ok is false when the path has
// no folder part.
func SplitCompanyAssetPath(p string) (string, string, bool) {
	key, rest, ok := strings.Cut(p, "/")
	if !ok || key == "" || rest == "" {
		return "", "", false
	}
	return key, rest, true
}

// toAssetServeSettings picks one asset from the joined rows and collects its
// headers in order. With preferCompany a company asset is picked over a global
// asset, otherwise the first asset is picked.
func toAssetServeSettings(rows []assetServeRow, preferCompany bool) *model.AssetServeSettings {
	if len(rows) == 0 {
		return nil
	}
	var pick *uuid.UUID
	for _, row := range rows {
		if row.AssetID == nil {
			continue
		}
		if pick == nil {
			pick = row.AssetID
		}
		if preferCompany && row.CompanyID != nil {
			pick = row.AssetID
			break
		}
	}
	if pick == nil {
		return nil
	}
	settings := &model.AssetServeSettings{AssetID: *pick}
	for _, row := range rows {
		if row.AssetID == nil || *row.AssetID != *pick {
			continue
		}
		settings.ContentType = row.ContentType
		if row.HeaderKey != nil && row.HeaderValue != nil {
			settings.Headers = append(settings.Headers, model.AssetServeHeader{
				Key:   *row.HeaderKey,
				Value: *row.HeaderValue,
			})
		}
	}
	return settings
}

// ToAssetHeader converts a header row to a model
func ToAssetHeader(row *database.AssetHeader) *model.AssetHeader {
	h := &model.AssetHeader{
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.ID != nil {
		h.ID = *row.ID
	}
	h.AssetID = nullable.NewNullNullable[uuid.UUID]()
	if row.AssetID != nil {
		h.AssetID.Set(*row.AssetID)
	}
	if key, err := vo.NewString127(row.Key); err == nil {
		h.Key = nullable.NewNullableWithValue(*key)
	}
	if value, err := vo.NewString2048(row.Value); err == nil {
		h.Value = nullable.NewNullableWithValue(*value)
	}
	return h
}

func ToAsset(row *database.Asset) *model.Asset {
	id := nullable.NewNullableWithValue(*row.ID)
	name := nullable.NewNullableWithValue(
		*vo.NewOptionalString127Must(row.Name),
	)
	description := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.Description),
	)
	// an old row with a path the value object now refuses loads without a path
	// instead of panicking, so listings keep working.
	path := nullable.NewNullNullable[vo.RelativeFilePath]()
	if p, err := vo.NewRelativeFilePath(row.Path); err == nil {
		path = nullable.NewNullableWithValue(*p)
	}
	contentType := nullable.NewNullableWithValue(*vo.NewEmptyOptionalString255())
	if ct, err := vo.NewOptionalString255(row.ContentType); err == nil {
		contentType = nullable.NewNullableWithValue(*ct)
	}
	asset := &model.Asset{
		ID:                  id,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		Name:                name,
		Description:         description,
		Path:                path,
		ContentType:         contentType,
		DetectedContentType: mime.TypeByExtension(filepath.Ext(row.Path)),
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
