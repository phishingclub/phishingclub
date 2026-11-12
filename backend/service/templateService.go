package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/yeqown/go-qrcode/v2"
)

const trackingPixelTemplate = "{{.Tracker}}"

// TemplateService is for handling things related to
// templates such as websites, emails, etc.
type Template struct {
	Common
	RecipientRepository        *repository.Recipient
	OptionRepository           *repository.Option
	RemoteBrowserRepository    *repository.RemoteBrowser
	MicrosoftDeviceCodeService *MicrosoftDeviceCode
}

// CreateMailTemplate creates a new mail template
func (t *Template) CreateMail(
	ctx context.Context,
	domainName string,
	idKey string,
	urlPath string,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender,
	companyID *uuid.UUID,
	flowCtx *FlowContext, // nil in preview, test and validation contexts
) *map[string]any {
	rid := campaignRecipient.ID.MustGet()
	ridStr := rid.String()
	baseURL := "https://" + domainName
	url := BuildLureURL(baseURL, urlPath, idKey, campaignRecipient)
	// set body
	trackingPixelPath := fmt.Sprintf(
		"%s/wf/open?upn=%s",
		baseURL,
		ridStr,
	)
	trackingPixel := fmt.Sprintf(
		"<img src=\"%s/wf/open?upn=%s\" alt=\"\" width=\"1\" height=\"1\" border=\"0\" style=\"height:1px !important;width:1px\" />",
		baseURL,
		ridStr,
	)
	data := t.newTemplateDataMap(
		ridStr,
		baseURL,
		url,
		campaignRecipient.Recipient,
		trackingPixelPath,
		trackingPixel,
		email,
		apiSender,
	)

	// add random recipient data only when the email actually uses it (the lookup is
	// costly), excluding the current recipient
	if email != nil {
		subject := ""
		if v, err := email.MailHeaderSubject.Get(); err == nil {
			subject = v.String()
		}
		content := ""
		if v, err := email.Content.Get(); err == nil {
			content = v.String()
		}
		if usesRandomRecipient(subject, content) {
			(*data)["RandomRecipient"] = t.getRandomRecipientData(ctx, companyID, &rid)
		}
	}

	// full report endpoint URL for this recipient on the campaign domain, so a report
	// header can carry a ready-to-call link instead of just the token
	(*data)["ReportURL"] = fmt.Sprintf("%s/%s/report?rid=%s", baseURL, t.reportPath(ctx), ridStr)

	// direct URLs to each stage of the flow, set only when a campaign flow context is
	// available. before and after stay empty when the flow has no such stage.
	if flowCtx != nil {
		before, landing, after := BuildFlowPageURLs(FlowPageURLParams{
			BaseURL:             baseURL,
			URLPath:             urlPath,
			URLIdentifier:       idKey,
			StateIdentifier:     flowCtx.StateIdentifier,
			CampaignRecipientID: ridStr,
			CampaignID:          flowCtx.CampaignID,
			HasBeforePage:       flowCtx.HasBeforePage,
			HasAfterPage:        flowCtx.HasAfterPage,
		})
		setFlowPageURLs(data, before, landing, after)
	}

	return data
}

// ValidatePageTemplate validates that a page template can be parsed and executed without errors
func (t *Template) ValidatePageTemplate(content string) error {
	_, err := template.New("validation").
		Funcs(TemplateFuncs()).
		Parse(content)

	if err != nil {
		return fmt.Errorf("failed to parse page template: %s", err)
	}

	// also try to execute with mock data to catch runtime errors
	_, err = t.ApplyPageMock(content)
	if err != nil {
		return fmt.Errorf("failed to execute page template: %s", utils.RedactCredentialsFromString(err.Error()))
	}

	return nil
}

// ValidateEmailTemplate validates that an email template can be parsed and executed without errors
func (t *Template) ValidateEmailTemplate(content string) error {
	_, err := template.New("validation").
		Funcs(TemplateFuncs()).
		Parse(content)

	if err != nil {
		return fmt.Errorf("failed to parse email template: %s", err)
	}

	// also try to execute with mock data to catch runtime errors
	domain := &model.Domain{
		Name: nullable.NewNullableWithValue(
			*vo.NewString255Must("example.test"),
		),
	}
	recipient := model.NewRecipientExample()
	campaignRecipient := model.CampaignRecipient{
		ID: nullable.NewNullableWithValue(
			uuid.New(),
		),
		Recipient: recipient,
	}
	email := model.NewEmailExample()
	email.Content = nullable.NewNullableWithValue(
		*vo.NewUnsafeOptionalString1MB(content),
	)
	apiSender := model.NewAPISenderExample()

	_, err = t.CreateMailBody(
		context.Background(),
		"id",
		"/test",
		domain,
		&campaignRecipient,
		email,
		apiSender,
		nil, // no company context for validation
	)
	if err != nil {
		return fmt.Errorf("failed to execute email template: %s", err)
	}

	return nil
}

