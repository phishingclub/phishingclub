package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	API_SENDER_TABLE = "api_senders"
)

type APISender struct {
	ID        *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`
	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index"`
	Name      string     `gorm:"not null;uniqueIndex:idx_api_senders_name_company_id;"`
	CompanyID *uuid.UUID `gorm:"uniqueIndex:idx_api_senders_name_company_id;type:uuid"`

	// Extra fields
	APIKey       string
	CustomField1 string
	CustomField2 string
	CustomField3 string
	CustomField4 string

	// Request fields
	RequestMethod  string
	RequestURL     string
	RequestHeaders string
	RequestBody    string

	// Response fields
	ExpectedResponseStatusCode int
	ExpectedResponseHeaders    string
	ExpectedResponseBody       string
}

func (e *APISender) Migrate(db *gorm.DB) error {
	// SQLITE
	// ensure name + null company id is unique
	return UniqueIndexNameAndNullCompanyID(db, "api_senders")
}

func (APISender) TableName() string {
	return API_SENDER_TABLE
}
