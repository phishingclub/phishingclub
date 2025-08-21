package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

func SeedIdentifiers(
	db *gorm.DB,
	identifierRepository *repository.Identifier,
) error {
	ids := []model.Identifier{
		{Name: nullable.NewNullableWithValue("action")},
		{Name: nullable.NewNullableWithValue("category")},
		{Name: nullable.NewNullableWithValue("categoryId")},
		{Name: nullable.NewNullableWithValue("context")},
		{Name: nullable.NewNullableWithValue("data")},
		{Name: nullable.NewNullableWithValue("filter")},
		{Name: nullable.NewNullableWithValue("id")},
		{Name: nullable.NewNullableWithValue("item")},
		{Name: nullable.NewNullableWithValue("key")},
		{Name: nullable.NewNullableWithValue("p")},
		{Name: nullable.NewNullableWithValue("page")},
		{Name: nullable.NewNullableWithValue("pageId")},
		{Name: nullable.NewNullableWithValue("param")},
		{Name: nullable.NewNullableWithValue("q")},
		{Name: nullable.NewNullableWithValue("ref")},
		{Name: nullable.NewNullableWithValue("s")},
		{Name: nullable.NewNullableWithValue("search")},
		{Name: nullable.NewNullableWithValue("session")},
		{Name: nullable.NewNullableWithValue("sessionId")},
		{Name: nullable.NewNullableWithValue("state")},
		{Name: nullable.NewNullableWithValue("state")},
		{Name: nullable.NewNullableWithValue("token")},
		{Name: nullable.NewNullableWithValue("type")},
		{Name: nullable.NewNullableWithValue("url")},
		{Name: nullable.NewNullableWithValue("userId")},
	}
	for _, identifier := range ids {
		// check if the entry already exists
		m, err := identifierRepository.GetByName(
			context.Background(),
			identifier.Name.MustGet(),
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if m != nil {
			continue
		}
		_, err = identifierRepository.Insert(
			context.Background(),
			&identifier,
		)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