// ValidateDomainTemplate validates that a domain template can be parsed and executed without errors
func (t *Template) ValidateDomainTemplate(content string) error {
	_, err := template.New("validation").
		Funcs(TemplateFuncs()).
		Parse(content)

	if err != nil {
		return fmt.Errorf("failed to parse domain template: %s", err)
	}

	// also try to execute with mock data to catch runtime errors
	// domains only have access to BaseURL variable
	data := map[string]any{
		"BaseURL": "https://example.test",
	}

	tmpl, err := template.New("domain").
		Funcs(TemplateFuncs()).
		Parse(content)
	if err != nil {
		return fmt.Errorf("failed to parse domain template: %s", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return fmt.Errorf("failed to execute domain template: %s", err)
	}

	return nil
}

// ApplyPageMock renders page content with example data, for previews and
// validation.
func (t *Template) ApplyPageMock(content string) (*bytes.Buffer, error) {
	return t.pageMock(context.Background(), content, model.NewEmailExample(), model.NewRecipientExample(), "example.test", "urlPath", nil)
}

// reportExampleRecipient is the example recipient with a plain email address,
// as a recipient's address is plain in a real campaign.
func reportExampleRecipient() *model.Recipient {
	r := model.NewRecipientExample()
	r.Email = nullable.NewNullableWithValue(*vo.NewEmailMust("rick@company.test"))
	return r
}

// pageMock renders page content for recipient with example identifiers on
// domainName and urlPath. When randomRecipient is set it is
// used as {{.RandomRecipient}} and no recipient is read from the database.
func (t *Template) pageMock(
	ctx context.Context,
	content string,
	email *model.Email,
	recipient *model.Recipient,
	domainName string,
	urlPath string,
	randomRecipient map[string]string,
) (*bytes.Buffer, error) {
	domain := &database.Domain{
		Name: domainName,
	}
	campaignRecipientID := uuid.New()
	campaignTemplate := &model.CampaignTemplate{
		URLIdentifier: &model.Identifier{
			Name: nullable.NewNullableWithValue("id"),
		},
		StateIdentifier: &model.Identifier{
			Name: nullable.NewNullableWithValue("state"),
		},
	}
	return t.createPhishingPage(
		ctx,
		domain,
		email,
		&campaignRecipientID,
		nil,
		recipient,
		content,
		campaignTemplate,
		"stateParam",
		urlPath,
		nil,
		nil, // no company context for a mock
		randomRecipient,
	)
}

// reportMockRandomRecipient is the fixed stand in for {{.RandomRecipient}} in
// report screenshot renders, so a capture never reads a real recipient.
func reportMockRandomRecipient() map[string]string {
	return map[string]string{
		"FirstName":       "John",
		"LastName":        "Smith",
		"Email":           "john.smith@example.test",
		"Phone":           "+1 555 0100",
		"Position":        "Finance Manager",
		"Department":      "Finance",
		"City":            "Springfield",
		"Country":         "United States",
		"ExtraIdentifier": "",
		"Misc":            "",
	}
}

// CreateMailBodyForReport renders an email's content for a campaign report
// screenshot with reportExampleRecipient and reportMockRandomRecipient, so the
// render never reads a real recipient. It does not use CreateMailBody, which
// reads a real recipient for {{.RandomRecipient}}. domainName and urlPath are
// the campaign domain and path the links and asset URLs point at.
func (t *Template) CreateMailBodyForReport(email *model.Email, domainName string, urlPath string) (string, error) {
	content, err := email.Content.Get()
	if err != nil {
		return "", fmt.Errorf("no email content set to render: %w", err)
	}
	return t.renderMailForReport(email, content.String(), domainName, urlPath)
}

// CreateMailSubjectForReport renders an email's subject for a campaign report
// the same way CreateMailBodyForReport renders its content.
func (t *Template) CreateMailSubjectForReport(email *model.Email, domainName string, urlPath string) (string, error) {
	subject, err := email.MailHeaderSubject.Get()
	if err != nil {
		return "", fmt.Errorf("no email subject set to render: %w", err)
	}
	return t.renderMailForReport(email, subject.String(), domainName, urlPath)
}

// renderMailForReport renders text, the content or subject of email, with
// reportExampleRecipient and reportMockRandomRecipient.
func (t *Template) renderMailForReport(email *model.Email, text string, domainName string, urlPath string) (string, error) {
	recipient := reportExampleRecipient()
	baseURL := "https://" + domainName
	recipientID := uuid.Nil.String()
	trackingPixelPath := fmt.Sprintf("%s/wf/open?upn=%s", baseURL, recipientID)
	trackingPixel := fmt.Sprintf(
		`<img src="%s" alt="" width="1" height="1" border="0" style="height:1px !important;width:1px" />`,
		trackingPixelPath,
	)
	data := t.newTemplateDataMap(
		recipientID,
		baseURL,
		baseURL+urlPath,
		recipient,
		trackingPixelPath,
		trackingPixel,
		email,
		nil, // no API sender context in a report screenshot render
	)
	(*data)["RandomRecipient"] = reportMockRandomRecipient()
	tmpl, err := template.New("reportEmailMock").Funcs(TemplateFuncs()).Parse(text)
	if err != nil {
		return "", fmt.Errorf("failed to parse email template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute email template: %w", err)
	}
	return buf.String(), nil
}

// CreatePageMockForReport renders a page's content for a campaign report
// screenshot with reportExampleRecipient and reportMockRandomRecipient, so the
// render never reads a real recipient. email supplies the sender values the
// page may use, such as {{.FromName}}, and may be nil, in which case an
// example email stands in for it. domainName and urlPath are the campaign
// domain and path the page is served on.
func (t *Template) CreatePageMockForReport(
	ctx context.Context,
	page *model.Page,
	email *model.Email,
	domainName string,
	urlPath string,
) (*bytes.Buffer, error) {
	content, err := page.Content.Get()
	if err != nil {
		return nil, fmt.Errorf("no page content set to render: %w", err)
	}
	if email == nil {
		email = model.NewEmailExample()
	}
	return t.pageMock(ctx, content.String(), email, reportExampleRecipient(), domainName, urlPath, reportMockRandomRecipient())
}

// CreateMailBody returns a rendered mail body to string
func (t *Template) CreateMailBody(
	ctx context.Context,
	urlIdentifier string,
	urlPath string,
	domain *model.Domain,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender, // can be nil
	companyID *uuid.UUID,
) (string, error) {
	return t.CreateMailBodyWithCustomURL(
		ctx,
		urlIdentifier,
		urlPath,
		domain,
		campaignRecipient,
		email,
		apiSender,
		"", // empty customURL means use template domain
		companyID,
	)
}

// CreateMailBodyWithCustomURL returns a rendered mail body to string with optional custom campaign URL
func (t *Template) CreateMailBodyWithCustomURL(
	ctx context.Context,
	urlIdentifier string,
	urlPath string,
	domain *model.Domain,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender, // can be nil
	customCampaignURL string, // if provided, overrides the default campaign URL
	companyID *uuid.UUID,
) (string, error) {
	return t.CreateMailBodyWithCustomURLAndRecipient(
		ctx,
		urlIdentifier,
		urlPath,
		domain,
		campaignRecipient,
		email,
		apiSender,
		customCampaignURL,
		companyID,
		nil,
		nil,
		nil,
	)
}

// CreateMailBodyWithCustomURLAndRecipient renders a mail body with an optional custom campaign URL.
// when campaignID and recipientID are non-nil the DeviceCode template functions are wired to the
// live implementation so that {{MicrosoftDeviceCode}} / {{MicrosoftDeviceCodeURL}} resolve correctly
// in email bodies and attachments.
func (t *Template) CreateMailBodyWithCustomURLAndRecipient(
	ctx context.Context,
	urlIdentifier string,
	urlPath string,
	domain *model.Domain,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender, // can be nil
	customCampaignURL string, // if provided, overrides the default campaign URL
	companyID *uuid.UUID,
	campaignID *uuid.UUID, // if non-nil, device code funcs are wired
	recipientID *uuid.UUID, // if non-nil, device code funcs are wired
	flowCtx *FlowContext, // nil when no campaign flow context is available
) (string, error) {
	mailData := t.CreateMail(
		ctx,
		domain.Name.MustGet().String(),
		urlIdentifier,
		urlPath,
		campaignRecipient,
		email,
		apiSender,
		companyID,
		flowCtx,
	)

	// override campaign URL if custom one is provided
	if customCampaignURL != "" {
		(*mailData)["URL"] = customCampaignURL
	}

	// use device code funcs when both ids are present so {{MicrosoftDeviceCode}} resolves
	var funcs template.FuncMap
	if campaignID != nil && recipientID != nil {
		funcs = t.TemplateFuncsWithDeviceCode(ctx, campaignID, recipientID)
	} else {
		funcs = t.TemplateFuncsWithCompany(ctx, companyID)
	}

	mailContentTemplate := template.New("mailContent")
	mailContentTemplate = mailContentTemplate.Funcs(funcs)
	content, err := email.Content.Get()
	if err != nil {
		t.Logger.Errorw("failed to get email content", "error", err)
		return "", errs.Wrap(err)
	}
	mailTemplate, err := mailContentTemplate.Parse(content.String())
	if err != nil {
		t.Logger.Errorw("failed to parse body", "error", err)
		return "", errs.Wrap(err)
	}
	var mailContent bytes.Buffer
	if err := mailTemplate.Execute(&mailContent, mailData); err != nil {
		t.Logger.Errorw("failed to execute mail template", "error", err)
		return "", errs.Wrap(err)
	}
	(*mailData)["Content"] = mailContent.String()
	var body bytes.Buffer
	if err := mailContentTemplate.Execute(&body, mailData); err != nil {
		t.Logger.Errorw("failed to execute body template", "error", err)
		return "", errs.Wrap(err)
	}
	return body.String(), nil
}

// CreatePhishingPage creates a new phishing page
func (t *Template) CreatePhishingPage(
	ctx context.Context,
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	companyID *uuid.UUID,
) (*bytes.Buffer, error) {
	return t.CreatePhishingPageWithCampaign(ctx, domain, email, campaignRecipientID, recipient, contentToRender, campaignTemplate, stateParam, urlPath, nil, companyID)
}

// CreatePhishingPageWithCampaign creates a new phishing page with optional campaign for deny URL support
func (t *Template) CreatePhishingPageWithCampaign(
	ctx context.Context,
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	campaign *model.Campaign,
	companyID *uuid.UUID,
) (*bytes.Buffer, error) {
	return t.CreatePhishingPageWithCampaignAndRecipient(ctx, domain, email, campaignRecipientID, nil, recipient, contentToRender, campaignTemplate, stateParam, urlPath, campaign, companyID)
}

// CreatePhishingPageWithCampaignAndRecipient creates a new phishing page with optional campaign and
// explicit recipient id for device code phishing support. when recipientID is non-nil the DeviceCode
// template function is wired to the live implementation.
func (t *Template) CreatePhishingPageWithCampaignAndRecipient(
	ctx context.Context,
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	campaign *model.Campaign,
	companyID *uuid.UUID,
) (*bytes.Buffer, error) {
	return t.createPhishingPage(ctx, domain, email, campaignRecipientID, recipientID, recipient, contentToRender, campaignTemplate, stateParam, urlPath, campaign, companyID, nil)
}

// createPhishingPage renders a phishing page. When randomRecipient is set it
// is used as {{.RandomRecipient}} and no recipient is read from the database.
func (t *Template) createPhishingPage(
	ctx context.Context,
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	campaign *model.Campaign,
	companyID *uuid.UUID,
	randomRecipient map[string]string,
) (*bytes.Buffer, error) {
	w := bytes.NewBuffer([]byte{})
	id := campaignRecipientID.String()
	baseURL := "https://" + domain.Name
	if len(domain.Name) == 0 {
		baseURL = ""
	}
	urlIdentifier := campaignTemplate.URLIdentifier.Name.MustGet()
	stateIdentifier := campaignTemplate.StateIdentifier.Name.MustGet()

	// construct URL with original path preserved
	fullURL := baseURL + urlPath

	// parse existing query parameters to avoid duplicates
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return w, fmt.Errorf("failed to parse URL for parameter checking: %w", err)
	}

	queryParams := parsedURL.Query()

	// the page flow url keeps the query form even in path mode, because the
	// encrypted state param driving the next page travels in the query string
	// regardless. path mode covers the delivered link, the one a recipient reads.
	// only add campaign parameters if they don't already exist
	if !queryParams.Has(urlIdentifier) {
		queryParams.Set(urlIdentifier, id)
	}
	if !queryParams.Has(stateIdentifier) {
		queryParams.Set(stateIdentifier, stateParam)
	}

	parsedURL.RawQuery = queryParams.Encode()
	finalURL := parsedURL.String()

	// create deny URL if campaign and deny page exist
	denyURL := ""
	if campaign != nil {
		if _, err := campaign.DenyPageID.Get(); err == nil {
			// create a special state param that indicates deny page should be served
			campaignID := campaign.ID.MustGet()
			denyStateParam, encErr := utils.Encrypt("deny", utils.UUIDToSecret(&campaignID))
			if encErr == nil {
				// create deny URL with proper parameter handling
				denyParsedURL, _ := url.Parse(fullURL)
				denyQueryParams := denyParsedURL.Query()
				if !denyQueryParams.Has(urlIdentifier) {
					denyQueryParams.Set(urlIdentifier, id)
				}
				denyQueryParams.Set(stateIdentifier, denyStateParam) // always set deny state
				denyParsedURL.RawQuery = denyQueryParams.Encode()
				denyURL = denyParsedURL.String()
			}
		}
	}

	var templateFuncs template.FuncMap
	if campaign != nil && recipientID != nil {
		campaignID := campaign.ID.MustGet()
		templateFuncs = t.TemplateFuncsWithDeviceCode(ctx, &campaignID, recipientID)
	} else {
		templateFuncs = t.TemplateFuncsWithCompany(ctx, companyID)
	}

	// Add RemoteBrowserScript - injects a <script> that opens a WS to the victim
	// endpoint and exposes window.RemoteBrowser.on(event, fn) / .send(event, data).
	// Usage in page template: {{RemoteBrowserScript "remote-browser-uuid"}}
	{
		wsPath := t.remoteBrowserWSPath(ctx)
		capturedID := id // close over the campaign recipient ID for this render
		hasCampaign := campaign != nil
		// opts is a client transport concern set per page, not stored on the
		// script: opts[0] = transport ("auto" default, or "polling"); opts[1] =
		// poll interval in ms (default 1000, clamped). Usage:
		//   {{RemoteBrowserScript "name"}}
		//   {{RemoteBrowserScript "name" "polling"}}
		//   {{RemoteBrowserScript "name" "polling" "2000"}}
		templateFuncs["RemoteBrowserScript"] = func(rbName string, opts ...string) string {
			if !hasCampaign || capturedID == "" || rbName == "" || t.RemoteBrowserRepository == nil {
				return ""
			}
			nameVO, err := vo.NewString64(rbName)
			if err != nil {
				return ""
			}
			rb, err := t.RemoteBrowserRepository.GetByNameAndCompanyID(ctx, nameVO, companyID, &repository.RemoteBrowserOption{})
			if err != nil {
				return ""
			}
			rbIDVal, err := rb.ID.Get()
			if err != nil {
				return ""
			}
			// transport mode and poll cadence are set per page here (client
			// concern), defaulting to auto / 1000ms.
			transport := model.TransportAuto
			if len(opts) > 0 && opts[0] != "" {
				transport = model.NormalizeTransport(opts[0])
			}
			pollMS := model.DefaultPollIntervalMS
			if len(opts) > 1 {
				if n, convErr := strconv.Atoi(strings.TrimSpace(opts[1])); convErr == nil {
					pollMS = model.ClampPollIntervalMS(n)
				}
			}
			script := strings.NewReplacer(
				"__WS_PATH__", wsPath,
				"__CR_ID__", capturedID,
				"__RB_ID__", rbIDVal.String(),
				"__RB_TRANSPORT__", transport,
				"__RB_POLL_MS__", strconv.Itoa(pollMS),
			).Replace(embedded.RemoteBrowserInjectJS)
			return "<script>" + script + "</script>"
		}
	}

	tmpl, err := template.New("page").
		Funcs(templateFuncs).
		Parse(contentToRender)

	if err != nil {
		return w, fmt.Errorf("failed to parse page template: %s", err)
	}
	data := t.newTemplateDataMapWithDenyURL(
		id,
		baseURL,
		finalURL,
		denyURL,
		recipient,
		"", // trackingPixelPath
		"", // trackingPixelMarkup
		email,
		nil, // apiSender
	)

	// add random recipient data only when the page template uses it (the lookup is
	// costly), excluding the current recipient
	if randomRecipient != nil {
		(*data)["RandomRecipient"] = randomRecipient
	} else if usesRandomRecipient(contentToRender) {
		var excludeRecipientID *uuid.UUID
		if rid, err := recipient.ID.Get(); err == nil {
			excludeRecipientID = &rid
		}
		(*data)["RandomRecipient"] = t.getRandomRecipientData(ctx, companyID, excludeRecipientID)
	}

	// direct URLs to each stage of the flow, available when this page is served as part
	// of a real campaign. before and after stay empty when the flow has no such stage.
	if campaign != nil {
		campaignID := campaign.ID.MustGet()
		flowCtx := NewFlowContext(campaignTemplate, campaignID)
		if flowCtx != nil {
			before, landing, after := BuildFlowPageURLs(FlowPageURLParams{
				BaseURL:             baseURL,
				URLPath:             urlPath,
				URLIdentifier:       urlIdentifier,
				StateIdentifier:     stateIdentifier,
				CampaignRecipientID: id,
				CampaignID:          flowCtx.CampaignID,
				HasBeforePage:       flowCtx.HasBeforePage,
				HasAfterPage:        flowCtx.HasAfterPage,
			})
			setFlowPageURLs(data, before, landing, after)
		}
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		return w, fmt.Errorf("failed to execute page template: %s", utils.RedactCredentialsFromString(err.Error()))
	}
	return w, nil
}

