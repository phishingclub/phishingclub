package app

import (
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/database"
	"gorm.io/gorm"
)

// SetupDatabase sets up the database
// this includes creating the database connection
func SetupDatabase(
	conf *config.Config,
) (*gorm.DB, error) {
	// create db connection
	return database.FromConfig(*conf)
}
