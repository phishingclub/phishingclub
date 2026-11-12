package service

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
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

//go:embed programReport.html
var programReportHTML string

var programReportTemplate = template.Must(template.New("programReport").Parse(programReportHTML))

// programReportMaxSpan bounds the period a report may cover
const programReportMaxSpan = 20 * 365 * 24 * time.Hour

// programReportLine is one label and value of the report
type programReportLine struct {
	Label string
	Value string
}

// programReportRow is one table row of the report, all cells as text
type programReportRow struct {
	Cells []string
}

// programReportData is what the report template renders. Every value is text
// so the template only prints, and html/template escapes all of it.
type programReportData struct {
	Name        string
	Generated   string
	Period      string
	Anonymous   bool
	Details     []programReportLine
	Headline    []programReportLine
	Funnel      []programReportLine
	Resilience  []programReportLine
	Exposure    []programReportRow
	Templates   []programReportRow
	Months      []programReportRow
	MinGroup    int
	HasExposure bool
}

// programReportSetup is what the report shows about how the program is set up
// beyond the program row: its template names and group names
type programReportSetup struct {
	TemplateNames []string
	GroupNames    []string
	HasTraining   bool
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

func rateText(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *v*100)
}

// ratioText prints a ratio with at most two decimals, such as 0.5
func ratioText(v *float64) string {
	if v == nil {
		return "-"
	}
	out := strings.TrimRight(fmt.Sprintf("%.2f", *v), "0")
	return strings.TrimSuffix(out, ".")
}

// exposureRateText prints the rate of an exposure row, a null count in a row
// with sends is a withheld one
func exposureRateText(e *ProgramExposureRow, count *int, rate *float64) string {
	if e.Sent == nil || (*e.Sent > 0 && count == nil) {
		return programWithheldText
	}
	return rateText(rate)
}

func countText(v *int) string {
	if v == nil {
		return programWithheldText
	}
	return strconv.Itoa(*v)
}

func minutesText(v *float64) string {
	if v == nil {
		return "-"
	}
	d := time.Duration(*v * float64(time.Minute))
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
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
		if r.From == r.To {
			out = append(out, from.Format("2 Jan 2006"))
			continue
		}
		out = append(out, from.Format("2 Jan 2006")+" to "+to.Format("2 Jan 2006"))
	}
	if len(out) == 0 {
		return "None"
	}
	return strings.Join(out, ", ")
}