// newTemplateDataMap creates a new data map for templates
func (t *Template) newTemplateDataMap(
	recipientID string,
	baseURL string,
	url string,
	recipient *model.Recipient,
	trackingPixelPath string,
	trackingPixelMarkup string,
	email *model.Email,
	apiSender *model.APISender,
) *map[string]any {
	recipientFirstName := ""
	if v, err := recipient.FirstName.Get(); err == nil {
		recipientFirstName = v.String()
	}
	recipientLastName := ""
	if v, err := recipient.LastName.Get(); err == nil {
		recipientLastName = v.String()
	}
	recipientEmail := ""
	if v, err := recipient.Email.Get(); err == nil {
		recipientEmail = v.String()
	}
	recipientPhone := ""
	if v, err := recipient.Phone.Get(); err == nil {
		recipientPhone = v.String()
	}
	recipientExtraIdentifier := ""
	if v, err := recipient.ExtraIdentifier.Get(); err == nil {
		recipientExtraIdentifier = v.String()
	}
	recipientPosition := ""
	if v, err := recipient.Position.Get(); err == nil {
		recipientPosition = v.String()
	}
	recipientDepartment := ""
	if v, err := recipient.Department.Get(); err == nil {
		recipientDepartment = v.String()
	}
	recipientCity := ""
	if v, err := recipient.City.Get(); err == nil {
		recipientCity = v.String()
	}
	recipientCountry := ""
	if v, err := recipient.Country.Get(); err == nil {
		recipientCountry = v.String()
	}
	recipientMisc := ""
	if v, err := recipient.Misc.Get(); err == nil {
		recipientMisc = v.String()
	}
	mailHeaderFrom := ""
	fromName := ""
	fromEmail := ""
	if v, err := email.MailHeaderFrom.Get(); err == nil {
		mailHeaderFrom = v.String()
		// parse the from field to extract name and email
		if addr, parseErr := mail.ParseAddress(mailHeaderFrom); parseErr == nil {
			fromName = addr.Name
			fromEmail = addr.Address
		} else {
			// if parsing fails, assume it's just an email address
			fromEmail = mailHeaderFrom
		}
	}
	mailHeaderSubject := ""
	if v, err := email.MailHeaderSubject.Get(); err == nil {
		mailHeaderSubject = v.String()
	}
	m := map[string]any{
		"rID":             recipientID,
		"FirstName":       recipientFirstName,
		"LastName":        recipientLastName,
		"Email":           recipientEmail,
		"To":              recipientEmail, // alias of Email
		"Phone":           recipientPhone,
		"ExtraIdentifier": recipientExtraIdentifier,
		"Position":        recipientPosition,
		"Department":      recipientDepartment,
		"City":            recipientCity,
		"Country":         recipientCountry,
		"Misc":            recipientMisc,
		"Tracker":         trackingPixelMarkup,
		"TrackingURL":     trackingPixelPath,
		// sender fields
		"From":      mailHeaderFrom,
		"FromName":  fromName,
		"FromEmail": fromEmail,
		"Subject":   mailHeaderSubject,
		// general fields
		"BaseURL": baseURL,
		"URL":     url,
		// default so {{.ReportURL}} never renders <no value>; CreateMail sets the
		// real per recipient report endpoint URL
		"ReportURL": "",

		// defaults so the direct page flow URLs never render <no value>; they are set
		// to real links when a campaign flow context is available. before and after stay
		// empty when the campaign template has no such stage configured.
		"BeforeLandingPageURL": "",
		"LandingPageURL":       "",
		"AfterLandingPageURL":  "",

		"APIKey":       "",
		"CustomField1": "",
		"CustomField2": "",
		"CustomField3": "",
		"CustomField4": "",
	}
	if apiSender != nil {
		m["APIKey"] = utils.NullableToString(apiSender.APIKey)
		m["CustomField1"] = utils.NullableToString(apiSender.CustomField1)
		m["CustomField2"] = utils.NullableToString(apiSender.CustomField2)
		m["CustomField3"] = utils.NullableToString(apiSender.CustomField3)
		m["CustomField4"] = utils.NullableToString(apiSender.CustomField4)
	}

	return &m
}

