package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// APISender is a service for API sender
type APISender struct {
	Common
	TemplateService         *Template
	CampaignTemplateService *CampaignTemplate
	APISenderRepository     *repository.APISender
	OAuthProviderService    *OAuthProvider
}

// APISenderTestResponse is a response for testing API sender
type APISenderTestResponse struct {
	APISender *model.APISender       `json:"apiSender"`
	Request   map[string]interface{} `json:"request"`
	Response  map[string]interface{} `json:"response"`
}

// Create creates a new API sender
func (a *APISender) Create(
	ctx context.Context,
	session *model.Session,
	apiSender *model.APISender,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("ApiSender.Create", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// validate
	if err := apiSender.Validate(); err != nil {
		a.Logger.Errorw("failed to validate API sender", "error", err)
		return nil, errs.Wrap(err)
	}
	var companyID *uuid.UUID
	if cid, err := apiSender.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	// check uniqueness
	name := apiSender.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		a.APISenderRepository.DB,
		"api_senders",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		a.Logger.Errorw("failed to check API sender uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		a.Logger.Debugw("AP sender name is already used", "name", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// Insert the entity
	id, err := a.APISenderRepository.Insert(ctx, apiSender)
	if err != nil {
		a.Logger.Errorw("failed to insert API sender", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	a.AuditLogAuthorized(ae)
	return id, nil
}

// GetAll gets all API senders with pagination
func (a *APISender) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	option repository.APISenderOption,
) (*model.Result[model.APISender], error) {
	ae := NewAuditEvent("ApiSender", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get all API senders
	result, err := a.APISenderRepository.GetAll(
		ctx,
		companyID,
		&option,
	)
	if err != nil {
		a.Logger.Errorw("failed to get all API senders", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log of reading
	return result, nil
}

// GetAllOverview gets all API senders with limited data
func (a *APISender) GetAllOverview(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	option repository.APISenderOption,
) (*model.Result[model.APISender], error) {
	ae := NewAuditEvent("ApiSender.GetAllOverview", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get all API senders
	result, err := a.APISenderRepository.GetAllOverview(
		ctx,
		companyID,
		&option,
	)
	if err != nil {
		a.Logger.Errorw("failed to get all API senders", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log of reading
	return result, nil
}

// GetByID gets a API sender by ID
func (a *APISender) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	option *repository.APISenderOption,
) (*model.APISender, error) {
	ae := NewAuditEvent("ApiSender.GetByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get API sender by ID
	ent, err := a.APISenderRepository.GetByID(
		ctx,
		id,
		option,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		a.Logger.Errorw("failed to get API sender by ID", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log for reading
	return ent, nil
}

// GetByCompanyID gets a API senders by company ID
func (a *APISender) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	option *repository.APISenderOption,
) (*model.Result[model.APISender], error) {
	ae := NewAuditEvent("ApiSender.GetByCompanyID", session)
	ae.Details["companyID"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get API sender by ID
	result, err := a.APISenderRepository.GetAllByCompanyID(
		ctx,
		id,
		option,
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err != nil {
		a.Logger.Error("failed to get API senders by company ID", zap.Error(err))
		return nil, errs.Wrap(err)
	}
	// no audit log of reading
	return result, nil
}

// Update updates a API sender
func (a *APISender) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.APISender,
) error {
	ae := NewAuditEvent("ApiSender.UpdateByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	var companyID *uuid.UUID
	if incoming.CompanyID.IsSpecified() && !incoming.CompanyID.IsNull() {
		cid := incoming.CompanyID.MustGet()
		companyID = &cid
	}
	if incoming.Name.IsSpecified() && !incoming.Name.IsNull() {
		// check uniqueness
		name := incoming.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			a.APISenderRepository.DB,
			"api_senders",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			a.Logger.Errorw("failed to check API sender uniqueness", "error", err)
			return err
		}
		if !isOK {
			a.Logger.Debugw("AP sender name is not unique", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}
	}
	// update the api sender - if a field is present not not null update it
	current, err := a.APISenderRepository.GetByID(ctx, id, &repository.APISenderOption{})
	if err != nil {
		a.Logger.Errorw("failed to get API sender by ID", "error", err)
		return err
	}
	if v, err := incoming.Name.Get(); err == nil {
		current.Name.Set(v)
	}
	if incoming.CompanyID.IsSpecified() {
		if v, err := incoming.CompanyID.Get(); err == nil {
			current.CompanyID.Set(v)
		} else {
			current.CompanyID.SetNull()
		}
	}
	if v, err := incoming.APIKey.Get(); err == nil {
		current.APIKey.Set(v)
	}
	if v, err := incoming.CustomField1.Get(); err == nil {
		current.CustomField1.Set(v)
	}
	if v, err := incoming.CustomField2.Get(); err == nil {
		current.CustomField2.Set(v)
	}
	if v, err := incoming.CustomField3.Get(); err == nil {
		current.CustomField3.Set(v)
	}
	if v, err := incoming.CustomField4.Get(); err == nil {
		current.CustomField4.Set(v)
	}
	if v, err := incoming.RequestMethod.Get(); err == nil {
		current.RequestMethod.Set(v)
	}
	if v, err := incoming.RequestURL.Get(); err == nil {
		current.RequestURL.Set(v)
	}
	if v, err := incoming.RequestHeaders.Get(); err == nil {
		current.RequestHeaders.Set(v)
	}
	if v, err := incoming.RequestBody.Get(); err == nil {
		current.RequestBody.Set(v)
	}
	if incoming.ExpectedResponseStatusCode.IsSpecified() {
		if v, err := incoming.ExpectedResponseStatusCode.Get(); err == nil {
			current.ExpectedResponseStatusCode.Set(v)
		} else {
			current.ExpectedResponseStatusCode.SetNull()
		}
	}
	if v, err := incoming.ExpectedResponseHeaders.Get(); err == nil {
		current.ExpectedResponseHeaders.Set(v)
	}
	if v, err := incoming.ExpectedResponseBody.Get(); err == nil {
		current.ExpectedResponseBody.Set(v)
	}
	if incoming.OAuthProviderID.IsSpecified() {
		if v, err := incoming.OAuthProviderID.Get(); err == nil {
			current.OAuthProviderID.Set(v)
		} else {
			current.OAuthProviderID.SetNull()
		}
	}
	if err := current.Validate(); err != nil {
		a.Logger.Errorw("failed to validate API sender", "error", err)
		return err
	}
	err = a.APISenderRepository.UpdateByID(ctx, id, current)
	if err != nil {
		a.Logger.Errorw("failed to update API sender", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes a API sender by ID
func (a *APISender) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("ApiSender.DeleteByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete the relation from the campaign templates
	err = a.CampaignTemplateService.removeAPISenderIDBySenderID(
		ctx,
		session,
		id,
	)
	if err != nil {
		a.Logger.Errorw("failed to remove API sender relation from campaign templates",
			"error", err,
		)
		return err
	}
	// delete the entity
	err = a.APISenderRepository.DeleteByID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to delete API sender", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)

	return nil
}

// SendTest sends a test request to the API sender
// and returns the response
func (a *APISender) SendTest(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*APISenderTestResponse, error) {
	ae := NewAuditEvent("ApiSender.SendTest", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.CampaignTemplateService.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	a.Logger.Debugw("sending test request to API sender", "id", id.String())
	// get the API sender with oauth provider
	apiSender, err := a.APISenderRepository.GetByID(ctx, id, &repository.APISenderOption{
		WithOAuthProvider: true,
	})
	if err != nil {
		a.Logger.Errorw("failed to get API sender by ID", "error", err)
		return nil, errs.Wrap(err)
	}

	// get oauth access token if oauth provider is configured on the api sender
	var oauthAccessToken string
	oauthProviderID, err := apiSender.OAuthProviderID.Get()
	if err == nil && a.OAuthProviderService != nil {
		// oauth provider is configured for this api sender
		token, tokenErr := a.OAuthProviderService.GetValidAccessToken(ctx, oauthProviderID)
		if tokenErr != nil {
			a.Logger.Errorw("failed to get oauth access token for test", "error", tokenErr, "oauthProviderID", oauthProviderID)
			return nil, errs.Wrap(tokenErr)
		}
		oauthAccessToken = token
		a.Logger.Debugw("got oauth access token for api test request", "oauthProviderID", oauthProviderID)
	}
	emailRaw := "bob@enterprise.test"
	email := *vo.NewEmailMust(emailRaw)
	cid := nullable.NewNullableWithValue(uuid.New())
	testEmail := &model.Email{
		Name: nullable.NewNullableWithValue(
			*vo.NewString64Must("Test Email"),
		),
		MailEnvelopeFrom: nullable.NewNullableWithValue(
			*vo.NewMailEnvelopeFromMust(emailRaw),
		),
		MailHeaderFrom: nullable.NewNullableWithValue(
			*vo.NewEmailMust(
				fmt.Sprintf("Bob <%s>", emailRaw),
			),
		),
		MailHeaderSubject: nullable.NewNullableWithValue(
			*vo.NewOptionalString255Must("Test Email Subject"),
		),
		Content: nullable.NewNullableWithValue(
			*vo.NewOptionalString1MBMust("Hi {{.FirstName}},\n\nThis is a test email.\n\nBest,\nBob"),
		),
		AddTrackingPixel: nullable.NewNullableWithValue(false),
	}
	testCampaignRecipient := &model.CampaignRecipient{
		ID: cid,
		Recipient: &model.Recipient{
			ID: cid,
			Email: nullable.NewNullableWithValue(
				email,
			),
			Phone: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("+1234567890"),
			),
			ExtraIdentifier: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("extra-test-identifier"),
			),
			FirstName: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Bob"),
			),
			LastName: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Test"),
			),
			Position: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Lead API Tester"),
			),
			Department: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Research and Development"),
			),
			City: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Odin"),
			),
			Country: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("Denmark"),
			),
			Misc: nullable.NewNullableWithValue(
				*vo.NewOptionalString127Must("This is a test recipient"),
			),
			Company: &model.Company{
				Name: nullable.NewNullableWithValue(
					*vo.NewString64Must("Ravn Enterprise."),
				),
			},
		},
	}
	url, headers, body, err := a.buildRequestWithCustomURL(
		apiSender,
		"api-sender-test.test",
		"id",
		"foo/bar",
		testCampaignRecipient,
		testEmail,
		"",
		oauthAccessToken,
	)
	if err != nil {
		a.Logger.Errorw("failed to build test request", "error", err)
		return nil, errs.Wrap(err)
	}
	requestBody := body.String()
	res, resBodyClose, err := a.sendRequest(
		ctx,
		apiSender,
		headers,
		url,
		body,
	)
	if err != nil {
		a.Logger.Errorw("failed to send test request", "error", err)
		return nil, errs.Wrap(err)
	}
	defer resBodyClose()
	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		a.Logger.Errorw("failed to read response body", "error", err)
		return nil, errs.Wrap(err)
	}
	resData := map[string]any{
		"code":    res.StatusCode,
		"status":  res.Status,
		"headers": res.Header,
		"body":    string(responseBody),
	}
	data := &APISenderTestResponse{
		APISender: apiSender,
		Request: map[string]any{
			"url":     url.String(),
			"headers": headers,
			"body":    requestBody,
		},
		Response: resData,
	}
	a.AuditLogAuthorized(ae)
	return data, nil
}

// Send is a service method that builds and sends a API Sender request
// it does not use auth and must not be used without consideration directly by a controller
func (a *APISender) Send(
	ctx context.Context,
	session *model.Session,
	cTemplate *model.CampaignTemplate,
	campaignRecipient *model.CampaignRecipient,
	domain *model.Domain,
	mailTmpl *template.Template,
	email *model.Email,
) error {
	return a.SendWithCustomURL(ctx, session, cTemplate, campaignRecipient, domain, mailTmpl, email, "")
}

// SendWithCustomURL sends an API request with optional custom campaign URL
func (a *APISender) SendWithCustomURL(
	ctx context.Context,
	session *model.Session,
	cTemplate *model.CampaignTemplate,
	campaignRecipient *model.CampaignRecipient,
	domain *model.Domain,
	mailTmpl *template.Template,
	email *model.Email,
	customCampaignURL string,
) error {
	// get sender details
	apiSenderID, err := cTemplate.APISenderID.Get()
	if err != nil {
		a.Logger.Infow(
			"failed to get API Sender relation from template. Template is incomplete",
			"error", err,
		)
		return err
	}
	apiSender, err := a.GetByID(
		ctx,
		session,
		&apiSenderID,
		&repository.APISenderOption{
			WithOAuthProvider: true,
		},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("api sender did not load: %s", err)
	}
	if apiSender == nil {
		return errors.New("api sender did not load")
	}

	// get oauth access token if oauth provider is configured on the api sender
	var oauthAccessToken string
	oauthProviderID, err := apiSender.OAuthProviderID.Get()
	if err == nil && a.OAuthProviderService != nil {
		// oauth provider is configured for this api sender
		token, err := a.OAuthProviderService.GetValidAccessToken(ctx, oauthProviderID)
		if err != nil {
			a.Logger.Errorw("failed to get oauth access token", "error", err, "oauthProviderID", oauthProviderID)
			return fmt.Errorf("failed to get oauth access token: %w", err)
		}
		oauthAccessToken = token
		a.Logger.Debugw("got oauth access token for api request", "oauthProviderID", oauthProviderID)
	}

	domainName := domain.Name.MustGet()
	urlIdentifier := cTemplate.URLIdentifier
	if urlIdentifier == nil {
		return errors.New("url identifier MUST be loaded in campaign template")
	}
	urlPath := cTemplate.URLPath.MustGet().String()
	url, headers, body, err := a.buildRequestWithCustomURL(
		apiSender,
		domainName.String(),
		urlIdentifier.Name.MustGet(),
		urlPath,
		campaignRecipient,
		email,
		customCampaignURL,
		oauthAccessToken,
	)
	if err != nil {
		a.Logger.Errorw("failed to build api sender request", "error", err)
		return err
	}

	resp, respBodyClose, err := a.sendRequest(
		ctx,
		apiSender,
		headers,
		url,
		body,
	)
	if err != nil {
		a.Logger.Errorw("failed to send api sender request", "error", err)
		return err
	}
	defer respBodyClose()

	// read response body once for reuse in error messages
	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		a.Logger.Errorw("failed to read response body", "error", err)
		return err
	}

	// check if response matches expectations
	nullableExpectedResponseCode := apiSender.ExpectedResponseStatusCode
	if nullableExpectedResponseCode.IsSpecified() && !nullableExpectedResponseCode.IsNull() {
		expectedResponseStatusCode := nullableExpectedResponseCode.MustGet()
		if resp.StatusCode != expectedResponseStatusCode {
			a.Logger.Debugw("api sender got unexpected response status code",
				"statusCode", resp.StatusCode,
				"responseBody", string(resBody),
			)
			return fmt.Errorf("unexpected response status code: %d, body: %s", resp.StatusCode, string(resBody))
		}
	}
	// check for expected headers
	nullableExpectedHeaders := apiSender.ExpectedResponseHeaders
	if nullableExpectedHeaders.IsSpecified() && !nullableExpectedHeaders.IsNull() {
		expectedHeaders := nullableExpectedHeaders.MustGet()
		for _, expectedHeader := range expectedHeaders.Headers {
			header := resp.Header.Get(expectedHeader.Key)

			if !strings.Contains(header, expectedHeader.Value) {
				a.Logger.Debugw("api sender got unexpected response header",
					"expectedKey", expectedHeader.Key,
					"expectedValue", expectedHeader.Value,
					"header", header,
					"responseBody", string(resBody),
				)
				return fmt.Errorf("unexpected response header: expected '%s' to contain '%s' but has '%s', body: %s", expectedHeader.Key, expectedHeader.Value, header, string(resBody))
			}
		}
	}
	nullableExpectedBody := apiSender.ExpectedResponseBody
	if nullableExpectedBody.IsSpecified() && !nullableExpectedBody.IsNull() {
		expectedBody := nullableExpectedBody.MustGet()
		// check for expected body
		if !bytes.Contains(resBody, []byte(expectedBody.String())) {
			a.Logger.Debugw("api sender got unexpected response body",
				"expectedBody", expectedBody,
				"body", string(resBody),
			)
			return fmt.Errorf(
				"unexpected response body: expected to contain '%s', got: %s",
				expectedBody,
				string(resBody),
			)
		}
	}
	return nil
}

func (a *APISender) buildHeader(
	apiSender *model.APISender,
	templateData *map[string]any,
) ([]*model.HTTPHeader, error) {
	// setup headers
	apiReqHeaders := []*model.HTTPHeader{}
	requestHeaders := apiSender.RequestHeaders
	if requestHeaders.IsSpecified() && !requestHeaders.IsNull() {
		for _, header := range requestHeaders.MustGet().Headers {
			keyTemplate := template.New("key")
			keyTemplate, err := keyTemplate.Parse(header.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to parse header key: %s", err)
			}
			keyTemplate = keyTemplate.Funcs(TemplateFuncs())
			var key bytes.Buffer
			if err := keyTemplate.Execute(&key, templateData); err != nil {
				return nil, errs.Wrap(err)
			}
			valueTemplate := template.New("value")
			valueTemplate, err = valueTemplate.Parse(header.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to parse header value: %s", err)
			}
			valueTemplate = valueTemplate.Funcs(TemplateFuncs())
			var value bytes.Buffer
			if err := valueTemplate.Execute(&value, templateData); err != nil {
				return nil, fmt.Errorf("failed to execute value template: %s", err)
			}
			apiReqHeaders = append(
				apiReqHeaders,
				&model.HTTPHeader{
					Key:   key.String(),
					Value: value.String(),
				},
			)
		}
	}
	return apiReqHeaders, nil
}

// sendRequest builds and sends the request to the API
// it returns the response, a function to close the response body, and an error
// the close method MUST be called to avoid leaking resources
func (a *APISender) sendRequest(
	ctx context.Context,
	apiSender *model.APISender,
	apiRequestHeaders []*model.HTTPHeader,
	apiRequestURL *apiRequestURL,
	apiRequestBody *apiRequestBody,
) (*http.Response, func(), error) {
	// prepare request
	reqCtx, reqCancel := context.WithTimeout(ctx, 10*time.Second)
	// context must stay alive until response body is read
	if apiRequestBody == nil {
		apiRequestBody = bytes.NewBuffer([]byte{})
	}
	req, err := http.NewRequestWithContext(
		reqCtx,
		apiSender.RequestMethod.MustGet().String(),
		apiRequestURL.String(),
		apiRequestBody,
	)
	if err != nil {
		reqCancel()
		return nil, func() {}, errs.Wrap(err)
	}
	// TODO these headers should be enrished with template variables like {{.FirstName}} or etc
	for _, header := range apiRequestHeaders {
		req.Header.Set(header.Key, header.Value)
	}
	// debug logging: output request details
	// build headers map for logging
	headersMap := make(map[string]string)
	for _, header := range apiRequestHeaders {
		headersMap[header.Key] = header.Value
	}

	// log request details
	a.Logger.Debugw("sending api request",
		"method", apiSender.RequestMethod.MustGet().String(),
		"url", apiRequestURL.String(),
		"headers", headersMap,
		"body", apiRequestBody.String(),
	)
	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		reqCancel()
		return nil, func() {}, errs.Wrap(err)
	}
	// return cleanup function that closes body and  cancels context
	// context must not be canceled until after response body is read
	// otherwise io.ReadAll(resp.Body) can fail with "context canceled"
	return resp, func() {
		resp.Body.Close()
		reqCancel()
	}, nil
}

type apiRequestURL = bytes.Buffer
type apiRequestBody = bytes.Buffer

func (a *APISender) buildRequest(
	apiSender *model.APISender,
	domainName string,
	urlKey string,
	urlPath string,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email, // todo is this superfluous? it should be in the campaign recipient?
) (*apiRequestURL, []*model.HTTPHeader, *apiRequestBody, error) {
	return a.buildRequestWithCustomURL(apiSender, domainName, urlKey, urlPath, campaignRecipient, email, "", "")
}

// buildRequestWithCustomURL builds an API request with optional custom campaign URL
func (a *APISender) buildRequestWithCustomURL(
	apiSender *model.APISender,
	domainName string,
	urlKey string,
	urlPath string,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	customCampaignURL string,
	oauthAccessToken string,
) (*apiRequestURL, []*model.HTTPHeader, *apiRequestBody, error) {
	// create template data first so it can be used in headers, url, and body
	t := a.TemplateService.CreateMail(
		domainName,
		urlKey,
		urlPath,
		campaignRecipient,
		email,
		apiSender,
	)

	// add oauth access token to template data if available
	if oauthAccessToken != "" {
		(*t)["OAuthAccessToken"] = oauthAccessToken
	}

	// override campaign URL if custom one is provided
	if customCampaignURL != "" {
		templateURL := fmt.Sprintf("https://%s%s?%s=%s", domainName, urlPath, urlKey, campaignRecipient.ID.MustGet().String())
		if customCampaignURL != templateURL {
			(*t)["URL"] = customCampaignURL
		}
	}

	// setup headers
	apiReqHeaders, err := a.buildHeader(apiSender, t)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to build headers: %s", err)
	}
	// setup URL
	requestURL := apiSender.RequestURL.MustGet()
	urlTemplate := template.New("url")
	urlTemplate = urlTemplate.Funcs(TemplateFuncs())
	urlTemplate, err = urlTemplate.Parse(requestURL.String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse url: %s", err)
	}
	var apiURL bytes.Buffer
	if err := urlTemplate.Execute(&apiURL, t); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to execute url template: %s", err)
	}
	// setup body
	// first parse and execute the mail content
	mailContentTemplate := template.New("mailContent")
	mailContentTemplate = mailContentTemplate.Funcs(TemplateFuncs())
	content, err := email.Content.Get()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get email content: %s", err)
	}
	mailTemplate, err := mailContentTemplate.Parse(content.String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse body: %s", err)
	}
	var mailContent bytes.Buffer
	if err := mailTemplate.Execute(&mailContent, t); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to execute mail template: %s", err)
	}
	// Properly encode for JSON
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(mailContent.String()); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to marshal mail content: %s", err)
	}
	// Remove the newline that Encode adds and the surrounding quotes
	jsonStr := strings.TrimSpace(buf.String())
	(*t)["Content"] = jsonStr[1 : len(jsonStr)-1]
	contentTemplate := template.New("content")
	contentTemplate = contentTemplate.Funcs(TemplateFuncs())
	contentTemplate, err = contentTemplate.Parse(apiSender.RequestBody.MustGet().String())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse body: %s", err)
	}
	var body bytes.Buffer
	if err := contentTemplate.Execute(&body, t); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to execute body template: %s", err)
	}
	return &apiURL, apiReqHeaders, &body, nil
}
