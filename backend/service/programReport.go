package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// programReportMaxSpan bounds the period a report may cover
const programReportMaxSpan = 20 * 365 * 24 * time.Hour

// programReportLine is one label and value of the report
type programReportLine struct {
	Label string
	Value string
	// Sub is a short line under the value, such as the share of the people sent to
	Sub string
	// Tone colors the line in the report: neutral, info, warning, danger, safe
	// or training
	Tone string
	// Withheld is set when an anonymous report does not show the value
	Withheld bool
	// Rate is the value as a percent from 0 to 100 for a chart, set when
	// HasRate is
	Rate    float64
	HasRate bool
}

// programReportRow is one table row of the report, all cells as text. Pcts
// holds the percent shown after a count cell, as "15%", empty for none.
type programReportRow struct {
	Cells []string
	Pcts  []string
}

// programReportBar is one bar of the trend chart. Height is the bar height as
// a percent of the chart height.
type programReportBar struct {
	Text     string
	Height   float64
	Withheld bool
}

// programReportTrendMonth is one month of the trend chart. Partial is set on a
// month the program or the period covers only in part.
type programReportTrendMonth struct {
	Label    string
	Partial  bool
	Clicked  programReportBar
	Reported programReportBar
}

// programReportData is what the program report template renders. Every value
// is text, a number or a flag so the template only prints, and
// htmlEscapeProgramReportData escapes every text before a render. The figures
// are the ones of the program page: the result tiles, the trend by month and
// the results by template.
type programReportData struct {
	Name        string
	CompanyName string
	Generated   string
	Period      string
	Started     string
	Closed      string
	Anonymous   bool
	// HiddenText is the text a withheld value is shown as
	HiddenText string
	Details    []programReportLine
	// Results are the result cards: Recipients, the phishing cards Sent,
	// Clicked link, Data submitted and Reported when the program has phishing,
	// the training cards when the program has training, and Repeat offenders
	// when it has phishing
	Results []programReportLine
	// Outcome holds Clicked link, Data submitted and Reported as a share of
	// the recipients sent to, for the outcome charts, empty when the program
	// has no phishing
	Outcome []programReportLine
	// Templates are the most sent templates, at most programReportTableTemplates,
	// of TemplatesTotal. Months are the last months of the period, at most
	// programReportTableMonths, of MonthsTotal.
	Templates      []programReportRow
	TemplatesTotal int
	Months         []programReportRow
	MonthsTotal    int
	// Trend is the last months of the period for the trend chart, TrendMax
	// is the rate in percent the top of the chart stands for
	Trend    []programReportTrendMonth
	TrendMax float64
	MinGroup int
	// RepeatOffenderMonths is the repeat offender window in months
	RepeatOffenderMonths int
	// HasTraining is set when the program has or had a training template, the
	// training cards show only then
	HasTraining bool
	// HasPhishing is set when the program has or had a phishing template, the
	// phishing cards show only then
	HasPhishing bool
	// HasSends is set when anything was sent in the period, phishing or
	// training. The outcome charts show only then, and only with phishing.
	HasSends bool
}

// programReportSetup is what the report shows about how the program is set up
// beyond the program row: its template names and group names
type programReportSetup struct {
	CompanyName   string
	TemplateNames []string
	GroupNames    []string
}

// programWithheldText stands in for a number an anonymous report does not show
const programWithheldText = "Hidden"

func pctText(part, whole *int) string {
	if part == nil || whole == nil {
		return programWithheldText
	}
	if *whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", float64(*part)*100/float64(*whole))
}

// pctOnlyText prints part of whole as a percent, empty when either is withheld
// or whole is zero
func pctOnlyText(part, whole *int) string {
	if part == nil || whole == nil || *whole == 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", float64(*part)*100/float64(*whole))
}

func countText(v *int) string {
	if v == nil {
		return programWithheldText
	}
	return strconv.Itoa(*v)
}

// durationText names a number of minutes in the largest whole unit, such as
// "2 weeks" or "1 month". A month is 30 days and a year 365 days.
func durationText(minutes int) string {
	const day = 24 * 60
	units := []struct {
		size int
		name string
	}{
		{365 * day, "year"},
		{30 * day, "month"},
		{7 * day, "week"},
		{day, "day"},
		{60, "hour"},
		{1, "minute"},
	}
	if minutes <= 0 {
		return "-"
	}
	for _, u := range units {
		if minutes%u.size != 0 {
			continue
		}
		n := minutes / u.size
		if n == 1 {
			return "1 " + u.name
		}
		return fmt.Sprintf("%d %ss", n, u.name)
	}
	return fmt.Sprintf("%d minutes", minutes)
}