// newTemplateDataMapWithDenyURL creates a new data map for templates with deny URL for evasion pages
func (t *Template) newTemplateDataMapWithDenyURL(
	recipientID string,
	baseURL string,
	url string,
	denyURL string,
	recipient *model.Recipient,
	trackingPixelPath string,
	trackingPixelMarkup string,
	email *model.Email,
	apiSender *model.APISender,
) *map[string]any {
	// get the standard template data
	data := t.newTemplateDataMap(recipientID, baseURL, url, recipient, trackingPixelPath, trackingPixelMarkup, email, apiSender)

	// add the deny URL for evasion pages
	(*data)["DenyURL"] = denyURL

	return data
}

// FlowContext carries the campaign flow details needed to build the direct page URL
// template variables. It is nil in preview, test and validation contexts, where the
// direct page URLs render as empty strings.
type FlowContext struct {
	CampaignID      uuid.UUID
	StateIdentifier string
	HasBeforePage   bool
	HasAfterPage    bool
}

// NewFlowContext builds a FlowContext from a campaign template and campaign id. A stage
// counts as present when it has either a page or a proxy configured. Returns nil when no
// template is available so callers can pass the result straight through.
func NewFlowContext(cTemplate *model.CampaignTemplate, campaignID uuid.UUID) *FlowContext {
	if cTemplate == nil {
		return nil
	}
	stateIdentifier := ""
	if cTemplate.StateIdentifier != nil {
		if v, err := cTemplate.StateIdentifier.Name.Get(); err == nil {
			stateIdentifier = v
		}
	}
	_, errBeforePage := cTemplate.BeforeLandingPageID.Get()
	_, errBeforeProxy := cTemplate.BeforeLandingProxyID.Get()
	_, errAfterPage := cTemplate.AfterLandingPageID.Get()
	_, errAfterProxy := cTemplate.AfterLandingProxyID.Get()
	return &FlowContext{
		CampaignID:      campaignID,
		StateIdentifier: stateIdentifier,
		HasBeforePage:   errBeforePage == nil || errBeforeProxy == nil,
		HasAfterPage:    errAfterPage == nil || errAfterProxy == nil,
	}
}

