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

var apiSenderAllowedColumns = assignTableToColumns(database.API_SENDER_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

// APISenderOption is options for preloading
type APISenderOption struct {
	*vo.QueryArgs

	WithRequestHeaders  bool
	WithResponseHeaders bool
}

// APISender is a API sender repository
type APISender struct {
	DB *gorm.DB
}

// preload applies the preloading options
func (a *APISender) preload(o *APISenderOption, db *gorm.DB) *gorm.DB {
	if o == nil {
		return db
	}
	return db
}

// Insert inserts a new API sender
func (a *APISender) Insert(
	ctx context.Context,
	apiSender *model.APISender,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := apiSender.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := a.DB.
		Model(&database.APISender{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetByID gets a API sender by ID
func (a *APISender) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	option *APISenderOption,
) (*model.APISender, error) {
	db := a.preload(option, a.DB)

	dbAPISender := &database.APISender{}
	res := db.
		Where("id = ?", id).
		First(&dbAPISender)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToAPISender(dbAPISender)
}

// GetAll gets API senders
func (a *APISender) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	option *APISenderOption,
) (*model.Result[model.APISender], error) {
	result := model.NewEmptyResult[model.APISender]()
	db := a.preload(option, a.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.API_SENDER_TABLE)
	db, err := useQuery(
		db,
		database.API_SENDER_TABLE,
		option.QueryArgs,
		apiSenderAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbAPISenders := []*database.APISender{}
	res := db.Find(&dbAPISenders)
	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(
		db,
		database.API_SENDER_TABLE,
		option.QueryArgs,
		apiSenderAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, dbAPISender := range dbAPISenders {
		apiSender, err := ToAPISender(dbAPISender)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, apiSender)
	}
	return result, nil
}

// GetAllOverview gets API senders with limited data
func (a *APISender) GetAllOverview(
	ctx context.Context,
	companyID *uuid.UUID,
	option *APISenderOption,
) (*model.Result[model.APISender], error) {
	result := model.NewEmptyResult[model.APISender]()
	db := a.preload(option, a.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.API_SENDER_TABLE)
	db, err := useQuery(
		db,
		database.API_SENDER_TABLE,
		option.QueryArgs,
		apiSenderAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbAPISenders := []*database.APISender{}
	res := db.
		Select(
			TableColumn(database.API_SENDER_TABLE, "id"),
			TableColumn(database.API_SENDER_TABLE, "name"),
		).
		Find(&dbAPISenders)

	if res.Error != nil {
		return result, res.Error
	}
	hasNextPage, err := useHasNextPage(
		db,
		database.API_SENDER_TABLE,
		option.QueryArgs,
		apiSenderAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage
	for _, dbAPISender := range dbAPISenders {
		apiSender, err := ToAPISenderOverview(dbAPISender)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, apiSender)
	}
	return result, nil
}

// GetAllByCompanyID gets API senders by company id
func (a *APISender) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	option *APISenderOption,
) (*model.Result[model.APISender], error) {
	result := model.NewEmptyResult[model.APISender]()
	db := a.preload(option, a.DB)
	db = whereCompany(db, database.API_SENDER_TABLE, companyID)
	db, err := useQuery(
		db,
		database.API_SENDER_TABLE,
		option.QueryArgs,
		apiSenderAllowedColumns...,
	)
	if err != nil {
		return result, errs.Wrap(err)
	}
	dbAPISenders := []*database.APISender{}
	res := db.Find(&dbAPISenders)
	if res.Error != nil {
		return result, res.Error
	}
	for _, dbAPISender := range dbAPISenders {
		apiSender, err := ToAPISender(dbAPISender)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, apiSender)
	}
	return result, nil
}

// GetByName gets a API sender by name
func (a *APISender) GetByName(
	ctx context.Context,
	name *vo.String64,
	companyID *uuid.UUID,
	option *APISenderOption,
) (*model.APISender, error) {
	db := a.preload(option, a.DB)
	db = withCompanyIncludingNullContext(db, companyID, "api_senders")

	dbAPISender := &database.APISender{}
	res := db.Where("name = ?", name.String()).First(&dbAPISender)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToAPISender(dbAPISender)
}

// UpdateByID updates a API sender by ID
func (a *APISender) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	ent *model.APISender,
) error {
	row := ent.ToDBMap()
	AddUpdatedAt(row)
	res := a.DB.
		Model(&database.APISender{}).
		Where("id = ?", id).
		Updates(row)

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID deletes a API sender by ID
func (a *APISender) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := a.DB.Where("id = ?", id).Delete(&database.APISender{})

	if res.Error != nil {
		return res.Error
	}
	return nil
}

// ToAPISender converts a API sender database  to a model
func ToAPISender(row *database.APISender) (*model.APISender, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	apiKey := nullable.NewNullNullable[vo.OptionalString255]()
	if row.APIKey != "" {
		apiKey.Set(*vo.NewOptionalString255Must(row.APIKey))
	} else {
		apiKey.SetUnspecified()
	}
	customField1 := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.CustomField1),
	)
	customField2 := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.CustomField2),
	)
	customField3 := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.CustomField3),
	)
	customField4 := nullable.NewNullableWithValue(
		*vo.NewOptionalString255Must(row.CustomField4),
	)
	requestMethod := nullable.NewNullableWithValue(
		*vo.NewHTTPMethodMust(row.RequestMethod),
	)
	requestURL := nullable.NewNullableWithValue(
		*vo.NewString255Must(row.RequestURL),
	)
	requestHeaders := nullable.NewNullNullable[model.APISenderHeaders]()
	if row.RequestHeaders != "" {
		a, err := model.NewAPISenderHeader(row.RequestHeaders)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		requestHeaders.Set(*a)
	} else {
		requestHeaders.SetUnspecified()
	}
	requestBody := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(row.RequestBody))
	expectedResponseStatusCode := nullable.NewNullNullable[int]()
	if row.ExpectedResponseStatusCode != 0 {
		expectedResponseStatusCode.Set(row.ExpectedResponseStatusCode)
	} else {
		expectedResponseStatusCode.SetNull()
	}
	expectedResponseBody := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(
		row.ExpectedResponseBody,
	))
	expectedResponseHeaders := nullable.NewNullNullable[model.APISenderHeaders]()
	if row.ExpectedResponseHeaders != "" {
		a, err := model.NewAPISenderHeader(row.ExpectedResponseHeaders)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		expectedResponseHeaders.Set(*a)
	} else {
		expectedResponseHeaders.SetUnspecified()
	}

	return &model.APISender{
		ID:                         id,
		CreatedAt:                  row.CreatedAt,
		UpdatedAt:                  row.UpdatedAt,
		CompanyID:                  companyID,
		Name:                       name,
		APIKey:                     apiKey,
		CustomField1:               customField1,
		CustomField2:               customField2,
		CustomField3:               customField3,
		CustomField4:               customField4,
		RequestMethod:              requestMethod,
		RequestURL:                 requestURL,
		RequestHeaders:             requestHeaders,
		RequestBody:                requestBody,
		ExpectedResponseStatusCode: expectedResponseStatusCode,
		ExpectedResponseBody:       expectedResponseBody,
		ExpectedResponseHeaders:    expectedResponseHeaders,
	}, nil
}

// ToAPISenderOverview converts a API sender database to a overview model
func ToAPISenderOverview(row *database.APISender) (*model.APISender, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))

	return &model.APISender{
		ID:        id,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		CompanyID: companyID,
		Name:      name,
	}, nil
}