// reportDateText prints a time as a date and time in the program zone
func reportDateText(t *time.Time, loc *time.Location) string {
	return t.In(loc).Format("2 Jan 2006 15:04")
}

// skipDatesText lists the skip date ranges, a range of one day as that day
func skipDatesText(p *model.Program) string {
	if !p.HasBlackouts() {
		return "None"
	}
	out := []string{}
	for _, r := range p.BlackoutDates.MustGet() {
		from, fErr := time.Parse(model.ProgramDateLayout, r.From)
		to, tErr := time.Parse(model.ProgramDateLayout, r.To)
		if fErr != nil || tErr != nil {
			continue
		}
		layout, suffix := "2 Jan 2006", ""
		if r.Yearly {
			layout, suffix = "2 Jan", " every year"
		}
		if r.From == r.To {
			out = append(out, from.Format(layout)+suffix)
			continue
		}
		out = append(out, from.Format(layout)+" to "+to.Format(layout)+suffix)
	}
	if len(out) == 0 {
		return "None"
	}
	return strings.Join(out, ", ")
}

// programReportDetailNames is how many template or group names the details
// page lists before it counts the rest
const programReportDetailNames = 5

// listTextFirst joins the first n names and counts the rest
func listTextFirst(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + " and " + strconv.Itoa(len(names)-n) + " more"
}

// listTextOr is listTextFirst, or none when there are no names
func listTextOr(names []string, n int, none string) string {
	if len(names) == 0 {
		return none
	}
	return listTextFirst(names, n)
}

func weekDaysText(p *model.Program) string {
	wd, err := p.ConstraintWeekDays.Get()
	if err != nil {
		return "Any day"
	}
	days := wd.AsSlice()
	if len(days) == 0 || len(days) == 7 {
		return "Any day"
	}
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	out := []string{}
	for _, d := range days {
		out = append(out, names[d])
	}
	return strings.Join(out, ", ")
}

// programReportDetails describes how the program is set up, in the words the
// program pages use. Every time is printed in the program time zone, which the
// generated line names.
func programReportDetails(p *model.Program, setup *programReportSetup) []programReportLine {
	anonymous := "No"
	if p.IsAnonymousOn() {
		anonymous = "Yes"
	}
	order := "Random order per recipient"
	if p.OrderModeOrDefault() == model.ProgramOrderSequential {
		order = "Same order for everyone"
	}
	loop := "Start over"
	if p.LoopModeOrDefault() == model.ProgramLoopComplete {
		loop = "Stop"
	}
	window := "Any time"
	start, sErr := p.ConstraintStartTime.Get()
	end, eErr := p.ConstraintEndTime.Get()
	if sErr == nil && eErr == nil {
		window = start.String() + " to " + end.String()
	}
	status := p.Status
	if status != "" {
		status = strings.ToUpper(status[:1]) + status[1:]
	}
	templates := strconv.Itoa(len(p.Steps))
	if len(setup.TemplateNames) > 0 {
		templates += ": " + listTextFirst(setup.TemplateNames, programReportDetailNames)
	}
	spread := "None"
	if v := int(p.SpreadOrDefault().Minutes()); v > 0 {
		spread = durationText(v)
	}
	newMembers := "Right away"
	if v, err := p.NewMemberDelayMinutes.Get(); err == nil && v > 0 {
		newMembers = "After " + durationText(v)
	}
	loc := p.Location()
	startText := "Not set"
	if t := p.StartAtTime(); t != nil {
		startText = reportDateText(t, loc)
	}
	endText := "Runs until closed"
	if t := p.EndAtTime(); t != nil {
		endText = reportDateText(t, loc)
	}
	return []programReportLine{
		{Label: "Status", Value: status},
		{Label: "Anonymous", Value: anonymous},
		{Label: "Templates", Value: templates},
		{Label: "Template order", Value: order},
		{Label: "After the last template", Value: loop},
		{Label: "Time between templates", Value: durationText(int(p.IntervalOrDefault().Minutes()))},
		{Label: "Spread sends over", Value: spread},
		{Label: "Sending days", Value: weekDaysText(p)},
		{Label: "Sending hours", Value: window},
		{Label: "Skip dates", Value: skipDatesText(p)},
		{Label: "First template to new members", Value: newMembers},
		{Label: "Start", Value: startText},
		{Label: "End", Value: endText},
		{Label: "Groups", Value: listTextOr(setup.GroupNames, programReportDetailNames, "None")},
	}
}

