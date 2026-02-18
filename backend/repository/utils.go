package repository

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var defaultAllowdSearchColumns = []string{
	"name",
}

var defaultAllowdColumns = map[string]struct{}{
	"name":       {},
	"created_at": {},
	"updated_at": {},
}

func withOffsetLimit(db *gorm.DB, offset, limit int) *gorm.DB {
	if offset == 0 && limit == 0 {
		return db
	}
	return db.Offset(offset).Limit(limit)
}

func WithOrderBy(db *gorm.DB, orderBy string, desc bool, allowed ...string) (*gorm.DB, error) {
	if orderBy == "" {
		return db, nil
	}
	// if no allowed columns are provided, use the default
	// else check if the column is allowed
	if len(allowed) == 0 {
		if _, ok := defaultAllowdColumns[orderBy]; !ok {
			return db, fmt.Errorf(
				"not known or allowed column: %s - allowd: %s",
				orderBy,
				defaultAllowdColumns,
			)
		}
	} else {
		if !slices.Contains(allowed, orderBy) {
			return db, fmt.Errorf(
				"not known or allowed column: %s - allowd: %s",
				orderBy,
				allowed,
			)
		}
	}
	// TODO this ruins all indexes performance but is a quick fix to work for all databases
	// to ensure that the order by is case insensitive
	// the real solution is to use a case insensitive collation
	// but these differ per database, another option would be LOWER indexes for some columns however this is also not ideal
	//orderBy = fmt.Sprintf("LOWER(%s)", orderBy)

	if !desc {
		return db.Order(orderBy + " COLLATE NOCASE ASC"), nil
	}
	return db.Order(orderBy + " COLLATE NOCASE DESC"), nil
}

func WithOrderByOnTable(db *gorm.DB, table string, orderBy string, desc bool, allowed ...string) (*gorm.DB, error) {
	if orderBy == "" {
		return db, nil
	}
	// only check default columns if no allowed columns are provided
	if _, ok := defaultAllowdColumns[orderBy]; !ok && len(allowed) == 0 {
		return db, fmt.Errorf("invalid column: %s", orderBy)
	}
	for _, allowedOrderBy := range allowed {
		if !slices.Contains(allowed, orderBy) {
			return db, fmt.Errorf("invalid column: %s", allowedOrderBy)
		}
	}

	// TODO this ruins all indexes performance but is a quick fix to work for all databases
	// to ensure that the order by is case insensitive
	// the real solution is to use a case insensitive collation
	// but these differ per database, another option would be LOWER indexes for some columns however this is also not ideal
	if !desc {
		return db.Order(
			//fmt.Sprintf("LOWER(`%s`.`%s`) ASC", table, orderBy),
			fmt.Sprintf("LOWER(`%s`.`%s`) COLLATE NOCAS ASC", table, orderBy),
		), nil
	}
	return db.Order(
		//fmt.Sprintf("LOWER(`%s`.`%s`) DESC", table, orderBy),
		fmt.Sprintf("LOWER(`%s`.`%s`) NO COLLATE DESC", table, orderBy),
	), nil
}

func assignTableToColumn(table, column string) string {
	// if the column already contains a dot, it is already formatted
	if strings.Contains(column, ".") {
		return column
	}
	return fmt.Sprintf("`%s`.`%s`", table, column)
}

func assignTableToColumns(table string, columns []string) []string {
	for i, column := range columns {
		columns[i] = assignTableToColumn(table, column)
	}
	return columns
}

func useQuery(db *gorm.DB, tableName string, q *vo.QueryArgs, allowdColumns ...string) (*gorm.DB, error) {
	if q == nil {
		return db, nil
	}
	db = withOffsetLimit(db, q.Offset, q.Limit)
	// only apply orderby if it's not empty to avoid generating invalid column names like `table`.``
	var err error
	if q.OrderBy != "" {
		db, err = WithOrderBy(db, assignTableToColumn(tableName, q.OrderBy), q.Desc, allowdColumns...)
		if err != nil {
			return db, errs.Wrap(err)
		}
	}
	// handle search
	if q.Search != "" {
		searchColumns := []string{}
		// add columns that are allowed to be searched in
		for _, column := range allowdColumns {
			searchColumns = append(
				searchColumns,
				assignTableToColumn(tableName, column),
			)
		}
		// if no columns has been added, use the default
		if len(searchColumns) == 0 {
			searchColumns = assignTableToColumns(tableName, defaultAllowdSearchColumns)
		}
		// remove search symbols
		search := strings.ReplaceAll(q.Search, "%", " ")
		search = strings.ReplaceAll(search, "_", " ")
		// build the LIKE query
		var searches []interface{}
		q := ""
		for i, column := range searchColumns {
			if i == 0 {
				q += fmt.Sprintf("%s LIKE ?", column)
			} else {
				q += fmt.Sprintf(" OR %s LIKE ?", column)
			}
			searches = append(searches, "%"+search+"%")
		}
		db.Where(q, searches...)
	}
	return db, errs.Wrap(err)
}

