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

type DevelopmentDomain struct {
	Name      string
	CompanyID string
}

func SeedDevelopmentDomains(
	domainRepository *repository.Domain,
	companyRepository *repository.Company,
	faker *gofakeit.Faker,
) error {
	domains := []DevelopmentDomain{
		{
			Name: TEST_DOMAIN_NAME_1,
		},
		{
			Name: TEST_DOMAIN_NAME_2,
		},
		{
			Name: TEST_DOMAIN_NAME_3,
		},
		{
			Name: TEST_DOMAIN_NAME_4,
		},
	}
	// random domains
	for i := 0; i < 10; i++ {
		domains = append(domains, DevelopmentDomain{
			Name: faker.DomainName() + ".test",
		})
	}
	err := createDevelopmentDomains(domains, faker, domainRepository)
	if err != nil {
		return errors.Errorf("failed to seed development domains: %w", err)
	}
	// random domains attached to companies
	err = forEachDevelopmentCompany(companyRepository, func(company *model.Company) error {
		domains := []DevelopmentDomain{}
		for i := 0; i < 10; i++ {
			domains = append(domains, DevelopmentDomain{
				Name:      "phishing.club." + gofakeit.DomainName() + ".test",
				CompanyID: company.ID.MustGet().String(),
			})
		}
		err := createDevelopmentDomains(domains, faker, domainRepository)
		if err != nil {
			return errors.Errorf("failed to seed development domains: %w", err)
		}
		return nil
	})
	if err != nil {
		return errors.Errorf("failed to seed development domains: %w", err)
	}
	return nil
}

func createDevelopmentDomains(
	domains []DevelopmentDomain,
	faker *gofakeit.Faker,
	domainRepository *repository.Domain,
) error {
	for _, domain := range domains {
		id := nullable.NewNullableWithValue(uuid.New())
		name := nullable.NewNullableWithValue(*vo.NewString255Must(domain.Name))
		managedTLS := nullable.NewNullableWithValue(true)
		hostWebsite := nullable.NewNullableWithValue(true)
		pageContent := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(faker.HackerPhrase()))
		pageNotFoundContent := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust("not found - " + faker.HipsterSentence(5)))
		redirectURL := nullable.NewNullableWithValue(*vo.NewOptionalString1024Must(""))

		createDomain := model.Domain{
			ID:                  id,
			Name:                name,
			ManagedTLS:          managedTLS,
			HostWebsite:         hostWebsite,
			PageContent:         pageContent,
			PageNotFoundContent: pageNotFoundContent,
			RedirectURL:         redirectURL,
		}
		if domain.CompanyID != "" {
			createDomain.CompanyID = nullable.NewNullableWithValue(uuid.MustParse(domain.CompanyID))
		}
		domainName := createDomain.Name.MustGet()
		d, err := domainRepository.GetByName(
			context.Background(),
			&domainName,
			&repository.DomainOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if d != nil {
			continue
		}
		_, err = domainRepository.Insert(context.TODO(), &createDomain)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
