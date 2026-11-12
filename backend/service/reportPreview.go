package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"text/template"
	"time"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// reportPreviewTimeout bounds a preview, the browser download on first use
// included. It is below the 3 minute timeout of the preview route.
const reportPreviewTimeout = 150 * time.Second

// PreviewPDF renders content, a report template, with sample data into a PDF
// the same way a campaign report is rendered, so the result shows the real
// page size, fonts and page breaks. The training sample is used when
// isTraining is set. A template that does not parse or run returns a
// validation error with the reason.
func (s *ReportTemplate) PreviewPDF(
	ctx context.Context,
	session *model.Session,
	content string,
	isTraining bool,
) ([]byte, error) {
	ae := NewAuditEvent("ReportTemplate.PreviewPDF", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// the same size limit as a stored template
	if _, err := vo.NewOptionalString1MB(content); err != nil {
		return nil, errs.NewValidationError(err)
	}
	html, err := renderReportPreview(content, isTraining)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	// a page that never finishes, such as an endless script, is stopped
	// before the route's write deadline
	ctx, cancel := context.WithTimeout(ctx, reportPreviewTimeout)
	defer cancel()
	rb := newReportBrowser(ctx, s.RemoteBrowserExecPath, s.Logger)
	defer rb.close()
	pdf, err := rb.renderPDF(html)
	if err != nil {
		s.Logger.Errorw("failed to render report preview PDF", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit log on read
	return pdf, nil
}

// renderReportPreview renders content with the sample report data, escaped
// the way a campaign report is.
func renderReportPreview(content string, isTraining bool) (string, error) {
	tmpl, err := template.New("report").Funcs(TemplateFuncs()).Parse(content)
	if err != nil {
		return "", fmt.Errorf("template does not parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, htmlEscapeReportData(reportSampleData(isTraining))); err != nil {
		return "", fmt.Errorf("template does not run: %w", err)
	}
	return buf.String(), nil
}

// reportScreenshotPlaceholder is a grey SVG data URI standing in for a
// screenshot in the sample report data.
func reportScreenshotPlaceholder(label string) string {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="500" viewBox="0 0 800 500">` +
		`<rect width="800" height="500" fill="#f3f4f6"/>` +
		`<rect x="12" y="12" width="776" height="476" fill="none" stroke="#d1d5db" stroke-width="2" stroke-dasharray="10 8"/>` +
		`<text x="400" y="240" text-anchor="middle" font-family="sans-serif" font-size="30" fill="#6b7280">` + label + `</text>` +
		`<text x="400" y="282" text-anchor="middle" font-family="sans-serif" font-size="18" fill="#9ca3af">Sample placeholder</text>` +
		`</svg>`
	return "data:image/svg+xml," + url.PathEscape(svg)
}

// reportSampleData is the report data a template preview is rendered with. It
// matches the sample data of the report editor preview.
func reportSampleData(isTraining bool) *model.ReportData {
	var shots []model.ReportTemplateScreenshots
	for n := 1; n <= 2; n++ {
		shots = append(shots, model.ReportTemplateScreenshots{
			Number:            n,
			Name:              fmt.Sprintf("Sample template %d", n),
			Email:             reportScreenshotPlaceholder(fmt.Sprintf("Template %d: email screenshot", n)),
			EmailFrom:         fmt.Sprintf("it-support@sample%d.test", n),
			EmailSubject:      fmt.Sprintf("Sample subject for template %d", n),
			BeforeLandingPage: reportScreenshotPlaceholder(fmt.Sprintf("Template %d: before landing page screenshot", n)),
			LandingPage:       reportScreenshotPlaceholder(fmt.Sprintf("Template %d: landing page screenshot", n)),
			AfterLandingPage:  reportScreenshotPlaceholder(fmt.Sprintf("Template %d: after landing page screenshot", n)),
		})
	}
	group := func(name string, total, clicked, submitted, reported, started, completed int, suppressed bool) model.ReportGroupStat {
		pct := func(n int) string {
			if total == 0 {
				return "0"
			}
			return fmt.Sprintf("%d", n*100/total)
		}
		return model.ReportGroupStat{
			Group:                    name,
			Total:                    total,
			Clicked:                  clicked,
			ClickedPercent:           pct(clicked),
			Submitted:                submitted,
			SubmittedPercent:         pct(submitted),
			Reported:                 reported,
			ReportedPercent:          pct(reported),
			TrainingStarted:          started,
			TrainingStartedPercent:   pct(started),
			TrainingCompleted:        completed,
			TrainingCompletedPercent: pct(completed),
			Suppressed:               suppressed,
		}
	}
	departments := []model.ReportGroupStat{
		group("Engineering", 70, 21, 9, 11, 0, 0, false),
		group("Sales", 49, 20, 10, 4, 0, 0, false),
		group("Other (small groups)", 1, 0, 0, 0, 0, 0, true),
	}
	positions := []model.ReportGroupStat{
		group("Head of operations", 40, 12, 5, 6, 0, 0, false),
		group("Account Executive", 35, 14, 7, 3, 0, 0, false),
		group("Other (small groups)", 1, 0, 0, 0, 0, 0, true),
	}
	rd := &model.ReportData{
		CampaignName:             "Q1 Phishing Simulation",
		CompanyName:              "World Corp",
		ReportDate:               time.Now().Format("2006-01-02"),
		CampaignStartDate:        "2025-01-01",
		CampaignEndDate:          "2025-01-31",
		CampaignClosedAt:         "2025-02-01",
		TotalTargets:             120,
		EmailsSent:               118,
		EmailsOpened:             74,
		ResultClicked:            32,
		ResultClickedPercent:     "27.1",
		ResultSubmitted:          14,
		ResultSubmittedPercent:   "11.9",
		ResultReported:           8,
		ResultReportedPercent:    "6.8",
		OpenedOfSent:             "62.7",
		ClickedOfOpened:          "43.2",
		SubmittedOfClicked:       "43.8",
		SentRate:                 98.3,
		OpenRate:                 61.7,
		ClickRate:                26.7,
		SubmitRate:               11.7,
		ReportRate:               6.7,
		StartedOfOpened:          "0",
		CompletedOfStarted:       "0",
		TrainingStartedPercent:   "0",
		TrainingCompletedPercent: "0",
		GroupsBy:                 "Department",
		Groups:                   departments,
		DepartmentGroups:         departments,
		PositionGroups:           positions,
		Recipients: []model.ReportRecipient{
			{FirstName: "Alice", LastName: "Andersen", Email: "alice@worldcorp.test", Department: "Research and Development", Position: "Head of operations", ClickedLink: true, SubmittedData: true},
			{FirstName: "Bob", LastName: "Berg", Email: "bob@worldcorp.test", Department: "Sales", Position: "Account Executive", ClickedLink: true},
			{FirstName: "Carol", LastName: "Chan", Email: "carol@worldcorp.test", Department: "Finance", Position: "Analyst", Reported: true},
		},
		TemplateScreenshots:         shots,
		TemplateCount:               len(shots),
		EmailScreenshot:             shots[0].Email,
		EmailFrom:                   shots[0].EmailFrom,
		EmailSubject:                shots[0].EmailSubject,
		BeforeLandingPageScreenshot: shots[0].BeforeLandingPage,
		LandingPageScreenshot:       shots[0].LandingPage,
		AfterLandingPageScreenshot:  shots[0].AfterLandingPage,
	}
	if isTraining {
		rd.CampaignName = "Q1 Awareness Training"
		rd.IsTraining = true
		rd.TrainingStarted = 52
		rd.TrainingCompleted = 41
		rd.TrainingStartedPercent = "43.3"
		rd.TrainingCompletedPercent = "34.2"
		rd.TrainingStartedRate = 43.3
		rd.TrainingCompletedRate = 34.2
		rd.StartedOfOpened = "70.3"
		rd.CompletedOfStarted = "78.8"
		rd.Groups = []model.ReportGroupStat{
			group("Engineering", 70, 0, 0, 0, 31, 25, false),
			group("Sales", 49, 0, 0, 0, 21, 16, false),
			group("Other (small groups)", 1, 0, 0, 0, 0, 0, true),
		}
		rd.DepartmentGroups = rd.Groups
		for i := range rd.Recipients {
			rd.Recipients[i].TrainingStarted = i < 2
			rd.Recipients[i].TrainingCompleted = i == 0
		}
	}
	return rd
}
