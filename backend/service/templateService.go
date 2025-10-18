package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"math/rand"
	"strings"
	"text/template"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/yeqown/go-qrcode/v2"
)

const trackingPixelTemplate = "{{.Tracker}}"

// TemplateService is for handling things related to
// templates such as websites, emails, etc.
type Template struct {
	Common
}

// CreateMailTemplate creates a new mail template
func (t *Template) CreateMail(
	domainName string,
	idKey string,
	urlPath string,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender,
) *map[string]any {
	baseURL := "https://" + domainName
	url := fmt.Sprintf(
		"%s%s?%s=%s",
		baseURL,
		urlPath,
		idKey,
		campaignRecipient.ID.MustGet().String(),
	)
	// set body
	trackingPixelPath := fmt.Sprintf(
		"%s/wf/open?upn=%s",
		baseURL,
		campaignRecipient.ID.MustGet().String(),
	)
	trackingPixel := fmt.Sprintf(
		"<img src=\"%s/wf/open?upn=%s\" alt=\"\" width=\"1\" height=\"1\" border=\"0\" style=\"height:1px !important;width:1px\" />",
		baseURL,
		campaignRecipient.ID.MustGet().String(),
	)
	return t.newTemplateDataMap(
		idKey,
		baseURL,
		url,
		campaignRecipient.Recipient,
		trackingPixelPath,
		trackingPixel,
		email,
		apiSender,
	)
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
		return fmt.Errorf("failed to execute page template: %s", err)
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
		"id",
		"/test",
		domain,
		&campaignRecipient,
		email,
		apiSender,
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

// ApplyPageMock
func (t *Template) ApplyPageMock(content string) (*bytes.Buffer, error) {
	// build response
	domain := &database.Domain{
		Name: "example.test",
	}
	email := model.NewEmailExample()
	campaignRecipientID := uuid.New()
	recipient := model.NewRecipientExample()
	urlIdentifier := &model.Identifier{
		Name: nullable.NewNullableWithValue(
			"id",
		),
	}
	stateIdentifier := &model.Identifier{
		Name: nullable.NewNullableWithValue(
			"state",
		),
	}
	campaignTemplate := &model.CampaignTemplate{
		URLIdentifier:   urlIdentifier,
		StateIdentifier: stateIdentifier,
	}
	return t.CreatePhishingPageWithCampaign(
		domain,
		email,
		&campaignRecipientID,
		recipient,
		content,
		campaignTemplate,
		"stateParam",
		"urlPath",
		nil, // no campaign for mock, so DenyURL will be empty string
	)
}

// CreateMailBody returns a rendered mail body to string
func (t *Template) CreateMailBody(
	urlIdentifier string,
	urlPath string,
	domain *model.Domain,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender, // can be nil
) (string, error) {
	return t.CreateMailBodyWithCustomURL(
		urlIdentifier,
		urlPath,
		domain,
		campaignRecipient,
		email,
		apiSender,
		"", // empty customURL means use template domain
	)
}

// CreateMailBodyWithCustomURL returns a rendered mail body to string with optional custom campaign URL
func (t *Template) CreateMailBodyWithCustomURL(
	urlIdentifier string,
	urlPath string,
	domain *model.Domain,
	campaignRecipient *model.CampaignRecipient,
	email *model.Email,
	apiSender *model.APISender, // can be nil
	customCampaignURL string, // if provided, overrides the default campaign URL
) (string, error) {
	mailData := t.CreateMail(
		domain.Name.MustGet().String(),
		urlIdentifier,
		urlPath,
		campaignRecipient,
		email,
		apiSender,
	)

	// override campaign URL if custom one is provided
	if customCampaignURL != "" {
		(*mailData)["URL"] = customCampaignURL
	}
	mailContentTemplate := template.New("mailContent")
	mailContentTemplate = mailContentTemplate.Funcs(TemplateFuncs())
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
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
) (*bytes.Buffer, error) {
	return t.CreatePhishingPageWithCampaign(domain, email, campaignRecipientID, recipient, contentToRender, campaignTemplate, stateParam, urlPath, nil)
}

// CreatePhishingPageWithCampaign creates a new phishing page with optional campaign for deny URL support
func (t *Template) CreatePhishingPageWithCampaign(
	domain *database.Domain,
	email *model.Email,
	campaignRecipientID *uuid.UUID,
	recipient *model.Recipient,
	contentToRender string,
	campaignTemplate *model.CampaignTemplate,
	stateParam string,
	urlPath string,
	campaign *model.Campaign,
) (*bytes.Buffer, error) {
	w := bytes.NewBuffer([]byte{})
	id := campaignRecipientID.String()
	baseURL := "https://" + domain.Name
	if len(domain.Name) == 0 {
		baseURL = ""
	}
	urlIdentifier := campaignTemplate.URLIdentifier.Name.MustGet()
	stateIdentifier := campaignTemplate.StateIdentifier.Name.MustGet()
	url := fmt.Sprintf("%s?%s=%s&%s=%s", baseURL, urlIdentifier, id, stateIdentifier, stateParam)

	// create deny URL if campaign and deny page exist
	denyURL := ""
	if campaign != nil {
		if _, err := campaign.DenyPageID.Get(); err == nil {
			// create a special state param that indicates deny page should be served
			campaignID := campaign.ID.MustGet()
			denyStateParam, encErr := utils.Encrypt("deny", utils.UUIDToSecret(&campaignID))
			if encErr == nil {
				denyURL = fmt.Sprintf("%s?%s=%s&%s=%s", baseURL, urlIdentifier, id, stateIdentifier, denyStateParam)
			}
		}
	}

	tmpl, err := template.New("page").
		Funcs(TemplateFuncs()).
		Parse(contentToRender)

	if err != nil {
		return w, fmt.Errorf("failed to parse page template: %s", err)
	}
	data := t.newTemplateDataMapWithDenyURL(
		id,
		baseURL,
		url,
		denyURL,
		recipient,
		"", // trackingPixelPath
		"", // trackingPixelMarkup
		email,
		nil, // apiSender
	)
	err = tmpl.Execute(w, data)
	if err != nil {
		return w, fmt.Errorf("failed to execute page template: %s", err)
	}
	return w, nil
}

// newTemplateDataMap creates a new data map for templates
func (t *Template) newTemplateDataMap(
	id string,
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
	if v, err := email.MailHeaderFrom.Get(); err == nil {
		mailHeaderFrom = v.String()
	}
	m := map[string]any{
		"rID":             id,
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
		"From": mailHeaderFrom,
		// general fields
		"BaseURL": baseURL,
		"URL":     url,

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
	id string,
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
	data := t.newTemplateDataMap(id, baseURL, url, recipient, trackingPixelPath, trackingPixelMarkup, email, apiSender)

	// add the deny URL for evasion pages
	(*data)["DenyURL"] = denyURL

	return data
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
			goFormat := convertDateFormat(format)
			return targetTime.Format(goFormat)
		},
		"base64": func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		},
	}
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

