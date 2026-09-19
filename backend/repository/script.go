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

var scriptAllowedColumns = assignTableToColumns(database.SCRIPT_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

type ScriptOption struct {
	*vo.QueryArgs
}

type Script struct {
	DB *gorm.DB
}

// Insert inserts a new script
func (r *Script) Insert(
	ctx context.Context,
	script *model.Script,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := script.ToDBMap()
	row["id"] = id
	AddTimestamps(row)

	res := r.DB.
		Model(&database.Script{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

// GetAll gets all scripts
func (r *Script) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ScriptOption,
) (*model.Result[model.Script], error) {
	result := model.NewEmptyResult[model.Script]()
	db := withCompanyIncludingNullContext(r.DB, companyID, database.SCRIPT_TABLE)
	db, err := useQuery(db, database.SCRIPT_TABLE, options.QueryArgs, scriptAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var rows []*database.Script
	res := db.
		Find(&rows)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.SCRIPT_TABLE, options.QueryArgs, scriptAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, row := range rows {
		result.Rows = append(result.Rows, ToScript(row))
	}
	return result, nil
}

// GetAllByCompanyID gets all scripts for a company
func (r *Script) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ScriptOption,
) ([]*model.Script, error) {
	out := []*model.Script{}
	db := whereCompany(r.DB, database.SCRIPT_TABLE, companyID)
	db, err := useQuery(db, database.SCRIPT_TABLE, options.QueryArgs, scriptAllowedColumns...)
	if err != nil {
		return out, errs.Wrap(err)
	}
	var rows []*database.Script
	res := db.
		Find(&rows)

	if res.Error != nil {
		return out, res.Error
	}
	for _, row := range rows {
		out = append(out, ToScript(row))
	}
	return out, nil
}

// GetByID gets a script by id
func (r *Script) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Script, error) {
	var row database.Script
	res := r.DB.
		Where(
			fmt.Sprintf(
				"%s = ?",
				TableColumnID(database.SCRIPT_TABLE),
			),
			id.String(),
		).
		First(&row)

	if res.Error != nil {
		return nil, res.Error
	}

	return ToScript(&row), nil
}

// GetByIDs fetches multiple scripts by their IDs in a single query
func (r *Script) GetByIDs(
	ctx context.Context,
	ids []*uuid.UUID,
) ([]*model.Script, error) {
	out := []*model.Script{}
	if len(ids) == 0 {
		return out, nil
	}
	idStrings := make([]string, 0, len(ids))
	for _, id := range ids {
		idStrings = append(idStrings, id.String())
	}
	var rows []*database.Script
	res := r.DB.
		Where(
			fmt.Sprintf("%s IN ?", TableColumnID(database.SCRIPT_TABLE)),
			idStrings,
		).
		Find(&rows)

	if res.Error != nil {
		return nil, res.Error
	}
	for _, row := range rows {
		out = append(out, ToScript(row))
	}
	return out, nil
}

// UpdateByID updates a script by id
func (r *Script) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	script *model.Script,
) error {
	row := script.ToDBMap()
	AddUpdatedAt(row)

	res := r.DB.
		Model(&database.Script{}).
		Where("id = ?", id).
		Updates(row)

	return res.Error
}

// DeleteByID deletes a script by id
func (r *Script) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	res := r.DB.
		Where("id = ?", id).
		Delete(&database.Script{})

	return res.Error
}

func ToScript(
	row *database.Script,
) *model.Script {
	id := nullable.NewNullableWithValue(*row.ID)
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	name := nullable.NewNullableWithValue(*vo.NewString127Must(row.Name))
	script := nullable.NewNullableWithValue(*vo.NewString1MBMust(row.Script))

	return &model.Script{
		ID:        id,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		CompanyID: companyID,
		Name:      name,
		Script:    script,
	}
}
