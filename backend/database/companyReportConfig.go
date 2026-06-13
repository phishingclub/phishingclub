package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	COMPANY_REPORT_CONFIG_TABLE = "company_report_configs"
)

// CompanyReportConfig holds the automatic report delivery configuration.
// a row with a NULL company_id is the global default used as a fallback when a
// company has no config of its own. when enabled, a campaign report PDF can be
// emailed to a recipient group, either on demand or when a campaign is closed.
type CompanyReportConfig struct {
	ID                  *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt           *time.Time `gorm:"not null;index;"`
	UpdatedAt           *time.Time `gorm:"not null;index;"`
	CompanyID           *uuid.UUID `gorm:"uniqueIndex;type:uuid"`           // NULL is the global default
	Enabled             bool       `gorm:"not null;default:false"`
	SendOnFinish        bool       `gorm:"not null;default:false"`         // auto send when a campaign is closed
	RecipientGroupID    *uuid.UUID `gorm:"type:uuid"`                      // group that receives the report
	SMTPConfigurationID *uuid.UUID `gorm:"type:uuid"`                      // smtp used to send the report
	SenderEmail         string     `gorm:"not null;default:''"`            // from address used for the report email
	EmailSubject        string     `gorm:"not null;default:'';type:text"`  // subject of the delivery email
	EmailBody           string     `gorm:"not null;default:'';type:text"`  // html body of the delivery email
	LastSentAt          *time.Time // nullable: last time a report was successfully delivered

	Company           *Company           `gorm:"foreignKey:CompanyID"`
	RecipientGroup    *RecipientGroup    `gorm:"foreignKey:RecipientGroupID"`
	SMTPConfiguration *SMTPConfiguration `gorm:"foreignKey:SMTPConfigurationID"`
}

func (e *CompanyReportConfig) Migrate(db *gorm.DB) error {
	// enforce at most one global config (company_id IS NULL)
	idx := `CREATE UNIQUE INDEX IF NOT EXISTS idx_company_report_configs_null_company_id ON company_report_configs ((company_id IS NULL)) WHERE (company_id IS NULL)`
	return db.Exec(idx).Error
}

func (CompanyReportConfig) TableName() string {
	return COMPANY_REPORT_CONFIG_TABLE
}