func useHasNextPage(
	db *gorm.DB,
	tableName string,
	q *vo.QueryArgs,
	allowdColumns ...string,
) (bool, error) {
	if q == nil {
		return false, nil
	}
	if q.Limit == 0 && q.Offset == 0 {
		return false, nil
	}
	db = withOffsetLimit(db, q.Offset+q.Limit, 1)
	db, err := WithOrderBy(db, assignTableToColumn(tableName, q.OrderBy), q.Desc, allowdColumns...)
	if err != nil {
		return false, errs.Wrap(err)
	}
	// handle search
	if q.Search != "" {
		searchColumns := []string{}
		// add columns that are allowed to be searched in
		for _, column := range allowdColumns {
			searchColumns = append(
				searchColumns,
				assignTableToColumn(tableName, column),
			)
		}
		// if no columns has been added, use the default
		if len(searchColumns) == 0 {
			searchColumns = assignTableToColumns(tableName, defaultAllowdSearchColumns)
		}
		// remove search symbols
		search := strings.ReplaceAll(q.Search, "%", " ")
		search = strings.ReplaceAll(search, "_", " ")
		// build the LIKE query
		var searches []interface{}
		q := ""
		for i, column := range searchColumns {
			if i == 0 {
				q += fmt.Sprintf("%s LIKE ?", column)
			} else {
				q += fmt.Sprintf(" OR %s LIKE ?", column)
			}
			searches = append(searches, "%"+search+"%")
		}
		db.Where(q, searches...)
	}
	// Check if there's at least one record
	var exists bool
	err = db.Select("1").Find(&exists).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errs.Wrap(err)
	}
	return exists, nil
}

/*
func useQueryWithTable(db *gorm.DB, table string, q *vo.QueryArgs, allowdColumns ...string) (*gorm.DB, error) {
	if q == nil {
		return db, nil
	}
	db = withOffsetLimit(db, q.Offset, q.Limit)
	db, err := WithOrderByOnTable(db, table, q.OrderBy, q.Desc, allowdColumns...)
	// handle search
	if q.Search != "" {
		searchColumns := []string{}
		// add columns that are allowed to be searched in
		for _, column := range allowdColumns {
			if column == "created_at" || column == "updated_at" {
				continue
			}
			searchColumns = append(searchColumns, column)
		}
		// if no columns has been added, use the default
		if len(searchColumns) == 0 {
			searchColumns = defaultAllowdSearchColumns
		}
		// remove search symbols
		search := strings.ReplaceAll(q.Search, "%", " ")
		search = strings.ReplaceAll(search, "_", " ")
		// build the LIKE query
		// todo perhaps this needs table prefix also
		var searches []interface{}
		q := ""
		for i, column := range searchColumns {
			if i == 0 {
				q += column + " LIKE ?"
			} else {
				q += " OR " + column + " LIKE ?"
			}
			searches = append(searches, "%"+search+"%")
		}
		db.Where(q, searches...)
	}
	return db,errs.Wrap(err)
}
*/

func SelectTable(tableName string) []string {
	return []string{fmt.Sprintf("`%s`.*", tableName)}
}

// SelectColumnAs creates a list of columns with aliases column is map[column]alias
func SelectColumnAs(tableName string, columns map[string]string) []string {
	var cols []string
	for key, value := range columns {
		cols = append(cols, fmt.Sprintf("`%s`.`%s` AS %s", tableName, value, key))
	}
	return cols
}

func useSelect(db *gorm.DB, fields []string) *gorm.DB {
	if len(fields) == 0 {
		return db
	}
	return db.Select(fields)
}

