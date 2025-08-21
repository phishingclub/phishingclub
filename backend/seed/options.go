package seed

import (
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"gorm.io/gorm"
)

func seedLogLevels(
	db *gorm.DB,
	logLevel string,
	dbLogLevel string,
) error {
	// seed log levels
	{
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyLogLevel).
			Count(&c)

		if res.Error != nil {
			return res.Error
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyLogLevel,
				Value: logLevel,
			})
			if res.Error != nil {
				return res.Error
			}
		}
	}
	{
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyDBLogLevel).
			Count(&c)

		if res.Error != nil {
			return res.Error
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyDBLogLevel,
				Value: dbLogLevel,
			})
			if res.Error != nil {
				return res.Error
			}
		}
	}
	return nil
}