// endOfDay returns the last instant of the calendar day of t, read in loc
func endOfDay(t time.Time, loc *time.Location) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

// errProgramReportWholeMonths is the error of an anonymous report period that
// is not whole calendar months
var errProgramReportWholeMonths = errors.New("Anonymous reports cover whole calendar months")

// parseProgramReportBound reads a report from or to query value: empty, a
// YYYY-MM-DD date read in loc, or an RFC 3339 time
func parseProgramReportBound(v string, loc *time.Location) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	if d, err := time.ParseInLocation(model.ProgramDateLayout, v, loc); err == nil {
		return &d, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, errors.New("must be YYYY-MM-DD or RFC 3339")
	}
	return &t, nil
}

// isMonthStart reports whether t is the first instant of a calendar month in loc
func isMonthStart(t time.Time, loc *time.Location) bool {
	l := t.In(loc)
	return l.Day() == 1 && l.Hour() == 0 && l.Minute() == 0 && l.Second() == 0 && l.Nanosecond() == 0
}

// programReportPeriod turns the from and to query values of a report into the
// period [from, to) it covers, read in the program time zone. A named program
// covers whole days: from moves to the start of its day and the day of to is
// included in full. An anonymous program covers whole calendar months: from
// must be the first day of a month, to the first day of a later month and is
// not included, or both are left out for all time.
func programReportPeriod(program *model.Program, fromText, toText string) (*time.Time, *time.Time, error) {
	loc := program.Location()
	from, err := parseProgramReportBound(fromText, loc)
	if err != nil {
		return nil, nil, validate.WrapErrorWithField(err, "from")
	}
	to, err := parseProgramReportBound(toText, loc)
	if err != nil {
		return nil, nil, validate.WrapErrorWithField(err, "to")
	}
	if program.IsAnonymousOn() {
		if from == nil && to == nil {
			return nil, nil, nil
		}
		if from == nil || to == nil || !isMonthStart(*from, loc) || !isMonthStart(*to, loc) || !from.Before(*to) {
			return nil, nil, errs.NewValidationError(errProgramReportWholeMonths)
		}
		return from, to, nil
	}
	if from != nil {
		f := from.In(loc)
		day := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, loc)
		from = &day
	}
	if to != nil {
		t := to.In(loc)
		next := time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		to = &next
	}
	return from, to, nil
}

// reportPDFError turns a PDF render failure into an error for the client. A
// failure with a public message keeps it, any other detail stays in the log.
func reportPDFError(err error) error {
	var opErr errs.OperationalError
	if errors.As(err, &opErr) {
		return errs.Wrap(err)
	}
	return errs.NewCustomError(errors.New("could not generate the report PDF, check the server logs for details"))
}

// programReportPDFTimeout bounds a program report download, the wait for a
// report browser included. It is below the 3 minute timeout of the route.
const programReportPDFTimeout = 150 * time.Second

