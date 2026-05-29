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
	CompanyID *uuid.UUID `gorm:"uniqueIndex;type:uuid"`
	Content   string     `gorm:"not null;type:text"`

	Company *Company
}

func (e *ReportTemplate) Migrate(db *gorm.DB) error {
	// enforce at most one global template (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_report_templates_null_company_id ON report_templates ((company_id IS NULL)) WHERE (company_id IS NULL)`
	return db.Exec(idx).Error
}

func (ReportTemplate) TableName() string {
	return REPORT_TEMPLATE_TABLE
}
