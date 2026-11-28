package repository

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxFieldValueRows is the most raw values GetFieldValueCounts reads
const MaxFieldValueRows = 2000

// FieldValueCount is one raw value of a recipient field and how many
// recipients have it
type FieldValueCount struct {
	Value string
	Count int
}

// FieldValueCounts are the raw values GetFieldValueCounts read. More tells
// whether there were more values than were read. Members is how many members
// the groups have and Distinct how many raw values there are, read or not.
type FieldValueCounts struct {
	Rows     []FieldValueCount
	More     bool
	Members  int
	Distinct int
}

// GetFieldValueCounts returns each raw value of a recipient field over the
// members of the groups with how many members have it, the most common
// first, at most MaxFieldValueRows of them. A recipient in more than one of
// the groups is counted once. Deleted and identity provider disabled
// recipients are left out. field must be one of model.LanguageRouteFields.
// When there are more values than were read, the members and the raw values
// are counted in two more queries.
func (rg *RecipientGroup) GetFieldValueCounts(
	ctx context.Context,
	field string,
	groupIDs []*uuid.UUID,
) (*FieldValueCounts, error) {
	out := &FieldValueCounts{Rows: []FieldValueCount{}}
	if !slices.Contains(model.LanguageRouteFields, field) {
		return nil, fmt.Errorf("invalid recipient field: %s", field)
	}
	if len(groupIDs) == 0 {
		return out, nil
	}
	recipientID := clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}
	members := make([]clause.Expression, 0, len(groupIDs))
	for _, id := range groupIDs {
		sub, err := rg.MemberIDsQuery(ctx, id)
		if err != nil {
			return nil, err
		}
		members = append(members, clause.Expr{SQL: "? IN (?)", Vars: []any{recipientID, sub}})
	}
	column := clause.Column{Table: database.RECIPIENT_TABLE, Name: field}
	base := func() *gorm.DB {
		return rg.DB.WithContext(ctx).
			Model(&database.Recipient{}).
			Where(clause.Eq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "deleted_at"}, Value: nil}).
			Where(clause.Eq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "scim_soft_deleted_at"}, Value: nil}).
			Where(clause.Or(members...))
	}
	var rows []FieldValueCount
	err := base().
		Select("COALESCE(?, '') AS value, COUNT(*) AS count", column).
		Clauses(clause.GroupBy{Columns: []clause.Column{column}}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "count"}, Desc: true},
			{Column: clause.Column{Name: "value"}},
		}}).
		Limit(MaxFieldValueRows + 1).
		Scan(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out.More = len(rows) > MaxFieldValueRows
	if out.More {
		rows = rows[:MaxFieldValueRows]
	}
	out.Rows = rows
	if !out.More {
		for _, row := range rows {
			out.Members += row.Count
		}
		out.Distinct = len(rows)
		return out, nil
	}
	var memberCount int64
	if err := base().Count(&memberCount).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	groups := base().
		Select("1").
		Clauses(clause.GroupBy{Columns: []clause.Column{column}})
	var distinct int64
	if err := rg.DB.WithContext(ctx).Table("(?) AS v", groups).Count(&distinct).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	out.Members = int(memberCount)
	out.Distinct = int(distinct)
	return out, nil
}
