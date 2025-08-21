//go:build dev

package seed

import (
	"context"
	"strings"

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

type DevelopmentRecipient struct {
	FirstName       string
	LastName        string
	Email           string
	Department      string
	Position        string
	ExtraIdentifier string
	Phone           string
	City            string
	Country         string
	Misc            string
	CompanyID       string
}

// tcmpRecpMap holds the company name and the recipient emails
// so it can later be added to different groups
// map[companyID][]recpUUID
var tmpRecpMap = map[string][]*uuid.UUID{}

func SeedDevelopmentRecipients(
	recipientRepository *repository.Recipient,
	companyRepository *repository.Company,
	faker *gofakeit.Faker,
) error {
	// add known recipients
	recipients := []DevelopmentRecipient{
		{
			Email:      TEST_RECIPIENT_EMAIL_1,
			FirstName:  "Alice",
			LastName:   "",
			Department: "Economy",
		},
		{
			Email:      TEST_RECIPIENT_EMAIL_2,
			FirstName:  "Bob",
			LastName:   "",
			Department: "Economy",
		},
		{

			Email:      TEST_RECIPIENT_EMAIL_3,
			FirstName:  "Mallory",
			LastName:   "",
			Department: "Marketing",
		},
		{
			Email:      TEST_RECIPIENT_EMAIL_4,
			FirstName:  "Vickey",
			LastName:   "",
			Department: "Marketing",
		},
	}
	// add random recipients
	country := faker.Country()
	domain := faker.DomainName()
	jobLevels := []string{}
	for i := 0; i < 3; i++ {
		jobLevels = append(jobLevels, faker.JobLevel())
	}
	for i := 0; i < 10; i++ {
		firstName := faker.FirstName()
		lastName := faker.LastName()
		jobLevel := jobLevels[faker.Number(0, len(jobLevels)-1)]
		emailPrefix := strings.ToLower(strings.Join(strings.Split(firstName, " "), "."))
		recipients = append(recipients, DevelopmentRecipient{
			Email:      emailPrefix + "@" + domain,
			FirstName:  firstName,
			LastName:   lastName,
			Department: jobLevel,
			Position:   faker.JobTitle(),
			City:       faker.City(),
			Phone:      faker.Phone(),
			Country:    country,
			Misc:       faker.Sentence(10),
		})
	}
	err := createDevelopmentRecipients(recipients, recipientRepository)
	if err != nil {
		return errors.Errorf("failed to seed development recipients: %w", err)
	}
	// random recipients attached to companies
	err = forEachDevelopmentCompany(companyRepository, func(company *model.Company) error {
		recipients := []DevelopmentRecipient{}
		country = faker.Country()
		domain := faker.DomainName()
		jobLevels := []string{}
		for i := 0; i < 3; i++ {
			jobLevels = append(jobLevels, faker.JobLevel())
		}
		for i := 0; i < 10; i++ {
			jobLevel := jobLevels[faker.Number(0, len(jobLevels)-1)]
			//emailPrefix := faker.Username()
			firstName := faker.FirstName()
			lastName := faker.LastName()
			emailPrefix := strings.ToLower(strings.Join(strings.Split(firstName, " "), "."))
			country := faker.Country()
			recipients = append(recipients, DevelopmentRecipient{
				Email:      emailPrefix + "@" + domain,
				FirstName:  firstName,
				LastName:   lastName,
				Department: jobLevel,
				Position:   faker.JobTitle(),
				City:       faker.City(),
				Country:    country,
				CompanyID:  company.ID.MustGet().String(),
			})
		}
		err := createDevelopmentRecipients(recipients, recipientRepository)
		if err != nil {
			return errors.Errorf("failed to seed development recipients: %w", err)
		}
		return nil
	})
	if err != nil {
		return errors.Errorf("failed to seed development recipients: %w", err)
	}
	// TODO remove the tmp data, not sure why but last i removed it, it
	// broke the seeding of the company data
	//tmpRecpMap = map[string][]*uuid.UUID{}
	return nil
}

// SeedDevelopmentRecipientGroups seeds recipient groups
func SeedDevelopmentRecipientGroups(
	faker *gofakeit.Faker,
	companyRepository *repository.Company,
	recipientRepository *repository.Recipient,
	recipientGroupRepository *repository.RecipientGroup,
) error {
	// recipients holds the recipients we want to add to the recipient group
	recipients := []*struct {
		Email string
		Model *model.Recipient
		Group string
	}{
		{
			Email: TEST_RECIPIENT_EMAIL_1,
			Model: nil,
			Group: TEST_RECIPIENT_GROUP_NAME_1,
		},
		{
			Email: TEST_RECIPIENT_EMAIL_2,
			Model: nil,
			Group: TEST_RECIPIENT_GROUP_NAME_1,
		},
		{
			Email: TEST_RECIPIENT_EMAIL_3,
			Model: nil,
			Group: TEST_RECIPIENT_GROUP_NAME_2,
		},
		{
			Email: TEST_RECIPIENT_EMAIL_4,
			Model: nil,
			Group: TEST_RECIPIENT_GROUP_NAME_2,
		},
	}
	for _, recipient := range recipients {
		email, err := vo.NewEmail(recipient.Email)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		recp, err := recipientRepository.GetByEmailAndCompanyID(
			context.Background(),
			email,
			nil,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if recp == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "recipient not found")
		}
		recipient.Model = recp

	}

	recipientGroups := []struct {
		Name string
	}{
		{
			Name: TEST_RECIPIENT_GROUP_NAME_1,
		},
		{
			Name: TEST_RECIPIENT_GROUP_NAME_2,
		},
	}
	for _, recipientGroup := range recipientGroups {
		rgID := nullable.NewNullableWithValue(uuid.New())
		name := nullable.NewNullableWithValue(*vo.NewString127Must(recipientGroup.Name))
		rg := model.RecipientGroup{
			ID:   rgID,
			Name: name,
		}
		r, err := recipientGroupRepository.GetByNameAndCompanyID(
			context.Background(),
			name.MustGet().String(),
			nil,
			&repository.RecipientGroupOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if r != nil {
			continue
		}
		id, err := recipientGroupRepository.Insert(
			context.Background(),
			&rg,
		)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		recpUUIDs := []*uuid.UUID{}
		for _, recipient := range recipients {
			if recipient.Group == recipientGroup.Name {
				id := recipient.Model.ID.MustGet()
				recpUUIDs = append(recpUUIDs, &id)
			}
		}
		err = recipientGroupRepository.AddRecipients(
			context.Background(),
			id,
			recpUUIDs,
		)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	// add random company recipients to random recipient groups
	err := forEachDevelopmentCompany(companyRepository, func(company *model.Company) error {
		companyID := company.ID.MustGet()
		groupNameVO := vo.NewString127Must(faker.BuzzWord())
		groupName := nullable.NewNullableWithValue(
			*groupNameVO,
		)
		recpUUIDs := tmpRecpMap[companyID.String()]
		// create the group
		rg := model.RecipientGroup{
			Name:      groupName,
			CompanyID: company.ID,
		}
		r, err := recipientGroupRepository.GetByNameAndCompanyID(
			context.Background(),
			groupNameVO.String(),
			&companyID,
			&repository.RecipientGroupOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("query failed: %s", err)
		}
		if r != nil {
			return nil
		}
		id, err := recipientGroupRepository.Insert(
			context.Background(),
			&rg,
		)
		if err != nil {
			return errors.Errorf("insert failed: %s", err)
		}
		err = recipientGroupRepository.AddRecipients(
			context.Background(),
			id,
			recpUUIDs,
		)
		if err != nil {
			return errors.Errorf(
				"failed to add recipients to group with (groupID: %s and recipients: %s): %w",
				id,
				recpUUIDs,
				err,
			)
		}
		return nil
	})
	if err != nil {
		return errors.Errorf("failed to create company data: %s", err)
	}

	return nil
}

func createDevelopmentRecipients(
	devRecps []DevelopmentRecipient,
	recipientRepository *repository.Recipient,
) error {
	for _, recipient := range devRecps {
		firstName := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.FirstName),
		)
		lastName := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.LastName),
		)
		email := nullable.NewNullNullable[vo.Email]()
		if recipient.Email != "" {
			email.Set(*vo.NewEmailMust(recipient.Email))
		}
		department := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.Department),
		)
		position := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.Position),
		)
		phone := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.Phone),
		)
		extraIdentifier := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.ExtraIdentifier),
		)
		city := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.City),
		)
		country := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.Country),
		)
		misc := nullable.NewNullableWithValue(
			*vo.NewOptionalString127Must(recipient.Misc),
		)
		companyID := nullable.NewNullNullable[uuid.UUID]()
		if recipient.CompanyID != "" {
			cid := uuid.MustParse(recipient.CompanyID)
			companyID.Set(cid)
		}
		recipient := model.Recipient{
			FirstName:       firstName,
			LastName:        lastName,
			Email:           email,
			Department:      department,
			Phone:           phone,
			ExtraIdentifier: extraIdentifier,
			Position:        position,
			City:            city,
			Country:         country,
			Misc:            misc,
			CompanyID:       companyID,
		}
		emailVO := recipient.Email.MustGet()
		var companyIDVO *uuid.UUID
		if recipient.CompanyID.IsSpecified() && !recipient.CompanyID.IsNull() {
			cid := recipient.CompanyID.MustGet()
			companyIDVO = &cid
		}
		r, err := recipientRepository.GetByEmailAndCompanyID(
			context.Background(),
			&emailVO,
			companyIDVO,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if r != nil {
			continue
		}
		id, err := recipientRepository.Insert(
			context.Background(),
			&recipient,
		)
		if err != nil {
			return errors.Errorf("failed to insert: %s", err)
		}
		// adding the recipient to the tmpRecpMap
		if recipient.CompanyID.IsSpecified() && !recipient.CompanyID.IsNull() {
			cid := recipient.CompanyID.MustGet()
			cidStr := cid.String()
			tmpRecpMap[cidStr] = append(tmpRecpMap[cidStr], id)
		}
	}
	return nil
}
