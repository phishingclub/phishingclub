package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetCompanyIDsByIDs loads the owning company of each row with one of the ids.
// The table must be a constant from the database package, never user input.
// A global row maps to nil. Ids that do not exist are absent from the result.
func GetCompanyIDsByIDs(
	ctx context.Context,
	db *gorm.DB,
	table string,
	ids []uuid.UUID,
) (map[uuid.UUID]*uuid.UUID, error) {
	result := map[uuid.UUID]*uuid.UUID{}
	if len(ids) == 0 {
		return result, nil
	}
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id)
	}
	rows := []struct {
		ID        uuid.UUID
		CompanyID *uuid.UUID
	}{}
	err := db.WithContext(ctx).
		Table(table).
		Select("id", "company_id").
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: values}).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = row.CompanyID
	}
	return result, nil
}