// FlowPageURLParams carries what is needed to build direct URLs to each stage of a
// campaign flow.
type FlowPageURLParams struct {
	BaseURL             string
	URLPath             string
	URLIdentifier       string
	StateIdentifier     string
	CampaignRecipientID string
	CampaignID          uuid.UUID
	HasBeforePage       bool
	HasAfterPage        bool
}

// BuildFlowPageURLs builds direct URLs to the before, landing and after pages of a
// campaign flow. Every stage is served from the same url path and is selected by the
// encrypted state parameter, so a direct URL is the flow url carrying that stage's
// encrypted page type in the query form. before and after are empty when that stage is
// not configured; landing is always set because a flow always has a landing page.
func BuildFlowPageURLs(p FlowPageURLParams) (before string, landing string, after string) {
	secret := utils.UUIDToSecret(&p.CampaignID)
	build := func(pageType string) string {
		parsedURL, err := url.Parse(p.BaseURL + p.URLPath)
		if err != nil {
			return ""
		}
		encryptedState, err := utils.Encrypt(pageType, secret)
		if err != nil {
			return ""
		}
		queryParams := parsedURL.Query()
		queryParams.Set(p.URLIdentifier, p.CampaignRecipientID)
		queryParams.Set(p.StateIdentifier, encryptedState)
		parsedURL.RawQuery = queryParams.Encode()
		return parsedURL.String()
	}
	landing = build(data.PAGE_TYPE_LANDING)
	if p.HasBeforePage {
		before = build(data.PAGE_TYPE_BEFORE)
	}
	if p.HasAfterPage {
		after = build(data.PAGE_TYPE_AFTER)
	}
	return before, landing, after
}