// ExportReportPDF renders the program report for a period as a PDF for a
// download and audits it. It uses the same switch and renderer as the campaign
// report. fromText and toText are the query values that programReportPeriod
// reads.
func (s *Program) ExportReportPDF(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	fromText, toText string,
) (_ []byte, _ string, err error) {
	ae := NewAuditEvent("Program.ExportReport", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil, "", err
	}
	if !s.programReportEnabled(ctx) {
		return nil, "", errs.Wrap(gorm.ErrRecordNotFound)
	}
	// an anonymous report reads whole months, so a narrow period can not single
	// out one person's send
	from, to, err := programReportPeriod(program, fromText, toText)
	if err != nil {
		return nil, "", err
	}
	if from != nil && to != nil {
		if !from.Before(*to) {
			return nil, "", errs.NewValidationError(errors.New("from must be before to"))
		}
		if to.Sub(*from) > programReportMaxSpan {
			return nil, "", errs.NewValidationError(errors.New("period is too long"))
		}
	}
	if from != nil {
		ae.Details["from"] = from.UTC().Format(time.RFC3339)
	}
	if to != nil {
		ae.Details["to"] = to.UTC().Format(time.RFC3339)
	}
	// the report is stopped before the 3 minute deadline of the route, so a
	// wait for a busy report browser ends with a message and not a cut answer
	ctx, cancel := context.WithTimeout(ctx, programReportPDFTimeout)
	defer cancel()
	htmlContent, err := s.buildReportHTML(ctx, session, program, time.Now(), from, to)
	if err != nil {
		return nil, "", err
	}
	rb := newReportBrowser(ctx, s.RemoteBrowserExecPath, s.Logger)
	defer rb.close()
	pdf, err := rb.renderPDF(htmlContent)
	if err != nil {
		s.Logger.Errorw("failed to render program report PDF", "error", err)
		return nil, "", reportPDFError(err)
	}
	s.AuditLogAuthorized(ae)
	return pdf, programName(program), nil
}

// buildReportHTML renders the report page
func (s *Program) buildReportHTML(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	now time.Time,
	from, to *time.Time,
) (string, error) {
	results, err := s.programResults(ctx, session, program, now, from, to)
	if err != nil {
		return "", err
	}
	d := programReportFromResults(program, s.programReportSetup(ctx, session, program), results, now, from, to)
	return s.renderProgramReport(ctx, program, d)
}

// programReportSetup reads the template and group names of a program for the
// report. A template that can no longer be read is left out.
func (s *Program) programReportSetup(ctx context.Context, session *model.Session, program *model.Program) *programReportSetup {
	setup := &programReportSetup{}
	infos, err := s.stepTemplateInfos(ctx, program)
	if err != nil {
		s.Logger.Warnw("failed to read the program templates for the report", "error", err)
	}
	for _, st := range program.Steps {
		templateID, err := st.CampaignTemplateID.Get()
		if err != nil {
			continue
		}
		if info, ok := infos[templateID]; ok {
			setup.TemplateNames = append(setup.TemplateNames, info.Name)
		}
	}
	for _, g := range program.RecipientGroups {
		if v, err := g.Name.Get(); err == nil {
			setup.GroupNames = append(setup.GroupNames, v.String())
		}
	}
	if companyID, err := program.CompanyID.Get(); err == nil && s.CompanyRepository != nil {
		if c, err := s.CompanyRepository.GetByID(ctx, &companyID); err == nil {
			if v, err := c.Name.Get(); err == nil {
				setup.CompanyName = v.String()
			}
		}
	}
	return setup
}

