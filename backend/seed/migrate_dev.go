//go:build dev

package seed

import (
	"context"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/app"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// test company
	TEST_COMPANY_NAME_1 = "Phish Security"
	TEST_COMPANY_NAME_2 = "Phish Yellowgrass Seeds"
	TEST_COMPANY_NAME_3 = "Phish FTW IT"
	TEST_COMPANY_NAME_4 = "Phish Bakery Bites"
	TEST_COMPANY_NAME_5 = "Phish Club"

	// test pages
	TEST_PAGE_NAME_1 = "Login M365"

	// test names
	TEST_EMAIL_NAME_1 = "Validate Account"

	// test domains
	TEST_DOMAIN_NAME_1 = "phishing.club.microsoft.test"
	TEST_DOMAIN_NAME_2 = "phishing.club.google.test"
	TEST_DOMAIN_NAME_3 = "phishing.club.vikings.test"
	TEST_DOMAIN_NAME_4 = "phishing.club.dark-water.test"

	// test recipients
	TEST_RECIPIENT_EMAIL_1 = "alice@black-boat.test"
	TEST_RECIPIENT_EMAIL_2 = "bob@black-boat.test"
	TEST_RECIPIENT_EMAIL_3 = "mallory@black-boat.test"
	TEST_RECIPIENT_EMAIL_4 = "vicky@black-boat.test"

	// test recipient groups
	TEST_RECIPIENT_GROUP_NAME_1 = "Management"
	TEST_RECIPIENT_GROUP_NAME_2 = "Marketing"

	// test smtp configurations
	TEST_SMTP_CONFIGURATION_NAME_1 = "Development"

	// test url param keys
	TEST_URL_IDENTIFIER_NAME = "id"

	// test cookie param keys
	TEST_STATE_IDENTIFIER_NAME = "p"

	// api senders
	TEST_API_SENDER_NAME_1 = "Test API"
)

// InitialInstallAndSeed installs the initial database migrations
func InitialInstallAndSeed(
	db *gorm.DB,
	repositories *app.Repositories,
	logger *zap.SugaredLogger,
	usingSystemd bool,
) error {
	err := initialInstallAndSeed(db, repositories, logger, usingSystemd)
	if err != nil {
		logger.Fatalw("failed to seed database", "error", err)
		return err
	}
	err = RunSeedDevelopmentData(repositories, db, logger)
	if err != nil {
		logger.Fatalw("Failed to seed development data", "error", err)
		return err
	}
	return nil
}

// RunSeedDevelopmentData seeds development data
func RunSeedDevelopmentData(
	repositories *app.Repositories,
	db *gorm.DB,
	logger *zap.SugaredLogger,
) error {
	// check if seeded option is set
	option, err := repositories.Option.GetByKey(context.TODO(), "development_seeded")
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.Wrap(err)
	}
	if option == nil {
		logger.Info("Creating development data")
		// TODO add persisted option to skip seeding
		gofakeit.Seed(1337) // make the fake create the same data every time
		err = SeedDevelopmentData(db, repositories, gofakeit.GlobalFaker)
		if err != nil {
			return errors.Errorf("seed error: %w", err)
		}
		// set seeded option
		id := uuid.New()
		optSeedDev := &model.Option{
			ID:    nullable.NewNullableWithValue(id),
			Key:   *vo.NewString64Must(data.OptionKeyDevelopmentSeeded),
			Value: *vo.NewOptionalString1MBMust(data.OptionValueSeeded),
		}
		_, err := repositories.Option.Insert(context.TODO(), optSeedDev)
		if err != nil {
			return errors.Errorf("failed to insert seeded option: %w", err)
		}
		logger.Info("Finished creating development data")
	}
	return nil
}

func SeedDevelopmentData(
	db *gorm.DB,
	repositories *app.Repositories,
	faker *gofakeit.Faker,
) error {
	var err error
	err = SeedDevelopmentCompanies(repositories.Company, faker)
	if err != nil {
		return errors.Errorf("failed to seed development companies: %w", err)
	}
	err = SeedDevelopmentDomains(repositories.Domain, repositories.Company, faker)
	if err != nil {
		return errors.Errorf("failed to seed development domains: %w", err)
	}
	err = SeedDevelopmentEmails(repositories.Email, repositories.Company, faker)
	if err != nil {
		return errors.Errorf("failed to seed development messages: %w", err)
	}
	err = SeedDevelopmentPages(repositories.Page, repositories.Company, faker)
	if err != nil {
		return errors.Errorf("failed to seed development pages: %w", err)
	}
	err = SeedDevelopmentSMTPConfiguration(repositories.SMTPConfiguration)
	if err != nil {
		return errors.Errorf("failed to seed development smtp configurations: %w", err)
	}
	err = SeedDevelopmentRecipients(repositories.Recipient, repositories.Company, faker)
	if err != nil {
		return errors.Errorf("failed to seed development recipients: %w", err)
	}
	err = SeedDevelopmentRecipientGroups(
		faker,
		repositories.Company,
		repositories.Recipient,
		repositories.RecipientGroup,
	)
	if err != nil {
		return errors.Errorf("failed to seed development recipient groups: %w", err)
	}
	err = SeedDevelopmentAPISenders(
		repositories.APISender,
	)
	if err != nil {
		return errors.Errorf("failed to seed development api senders: %w", err)
	}
	err = SeedDevelopmentCampaignTemplates(
		repositories.APISender,
		repositories.Domain,
		repositories.Page,
		repositories.Email,
		repositories.SMTPConfiguration,
		repositories.CampaignTemplate,
		repositories.Identifier,
	)
	if err != nil {
		return errors.Errorf("failed to seed development templates: %w", err)
	}
	if err := SeedDevelopmentWebhooks(repositories.Webhook); err != nil {
		return errors.Errorf("failed to seed development webhooks: %w", err)
	}
	return nil
}
