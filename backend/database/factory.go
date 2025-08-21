package database

import (
	"fmt"

	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// FromConfig database factory from config
func FromConfig(conf config.Config) (*gorm.DB, error) {
	var db *gorm.DB
	switch conf.Database().Engine {
	case config.DefaultAdministrationUseSqlite:
		var err error
		dsn := fmt.Sprintf(
			"%s?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON",
			conf.Database().DSN,
		)
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			return nil, errs.Wrap(err)
		}
		// SetMaxOpenConns sets the maximum number of open connections to the database.
		// without this, gorutines doing simultaneous db operations will cause
		// "database is locked" error when using sqlite with a high concurrency
		// this is because sqlite only allows one write operation at a time
		// and locks the whole database for the duration any write operation
		innerDB, err := db.DB()
		if err != nil {
			return nil, errs.Wrap(err)
		}
		innerDB.SetMaxIdleConns(1)
	default:
		return nil, config.ErrInvalidDatabase
	}
	return db, nil
}
