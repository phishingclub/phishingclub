//go:build dev

package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// SeedDevelopmentCampaignTemplates seeds templates
func SeedDevelopmentCampaignTemplates(
	apiRepository *repository.APISender,
	domainRepository *repository.Domain,
	pageRepository *repository.Page,
	emailRepository *repository.Email,
	smtpRepository *repository.SMTPConfiguration,
	templateRepository *repository.CampaignTemplate,
	identifierRepository *repository.Identifier,
) error {
	templates := []struct {
		Name                        string
		DomainName                  string
		LandingPageName             string
		LandingPageTypeName         string
		BeforeLandingPageName       string
		BeforeLandingPageTypeName   string
		AfterLandingPageName        string
		AfterLandingPageTypeName    string
		AfterLandingPageRedirectURL string
		EmailName                   string
		SMTPConfigName              string
		APISenderName               string
		UrlIdentifierName           string
		StateIdentifierName         string
	}{
		{
			Name:                "Phishing",
			DomainName:          TEST_DOMAIN_NAME_1,
			LandingPageName:     TEST_PAGE_NAME_1,
			LandingPageTypeName: data.PAGE_TYPE_LANDING,
			EmailName:           TEST_EMAIL_NAME_1,
			SMTPConfigName:      TEST_SMTP_CONFIGURATION_NAME_1,
			UrlIdentifierName:   TEST_URL_IDENTIFIER_NAME,
			StateIdentifierName: TEST_STATE_IDENTIFIER_NAME,
		},
		{
			Name:                "Test API - Forgot password",
			DomainName:          TEST_DOMAIN_NAME_1,
			LandingPageName:     TEST_PAGE_NAME_1,
			LandingPageTypeName: data.PAGE_TYPE_LANDING,
			EmailName:           TEST_EMAIL_NAME_1,
			APISenderName:       TEST_API_SENDER_NAME_1,
			UrlIdentifierName:   TEST_URL_IDENTIFIER_NAME,
			StateIdentifierName: TEST_STATE_IDENTIFIER_NAME,
		},
	}
	for _, template := range templates {
		urlIdentifier, err := identifierRepository.GetByName(
			context.Background(),
			template.UrlIdentifierName,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if urlIdentifier == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "URL param key not found")
		}
		stateKeyIdentifier, err := identifierRepository.GetByName(
			context.Background(),
			template.StateIdentifierName,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if stateKeyIdentifier == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "state param key not found")
		}
		domainName, err := vo.NewString255(template.DomainName)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		domain, err := domainRepository.GetByName(
			context.Background(),
			domainName,
			&repository.DomainOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if domain == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "domain not found")
		}
		landingPageName, err := vo.NewString64(template.LandingPageName)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		var beforeLandingPageID nullable.Nullable[uuid.UUID]
		if template.BeforeLandingPageName != "" {
			beforeLandingPageName, err := vo.NewString64(template.BeforeLandingPageName)
			if err != nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			beforeLandingPage, err := pageRepository.GetByNameAndCompanyID(
				context.Background(),
				beforeLandingPageName,
				nil,
				&repository.PageOption{},
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			if beforeLandingPage == nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "before landing page not found")
			}
			beforeLandingPageID = beforeLandingPage.ID
		}
		var afterLandingPageUUID nullable.Nullable[uuid.UUID]
		if template.AfterLandingPageName != "" {
			afterLandingPageName, err := vo.NewString64(template.AfterLandingPageName)
			if err != nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			afterLandingPage, err := pageRepository.GetByNameAndCompanyID(
				context.Background(),
				afterLandingPageName,
				nil,
				&repository.PageOption{},
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			if afterLandingPage == nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "before landing page not found")
			}
			afterLandingPageUUID = afterLandingPage.ID
		}

		landingPage, err := pageRepository.GetByNameAndCompanyID(
			context.Background(),
			landingPageName,
			nil,
			&repository.PageOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if landingPage == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "landing page not found")
		}
		t, err := templateRepository.GetByNameAndCompanyID(
			context.Background(),
			template.Name,
			nil,
			&repository.CampaignTemplateOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if t != nil {
			continue
		}
		emailName, err := vo.NewString64(template.EmailName)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		email, err := emailRepository.GetByNameAndCompanyID(
			context.Background(),
			emailName,
			nil,
			&repository.EmailOption{},
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if email == nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "email not found")
		}
		var smtpConfigID nullable.Nullable[uuid.UUID]
		if template.SMTPConfigName != "" {
			smtpConfigurationName, err := vo.NewString127(template.SMTPConfigName)
			if err != nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			smtpConfiguration, err := smtpRepository.GetByNameAndCompanyID(
				context.Background(),
				smtpConfigurationName,
				nil,
				&repository.SMTPConfigurationOption{},
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			if smtpConfiguration == nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "smtp configuration not found")
			}
			smtpConfigID = smtpConfiguration.ID
		}
		var apiSenderID nullable.Nullable[uuid.UUID]
		if template.APISenderName != "" {
			APISenderName, err := vo.NewString64(template.APISenderName)
			if err != nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			apiSender, err := apiRepository.GetByName(
				context.Background(),
				APISenderName,
				nil,
				&repository.APISenderOption{},
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
			}
			if apiSender == nil {
				return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, "API sender not found")
			}
			apiSenderID = apiSender.ID
		}
		id := nullable.NewNullableWithValue(uuid.New())
		name := nullable.NewNullableWithValue(*vo.NewString64Must(template.Name))
		domainID := nullable.NewNullableWithValue(domain.ID.MustGet())
		landingPageID := nullable.NewNullableWithValue(landingPage.ID.MustGet())
		afterLandingPageRedirectURL := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(template.AfterLandingPageRedirectURL))

		createTemplate := model.CampaignTemplate{
			ID:                          id,
			Name:                        name,
			DomainID:                    domainID,
			LandingPageID:               landingPageID,
			BeforeLandingPageID:         beforeLandingPageID,
			AfterLandingPageID:          afterLandingPageUUID,
			AfterLandingPageRedirectURL: afterLandingPageRedirectURL,
			EmailID:                     email.ID,
			SMTPConfigurationID:         smtpConfigID,
			APISenderID:                 apiSenderID,
			URLIdentifierID:             nullable.NewNullableWithValue(urlIdentifier.ID.MustGet()),
			StateIdentifierID:           nullable.NewNullableWithValue(stateKeyIdentifier.ID.MustGet()),
		}
		_, err = templateRepository.Insert(context.TODO(), &createTemplate)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