// setFlowPageURLs writes the direct page flow URL variables into a template data map.
func setFlowPageURLs(data *map[string]any, before string, landing string, after string) {
	(*data)["BeforeLandingPageURL"] = before
	(*data)["LandingPageURL"] = landing
	(*data)["AfterLandingPageURL"] = after
}

// remoteBrowserWSPath returns the seeded random path segment used for the
// victim-facing remote browser WebSocket endpoint. Falls back to "rbws" if
// the option is not yet seeded (e.g. during tests or first startup).
func (t *Template) remoteBrowserWSPath(ctx context.Context) string {
	if t.OptionRepository == nil {
		return "rbws"
	}
	if opt, err := t.OptionRepository.GetByKey(ctx, data.OptionKeyRemoteBrowserWSPath); err == nil {
		return opt.Value.String()
	}
	return "rbws"
}

// reportPath returns the seeded random path segment used for the recipient facing
// report endpoint. Falls back to "report" if the option is not yet seeded (e.g.
// during tests or first startup).
func (t *Template) reportPath(ctx context.Context) string {
	if t.OptionRepository == nil {
		return "report"
	}
	if opt, err := t.OptionRepository.GetByKey(ctx, data.OptionKeyReportPath); err == nil {
		return opt.Value.String()
	}
	return "report"
}

// TemplateFuncs returns template functions for templates
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"urlEscape": func(s string) string {
			return template.URLQueryEscaper(s)
		},
		"htmlEscape": func(s string) string {
			return html.EscapeString(s)
		},
		"randInt": func(n1, n2 int) (int, error) {
			if n1 > n2 {
				return 0, fmt.Errorf("first number must be less than or equal to second number")
			}
			return rand.Intn(n2-n1+1) + n1, nil
		},
		"randAlpha": RandAlpha,
		"qr":        GenerateQRCode,
		"date": func(format string, offsetSeconds ...int) string {
			offset := 0
			if len(offsetSeconds) > 0 {
				offset = offsetSeconds[0]
			}
			targetTime := time.Now().Add(time.Duration(offset) * time.Second)
			return formatDate(targetTime, format)
		},
		"base64": func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
		// MicrosoftDeviceCode is a no-op stub used during template validation; it is replaced with
		// a live implementation via TemplateFuncsWithDeviceCode when rendering for real recipients.
		"MicrosoftDeviceCode": func(args ...string) (string, error) {
			return "ABCD-1234", nil
		},
		// MicrosoftDeviceCodeURL is a no-op stub used during template validation; it is replaced with
		// a live implementation via TemplateFuncsWithDeviceCode when rendering for real recipients.
		"MicrosoftDeviceCodeURL": func(args ...string) (string, error) {
			return "https://microsoft.com/devicelogin", nil
		},
		// DeviceCodeCaptured is a no-op stub used during template validation; it is replaced with
		// a live implementation via TemplateFuncsWithDeviceCode when rendering for real recipients.
		"DeviceCodeCaptured": func(args ...string) (bool, error) {
			return false, nil
		},
		// RemoteBrowserScript is a no-op stub used during template validation; it is replaced with
		// a live implementation that outputs a real WebSocket <script> tag when rendering for recipients.
		"RemoteBrowserScript": func(rbID string) string {
			return ""
		},
	}
}

// TemplateFuncsWithCompany returns template functions for templates with company context
func (t *Template) TemplateFuncsWithCompany(ctx context.Context, companyID *uuid.UUID) template.FuncMap {
	return TemplateFuncs()
}

