package seed

import (
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
)

// SeedReportTemplate inserts the default global report template if none exists.
// The seeded template can be freely edited through the UI; this only runs when
// no global template (company_id IS NULL) is present in the database.
func SeedReportTemplate(db *gorm.DB) error {
	var count int64
	res := db.
		Model(&database.ReportTemplate{}).
		Where("company_id IS NULL").
		Count(&count)
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	if count > 0 {
		return nil
	}

	id := uuid.New()
	now := time.Now().UTC()
	row := &database.ReportTemplate{
		ID:        &id,
		CreatedAt: &now,
		UpdatedAt: &now,
		Content:   embedded.DefaultReportHTML,
		// CompanyID intentionally nil → global template
	}
	res = db.Create(row)
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}
