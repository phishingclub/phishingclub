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

var domainAllowedColumns = assignTableToColumns(database.DOMAIN_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"redirect_url",
	"host_website",
})

// DomainOption is for deciding if we should load full domain entities
type DomainOption struct {
	*vo.QueryArgs
	WithCompany         bool
	ExcludeProxyDomains bool
}

// Domain is a Domain repository
type Domain struct {
	DB *gorm.DB
}

// load loads the table relations
func (r *Domain) load(db *gorm.DB) *gorm.DB {
	return db.Joins("Company")
}

// Insert inserts a new domain
func (r *Domain) Insert(
	ctx context.Context,
	domain *model.Domain,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := domain.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	result := r.DB.
		Model(&database.Domain{}).
		Create(row)
	if result.Error != nil {
		return nil, result.Error
	}
	return &id, nil
}

// GetAll gets domains
func (r *Domain) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *DomainOption,
) (*model.Result[model.Domain], error) {
	result := model.NewEmptyResult[model.Domain]()
	var dbDomains []database.Domain
	db := r.DB
	if options.WithCompany {
		db = r.load(db)
	}
	db = withCompanyIncludingNullContext(db, companyID, database.DOMAIN_TABLE)
	db, err := useQuery(db, database.DOMAIN_TABLE, options.QueryArgs, domainAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.Find(&dbDomains)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.DOMAIN_TABLE,
		options.QueryArgs,
		domainAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbDomain := range dbDomains {
		result.Rows = append(result.Rows, ToDomain(&dbDomain))
	}
	return result, nil
}

// GetAllByCompanyID gets domains by company ID
func (r *Domain) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *DomainOption,
) (*model.Result[model.Domain], error) {
	result := model.NewEmptyResult[model.Domain]()
	var dbDomains []database.Domain
	db := r.DB
	if options.WithCompany {
		db = r.load(db)
	}
	db = whereCompany(db, database.DOMAIN_TABLE, companyID)
	db, err := useQuery(db, database.DOMAIN_TABLE, options.QueryArgs, domainAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbRes := db.Find(&dbDomains)

	hasNextPage, err := useHasNextPage(
		db,
		database.DOMAIN_TABLE,
		options.QueryArgs,
		domainAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	if dbRes.Error != nil {
		return result, dbRes.Error
	}
	for _, dbDomain := range dbDomains {
		result.Rows = append(result.Rows, ToDomain(&dbDomain))
	}
	return result, nil
}

// GetAllSubset gets a subset of domains
// options only support sorting and searching
func (r *Domain) GetAllSubset(
	ctx context.Context,
	companyID *uuid.UUID,
	options *DomainOption,
) (*model.Result[model.DomainOverview], error) {
	result := model.NewEmptyResult[model.DomainOverview]()
	db := withCompanyIncludingNullContext(r.DB, companyID, database.DOMAIN_TABLE)
	// exclude proxy domains (MITM domains) if requested
	if options.ExcludeProxyDomains {
		db = db.Where("proxy_id IS NULL")
	}
	db, err := useQuery(db, database.DOMAIN_TABLE, options.QueryArgs, domainAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}

	var dbDomains []database.Domain
	res := db.
		Omit(
			TableColumn(database.DOMAIN_TABLE, "page_content"),
			TableColumn(database.DOMAIN_TABLE, "page_not_found_content"),
		).
		Find(&dbDomains)

	if res.Error != nil {
		return nil, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.DOMAIN_TABLE, options.QueryArgs, domainAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbDomain := range dbDomains {
		result.Rows = append(result.Rows, ToDomainSubset(&dbDomain))
	}
	return result, nil
}

// GetByID gets a domain by id
func (r *Domain) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *DomainOption,
) (*model.Domain, error) {
	dbDomain := &database.Domain{}
	db := r.DB
	if options.WithCompany {
		db = r.load(db)
	}
	result := db.
		Model(&database.Domain{}).
		Where(TableColumnID(database.DOMAIN_TABLE)+" = ?", id.String()).
		First(&dbDomain)
	if result.Error != nil {
		return nil, result.Error
	}
	return ToDomain(dbDomain), nil
}

// GetByName gets a domain by name
func (r *Domain) GetByName(
	ctx context.Context,
	name *vo.String255,
	options *DomainOption,
) (*model.Domain, error) {
	db := r.DB
	dbDomain := &database.Domain{}
	if options.WithCompany {
		db = r.load(db)
	}
	result := db.
		Where(
			TableColumnName(database.DOMAIN_TABLE)+" = ?", name.String(),
		).
		First(&dbDomain)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToDomain(dbDomain), nil
}

// UpdateByID updates a domain by id
func (r *Domain) UpdateByID(
	ctx context.Context,
	domain *model.Domain,
) error {
	row := domain.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.Domain{}).
		Where(
			TableColumnID(database.DOMAIN_TABLE)+" = ?", domain.ID.MustGet()).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a domain by id
func (r *Domain) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	result := r.DB.
		Where(
			TableColumnID(database.DOMAIN_TABLE)+" = ?", id.String()).
		Delete(&database.Domain{})

	if result.Error != nil {
		return result.Error
	}
	return nil
}

// ToDomain converts a domain db row to model
func ToDomain(row *database.Domain) *model.Domain {
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	proxyID := nullable.NewNullNullable[uuid.UUID]()
	if row.ProxyID != nil {
		proxyID.Set(*row.ProxyID)
	}
	var company *model.Company
	if row.Company != nil {
		company = ToCompany(row.Company)
	}
	id := nullable.NewNullableWithValue(row.ID)
	name := nullable.NewNullableWithValue(*vo.NewString255Must(row.Name))

	// Handle domain type
	domainType := row.Type
	if domainType == "" {
		domainType = "regular"
	}
	domainTypeValue := nullable.NewNullableWithValue(*vo.NewString32Must(domainType))

	// Handle proxy target domain
	proxyTargetDomain := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.ProxyTargetDomain))

	managedTLS := nullable.NewNullableWithValue(row.ManagedTLSCerts)
	ownManagedTLS := nullable.NewNullableWithValue(row.OwnManagedTLS)
	hostWebsite := nullable.NewNullableWithValue(row.HostWebsite)
	staticPage := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.PageContent))
	staticNotFound := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.PageNotFoundContent))
	redirectURL := nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(row.RedirectURL))

	return &model.Domain{
		ID:                  id,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		Name:                name,
		Type:                domainTypeValue,
		ProxyTargetDomain:   proxyTargetDomain,
		ManagedTLS:          managedTLS,
		OwnManagedTLS:       ownManagedTLS,
		HostWebsite:         hostWebsite,
		PageContent:         staticPage,
		PageNotFoundContent: staticNotFound,
		RedirectURL:         redirectURL,
		CompanyID:           companyID,
		ProxyID:             proxyID,
		Company:             company,
	}
}