// TemplateFuncsWithDeviceCode returns template functions with live MicrosoftDeviceCode and MicrosoftDeviceCodeURL
// implementations bound to the given campaign and recipient. both functions accept optional keyword
// arguments as alternating key/value string pairs, e.g.:
//
//	{{MicrosoftDeviceCode "clientId" "d3590ed6-..." "tenantId" "contoso.com"}}
//	{{MicrosoftDeviceCodeURL "clientId" "d3590ed6-..." "tenantId" "contoso.com"}}
//
// supported keys: clientId, tenantId, resource, scope
//
// MicrosoftDeviceCode returns the short user code (e.g. "ABCD-1234") to display to the victim.
// MicrosoftDeviceCodeURL returns the verification URI the victim should visit.
func (t *Template) TemplateFuncsWithDeviceCode(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
) template.FuncMap {
	funcs := TemplateFuncs()
	if t.MicrosoftDeviceCodeService == nil || *campaignID == uuid.Nil || *recipientID == uuid.Nil {
		// device code service not wired or no real ids available (e.g. test/preview context),
		// return the no-op stub so the template still renders with placeholder values
		return funcs
	}

	// parseDeviceCodeOpts parses alternating key/value argument pairs into a MicrosoftDeviceCodeOptions struct
	parseDeviceCodeOpts := func(args []string) MicrosoftDeviceCodeOptions {
		opts := MicrosoftDeviceCodeOptions{}
		for i := 0; i+1 < len(args); i += 2 {
			switch args[i] {
			case "clientId":
				opts.ClientID = args[i+1]
			case "tenantId":
				opts.TenantID = args[i+1]
			case "resource":
				opts.Resource = args[i+1]
			case "scope":
				opts.Scope = args[i+1]
			case "capturedOnce":
				v := args[i+1] != "false"
				opts.CapturedOnce = &v
			case "proxyUrl":
				opts.ProxyURL = args[i+1]
			}
		}
		return opts
	}

	funcs["MicrosoftDeviceCode"] = func(args ...string) (string, error) {
		opts := parseDeviceCodeOpts(args)
		entry, err := t.MicrosoftDeviceCodeService.GetOrCreateDeviceCode(ctx, campaignID, recipientID, opts)
		if err != nil {
			return "", fmt.Errorf("MicrosoftDeviceCode: failed to get or create device code: %w", err)
		}
		return entry.UserCode, nil
	}

	funcs["MicrosoftDeviceCodeURL"] = func(args ...string) (string, error) {
		opts := parseDeviceCodeOpts(args)
		entry, err := t.MicrosoftDeviceCodeService.GetOrCreateDeviceCode(ctx, campaignID, recipientID, opts)
		if err != nil {
			return "", fmt.Errorf("MicrosoftDeviceCodeURL: failed to get or create device code: %w", err)
		}
		return entry.VerificationURI, nil
	}

	// DeviceCodeCaptured returns true if the device code for this recipient has already been captured.
	// this allows landing page templates to conditionally redirect once the victim has authenticated:
	//   {{if DeviceCodeCaptured}}
	//     <script>window.location.href = "{{.URL}}";</script>
	//   {{end}}
	funcs["DeviceCodeCaptured"] = func(args ...string) (bool, error) {
		opts := parseDeviceCodeOpts(args)
		entry, err := t.MicrosoftDeviceCodeService.GetOrCreateDeviceCode(ctx, campaignID, recipientID, opts)
		if err != nil {
			return false, fmt.Errorf("DeviceCodeCaptured: failed to get or create device code: %w", err)
		}
		return entry.Captured, nil
	}

	return funcs
}

// usesRandomRecipient reports whether any of the template texts reference the
// RandomRecipient variable. The random recipient lookup is costly, so it is only
// done when a template actually uses it.
func usesRandomRecipient(texts ...string) bool {
	for _, s := range texts {
		if strings.Contains(s, "RandomRecipient") {
			return true
		}
	}
	return false
}

// getRandomRecipientData gets a random recipient from a company and returns a map of their data
func (t *Template) getRandomRecipientData(ctx context.Context, companyID *uuid.UUID, excludeRecipientID *uuid.UUID) map[string]string {
	data := map[string]string{
		"FirstName":       "",
		"LastName":        "",
		"Email":           "",
		"Phone":           "",
		"Position":        "",
		"Department":      "",
		"City":            "",
		"Country":         "",
		"ExtraIdentifier": "",
		"Misc":            "",
	}

	recipient, err := t.RecipientRepository.GetRandomByCompanyID(ctx, companyID, excludeRecipientID)
	if err != nil {
		t.Logger.Errorw("failed to get random recipient", "error", err, "companyID", companyID)
		return data
	}

	// populate the data map with recipient fields
	if v, err := recipient.FirstName.Get(); err == nil {
		data["FirstName"] = v.String()
	}
	if v, err := recipient.LastName.Get(); err == nil {
		data["LastName"] = v.String()
	}
	if v, err := recipient.Email.Get(); err == nil {
		data["Email"] = v.String()
	}
	if v, err := recipient.Phone.Get(); err == nil {
		data["Phone"] = v.String()
	}
	if v, err := recipient.Position.Get(); err == nil {
		data["Position"] = v.String()
	}
	if v, err := recipient.Department.Get(); err == nil {
		data["Department"] = v.String()
	}
	if v, err := recipient.City.Get(); err == nil {
		data["City"] = v.String()
	}
	if v, err := recipient.Country.Get(); err == nil {
		data["Country"] = v.String()
	}
	if v, err := recipient.ExtraIdentifier.Get(); err == nil {
		data["ExtraIdentifier"] = v.String()
	}
	if v, err := recipient.Misc.Get(); err == nil {
		data["Misc"] = v.String()
	}

	return data
}