// convertDateFormat converts readable date format (YmdHis) to Go's reference format
func convertDateFormat(dateFormat string) string {
	goFormat := dateFormat

	// year formats
	goFormat = strings.ReplaceAll(goFormat, "Y", "2006") // 4-digit year
	goFormat = strings.ReplaceAll(goFormat, "y", "06")   // 2-digit year

	// month formats
	goFormat = strings.ReplaceAll(goFormat, "m", "01")      // 2-digit month
	goFormat = strings.ReplaceAll(goFormat, "n", "1")       // month without leading zero
	goFormat = strings.ReplaceAll(goFormat, "M", "Jan")     // short month name
	goFormat = strings.ReplaceAll(goFormat, "F", "January") // full month name

	// day formats
	goFormat = strings.ReplaceAll(goFormat, "d", "02") // 2-digit day
	goFormat = strings.ReplaceAll(goFormat, "j", "2")  // day without leading zero

	// hour formats
	goFormat = strings.ReplaceAll(goFormat, "H", "15") // 24-hour format
	goFormat = strings.ReplaceAll(goFormat, "h", "03") // 12-hour format
	goFormat = strings.ReplaceAll(goFormat, "G", "15") // 24-hour without leading zero (Go doesn't support this exactly)
	goFormat = strings.ReplaceAll(goFormat, "g", "3")  // 12-hour without leading zero

	// minute and second formats
	goFormat = strings.ReplaceAll(goFormat, "i", "04") // minutes
	goFormat = strings.ReplaceAll(goFormat, "s", "05") // seconds

	// am/pm formats
	goFormat = strings.ReplaceAll(goFormat, "A", "PM") // uppercase AM/PM
	goFormat = strings.ReplaceAll(goFormat, "a", "pm") // lowercase am/pm

	return goFormat
}
