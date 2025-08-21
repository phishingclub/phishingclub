package app

import (
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// Repositories is a collection of repositories
type Repositories struct {
	Asset             *repository.Asset
	Attachment        *repository.Attachment
	Company           *repository.Company
	Option            *repository.Option
	Page              *repository.Page
	Role              *repository.Role
	Session           *repository.Session
	User              *repository.User
	Domain            *repository.Domain
	Recipient         *repository.Recipient
	RecipientGroup    *repository.RecipientGroup
	SMTPConfiguration *repository.SMTPConfiguration
	Email             *repository.Email
	Campaign          *repository.Campaign
	CampaignRecipient *repository.CampaignRecipient
	CampaignTemplate  *repository.CampaignTemplate
	APISender         *repository.APISender
	AllowDeny         *repository.AllowDeny
	Webhook           *repository.Webhook
	Identifier        *repository.Identifier
}

// NewRepositories creates a collection of repositories
func NewRepositories(
	db *gorm.DB,
) *Repositories {
	option := &repository.Option{DB: db}
	return &Repositories{
		Asset:             &repository.Asset{DB: db},
		Attachment:        &repository.Attachment{DB: db},
		Company:           &repository.Company{DB: db},
		Option:            option,
		Page:              &repository.Page{DB: db},
		Role:              &repository.Role{DB: db},
		Session:           &repository.Session{DB: db},
		User:              &repository.User{DB: db},
		Domain:            &repository.Domain{DB: db},
		Recipient:         &repository.Recipient{DB: db, OptionRepository: option},
		RecipientGroup:    &repository.RecipientGroup{DB: db},
		SMTPConfiguration: &repository.SMTPConfiguration{DB: db},
		Email:             &repository.Email{DB: db},
		Campaign:          &repository.Campaign{DB: db},
		CampaignRecipient: &repository.CampaignRecipient{DB: db},
		CampaignTemplate:  &repository.CampaignTemplate{DB: db},
		APISender:         &repository.APISender{DB: db},
		AllowDeny:         &repository.AllowDeny{DB: db},
		Webhook:           &repository.Webhook{DB: db},
		Identifier:        &repository.Identifier{DB: db},
	}
}
