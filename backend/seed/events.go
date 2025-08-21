package seed

import (
	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
)

func SeedEvents(
	db *gorm.DB,
) error {
	for _, event := range data.Events {
		dbEvent := &database.Event{}
		res := db.Where("name = ?", event).First(dbEvent)
		if res.Error != nil && !errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, res.Error)
		}
		if dbEvent.ID != nil {
			err := cacheEvent(dbEvent.ID, event)
			if err != nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			continue
		}
		// put in in the db
		id := uuid.New()
		res = db.Create(&database.Event{
			ID:   &id,
			Name: event,
		})
		if res.Error != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, res.Error)
		}
		err := cacheEvent(&id, event)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}

func cacheEvent(
	eventID *uuid.UUID,
	name string,
) error {
	// the ids are stored in memory for quick access
	// never stored in the database
	cache.EventIDByName[name] = eventID
	cache.EventNameByID[eventID.String()] = name

	return nil
}
