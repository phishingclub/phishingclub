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

// SeedDevelopmentAPISenders seeds api sender templates
func SeedDevelopmentAPISenders(
	apiSenderRepository *repository.APISender,
) error {
	apiSenders := []struct {
		Name                       string
		APIKey                     string
		CustomField1               string
		CustomField2               string
		CustomField3               string
		CustomField4               string
		RequestMethod              string
		RequestURL                 string
		RequestHeaders             string
		RequestBody                string
		ExpectedResponseStatusCode int
		ExpectedResponseHeaders    string
		ExpectedResponseBody       string
	}{
		{
			Name:                       TEST_API_SENDER_NAME_1,
			APIKey:                     "BAC0N#CH1P5",
			CustomField1:               "5200",
			CustomField2:               "ALERT",
			CustomField3:               "",
			CustomField4:               "",
			RequestMethod:              "POST",
			RequestURL:                 "http://api-test-server/api-sender/{{urlEscape .CustomField1}}",
			RequestHeaders:             "Content-Type: application/json",
			RequestBody:                "{\"to\": \"{{urlEscape .To}}\", \"from\": \"{{urlEscape .CustomField2}}\", \"content\": \"{{.Content}}\", \"apiKey\": \"{{urlEscape .APIKey}}\" }",
			ExpectedResponseStatusCode: 200,
			ExpectedResponseHeaders:    "Content-Type: application/json",
			ExpectedResponseBody:       "message sent",
		},
	}
	for _, apiSender := range apiSenders {
		id := nullable.NewNullableWithValue(uuid.New())
		apiKey := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(apiSender.APIKey))
		name := nullable.NewNullableWithValue(*vo.NewString64Must(apiSender.Name))
		customField1 := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(apiSender.CustomField1))
		customField2 := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(apiSender.CustomField2))
		customField3 := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(apiSender.CustomField3))
		customField4 := nullable.NewNullableWithValue(*vo.NewOptionalString255Must(apiSender.CustomField4))
		requestMethod := nullable.NewNullableWithValue(*vo.NewHTTPMethodMust(apiSender.RequestMethod))
		requestURL := nullable.NewNullableWithValue(*vo.NewString255Must(apiSender.RequestURL))
		requestHeaders := nullable.NewNullNullable[model.APISenderHeaders]()
		requestBody := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(apiSender.RequestBody))
		expectedResponseStatusCode := nullable.NewNullableWithValue(apiSender.ExpectedResponseStatusCode)
		expectedResponseHeaders := nullable.NewNullNullable[model.APISenderHeaders]()
		expectedResponseBody := nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(apiSender.ExpectedResponseBody))

		apiSender := model.APISender{
			ID:                         id,
			APIKey:                     apiKey,
			Name:                       name,
			CustomField1:               customField1,
			CustomField2:               customField2,
			CustomField3:               customField3,
			CustomField4:               customField4,
			RequestMethod:              requestMethod,
			RequestURL:                 requestURL,
			RequestHeaders:             requestHeaders,
			RequestBody:                requestBody,
			ExpectedResponseStatusCode: expectedResponseStatusCode,
			ExpectedResponseHeaders:    expectedResponseHeaders,
			ExpectedResponseBody:       expectedResponseBody,
		}
		n := apiSender.Name.MustGet()
		a, err := apiSenderRepository.GetByName(context.TODO(), &n, nil, nil)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if a != nil {
			continue
		}
		_, err = apiSenderRepository.Insert(context.TODO(), &apiSender)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