// programReportFromResults turns the results into report text, with the
// figures and names of the program page
func programReportFromResults(
	program *model.Program,
	setup *programReportSetup,
	results *ProgramResults,
	now time.Time,
	from, to *time.Time,
) *programReportData {
	loc := program.Location()
	// to is not included, so the period names the day before it as its last
	lastDay := func(t *time.Time) string {
		return t.Add(-time.Nanosecond).In(loc).Format("2 Jan 2006")
	}
	period := "All time"
	switch {
	case from != nil && to != nil:
		period = from.In(loc).Format("2 Jan 2006") + " to " + lastDay(to)
	case from != nil:
		period = "From " + from.In(loc).Format("2 Jan 2006")
	case to != nil:
		period = "Until " + lastDay(to)
	}
	// an anonymous program counts whole settled months, name the last one
	// when the period reaches past it
	if results.Anonymous && results.CountedTo != nil && (to == nil || results.CountedTo.Before(*to)) {
		period += ", counted to " + lastDay(results.CountedTo)
	}
	sum := results.Summary
	if sum == nil {
		sum = &ProgramSummary{}
	}
	d := &programReportData{
		Name:                 programName(program),
		CompanyName:          setup.CompanyName,
		Generated:            now.In(loc).Format("2 Jan 2006 15:04 MST"),
		Period:               period,
		Anonymous:            results.Anonymous,
		HiddenText:           programWithheldText,
		Details:              programReportDetails(program, setup),
		MinGroup:             programMinGroup,
		RepeatOffenderMonths: sum.RepeatOffenderMonths,
		HasTraining:          sum.HasTraining,
		HasPhishing:          sum.HasPhishing,
		// HasSent is known also when an anonymous program withholds the counts,
		// so a small month report does not read as nothing sent
		HasSends: sum.HasSent,
	}
	// the cover names the day the program first ran, else its planned start
	if t := program.StartedAt; t != nil {
		d.Started = t.In(loc).Format("2 Jan 2006")
	} else if t := program.StartAtTime(); t != nil {
		d.Started = t.In(loc).Format("2 Jan 2006")
	}
	if t, err := program.ClosedAt.Get(); err == nil {
		d.Closed = t.In(loc).Format("2 Jan 2006")
	}
	// card is a result card. Its sub line is the share of a base of the same
	// people, named as on the program page tiles: an outcome of the people
	// sent to, a completed training of the people who started it. The people
	// enrolled are no base, as a period or a removed recipient leaves sends
	// that do not match them. An anonymous program counts sends, so it shows
	// no share.
	card := func(label, tone string, v, base *int, baseLabel string) programReportLine {
		if v == nil {
			return programReportLine{Label: label, Value: programWithheldText, Tone: tone, Withheld: true}
		}
		l := programReportLine{Label: label, Value: strconv.Itoa(*v), Tone: tone}
		if base != nil && !results.Anonymous {
			if p := pctOnlyText(intPtr(min(*v, *base)), base); p != "" {
				l.Sub = p + " " + baseLabel
			}
		}
		return l
	}
	// outcome is a chart of a share of the people sent to
	outcome := func(label, tone string, v *int) programReportLine {
		l := card(label, tone, v, nil, "")
		if v != nil && sum.Sent != nil && *sum.Sent > 0 {
			l.Rate = float64(min(*v, *sum.Sent)) * 100 / float64(*sum.Sent)
			l.HasRate = true
		}
		return l
	}
	d.Results = []programReportLine{
		{Label: "Recipients", Value: strconv.Itoa(sum.Recipients), Tone: "neutral"},
	}
	if sum.HasPhishing {
		d.Results = append(d.Results,
			card("Sent", "info", sum.Sent, nil, ""),
			card("Clicked link", "warning", sum.ClickedLink, sum.Sent, "of sent"),
			card("Data submitted", "danger", sum.SubmittedData, sum.Sent, "of sent"),
			card("Reported", "safe", sum.Reported, sum.Sent, "of sent"),
		)
	}
	if sum.HasTraining {
		// a program with training only shows its training sends as Sent, as the
		// Sent tile on the program page does
		if !sum.HasPhishing {
			d.Results = append(d.Results, card("Sent", "info", sum.TrainingSent, nil, ""))
		}
		d.Results = append(d.Results,
			card("Training started", "training", sum.TrainingStarted, sum.TrainingSent, "of training sent"),
			card("Training completed", "safe", sum.TrainingCompleted, sum.TrainingStarted, "of started"),
		)
	}
	// an anonymous program never counts repeat offenders
	if !results.Anonymous && sum.HasPhishing {
		d.Results = append(d.Results, card("Repeat offenders", "danger", sum.RepeatOffenders, nil, ""))
	}
	if sum.HasPhishing {
		d.Outcome = []programReportLine{
			outcome("Clicked link", "warning", sum.ClickedLink),
			outcome("Data submitted", "danger", sum.SubmittedData),
			outcome("Reported", "safe", sum.Reported),
		}
	}
	// the templates come most sent first, the months oldest first
	d.TemplatesTotal = len(results.Templates)
	templates := results.Templates
	if len(templates) > programReportTableTemplates {
		templates = templates[:programReportTableTemplates]
	}
	for _, t := range templates {
		d.Templates = append(d.Templates, programReportResultRow(t.Name, t, sum.HasPhishing, sum.HasTraining))
	}
	d.MonthsTotal = len(results.Months)
	months := results.Months
	if len(months) > programReportTableMonths {
		months = months[len(months)-programReportTableMonths:]
	}
	for _, m := range months {
		name := m.Name
		if m.Partial {
			name += " (partial)"
		}
		d.Months = append(d.Months, programReportResultRow(name, m, sum.HasPhishing, sum.HasTraining))
	}
	// the trend shows the click and report rates, so only with phishing
	if sum.HasPhishing {
		d.Trend, d.TrendMax = programReportTrend(results.Months)
	}
	return d
}

