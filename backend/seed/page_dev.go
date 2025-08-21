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

const developmentHtmlInput = `
<br><br>
<form action="{{.URL}}" method="post">
	<input type="text" name="username" placeholder="Username">
	<input type="password" name="password" placeholder="Password">
	<input type="submit" value="Login">
</form>
`

type DevelopmentPage struct {
	Name      string
	Content   string
	CompanyID string
}

// SeedDevelopmentPages seeds pages
func SeedDevelopmentPages(
	pageRepository *repository.Page,
	companyRepository *repository.Company,
	faker *gofakeit.Faker,
) error {
	// add dev pages
	pages := []DevelopmentPage{
		{
			Name:    TEST_PAGE_NAME_1,
			Content: "Welcome to the Phishing Club" + developmentHtmlInput,
		},
	}
	err := createDevelopmentPages(pages, pageRepository, nil)
	if err != nil {
		return errors.Errorf("failed to seed development pages: %w", err)
	}
	// random pages
	for i := 0; i < 10; i++ {
		pages = append(pages, DevelopmentPage{
			Name:    faker.ProductName(),
			Content: faker.Sentence(50) + developmentHtmlInput,
		})
	}
	err = createDevelopmentPages(pages, pageRepository, nil)
	if err != nil {
		return errors.Errorf("failed to seed development pages: %w", err)
	}
	// random pages attached to companies
	err = forEachDevelopmentCompany(companyRepository, func(company *model.Company) error {
		pages := []DevelopmentPage{}
		companyID := company.ID.MustGet()
		for i := 0; i < 10; i++ {
			pages = append(pages, DevelopmentPage{
				Name:      faker.ProductName(),
				Content:   faker.Sentence(50) + developmentHtmlInput,
				CompanyID: companyID.String(),
			})
		}
		err := createDevelopmentPages(pages, pageRepository, &companyID)
		if err != nil {
			return errors.Errorf("failed to seed development pages: %w", err)
		}
		return nil
	})
	if err != nil {
		return errors.Errorf("failed to seed development pages: %w", err)
	}
	return nil
}

func createDevelopmentPages(
	pages []DevelopmentPage,
	pageRepository *repository.Page,
	companyID *uuid.UUID,
) error {
	for _, page := range pages {
		id := uuid.New()
		name := vo.NewString64Must(page.Name)
		content := vo.NewOptionalString1MBMust(page.Content)
		nullableCompanyID := nullable.NewNullNullable[uuid.UUID]()
		if page.CompanyID != "" {
			cid := uuid.MustParse(page.CompanyID)
			nullableCompanyID.Set(cid)
		}
		createPage := model.Page{
			ID:        nullable.NewNullableWithValue(id),
			Name:      nullable.NewNullableWithValue(*name),
			Content:   nullable.NewNullableWithValue(*content),
			CompanyID: nullableCompanyID,
		}
		p, err := pageRepository.GetByNameAndCompanyID(
			context.Background(),
			name,
			companyID,
			&repository.PageOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if p != nil {
			continue
		}
		_, err = pageRepository.Insert(context.TODO(), &createPage)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
