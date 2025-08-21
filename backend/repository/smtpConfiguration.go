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

var allowedSMTPConfigurationsSortBy = assignTableToColumns(database.SMTP_CONFIGURATION_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"host",
	"port",
	"username",
})

// SMTPConfigurationOption is options for preloading
type SMTPConfigurationOption struct {
	*vo.QueryArgs

	WithCompany bool
	WithHeaders bool
}

// SMTPConfiguration is a SMTP configuration repository
type SMTPConfiguration struct {
	DB *gorm.DB
}

// preload applies the preloading options
func (r SMTPConfiguration) preload(o *SMTPConfigurationOption, db *gorm.DB) *gorm.DB {
	if o == nil {
		return db
	}
	if o.WithCompany {
		db = db.Preload("Company")
	}
	if o.WithHeaders {
		db = db.Preload("Headers", func(db *gorm.DB) *gorm.DB {
			return db.Order("smtp_headers.key ASC")
		})
	}
	return db
}

// Insert inserts a new SMTP configuration
func (r *SMTPConfiguration) Insert(
	ctx context.Context,
	conf *model.SMTPConfiguration,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := conf.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.SMTPConfiguration{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetByID gets a SMTP configuration by ID
func (r *SMTPConfiguration) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	option *SMTPConfigurationOption,
) (*model.SMTPConfiguration, error) {
	db := r.preload(option, r.DB)
	dbSMTP := &database.SMTPConfiguration{}

	res := db.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.SMTP_CONFIGURATION_TABLE),
			),
			id.String(),
		).
		First(&dbSMTP)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToSMTPConfiguration(dbSMTP), nil
}

// GetAllByCompanyID gets SMTP configurations by company ID
func (r *SMTPConfiguration) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *SMTPConfigurationOption,
) (*model.Result[model.SMTPConfiguration], error) {
	result := model.NewEmptyResult[model.SMTPConfiguration]()
	db := r.preload(options, r.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.SMTP_CONFIGURATION_TABLE)
	db, err := useQuery(
		db,
		database.SMTP_CONFIGURATION_TABLE,
		options.QueryArgs,
		allowedSMTPConfigurationsSortBy...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbSMTPs := []database.SMTPConfiguration{}
	res := db.Find(&dbSMTPs)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.SMTP_CONFIGURATION_TABLE,
		options.QueryArgs,
		allowedSMTPConfigurationsSortBy...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbSMTP := range dbSMTPs {
		result.Rows = append(result.Rows, ToSMTPConfiguration(&dbSMTP))
	}
	return result, nil
}

// GetByNameAndCompanyID gets a SMTP configuration by name
func (r *SMTPConfiguration) GetByNameAndCompanyID(
	ctx context.Context,
	name *vo.String127,
	companyID *uuid.UUID, // can be null
	option *SMTPConfigurationOption,
) (*model.SMTPConfiguration, error) {
	db := r.preload(option, r.DB)
	dbSMTP := &database.SMTPConfiguration{}
	whereCompany := fmt.Sprintf(
		"%s IS NULL",
		TableColumn(database.SMTP_CONFIGURATION_TABLE, "company_id"),
	)
	if companyID != nil {
		whereCompany = fmt.Sprintf(
			"%s = ?",
			TableColumn(database.SMTP_CONFIGURATION_TABLE, "company_id"),
		)
	}
	res := db.
		Where(
			fmt.Sprintf(
				"%s = ? AND %s",
				TableColumnName(database.SMTP_CONFIGURATION_TABLE),
				whereCompany,
			),
			name.String(),
			companyID,
		).
		First(&dbSMTP)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToSMTPConfiguration(dbSMTP), nil
}

// UpdateByID updates a SMTP configuration by ID
func (r *SMTPConfiguration) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	conf *model.SMTPConfiguration,
) error {
	row := conf.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.
		Model(&database.SMTPConfiguration{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// AddHeader adds a header to a SMTP configuration
func (r *SMTPConfiguration) AddHeader(
	ctx context.Context,
	header *model.SMTPHeader,
) (*uuid.UUID, error) {
	id := uuid.New()
	updateMap := header.ToDBMap()
	updateMap["id"] = id
	AddTimestamps(updateMap)

	res := r.DB.
		Model(&database.SMTPHeader{}).
		Create(updateMap)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// RemoveHeader removes a header from a SMTP configuration
func (r *SMTPConfiguration) RemoveHeader(
	ctx context.Context,
	headerID *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", headerID).
		Delete(&database.SMTPHeader{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a SMTP configuration by ID
// including all headers attached to it
func (r *SMTPConfiguration) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	// delete headers
	res := r.DB.
		Where("smtp_configuration_id = ?", id).
		Delete(&database.SMTPHeader{})

	if res.Error != nil {
		return res.Error
	}
	// delete smtp
	res = r.DB.
		Where("id = ?", id).
		Delete(&database.SMTPConfiguration{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

func ToSMTPConfiguration(
	row *database.SMTPConfiguration,
) *model.SMTPConfiguration {
	headers := []*model.SMTPHeader{}
	for _, header := range row.Headers {
		k := vo.NewString127Must(header.Key)
		key := nullable.NewNullableWithValue(*k)
		v := vo.NewString255Must(header.Value)
		value := nullable.NewNullableWithValue(*v)
		headers = append(headers, &model.SMTPHeader{
			ID:        *header.ID,
			CreatedAt: header.CreatedAt,
			UpdatedAt: header.UpdatedAt,
			Key:       key,
			Value:     value,
			SmtpID:    nullable.NewNullableWithValue(*header.SMTPConfigurationID),
		})
	}
	id := nullable.NewNullableWithValue(row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	host := nullable.NewNullableWithValue(*vo.NewString255Must(row.Host))
	port := nullable.NewNullableWithValue(*vo.NewPortMust(row.Port))
	username := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.Username))
	password := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(row.Password))
	ignoreCertErrors := nullable.NewNullableWithValue(row.IgnoreCertErrors)

	return &model.SMTPConfiguration{
		ID:               id,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		CompanyID:        companyID,
		Name:             name,
		Host:             host,
		Port:             port,
		Username:         username,
		Password:         password,
		IgnoreCertErrors: ignoreCertErrors,
		Headers:          headers,
	}
}
