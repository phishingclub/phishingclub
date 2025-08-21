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

type DevelopmentCompany struct {
	Name string
}

// SeedDevelopmentCompanies seeds companies
func SeedDevelopmentCompanies(
	companyRepository *repository.Company,
	faker *gofakeit.Faker,
) error {
	companies := []DevelopmentCompany{
		{
			Name: TEST_COMPANY_NAME_1,
		},
		{
			Name: TEST_COMPANY_NAME_2,
		},
		{
			Name: TEST_COMPANY_NAME_3,
		},
		{
			Name: TEST_COMPANY_NAME_4,
		},
		{
			Name: TEST_COMPANY_NAME_5,
		},
	}
	// add random companies
	for i := 0; i < 10; i++ {
		companies = append(companies, DevelopmentCompany{
			Name: faker.Company(),
		})
	}
	for _, company := range companies {
		id := nullable.NewNullableWithValue(uuid.New())
		n := vo.NewString64Must(company.Name)
		name := nullable.NewNullableWithValue(*n)
		createCompany := model.Company{
			ID:   id,
			Name: name,
		}
		c, err := companyRepository.GetByName(context.Background(), company.Name)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if c != nil {
			continue
		}
		_, err = companyRepository.Insert(context.TODO(), &createCompany)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
