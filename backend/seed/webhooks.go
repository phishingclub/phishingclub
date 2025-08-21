package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// SeedDevelopmentWebhooks seeds webhooks
func SeedDevelopmentWebhooks(
	webhooksRepository *repository.Webhook,
) error {
	webhooks := []model.Webhook{
		{
			Name:   nullable.NewNullableWithValue(*vo.NewString127Must("Test Webhook")),
			URL:    nullable.NewNullableWithValue(*vo.NewString1024Must("http://api-test-server/webhook")),
			Secret: nullable.NewNullableWithValue(*vo.NewOptionalString1024Must("WEBHOOK_TEST_KEY@1234")),
		},
	}
	for _, webhook := range webhooks {
		name := webhook.Name.MustGet()
		wh, err := webhooksRepository.GetByName(context.TODO(), &name)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if wh != nil {
			continue
		}
		_, err = webhooksRepository.Insert(context.TODO(), &webhook)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