// GetByProxyID gets domains by proxy ID
func (r *Domain) GetByProxyID(
	ctx context.Context,
	proxyID *uuid.UUID,
	options *DomainOption,
) (*model.Result[model.Domain], error) {
	result := model.NewEmptyResult[model.Domain]()
	var dbDomains []database.Domain
	db := r.DB
	if options.WithCompany {
		db = r.load(db)
	}
	db = db.Where("proxy_id = ?", proxyID)
	dbRes := db.Find(&dbDomains)

	if dbRes.Error != nil {
		return result, dbRes.Error
	}

	for _, dbDomain := range dbDomains {
		result.Rows = append(result.Rows, ToDomain(&dbDomain))
	}
	return result, nil
}

// ToDomainSubset converts a domain subset from db row to model
func ToDomainSubset(dbDomain *database.Domain) *model.DomainOverview {
	domainType := dbDomain.Type
	if domainType == "" {
		domainType = "regular"
	}

	return &model.DomainOverview{
		ID:                dbDomain.ID,
		CreatedAt:         dbDomain.CreatedAt,
		UpdatedAt:         dbDomain.UpdatedAt,
		Name:              dbDomain.Name,
		Type:              domainType,
		ProxyTargetDomain: dbDomain.ProxyTargetDomain,
		HostWebsite:       dbDomain.HostWebsite,
		ManagedTLS:        dbDomain.ManagedTLSCerts,
		OwnManagedTLS:     dbDomain.OwnManagedTLS,
		RedirectURL:       dbDomain.RedirectURL,
		CompanyID:         dbDomain.CompanyID,
		ProxyID:           dbDomain.ProxyID,
	}
}
