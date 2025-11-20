package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// APISender is a API sender
type APISender struct {
	ID                         nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt                  *time.Time                              `json:"createdAt"`
	UpdatedAt                  *time.Time                              `json:"updatedAt"`
	CompanyID                  nullable.Nullable[uuid.UUID]            `json:"companyID"`
	Name                       nullable.Nullable[vo.String64]          `json:"name"`
	APIKey                     nullable.Nullable[vo.OptionalString255] `json:"apiKey"`
	CustomField1               nullable.Nullable[vo.OptionalString255] `json:"customField1"`
	CustomField2               nullable.Nullable[vo.OptionalString255] `json:"customField2"`
	CustomField3               nullable.Nullable[vo.OptionalString255] `json:"customField3"`
	CustomField4               nullable.Nullable[vo.OptionalString255] `json:"customField4"`
	OAuthProviderID            nullable.Nullable[uuid.UUID]            `json:"oauthProviderID"`
	OAuthProvider              *OAuthProvider                          `json:"oauthProvider"`
	RequestMethod              nullable.Nullable[vo.HTTPMethod]        `json:"requestMethod"`
	RequestURL                 nullable.Nullable[vo.String255]         `json:"requestURL"`
	RequestHeaders             nullable.Nullable[APISenderHeaders]     `json:"requestHeaders"`
	RequestBody                nullable.Nullable[vo.OptionalString1MB] `json:"requestBody"`
	ExpectedResponseStatusCode nullable.Nullable[int]                  `json:"expectedResponseStatusCode"`
	ExpectedResponseHeaders    nullable.Nullable[APISenderHeaders]     `json:"expectedResponseHeaders"`
	ExpectedResponseBody       nullable.Nullable[vo.OptionalString1MB] `json:"expectedResponseBody"`
}