func LeftJoinOn(tableLeft, columnLeft, tableRight, columnRight string) string {
	return fmt.Sprintf("LEFT JOIN `%s` on `%s`.`%s` = `%s`.`%s`", tableRight, tableLeft, columnLeft, tableRight, columnRight)
}

func LeftJoinOnWithAlias(tableLeft, columnLeft, tableRight, columnRight, alias string) string {
	return fmt.Sprintf("LEFT JOIN `%s` '%s' on `%s`.`%s` = `%s`.`%s`", tableRight, alias, tableLeft, columnLeft, alias, columnRight)
}

// withCompanyTableContext adds a company context to the query
func withCompanyIncludingNullContext(db *gorm.DB, companyID *uuid.UUID, tableName string) *gorm.DB {
	column := fmt.Sprintf("`%s`.company_id", tableName)
	if companyID != nil {
		return db.Where(
			fmt.Sprintf("(%s = ? OR %s IS NULL)", column, column), companyID)
	}
	return db.Where(
		fmt.Sprintf("(%s IS NULL)", column),
	)
}

// withCompany adds a where company id
// if companyID is NIL it will add a companyID IS NULL
func whereCompany(db *gorm.DB, tableName string, companyID *uuid.UUID) *gorm.DB {
	column := fmt.Sprintf("`%s`.company_id", tableName)
	if companyID == nil {
		return db.Where(fmt.Sprintf("%s IS NULL", column))
	} else {
		return db.Where(
			fmt.Sprintf("%s = ?", column), companyID)
	}
}

// withCompany adds a where company id is null
func whereCompanyIsNull(db *gorm.DB, tableName string) *gorm.DB {
	column := fmt.Sprintf("`%s`.company_id", tableName)
	return db.Where(
		fmt.Sprintf("%s IS NULL", column))
}

// AddTimestamps adds created_at and updated_at to a map
func AddTimestamps(row map[string]interface{}) {
	now := utils.NowRFC3339UTC()
	row["created_at"] = now
	row["updated_at"] = now
}

// AddCreatedAt adds updated_at to a map
func AddUpdatedAt(row map[string]interface{}) {
	row["updated_at"] = utils.NowRFC3339UTC()
}

// CheckColumnIsUnique checks if a column is unique within a company and globally
// if companyID is nil, it is global no other row should use the name, period.
// if companyID is set, then the same column id should not use it, and no null company ID
// columns is not sqli safe
func CheckColumnIsUnique(
	ctx context.Context,
	db *gorm.DB,
	table string,
	column string,
	value string,
	companyID *uuid.UUID,
	currentID *uuid.UUID, // if currentID is set, it is allowed to use the same value
) (bool, error) {
	var count int64
	tx := db.Table(table)

	if companyID != nil {
		tx = tx.Where(column+" = ? AND (company_id = ? OR company_id IS NULL)", value, companyID)
	} else {
		tx = tx.Where(column+" = ?", value)
	}
	if currentID != nil {
		tx = tx.Where("id != ?", currentID)
	}

	result := tx.Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count == 0, nil
}

func UUIDsToStrings(ids []*uuid.UUID) []string {
	args := []string{}
	for _, s := range ids {
		args = append(args, s.String())
	}
	return args
}

// CheckNameIsUnique checks if a name is unique within a company and globally
// if companyID is nil, it is global no other row should use the name, period.
// if companyID is set, then the same company id should not use it, and no null company ID
func CheckNameIsUnique(
	ctx context.Context,
	db *gorm.DB,
	table string,
	name string,
	companyID *uuid.UUID,
	currentID *uuid.UUID,
) (bool, error) {
	return CheckColumnIsUnique(ctx, db, table, "name", name, companyID, currentID)
}

func TableSelect(selects ...string) string {
	return strings.Join(
		selects,
		",",
	)
}

func TableColumn(tableName, columnName string) string {
	return fmt.Sprintf("`%s`.`%s`", tableName, columnName)
}

func TableColumnAlias(tableName, columnName, alias string) string {
	return fmt.Sprintf("`%s`.`%s` AS `%s`", tableName, columnName, alias)
}

func TableColumnAll(tableName string) string {
	return fmt.Sprintf("`%s`.*", tableName)
}

func TableColumnID(tableName string) string {
	return TableColumn(tableName, "id")
}

func TableColumnName(tableName string) string {
	return TableColumn(tableName, "name")
}
