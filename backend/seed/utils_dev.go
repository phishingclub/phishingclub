//go:build dev

package seed

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// forEachDevelopmentCompany runs a function for n iterations for each test company
func forEachDevelopmentCompany(
	companyRepository *repository.Company,
	f func(company *model.Company) error,
) error {
	for _, company := range []string{
		TEST_COMPANY_NAME_1,
		TEST_COMPANY_NAME_2,
		TEST_COMPANY_NAME_3,
		TEST_COMPANY_NAME_4,
		TEST_COMPANY_NAME_5,
	} {
		company, err := companyRepository.GetByName(
			context.Background(),
			company,
		)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		err = f(company)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
