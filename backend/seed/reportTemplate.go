package seed

import (
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
)

// SeedReportTemplate inserts the default global report templates if they are
// missing. There is one global template per kind (phishing and awareness
// training); each is seeded independently and only when absent, so a template
// already edited through the UI is never overwritten.
func SeedReportTemplate(db *gorm.DB) error {
	if err := seedGlobalReportTemplate(db, false, embedded.DefaultReportHTML); err != nil {
		return err
	}
	return seedGlobalReportTemplate(db, true, embedded.DefaultTrainingReportHTML)
}

func seedGlobalReportTemplate(db *gorm.DB, isTraining bool, content string) error {
	var count int64
	res := db.
		Model(&database.ReportTemplate{}).
		Where("company_id IS NULL AND is_training = ?", isTraining).
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
		ID:         &id,
		CreatedAt:  &now,
		UpdatedAt:  &now,
		Content:    content,
		IsTraining: isTraining,
		// CompanyID intentionally nil → global template
	}
	res = db.Create(row)
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}
