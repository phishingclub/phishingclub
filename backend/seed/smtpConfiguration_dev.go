//go:build dev

package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// SeedDevelopmentSMTPConfiguration seeds development SMTP configuration
func SeedDevelopmentSMTPConfiguration(
	smtpConfigurationRepository *repository.SMTPConfiguration,
) error {
	configurations := []model.SMTPConfiguration{
		{
			Name:             nullable.NewNullableWithValue(*vo.NewString127Must(TEST_SMTP_CONFIGURATION_NAME_1)),
			Host:             nullable.NewNullableWithValue(*vo.NewString255Must("mailer")),
			Port:             nullable.NewNullableWithValue(*vo.NewPortMust(1025)),
			Username:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
			Password:         nullable.NewNullableWithValue(*vo.NewOptionalString255Must("")),
			IgnoreCertErrors: nullable.NewNullableWithValue(true),
		},
	}
	for _, configuration := range configurations {
		id := uuid.New()
		name := configuration.Name.MustGet()
		configuration.ID = nullable.NewNullableWithValue(id)
		c, err := smtpConfigurationRepository.GetByNameAndCompanyID(
			context.Background(),
			&name,
			nil,
			&repository.SMTPConfigurationOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if c != nil {
			continue
		}
		_, err = smtpConfigurationRepository.Insert(
			context.Background(),
			&configuration,
		)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