// listText joins names, or returns none when there are no names
func listText(names []string, none string) string {
	if len(names) == 0 {
		return none
	}
	return strings.Join(names, ", ")
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
	order := "Random order per person"
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
		templates += ": " + strings.Join(setup.TemplateNames, ", ")
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
		{"Status", status},
		{"Anonymous", anonymous},
		{"Templates", templates},
		{"Template order", order},
		{"After the last template", loop},
		{"Time between emails", durationText(int(p.IntervalOrDefault().Minutes()))},
		{"Spread", spread},
		{"Sending days", weekDaysText(p)},
		{"Sending hours", window},
		{"Skip dates", skipDatesText(p)},
		{"First email to new members", newMembers},
		{"Start", startText},
		{"End", endText},
		{"Groups", listText(setup.GroupNames, "None")},
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

// BuildReportPDF renders the program report for a period as a PDF. It uses the
// same switch and renderer as the campaign report. fromText and toText are the
// query values that programReportPeriod reads.
func (s *Program) BuildReportPDF(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	fromText, toText string,
) ([]byte, string, error) {
	ae := NewAuditEvent("Program.Report", session)
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
	htmlContent, err := s.buildReportHTML(ctx, session, program, time.Now(), from, to)
	if err != nil {
		return nil, "", err
	}
	rb := newReportBrowser(ctx, s.CampaignService.RemoteBrowserExecPath, s.Logger)
	defer rb.close()
	pdf, err := rb.renderPDF(htmlContent)
	if err != nil {
		s.Logger.Errorw("failed to render program report PDF", "error", err)
		return nil, "", reportPDFError(err)
	}
	if from != nil {
		ae.Details["from"] = from.UTC().Format(time.RFC3339)
	}
	if to != nil {
		ae.Details["to"] = to.UTC().Format(time.RFC3339)
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
	facts, err := s.loadProgramFacts(ctx, session, program, nil)
	if err != nil {
		return "", err
	}
	results, err := s.resultsFromFacts(ctx, program, facts, now, from, to)
	if err != nil {
		return "", err
	}
	headline := programReportHeadlineResults(facts, program.Location(), now, to)
	d := programReportFromResults(program, s.programReportSetup(ctx, session, program), results, headline, facts, now, from, to)
	var buf bytes.Buffer
	if err := programReportTemplate.Execute(&buf, d); err != nil {
		return "", errs.Wrap(err)
	}
	return buf.String(), nil
}

// programReportSetup reads the template and group names of a program for the
// report. A template that can no longer be read is left out.
func (s *Program) programReportSetup(ctx context.Context, session *model.Session, program *model.Program) *programReportSetup {
	setup := &programReportSetup{}
	for _, st := range program.Steps {
		templateID := st.CampaignTemplateID.MustGet()
		t, err := s.CampaignTemplateService.GetByID(ctx, session, &templateID, &repository.CampaignTemplateOption{})
		if err != nil {
			continue
		}
		if v, err := t.Name.Get(); err == nil {
			setup.TemplateNames = append(setup.TemplateNames, v.String())
		}
		if v, err := t.IsTraining.Get(); err == nil && v {
			setup.HasTraining = true
		}
	}
	for _, g := range program.RecipientGroups {
		if v, err := g.Name.Get(); err == nil {
			setup.GroupNames = append(setup.GroupNames, v.String())
		}
	}
	return setup
}

// programReportHeadlineResults computes the results the report headline reads:
// the phish prone rate over 12 months, the repeat clickers and the report rate
// over 30 days. They are read from every phishing send before the end of the
// report, to or now, so they keep their own window whatever the period is.
func programReportHeadlineResults(facts *programFacts, loc *time.Location, now time.Time, to *time.Time) *ProgramResults {
	anchor := now
	if to != nil && to.Before(now) {
		anchor = *to
	}
	return buildProgramResults(facts, nil, loc, anchor, nil, &anchor)
}

// programReportFromResults turns the results into report text. headline holds
// the results the headline windows read, see programReportHeadlineResults.
func programReportFromResults(
	program *model.Program,
	setup *programReportSetup,
	results *ProgramResults,
	headline *ProgramResults,
	facts *programFacts,
	now time.Time,
	from, to *time.Time,
) *programReportData {
	loc := program.Location()
	// an anonymous report counts only settled sends and outcomes
	facts = facts.countable(now, loc)
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
	d := &programReportData{
		Name:      programName(program),
		Generated: now.In(loc).Format("2 Jan 2006 15:04 MST"),
		Period:    period,
		Anonymous: results.Anonymous,
		Details:   programReportDetails(program, setup),
		MinGroup:  programMinGroup,
	}
	// the funnel counts the phishing sends of the period
	anonymous := results.Anonymous
	funnel := &resultCounts{}
	var trainingSent, trainingDone programTally
	// training rows show when the program has a training template, or had one
	// that was sent
	hasTraining := setup.HasTraining
	for _, sd := range facts.sends {
		if sd.Training {
			hasTraining = true
		}
		if !inPeriod(sd, from, to) {
			continue
		}
		if sd.Training {
			trainingSent.add(sd.Person)
			if sd.TrainingCompletedAt != nil {
				trainingDone.add(sd.Person)
			}
			continue
		}
		funnel.add(sd)
	}
	withheld := funnel.tooFew(anonymous)
	val := func(v int) string {
		if withheld {
			return programWithheldText
		}
		return strconv.Itoa(v)
	}
	of := func(t *programTally) string {
		if withheld || funnel.outcome(t, anonymous) == nil {
			return programWithheldText
		}
		if funnel.sent.sends == 0 {
			return "-"
		}
		return fmt.Sprintf("%d (%.0f%%)", t.sends, float64(t.sends)*100/float64(funnel.sent.sends))
	}
	d.Funnel = []programReportLine{
		{"Emails sent", val(funnel.sent.sends)},
		{"Clicked", of(&funnel.clicked)},
		{"Submitted data", of(&funnel.submitted)},
		{"Reported", of(&funnel.reported)},
	}
	trainingSentText, trainingDoneText := programWithheldText, programWithheldText
	if !anonymous || trainingSent.persons() >= programMinGroup {
		trainingSentText = strconv.Itoa(trainingSent.sends)
		if !anonymous || !trainingDone.withheldIn(&trainingSent) {
			trainingDoneText = strconv.Itoa(trainingDone.sends)
		}
	}
	if hasTraining {
		d.Funnel = append(d.Funnel,
			programReportLine{"Training sent", trainingSentText},
			programReportLine{"Training completed", trainingDoneText},
		)
	}
	reportRate30 := programWithheldText
	if l := headline.Last30; l != nil && !l.Suppressed {
		reportRate30 = pctText(l.Reported, l.Sent)
	}
	prone := programWithheldText
	if p := headline.PhishProne; p != nil && !p.Suppressed {
		prone = pctText(p.Prone, p.People) + " (" + countText(p.Prone) + " of " + countText(p.People) + " people)"
	}
	repeat := programWithheldText
	if r := headline.RepeatClickers; r != nil && r.TwoPlus != nil {
		repeat = countText(r.TwoPlus) + " of " + countText(r.Eligible2) + " people"
	}
	// the reporting measures follow the selected period
	reportRate, noClick, ratio, median := programWithheldText, programWithheldText, programWithheldText, programWithheldText
	if r := results.Resilience; r != nil {
		if r.Suppressed {
			d.Resilience = []programReportLine{{"Reporting", programWithheldText}}
		} else {
			hidden := map[string]bool{}
			for _, name := range r.Withheld {
				hidden[name] = true
			}
			shown := func(name, text string) string {
				if hidden[name] {
					return programWithheldText
				}
				return text
			}
			reportRate = shown("reportRate", rateText(r.ReportRate))
			noClick = shown("reportedWithoutClickRate", rateText(r.ReportedWithoutClickRate))
			ratio = shown("resilienceRatio", ratioText(r.ResilienceRatio))
			median = shown("medianMinutesToReport", minutesText(r.MedianMinutesToReport))
			d.Resilience = []programReportLine{
				{"Report rate", reportRate},
				{"Reported, no click", noClick},
				{"Reports per click", ratio},
				{"Median time to report", median},
			}
		}
	}
	d.Headline = []programReportLine{
		{"Report rate (" + strconv.Itoa(int(programLastPeriod/(24*time.Hour))) + " days)", reportRate30},
		{"Reported, no click", noClick},
		{"Phish prone (12 months)", prone},
		{"Median time to report", median},
		{"Reports per click", ratio},
		{"Repeat clickers (2 or more of last " + strconv.Itoa(programRepeatLastSends) + " emails)", repeat},
	}
	for _, e := range results.Exposure {
		d.Exposure = append(d.Exposure, programReportRow{Cells: []string{
			e.Label, countText(e.Sent), exposureRateText(e, e.Clicked, e.ClickRate), exposureRateText(e, e.Reported, e.ReportRate),
		}})
	}
	d.HasExposure = len(d.Exposure) > 0
	for _, t := range results.Templates {
		difficulty := t.Difficulty
		if difficulty == "" {
			difficulty = "-"
		}
		d.Templates = append(d.Templates, programReportRow{Cells: []string{
			t.Name, countText(t.Sent), pctText(t.Clicked, t.Sent), pctText(t.Submitted, t.Sent), pctText(t.Reported, t.Sent), difficulty,
		}})
	}
	for _, m := range results.Months {
		d.Months = append(d.Months, programReportRow{Cells: []string{
			m.Name, countText(m.Sent), pctText(m.Clicked, m.Sent), pctText(m.Submitted, m.Sent), pctText(m.Reported, m.Sent),
		}})
	}
	return d
}

// programReportEnabled reports whether PDF reports are turned on
func (s *Program) programReportEnabled(ctx context.Context) bool {
	if s.CampaignService == nil || s.CampaignService.OptionService == nil {
		return false
	}
	opt, _ := s.CampaignService.OptionService.GetOptionWithoutAuth(ctx, data.OptionKeyReportPDFEnabled)
	return opt != nil && opt.Value.String() == "true"
}
