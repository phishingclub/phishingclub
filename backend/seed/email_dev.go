//go:build dev

package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

type DevelopmentEmail struct {
	Name             string
	MailEnvelopeFrom string
	MailFrom         string
	Subject          string
	Content          string
	CompanyID        string
}

func SeedDevelopmentEmails(
	emailRepository *repository.Email,
	companyRepository *repository.Company,
	faker *gofakeit.Faker,
) error {
	// known test data
	emails := []DevelopmentEmail{
		{
			Name:             TEST_EMAIL_NAME_1,
			Subject:          "Welcome to The Phishing Club",
			MailEnvelopeFrom: "envelope-sender@phish.internal",
			MailFrom:         "Fresh Fish <ff@phish.internal>",
			Content:          "Hi {{.FirstName}} Welcome to The Phishing Club! We are excited to have you here. Click <a href='{{.URL}}'>here</a> to get started.",
		},
	}
	for i := 0; i < 10; i++ {
		envelopeFrom := faker.Email()
		emails = append(emails, DevelopmentEmail{
			Name:             faker.ProductName(),
			Subject:          faker.Sentence(4),
			MailEnvelopeFrom: envelopeFrom,
			MailFrom:         faker.Name() + " <" + envelopeFrom + ">",
			Content:          "Hi {{.FirstName}} <br> " + faker.Sentence(10) + "<br><a href='{{.URL}}'>Click here</a> to get started.",
		})
	}
	err := createDevelopmentEmails(emails, emailRepository, nil)
	if err != nil {
		return errors.Errorf("failed to seed development emails: %w", err)
	}
	// random emails attached to companies
	err = forEachDevelopmentCompany(companyRepository, func(company *model.Company) error {
		emails := []DevelopmentEmail{}
		companyID := company.ID.MustGet()
		for i := 0; i < 10; i++ {
			envelopeFrom := faker.Email()
			emails = append(emails, DevelopmentEmail{
				Name:             faker.ProductName(),
				Subject:          faker.Sentence(4),
				MailEnvelopeFrom: envelopeFrom,
				MailFrom:         faker.Name() + " <" + envelopeFrom + ">",
				Content:          "Hi {{.FirstName}} <br> " + faker.Sentence(10) + "<br><a href='{{.URL}}'>Click here</a> to get started.",
				CompanyID:        companyID.String(),
			})
		}
		err := createDevelopmentEmails(emails, emailRepository, &companyID)
		if err != nil {
			return errors.Errorf("failed to seed development emails: %w", err)
		}
		return nil
	})
	if err != nil {
		return errors.Errorf("failed to seed development emails: %w", err)
	}
	return nil
}

func createDevelopmentEmails(
	emails []DevelopmentEmail,
	emailRepository *repository.Email,
	companyID *uuid.UUID,
) error {
	for _, email := range emails {
		name := vo.NewString64Must(email.Name)
		createEmail := model.Email{
			ID:                nullable.NewNullableWithValue(uuid.New()),
			Name:              nullable.NewNullableWithValue(*name),
			MailEnvelopeFrom:  nullable.NewNullableWithValue(*vo.NewMailEnvelopeFromMust(email.MailEnvelopeFrom)),
			MailHeaderSubject: nullable.NewNullableWithValue(*vo.NewOptionalString255Must(email.Subject)),
			MailHeaderFrom:    nullable.NewNullableWithValue(*vo.NewEmailMust(email.MailFrom)),
			AddTrackingPixel:  nullable.NewNullableWithValue(true),
			Content:           nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(email.Content + "{{.Tracker}}")),
		}
		if email.CompanyID != "" {
			createEmail.CompanyID = nullable.NewNullableWithValue(uuid.MustParse(email.CompanyID))
		}
		m, err := emailRepository.GetByNameAndCompanyID(
			context.Background(),
			name,
			companyID,
			&repository.EmailOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if m != nil {
			continue
		}
		_, err = emailRepository.Insert(context.TODO(), &createEmail)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