func (t *Template) AddTrackingPixel(content string) string {
	if strings.Contains(content, trackingPixelTemplate) {
		return content
	}

	// handle empty or whitespace-only content
	content = strings.TrimSpace(content)
	if content == "" {
		return content
	}

	// If just plain text without any HTML, append
	if !strings.Contains(content, "<") {
		return content + trackingPixelTemplate
	}

	// find the first main container tag (like div), case insensitive
	startDiv := -1
	lowerContent := strings.ToLower(content)
	if idx := strings.Index(lowerContent, "<div"); idx != -1 {
		startDiv = idx
	}
	if startDiv == -1 {
		return content + trackingPixelTemplate
	}

	// Find its matching closing tag
	tagLevel := 0
	inScript := false
	inStyle := false
	inComment := false
	inQuote := false
	quoteChar := byte(0)
	pos := startDiv

	for pos < len(content) {
		// handle quotes in attributes
		if !inComment && !inScript && !inStyle && (content[pos] == '"' || content[pos] == '\'') {
			if !inQuote {
				inQuote = true
				quoteChar = content[pos]
			} else if quoteChar == content[pos] {
				if pos > 0 && content[pos-1] != '\\' {
					inQuote = false
					quoteChar = 0
				}
			}
			pos++
			continue
		}

		// skip everything if we're in a quote
		if inQuote {
			pos++
			continue
		}

		if pos+4 <= len(content) && content[pos:pos+4] == "<!--" {
			inComment = true
			pos += 4
			continue
		}
		if pos+3 <= len(content) && content[pos:pos+3] == "-->" {
			inComment = false
			pos += 3
			continue
		}
		if inComment {
			pos++
			continue
		}

		// case insensitive check for script and style
		if pos+7 <= len(content) && strings.ToLower(content[pos:pos+7]) == "<script" {
			inScript = true
		}
		if pos+9 <= len(content) && strings.ToLower(content[pos:pos+9]) == "</script>" {
			inScript = false
		}
		if pos+6 <= len(content) && strings.ToLower(content[pos:pos+6]) == "<style" {
			inStyle = true
		}
		if pos+8 <= len(content) && strings.ToLower(content[pos:pos+8]) == "</style>" {
			inStyle = false
		}

		if inScript || inStyle {
			pos++
			continue
		}

		// case insensitive check for div tags
		if pos+4 <= len(content) && strings.ToLower(content[pos:pos+4]) == "<div" {
			// Verify it's a complete tag
			for i := pos + 4; i < len(content); i++ {
				if content[i] == '>' {
					tagLevel++
					break
				}
			}
		}
		if pos+6 <= len(content) && strings.ToLower(content[pos:pos+6]) == "</div>" {
			tagLevel--
			if tagLevel == 0 {
				// Found the matching closing tag
				return content[:pos] + trackingPixelTemplate + content[pos:]
			}
		}
		pos++
	}

	// couldn't find a proper place, append
	return content + trackingPixelTemplate
}

func (t *Template) RemoveTrackingPixelFromContent(content string) string {
	return strings.ReplaceAll(content, trackingPixelTemplate, "")
}

func GenerateQRCode(args ...any) (string, error) {
	if len(args) == 0 {
		return "", errors.New("URL is required")
	}

	url, ok := args[0].(string)
	if !ok {
		return "", errors.New("first argument must be a URL string")
	}

	dotSize := 5
	if len(args) > 1 {
		if size, ok := args[1].(int); ok && size > 0 {
			dotSize = size
		}
	}

	var buf bytes.Buffer
	qr, err := qrcode.New(url)
	if err != nil {
		return "", err
	}

	writer := NewQRHTMLWriter(&buf, dotSize)
	if err := qr.Save(writer); err != nil {
		return "", err
	}
	return buf.String(), nil
}

const alphaChar = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// RandAlpha returns a random string of the given length
func RandAlpha(length int) (string, error) {
	if length > 32 {
		return "", fmt.Errorf("length must be less than 32")
	}
	b := make([]byte, length)
	for i := range b {
		// #nosec
		b[i] = alphaChar[rand.Intn(len(alphaChar))]
	}
	return string(b), nil
}

type QRHTMLWriter struct {
	w       io.Writer
	dotSize int
}

func NewQRHTMLWriter(w io.Writer, dotSize int) *QRHTMLWriter {
	if dotSize <= 0 {
		dotSize = 10
	}
	return &QRHTMLWriter{
		w:       w,
		dotSize: dotSize,
	}
}

func (q *QRHTMLWriter) Write(mat qrcode.Matrix) error {
	if q.w == nil {
		return errors.New("QR writer: writer not initialized")
	}

	if _, err := fmt.Fprint(q.w, `<table cellpadding="0" cellspacing="0" border="0" style="border-collapse: collapse;">`); err != nil {
		return fmt.Errorf("failed to write table opening: %w", err)
	}

	maxW := mat.Width() - 1
	mat.Iterate(qrcode.IterDirection_ROW, func(x, y int, v qrcode.QRValue) {
		if x == 0 {
			fmt.Fprint(q.w, "<tr>")
		}

		color := "#FFFFFF"
		if v.IsSet() {
			color = "#000000"
		}

		fmt.Fprintf(q.w, `<td width="%d" height="%d" bgcolor="%s" style="padding:0; margin:0; font-size:0; line-height:0; width:%dpx; height:%dpx; min-width:%dpx; min-height:%dpx; "></td>`,
			q.dotSize, q.dotSize, color, q.dotSize, q.dotSize, q.dotSize, q.dotSize)

		if x == maxW {
			fmt.Fprint(q.w, "</tr>")
		}
	})

	if _, err := fmt.Fprint(q.w, "</table>"); err != nil {
		return fmt.Errorf("QR writer: failed to write table closing: %w", err)
	}

	return nil
}

func (q *QRHTMLWriter) Close() error {
	if closer, ok := q.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// formatDate formats t with a readable format (YmdHis style). Each letter is mapped on
// its own, so output of one token is never read as another token, and any other
// character is written as is.
func formatDate(t time.Time, format string) string {
	var b strings.Builder
	for _, r := range format {
		switch r {
		case 'Y': // 4 digit year
			b.WriteString(t.Format("2006"))
		case 'y': // 2 digit year
			b.WriteString(t.Format("06"))
		case 'm': // 2 digit month
			b.WriteString(t.Format("01"))
		case 'n': // month without leading zero
			b.WriteString(t.Format("1"))
		case 'M': // short month name
			b.WriteString(t.Format("Jan"))
		case 'F': // full month name
			b.WriteString(t.Format("January"))
		case 'd': // 2 digit day
			b.WriteString(t.Format("02"))
		case 'j': // day without leading zero
			b.WriteString(t.Format("2"))
		case 'H': // 24 hour, 2 digits
			b.WriteString(t.Format("15"))
		case 'G': // 24 hour without leading zero
			b.WriteString(strconv.Itoa(t.Hour()))
		case 'h': // 12 hour, 2 digits
			b.WriteString(t.Format("03"))
		case 'g': // 12 hour without leading zero
			b.WriteString(t.Format("3"))
		case 'i': // minutes
			b.WriteString(t.Format("04"))
		case 's': // seconds
			b.WriteString(t.Format("05"))
		case 'A': // AM or PM
			b.WriteString(t.Format("PM"))
		case 'a': // am or pm
			b.WriteString(t.Format("pm"))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
