package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	REPORT_TEMPLATE_TABLE = "report_templates"
)

// ReportTemplate is a gorm data model
type ReportTemplate struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	CompanyID *uuid.UUID `gorm:"type:uuid"`
	Content   string     `gorm:"not null;type:text"`

	// IsTraining selects the report used for awareness training campaigns, so a
	// company can hold a separate phishing and training report template. Uniqueness
	// is enforced per kind by the indexes in Migrate.
	IsTraining bool `gorm:"not null;default:false"`

	Company *Company
}

func (e *ReportTemplate) Migrate(db *gorm.DB) error {
	// drop the legacy indexes that enforced one template per company and one global,
	// replaced by the per kind indexes below
	db.Exec(`DROP INDEX IF EXISTS idx_report_templates_company_id`)
	db.Exec(`DROP INDEX IF EXISTS idx_report_templates_null_company_id`)
	// one template per company per kind (the global case is covered separately)
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_report_templates_company_training ON report_templates(company_id, is_training) WHERE company_id IS NOT NULL`).Error; err != nil {
		return err
	}
	// enforce at most one global template per kind (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_report_templates_null_company_training ON report_templates(is_training) WHERE company_id IS NULL`
	return db.Exec(idx).Error
}

func (ReportTemplate) TableName() string {
	return REPORT_TEMPLATE_TABLE
}