// Validate checks if the API sender has a valid state
func (a *APISender) Validate() error {
	if err := validate.NullableFieldRequired("name", a.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("requestMethod", a.RequestMethod); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("requestURL", a.RequestURL); err != nil {
		return err
	}
	// one of the following is required
	if (!a.ExpectedResponseStatusCode.IsSpecified() || a.ExpectedResponseStatusCode.IsNull()) &&
		!isSpecifiedStringWithContent(a.ExpectedResponseBody) &&
		!isSpecifiedStringWithContent(a.ExpectedResponseHeaders) {
		return validate.WrapErrorWithField(errors.New("expectedResponseStatusCode, expectedResponseBody or expectedResponseHeaders must be supplied"), "Missing field")
	}
	return nil
}

func isSpecifiedStringWithContent[T fmt.Stringer](s nullable.Nullable[T]) bool {
	return s.IsSpecified() && !s.IsNull() && s.MustGet().String() != ""
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (a *APISender) ToDBMap() map[string]interface{} {
	m := map[string]interface{}{}
	if a.Name.IsSpecified() {
		m["name"] = nil
		if name, err := a.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if a.CompanyID.IsSpecified() {
		if a.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = a.CompanyID.MustGet()
		}
	}
	if a.APIKey.IsSpecified() {
		m["api_key"] = nil
		if apiKey, err := a.APIKey.Get(); err == nil {
			m["api_key"] = apiKey.String()
		}
	}
	if a.CustomField1.IsSpecified() {
		m["custom_field1"] = nil
		if customField1, err := a.CustomField1.Get(); err == nil {
			m["custom_field1"] = customField1.String()
		}
	}
	if a.CustomField2.IsSpecified() {
		m["custom_field2"] = nil
		if customField2, err := a.CustomField2.Get(); err == nil {
			m["custom_field2"] = customField2.String()
		}
	}
	if a.CustomField3.IsSpecified() {
		m["custom_field3"] = nil
		if customField3, err := a.CustomField3.Get(); err == nil {
			m["custom_field3"] = customField3.String()
		}
	}
	if a.CustomField4.IsSpecified() {
		m["custom_field4"] = nil
		if customField4, err := a.CustomField4.Get(); err == nil {
			m["custom_field4"] = customField4.String()
		}
	}
	if a.RequestMethod.IsSpecified() {
		m["request_method"] = nil
		if requestMethod, err := a.RequestMethod.Get(); err == nil {
			m["request_method"] = requestMethod.String()
		}
	}
	if a.RequestURL.IsSpecified() {
		m["request_url"] = nil
		if requestURL, err := a.RequestURL.Get(); err == nil {
			m["request_url"] = requestURL.String()
		}
	}
	if a.RequestHeaders.IsSpecified() {
		m["request_headers"] = nil
		if requestHeaders, err := a.RequestHeaders.Get(); err == nil {
			m["request_headers"] = requestHeaders.String()
		}
	}
	if a.RequestBody.IsSpecified() {
		m["request_body"] = nil
		if requestBody, err := a.RequestBody.Get(); err == nil {
			m["request_body"] = requestBody.String()
		}
	}
	if a.ExpectedResponseStatusCode.IsSpecified() {
		m["expected_response_status_code"] = nil
		if expectedResponseStatusCode, err := a.ExpectedResponseStatusCode.Get(); err == nil {
			m["expected_response_status_code"] = expectedResponseStatusCode
		}
	}
	if a.ExpectedResponseHeaders.IsSpecified() {
		m["expected_response_headers"] = nil
		if expectedResponseHeaders, err := a.ExpectedResponseHeaders.Get(); err == nil {
			m["expected_response_headers"] = expectedResponseHeaders.String()
		}
	}
	if a.ExpectedResponseBody.IsSpecified() {
		m["expected_response_body"] = nil
		if expectedResponseBody, err := a.ExpectedResponseBody.Get(); err == nil {
			m["expected_response_body"] = expectedResponseBody.String()
		}
	}
	if a.OAuthProviderID.IsSpecified() {
		if a.OAuthProviderID.IsNull() {
			m["o_auth_provider_id"] = nil
		} else {
			m["o_auth_provider_id"] = a.OAuthProviderID.MustGet()
		}
	}
	return m
}

// TODO should the rest of the code be moved to value object
type HTTPHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// APISenderHeaders is a header for a API sender
type APISenderHeaders struct {
	Headers []*HTTPHeader
}

// NewAPISenderHeader creates a new APISenderHeader
// it takes a newline separated string with headers of the format .+: .+ (key: value)
func NewAPISenderHeader(headers string) (*APISenderHeaders, error) {
	headers = strings.TrimSpace(headers)
	if headers == "" {
		return &APISenderHeaders{}, nil
	}
	// split the headers
	lines := strings.Split(headers, "\n")
	// if there is a single header
	if len(lines) == 0 {
		return &APISenderHeaders{
			Headers: []*HTTPHeader{},
		}, nil
	}
	headerLines := []*HTTPHeader{}
	for _, line := range lines {
		// split the key value
		parts := strings.Split(line, ":")
		// there should be atleast 2 parts, key and value
		if len(parts) < 2 {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("invalid header: %s", line),
				"header",
			)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(strings.Join(parts[1:], ":"))
		headerLines = append(headerLines, &HTTPHeader{
			Key:   key,
			Value: value,
		})
	}
	return &APISenderHeaders{
		Headers: headerLines,
	}, nil
}

// MarshalJSON implements the json.Marshaler interface
func (s APISenderHeaders) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON unmarshals the json into a string
func (s *APISenderHeaders) UnmarshalJSON(data []byte) error {
	var header string
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	ss, err := NewAPISenderHeader(header)
	if err != nil {
		return err
	}
	s.Headers = ss.Headers
	return nil
}

// String returns the string representation of the APISenderHeader
func (a APISenderHeaders) String() string {
	headers := ""
	for _, header := range a.Headers {
		headers += header.Key + ": " + header.Value + "\n"
	}
	return headers
}

func NewAPISenderExample() *APISender {
	apiSenderRequestHeaders, err := NewAPISenderHeader("foo: bar")
	if err != nil {
		panic("APISender example data MUST be valid")
	}
	return &APISender{
		Name: nullable.NewNullableWithValue(
			*vo.NewString64Must("Example"),
		),
		APIKey: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must(
				"rj90jf09jr09j2r",
			),
		),
		CustomField1: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must(
				"custom1",
			),
		),
		CustomField2: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must(
				"custom2",
			),
		),
		CustomField3: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must(
				"custom3",
			),
		),
		CustomField4: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must(
				"custom4",
			),
		),
		RequestMethod: nullable.NewNullableWithValue(
			*vo.NewHTTPMethodMust("POST"),
		),
		RequestURL: nullable.NewNullableWithValue(
			*vo.NewString255Must("https://example.com"),
		),
		RequestHeaders: nullable.NewNullableWithValue(
			*apiSenderRequestHeaders,
		),
		RequestBody: nullable.NewNullableWithValue(
			*vo.NewOptionalString1MBMust("<p>Hello World</p>"),
		),
		ExpectedResponseStatusCode: nullable.NewNullableWithValue(200),
		ExpectedResponseHeaders: nullable.NewNullableWithValue(
			*apiSenderRequestHeaders,
		),
		ExpectedResponseBody: nullable.NewNullableWithValue(
			*vo.NewOptionalString1MBMust("<p>World</p>"),
		),
	}
}
