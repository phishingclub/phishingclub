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

var proxyAllowedColumns = assignTableToColumns(database.PROXY_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"start_url",
})

// ProxyOption is for eager loading
type ProxyOption struct {
	Fields []string
	*vo.QueryArgs
	WithCompany bool
}

// Proxy is a proxy repository
type Proxy struct {
	DB *gorm.DB
}

// load preloads the table relations
func (m *Proxy) load(
	options *ProxyOption,
	db *gorm.DB,
) *gorm.DB {
	if options.WithCompany {
		db = db.Joins("Company")
	}
	return db
}

// Insert inserts a proxy
func (m *Proxy) Insert(
	ctx context.Context,
	proxy *model.Proxy,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := proxy.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := m.DB.
		Model(&database.Proxy{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets proxies
func (m *Proxy) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ProxyOption,
) (*model.Result[model.Proxy], error) {
	result := model.NewEmptyResult[model.Proxy]()
	var dbProxies []database.Proxy
	db := m.load(options, m.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.PROXY_TABLE)
	db, err := useQuery(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.Fields != nil {
		fields := assignTableToColumns(database.PROXY_TABLE, options.Fields)
		db = db.Select(strings.Join(fields, ","))
	}
	dbRes := db.
		Find(&dbProxies)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbProxy := range dbProxies {
		proxy, err := ToProxy(&dbProxy)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, proxy)
	}
	return result, nil
}

// GetAllSubset gets proxies with limited data
func (m *Proxy) GetAllSubset(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ProxyOption,
) (*model.Result[model.ProxyOverview], error) {
	result := model.NewEmptyResult[model.ProxyOverview]()
	var dbProxies []database.Proxy
	db := withCompanyIncludingNullContext(m.DB, companyID, database.PROXY_TABLE)
	db, err := useQuery(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.
		Select("id, created_at, updated_at, name, description, start_url, company_id").
		Find(&dbProxies)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbProxy := range dbProxies {
		proxyOverview := model.ProxyOverview{
			ID:          *dbProxy.ID,
			CreatedAt:   dbProxy.CreatedAt,
			UpdatedAt:   dbProxy.UpdatedAt,
			Name:        dbProxy.Name,
			Description: dbProxy.Description,
			StartURL:    dbProxy.StartURL,
			CompanyID:   dbProxy.CompanyID,
		}
		result.Rows = append(result.Rows, &proxyOverview)
	}
	return result, nil
}

// GetAllByCompanyID gets proxies by company id
func (m *Proxy) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ProxyOption,
) (*model.Result[model.Proxy], error) {
	result := model.NewEmptyResult[model.Proxy]()
	var dbProxies []database.Proxy
	db := m.load(options, m.DB)
	db = whereCompany(db, database.PROXY_TABLE, companyID)
	db, err := useQuery(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if options.Fields != nil {
		fields := assignTableToColumns(database.PROXY_TABLE, options.Fields)
		db = db.Select(strings.Join(fields, ","))
	}
	dbRes := db.
		Find(&dbProxies)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(db, database.PROXY_TABLE, options.QueryArgs, proxyAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbProxy := range dbProxies {
		proxy, err := ToProxy(&dbProxy)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, proxy)
	}
	return result, nil
}

// GetByID gets proxy by id
func (m *Proxy) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *ProxyOption,
) (*model.Proxy, error) {
	dbProxy := database.Proxy{}
	db := m.load(options, m.DB)
	result := db.
		Where(TableColumnID(database.PROXY_TABLE)+" = ?", id).
		First(&dbProxy)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToProxy(&dbProxy)
}

// GetByNameAndCompanyID gets proxy by name
func (m *Proxy) GetByNameAndCompanyID(
	ctx context.Context,
	name *vo.String64,
	companyID *uuid.UUID, // can be null
	options *ProxyOption,
) (*model.Proxy, error) {
	proxy := database.Proxy{}
	db := m.load(options, m.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.PROXY_TABLE)
	result := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumn(database.PROXY_TABLE, "name"),
			),
			name.String(),
		).
		First(&proxy)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToProxy(&proxy)
}

// UpdateByID updates a proxy by id
func (m *Proxy) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	proxy *model.Proxy,
) error {
	row := proxy.ToDBMap()
	AddUpdatedAt(row)
	res := m.DB.
		Model(&database.Proxy{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a proxy by id
func (m *Proxy) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := m.DB.Delete(&database.Proxy{}, id)

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func ToProxy(row *database.Proxy) (*model.Proxy, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))

	description, err := vo.NewOptionalString1024(row.Description)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	descriptionNullable := nullable.NewNullableWithValue(*description)

	startURL := nullable.NewNullableWithValue(*vo.NewString1024Must(row.StartURL))

	proxyConfig, err := vo.NewString1MB(row.ProxyConfig)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	proxyConfigNullable := nullable.NewNullableWithValue(*proxyConfig)

	return &model.Proxy{
		ID:          id,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		CompanyID:   companyID,
		Name:        name,
		Description: descriptionNullable,
		StartURL:    startURL,
		ProxyConfig: proxyConfigNullable,
	}, nil
}