// programReportResultRow turns a month or template row into report cells: the
// name, then with phishing the sends and each outcome as a count with its
// percent of the sends, then with training the training sends, the trainings
// started with their percent of the training sends and the trainings
// completed with their percent of the trainings started. A count the row does
// not hold is shown as "-".
func programReportResultRow(name string, r *ProgramResultRow, phishing, training bool) programReportRow {
	// a count is null when an anonymous program hides it, else when the row
	// does not hold it
	cell := func(v *int) string {
		if v == nil && !r.OutcomesHidden {
			return "-"
		}
		return countText(v)
	}
	row := programReportRow{Cells: []string{name}, Pcts: []string{""}}
	if phishing {
		row.Cells = append(row.Cells, cell(r.Sent), cell(r.Clicked), cell(r.Submitted), cell(r.Reported))
		row.Pcts = append(row.Pcts, "", pctOnlyText(r.Clicked, r.Sent), pctOnlyText(r.Submitted, r.Sent), pctOnlyText(r.Reported, r.Sent))
	}
	if training {
		row.Cells = append(row.Cells, cell(r.TrainingSent), cell(r.TrainingStarted), cell(r.TrainingCompleted))
		row.Pcts = append(row.Pcts, "", pctOnlyText(r.TrainingStarted, r.TrainingSent), pctOnlyText(r.TrainingCompleted, r.TrainingStarted))
	}
	if r.Suppressed {
		for i := 1; i < len(row.Cells); i++ {
			row.Cells[i] = programWithheldText
		}
	}
	return row
}

// programReportTrendMonths is how many months the trend chart shows, the last
// ones of the period
const programReportTrendMonths = 12

// programReportTableMonths and programReportTableTemplates bound the month
// and template tables, so a long program fits on its page
const (
	programReportTableMonths    = 12
	programReportTableTemplates = 15
)

// programReportTrend builds the trend chart of the click and report rate per
// month and the rate in percent the top of the chart stands for, rounded up to
// a multiple of 10. A withheld rate has no bar.
func programReportTrend(months []*ProgramResultRow) ([]programReportTrendMonth, float64) {
	if len(months) > programReportTrendMonths {
		months = months[len(months)-programReportTrendMonths:]
	}
	type rate struct {
		value float64
		shown bool
	}
	read := func(m *ProgramResultRow, count *int) rate {
		if m.Suppressed || m.Sent == nil || count == nil || *m.Sent == 0 {
			return rate{}
		}
		return rate{value: float64(*count) * 100 / float64(*m.Sent), shown: true}
	}
	top := 10.0
	rates := make([][2]rate, len(months))
	for i, m := range months {
		rates[i] = [2]rate{read(m, m.Clicked), read(m, m.Reported)}
		for _, r := range rates[i] {
			if r.shown && r.value > top {
				top = r.value
			}
		}
	}
	ceiling := int(top+9.999) / 10 * 10
	if ceiling > 100 {
		ceiling = 100
	}
	bar := func(m *ProgramResultRow, count *int, r rate) programReportBar {
		if !r.shown {
			withheld := m.Suppressed || (m.Sent != nil && *m.Sent > 0 && count == nil)
			return programReportBar{Text: pctText(count, m.Sent), Withheld: withheld}
		}
		// a rate over the chart top is drawn full height
		return programReportBar{Text: fmt.Sprintf("%.0f%%", r.value), Height: math.Min(r.value*100/float64(ceiling), 100)}
	}
	out := make([]programReportTrendMonth, 0, len(months))
	for i, m := range months {
		out = append(out, programReportTrendMonth{
			Label:    m.Name,
			Partial:  m.Partial,
			Clicked:  bar(m, m.Clicked, rates[i][0]),
			Reported: bar(m, m.Reported, rates[i][1]),
		})
	}
	return out, float64(ceiling)
}

// programReportEnabled reports whether PDF reports are turned on
func (s *Program) programReportEnabled(ctx context.Context) bool {
	if s.OptionService == nil {
		return false
	}
	opt, _ := s.OptionService.GetOptionWithoutAuth(ctx, data.OptionKeyReportPDFEnabled)
	return opt != nil && opt.Value.String() == "true"
}
