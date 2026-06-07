package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var remoteBrowserAllowedColumns = assignTableToColumns(database.REMOTE_BROWSER_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

// RemoteBrowserOption controls eager loading and query params.
type RemoteBrowserOption struct {
	*vo.QueryArgs
	WithCompany bool
}

// RemoteBrowser is the remote browser repository.
type RemoteBrowser struct {
	DB *gorm.DB
}

func (m *RemoteBrowser) load(options *RemoteBrowserOption, db *gorm.DB) *gorm.DB {
	if options.WithCompany {
		db = db.Joins("Company")
	}
	return db
}

// Insert creates a new remote browser record.
func (m *RemoteBrowser) Insert(ctx context.Context, rb *model.RemoteBrowser) (*uuid.UUID, error) {
	id := uuid.New()
	row := rb.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := m.DB.Model(&database.RemoteBrowser{}).Create(row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll returns a paginated list of remote browsers for the given company.
func (m *RemoteBrowser) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RemoteBrowserOption,
) (*model.Result[model.RemoteBrowser], error) {
	result := model.NewEmptyResult[model.RemoteBrowser]()
	var rows []database.RemoteBrowser

	db := m.load(options, m.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.REMOTE_BROWSER_TABLE)
	db, err := useQuery(db, database.REMOTE_BROWSER_TABLE, options.QueryArgs, remoteBrowserAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if res := db.Find(&rows); res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.REMOTE_BROWSER_TABLE, options.QueryArgs, remoteBrowserAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, row := range rows {
		rb, err := ToRemoteBrowser(&row)
		if err != nil {
			return result, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, rb)
	}
	return result, nil
}

// GetAllSubset returns lightweight overview rows.
func (m *RemoteBrowser) GetAllSubset(
	ctx context.Context,
	companyID *uuid.UUID,
	options *RemoteBrowserOption,
) (*model.Result[model.RemoteBrowserOverview], error) {
	result := model.NewEmptyResult[model.RemoteBrowserOverview]()
	var rows []database.RemoteBrowser

	db := withCompanyIncludingNullContext(m.DB, companyID, database.REMOTE_BROWSER_TABLE)
	db, err := useQuery(db, database.REMOTE_BROWSER_TABLE, options.QueryArgs, remoteBrowserAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if res := db.Select("id, created_at, updated_at, name, description, company_id").Find(&rows); res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.REMOTE_BROWSER_TABLE, options.QueryArgs, remoteBrowserAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, row := range rows {
		result.Rows = append(result.Rows, &model.RemoteBrowserOverview{
			ID:          *row.ID,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
			Name:        row.Name,
			Description: row.Description,
			CompanyID:   row.CompanyID,
		})
	}
	return result, nil
}

// GetByID returns a single remote browser by ID.
func (m *RemoteBrowser) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *RemoteBrowserOption,
) (*model.RemoteBrowser, error) {
	row := database.RemoteBrowser{}
	db := m.load(options, m.DB)
	if res := db.Where(TableColumnID(database.REMOTE_BROWSER_TABLE)+" = ?", id).First(&row); res.Error != nil {
		return nil, res.Error
	}
	return ToRemoteBrowser(&row)
}

// GetByNameAndCompanyID returns a remote browser by name (used for uniqueness checks).
func (m *RemoteBrowser) GetByNameAndCompanyID(
	ctx context.Context,
	name *vo.String64,
	companyID *uuid.UUID,
	options *RemoteBrowserOption,
) (*model.RemoteBrowser, error) {
	row := database.RemoteBrowser{}
	db := m.load(options, m.DB)
	db = withCompanyIncludingNullContext(db, companyID, database.REMOTE_BROWSER_TABLE)
	res := db.Where(
		fmt.Sprintf("%s = ?", TableColumn(database.REMOTE_BROWSER_TABLE, "name")),
		name.String(),
	).First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return ToRemoteBrowser(&row)
}

// UpdateByID updates the mutable fields of a remote browser.
func (m *RemoteBrowser) UpdateByID(ctx context.Context, id *uuid.UUID, rb *model.RemoteBrowser) error {
	row := rb.ToDBMap()
	AddUpdatedAt(row)
	res := m.DB.Model(&database.RemoteBrowser{}).Where("id = ?", id).Updates(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// DeleteByID hard-deletes a remote browser.
func (m *RemoteBrowser) DeleteByID(ctx context.Context, id *uuid.UUID) error {
	if res := m.DB.Delete(&database.RemoteBrowser{}, id); res.Error != nil {
		return res.Error
	}
	return nil
}

// ToRemoteBrowser maps a database row to the model type.
func ToRemoteBrowser(row *database.RemoteBrowser) (*model.RemoteBrowser, error) {
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

	script, err := vo.NewString1MB(row.Script)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	scriptNullable := nullable.NewNullableWithValue(*script)

	var cfg model.RemoteBrowserConfig
	if row.Config != "" {
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return nil, errs.Wrap(fmt.Errorf("invalid stored config: %w", err))
		}
	}
	configNullable := nullable.NewNullableWithValue(cfg)

	return &model.RemoteBrowser{
		ID:          id,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		CompanyID:   companyID,
		Name:        name,
		Description: descriptionNullable,
		Script:      scriptNullable,
		Config:      configNullable,
	}, nil
}
