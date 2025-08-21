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

var identifierAllowedColumns = assignTableToColumns(database.IDENTIFIER_TABLE, []string{
	TableColumn(database.IDENTIFIER_TABLE, "name"),
})

// IdentifierOption is options for loading
type IdentifierOption struct {
	*vo.QueryArgs
}

type Identifier struct {
	DB *gorm.DB
}

func (i *Identifier) Insert(
	ctx context.Context,
	identifier *model.Identifier,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := identifier.ToDBMap()
	row["id"] = id
	// AddTimestamps(row)

	res := i.DB.
		Model(&database.Identifier{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

func (i *Identifier) GetByName(
	ctx context.Context,
	name string,
) (*model.Identifier, error) {
	var row database.Identifier
	res := i.DB.
		Model(&database.Identifier{}).
		Where(
			fmt.Sprintf("%s = ?", TableColumnName(database.IDENTIFIER_TABLE)),
			name,
		).
		First(&row)

	if res.Error != nil {
		return nil, res.Error
	}
	return ToIdentifier(&row), nil
}

func (i *Identifier) GetAll(
	ctx context.Context,
	option *IdentifierOption,
) (*model.Result[model.Identifier], error) {
	result := model.NewEmptyResult[model.Identifier]()
	rows := []database.Identifier{}
	db, err := useQuery(i.DB, database.IDENTIFIER_TABLE, option.QueryArgs, identifierAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	res := db.
		Model(&database.Identifier{}).
		Find(&rows)

	if res.Error != nil {
		return result, res.Error
	}

	hasNextPage, err := useHasNextPage(db, database.IDENTIFIER_TABLE, option.QueryArgs, identifierAllowedColumns...)

	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage

	for _, row := range rows {
		result.Rows = append(result.Rows, ToIdentifier(&row))
	}
	return result, nil
}

func ToIdentifier(row *database.Identifier) *model.Identifier {
	id := nullable.NewNullableWithValue(row.ID)
	name := nullable.NewNullableWithValue(row.Name)

	return &model.Identifier{
		ID:   id,
		Name: name,
	}
}
