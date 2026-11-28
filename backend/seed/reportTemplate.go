package seed

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SeedReportTemplate inserts the default global report templates if they are
// missing. There is one global template per kind (phishing, awareness
// training and program); each is seeded independently and only when absent, so
// a template already edited through the UI is never overwritten. A global
// template that still holds an earlier built in default of its kind gets the
// current one.
func SeedReportTemplate(db *gorm.DB) error {
	if err := seedGlobalReportTemplate(db, false, false, embedded.DefaultReportHTML); err != nil {
		return err
	}
	if err := seedGlobalReportTemplate(db, true, false, embedded.DefaultTrainingReportHTML); err != nil {
		return err
	}
	if err := seedGlobalReportTemplate(db, false, true, embedded.DefaultProgramReportHTML); err != nil {
		return err
	}
	if err := upgradeGlobalCampaignReportTemplates(db); err != nil {
		return err
	}
	return upgradeGlobalProgramReportTemplate(db)
}

// upgradeGlobalCampaignReportTemplates gives a global simulation or training
// report template that still holds an earlier built in default the current
// one
func upgradeGlobalCampaignReportTemplates(db *gorm.DB) error {
	repo := &repository.ReportTemplate{DB: db}
	if err := repo.UpgradeGlobalCampaignDefault(
		context.Background(),
		false,
		embedded.PreviousDefaultReportHashes,
		embedded.ReportContentHash,
		embedded.DefaultReportHTML,
	); err != nil {
		return err
	}
	return repo.UpgradeGlobalCampaignDefault(
		context.Background(),
		true,
		embedded.PreviousDefaultTrainingReportHashes,
		embedded.ReportContentHash,
		embedded.DefaultTrainingReportHTML,
	)
}

// upgradeGlobalProgramReportTemplate gives a global program report template
// that still holds an earlier built in default the current one
func upgradeGlobalProgramReportTemplate(db *gorm.DB) error {
	repo := &repository.ReportTemplate{DB: db}
	return repo.UpgradeGlobalProgramDefault(
		context.Background(),
		embedded.PreviousDefaultProgramReportHashes,
		embedded.ReportContentHash,
		embedded.DefaultProgramReportHTML,
	)
}

func seedGlobalReportTemplate(db *gorm.DB, isTraining bool, isProgram bool, content string) error {
	var count int64
	res := db.
		Model(&database.ReportTemplate{}).
		Where(clause.Eq{Column: clause.Column{Name: "company_id"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "is_training"}, Value: isTraining}).
		Where(clause.Eq{Column: clause.Column{Name: "is_program"}, Value: isProgram}).
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
		IsProgram:  isProgram,
		// CompanyID intentionally nil → global template
	}
	res = db.Create(row)
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	return nil
}
