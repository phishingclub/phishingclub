package database

import (
	"fmt"

	"gorm.io/gorm"
)

type Migrater interface {
	Migrate(db *gorm.DB) error
}

func UniqueIndexNameAndNullCompanyID(db *gorm.DB, tableName string) error {
	// SQLITE / POSTGRES
	// ensure name + null company id is unique
	idx := fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%s_name_null_company_id ON %s (name) WHERE (company_id IS NULL)", tableName, tableName)
	res := db.Exec(idx)
	if res.Error != nil {
		return fmt.Errorf("error creating index: %v on table %s", res.Error, tableName)
	}

	return nil
}
