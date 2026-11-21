package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"text/template"
	"time"

	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// programReportContent returns the program report template of the program's
// company, else the global one, else the built in default.
func (s *Program) programReportContent(ctx context.Context, program *model.Program) (string, error) {
	if s.ReportTemplateRepository == nil {
		return embedded.DefaultProgramReportHTML, nil
	}
	tmpl, err := s.ReportTemplateRepository.GetForProgram(ctx, optionalID(program.CompanyID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return embedded.DefaultProgramReportHTML, nil
	}
	if err != nil {
		return "", errs.Wrap(err)
	}
	content, err := tmpl.Content.Get()
	if err != nil {
		return embedded.DefaultProgramReportHTML, nil
	}
	return content.String(), nil
}

// renderProgramReport renders the report data with the program's report template
func (s *Program) renderProgramReport(ctx context.Context, program *model.Program, d *programReportData) (string, error) {
	content, err := s.programReportContent(ctx, program)
	if err != nil {
		return "", err
	}
	out, err := renderProgramReportContent(content, d)
	if err != nil {
		s.Logger.Errorw("failed to render program report template", "error", err)
		return "", errs.NewCustomError(err)
	}
	return out, nil
}

// renderProgramReportContent renders a program report template the way a
// campaign report is rendered: text/template with the report functions, on a
// copy of the data with every text HTML escaped.
func renderProgramReportContent(content string, d *programReportData) (string, error) {
	tmpl, err := template.New("report").Funcs(TemplateFuncs()).Parse(content)
	if err != nil {
		return "", fmt.Errorf("template does not parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, htmlEscapeProgramReportData(d)); err != nil {
		return "", fmt.Errorf("template does not run: %w", err)
	}
	return buf.String(), nil
}

// htmlEscapeProgramReportData returns a copy of the report data with every
// text HTML escaped. Program, template and group names are entered by users,
// and text/template prints values as they are.
func htmlEscapeProgramReportData(d *programReportData) *programReportData {
	if d == nil {
		return nil
	}
	cp := *d
	esc := template.HTMLEscapeString
	cp.Name = esc(d.Name)
	cp.CompanyName = esc(d.CompanyName)
	cp.Generated = esc(d.Generated)
	cp.Period = esc(d.Period)
	cp.Started = esc(d.Started)
	cp.Closed = esc(d.Closed)
	cp.HiddenText = esc(d.HiddenText)
	lines := func(in []programReportLine) []programReportLine {
		if in == nil {
			return nil
		}
		out := make([]programReportLine, len(in))
		for i, l := range in {
			out[i] = l
			out[i].Label = esc(l.Label)
			out[i].Value = esc(l.Value)
			out[i].Sub = esc(l.Sub)
			out[i].Tone = esc(l.Tone)
		}
		return out
	}
	texts := func(in []string) []string {
		if in == nil {
			return nil
		}
		out := make([]string, len(in))
		for i, c := range in {
			out[i] = esc(c)
		}
		return out
	}
	rows := func(in []programReportRow) []programReportRow {
		if in == nil {
			return nil
		}
		out := make([]programReportRow, len(in))
		for i, r := range in {
			out[i] = programReportRow{Cells: texts(r.Cells), Pcts: texts(r.Pcts)}
		}
		return out
	}
	cp.Details = lines(d.Details)
	cp.Results = lines(d.Results)
	cp.Outcome = lines(d.Outcome)
	cp.Templates = rows(d.Templates)
	cp.Months = rows(d.Months)
	if d.Trend != nil {
		cp.Trend = make([]programReportTrendMonth, len(d.Trend))
		for i, m := range d.Trend {
			cp.Trend[i] = m
			cp.Trend[i].Label = esc(m.Label)
			cp.Trend[i].Clicked.Text = esc(m.Clicked.Text)
			cp.Trend[i].Reported.Text = esc(m.Reported.Text)
		}
	}
	return &cp
}

// programReportSampleData is the data a program report template preview is
// rendered with. It matches the sample data of the report editor preview.
func programReportSampleData() *programReportData {
	l := func(label, value string) programReportLine {
		return programReportLine{Label: label, Value: value}
	}
	n := func(v int) *int { return &v }
	results := &ProgramResults{
		Summary: &ProgramSummary{
			Recipients:           120,
			Sent:                 n(118),
			ClickedLink:          n(31),
			SubmittedData:        n(11),
			Reported:             n(46),
			RepeatOffenders:      n(6),
			RepeatOffenderMonths: 12,
			HasPhishing:          true,
			HasTraining:          true,
			TrainingSent:         n(41),
			TrainingStarted:      n(34),
			TrainingCompleted:    n(29),
			SentTotal:            159,
			HasSent:              true,
		},
		Months: []*ProgramResultRow{
			{Key: "2025-10", Name: "Oct 2025", Sent: n(80), Clicked: n(15), Submitted: n(5), Reported: n(16), TrainingSent: n(12), TrainingStarted: n(9), TrainingCompleted: n(7), Partial: true},
			{Key: "2025-11", Name: "Nov 2025", Sent: n(78), Clicked: n(12), Submitted: n(4), Reported: n(19), TrainingSent: n(14), TrainingStarted: n(12), TrainingCompleted: n(10)},
			{Key: "2025-12", Name: "Dec 2025", Sent: n(78), Clicked: n(10), Submitted: n(3), Reported: n(22), TrainingSent: n(15), TrainingStarted: n(13), TrainingCompleted: n(12)},
		},
		Templates: []*ProgramResultRow{
			{Key: "t1", Name: "Password expiry", Sent: n(120), Clicked: n(24), Submitted: n(8), Reported: n(25)},
			{Key: "t2", Name: "Parcel notice", Sent: n(98), Clicked: n(11), Submitted: n(3), Reported: n(27)},
			{Key: "t3", Name: "Invoice reminder", Sent: n(18), Clicked: n(2), Submitted: n(1), Reported: n(5)},
			{Key: "t4", Name: "Security training", TrainingSent: n(41), TrainingStarted: n(34), TrainingCompleted: n(29)},
		},
	}
	program := &model.Program{Status: model.ProgramStatusActive}
	d := programReportFromResults(program, &programReportSetup{CompanyName: "World Corp"}, results, time.Now().UTC(), nil, nil)
	d.Name = "Sample Program"
	d.Period = "1 Oct 2025 to 31 Dec 2025"
	d.Started = "1 Sep 2025"
	d.Details = []programReportLine{
		l("Status", "Active"),
		l("Anonymous", "No"),
		l("Templates", "4: Password expiry, Parcel notice, Invoice reminder, Security training"),
		l("Template order", "Random order per recipient"),
		l("After the last template", "Start over"),
		l("Time between templates", "2 weeks"),
		l("Spread sends over", "1 week"),
		l("Sending days", "Mon, Tue, Wed, Thu, Fri"),
		l("Sending hours", "09:00 to 16:00"),
		l("Skip dates", "None"),
		l("First template to new members", "After 2 weeks"),
		l("Start", "1 Sep 2025 09:00"),
		l("End", "Runs until closed"),
		l("Groups", "All staff"),
	}
	return d
}
