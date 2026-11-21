package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqliteMaxConns is the size of the sqlite connection pool
const sqliteMaxConns = 8

// sqliteDriverName is the sqlite driver with the connection setup below. It
// is registered once, sql.Register panics on a second name use.
const sqliteDriverName = "sqlite3_phishingclub"

// sqliteJournalSizeLimit caps the write ahead log file that is left on disk
// after a checkpoint has reset it, 64 MB. The driver has no DSN parameter for
// it, so it is set on every new connection.
const sqliteJournalSizeLimit = "PRAGMA journal_size_limit=67108864"

func init() {
	sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			_, err := conn.Exec(sqliteJournalSizeLimit, nil)
			return err
		},
	})
}

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
		db, err = gorm.Open(sqlite.New(sqlite.Config{
			DriverName: sqliteDriverName,
			DSN:        dsn,
		}), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			return nil, errs.Wrap(err)
		}
		// the pool is bounded so a burst of requests waits for a free
		// connection instead of opening a new sqlite connection per request,
		// which can run out of file handles and leaves every extra writer
		// waiting inside the sqlite busy handler. idle matches open so
		// connections are reused rather than closed and reopened.
		// more than one connection keeps reads running alongside a write
		// (WAL), so code must never query the main handle while holding a
		// transaction, or it can wait forever for a free connection.
		innerDB, err := db.DB()
		if err != nil {
			return nil, errs.Wrap(err)
		}
		innerDB.SetMaxOpenConns(sqliteMaxConns)
		innerDB.SetMaxIdleConns(sqliteMaxConns)
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
