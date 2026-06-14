package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		// ensure the directory holding the sqlite file exists, sqlite creates
		// the database file but not its parent directory, so a fresh checkout
		// where the data directory is absent fails with "unable to open database file"
		if dir := sqliteDir(conf.Database().DSN); dir != "" {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, errs.Wrap(err)
			}
		}
		// determine the correct separator for additional parameters
		// use & if user already has query params, otherwise use ?
		separator := "?"
		if strings.Contains(conf.Database().DSN, "?") {
			separator = "&"
		}
		dsn := fmt.Sprintf(
			"%s%s_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON",
			conf.Database().DSN,
			separator,
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

// sqliteDir returns the directory that must exist for the sqlite DSN.
// it returns an empty string for in memory databases or when the path has no
// directory component, in which case no directory needs to be created.
func sqliteDir(dsn string) string {
	path := strings.TrimPrefix(dsn, "file:")
	// drop any query parameters
	if i := strings.Index(path, "?"); i != -1 {
		path = path[:i]
	}
	if path == "" || strings.Contains(path, ":memory:") {
		return ""
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
