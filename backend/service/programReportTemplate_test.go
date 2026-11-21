package service

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/model"
)

// defaultProgramReportRenderer renders the built in program report template
// the way a program report is rendered, for tests
type defaultProgramReportRenderer struct{}

func (defaultProgramReportRenderer) Execute(w io.Writer, d *programReportData) error {
	out, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, out)
	return err
}

// programReportTemplate is the built in program report, for tests
var programReportTemplate = defaultProgramReportRenderer{}

// setAllStrings sets every string reachable in v to s
func setAllStrings(v reflect.Value, s string) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			setAllStrings(v.Elem(), s)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				setAllStrings(v.Field(i), s)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			setAllStrings(v.Index(i), s)
		}
	case reflect.String:
		v.SetString(s)
	}
}

// findString reports whether a string reachable in v holds s
func findString(v reflect.Value, s string) bool {
	switch v.Kind() {
	case reflect.Pointer:
		return !v.IsNil() && findString(v.Elem(), s)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if findString(v.Field(i), s) {
				return true
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if findString(v.Index(i), s) {
				return true
			}
		}
	case reflect.String:
		return strings.Contains(v.String(), s)
	}
	return false
}

func TestHTMLEscapeProgramReportDataEscapesEveryText(t *testing.T) {
	d := programReportSampleData()
	setAllStrings(reflect.ValueOf(d), "<b>x</b>")
	escaped := htmlEscapeProgramReportData(d)
	if findString(reflect.ValueOf(escaped), "<b>") {
		t.Fatalf("every text of the program report data must be escaped")
	}
	// the original is left as it was
	if d.Name != "<b>x</b>" {
		t.Fatalf("the original data must not change")
	}
}

func TestRenderProgramReportContent(t *testing.T) {
	out, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, programReportSampleData())
	if err != nil {
		t.Fatalf("default program report must render: %v", err)
	}
	for _, want := range []string{"Sample Program", "Program Details", "Trend by Month", "Repeat offenders"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
	// the template functions of the campaign report are there
	if _, err := renderProgramReportContent(`{{htmlEscape .Name}} {{len .Months}} {{index (index .Months 0).Cells 0}}`, programReportSampleData()); err != nil {
		t.Fatalf("functions: %v", err)
	}
	if _, err := renderProgramReportContent(`{{.Nope`, programReportSampleData()); err == nil {
		t.Fatalf("a template that does not parse must fail")
	}
	if _, err := renderProgramReportContent(`{{.CampaignName}}`, programReportSampleData()); err == nil {
		t.Fatalf("a field the program data does not have must fail")
	}
	// a withheld value renders as the withheld text, never as a number
	d := programReportSampleData()
	d.Anonymous = true
	d.Results = []programReportLine{{Label: "Clicked link", Value: programWithheldText, Tone: "warning", Withheld: true}}
	d.Outcome = d.Results
	out, err = renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil || !strings.Contains(out, `mcard-value hidden">`+programWithheldText) {
		t.Fatalf("withheld value must render as withheld: %v", err)
	}
	if strings.Contains(out, "of sent</text>") {
		t.Fatalf("a withheld outcome must not draw a rate")
	}
}

func TestRenderReportPreviewKind(t *testing.T) {
	for _, kind := range model.ReportKinds {
		if _, err := renderReportPreviewKind(defaultReportContentForKind(kind), kind); err != nil {
			t.Fatalf("default %s report must preview: %v", kind, err)
		}
	}
	// a campaign template does not run on program data and the other way round
	if _, err := renderReportPreviewKind(embedded.DefaultReportHTML, model.ReportKindProgram); err == nil {
		t.Fatalf("a campaign template must not run on program data")
	}
	if _, err := renderReportPreviewKind(embedded.DefaultProgramReportHTML, model.ReportKindSimulation); err == nil {
		t.Fatalf("a program template must not run on campaign data")
	}
	if _, err := renderReportPreviewKind("x", "other"); err == nil {
		t.Fatalf("an unknown kind must fail")
	}
}

func TestProgramReportTrend(t *testing.T) {
	n := func(v int) *int { return &v }
	months := []*ProgramResultRow{}
	for i := 0; i < 14; i++ {
		months = append(months, &ProgramResultRow{Name: "m", Sent: n(10), Clicked: n(1), Reported: n(2)})
	}
	months[12] = &ProgramResultRow{Name: "small", Suppressed: true}
	months[13] = &ProgramResultRow{Name: "last", Sent: n(20), Clicked: nil, Reported: n(7)}
	trend, top := programReportTrend(months)
	if len(trend) != programReportTrendMonths {
		t.Fatalf("the chart shows the last %d months, got %d", programReportTrendMonths, len(trend))
	}
	if top != 40 {
		t.Fatalf("the top of the chart rounds the highest rate up to 10s, got %v", top)
	}
	small := trend[len(trend)-2]
	if !small.Clicked.Withheld || !small.Reported.Withheld || small.Clicked.Height != 0 {
		t.Fatalf("a suppressed month has no bars: %+v", small)
	}
	last := trend[len(trend)-1]
	if !last.Clicked.Withheld || last.Clicked.Height != 0 || last.Clicked.Text != programWithheldText {
		t.Fatalf("a withheld rate has no bar: %+v", last.Clicked)
	}
	if last.Reported.Withheld || last.Reported.Text != "35%" || last.Reported.Height != 87.5 {
		t.Fatalf("a shown rate has a bar: %+v", last.Reported)
	}
	// an anonymous report shows a withheld bar as hidden, never as a number
	d := programReportSampleData()
	d.Anonymous = true
	d.Trend = trend
	out, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, `<div class="fill hidden"></div>`) {
		t.Fatalf("a withheld bar must render as hidden")
	}
	for _, want := range []string{"Program Report", "World Corp", "Program Details"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

// TestProgramReportConditionalSections checks the sections without data are
// left out: training without training, the outcome without sends, and the
// trend and template pages without rows
func TestProgramReportConditionalSections(t *testing.T) {
	d := programReportSampleData()
	d.HasTraining = false
	kept := []programReportLine{}
	for _, l := range d.Results {
		if l.Tone != "training" && l.Label != "Training completed" {
			kept = append(kept, l)
		}
	}
	d.Results = kept
	d.HasSends = false
	d.Months = nil
	d.Trend = nil
	d.Templates = nil
	out, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, gone := range []string{"Training started", "Training completed", ">Outcome<", "Trend by Month", "Results by Template"} {
		if strings.Contains(out, gone) {
			t.Fatalf("report must leave out %q", gone)
		}
	}
	if !strings.Contains(out, "Nothing was sent in this period.") {
		t.Fatalf("a period without sends must say so")
	}
	d = programReportSampleData()
	out, err = renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"Training started", "c-training", "Repeat offenders", "Oct 2025 (partial)", "<em>partial</em>", "15 <span class=\"dim\">(19%)</span>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

// TestProgramReportSampleCards checks the preview sample holds the cards a
// mixed program report gives, with the share lines of the real report. The
// report editor sample in the frontend mirrors these values.
func TestProgramReportSampleCards(t *testing.T) {
	d := programReportSampleData()
	want := [][3]string{
		{"Recipients", "120", ""},
		{"Sent", "118", ""},
		{"Clicked link", "31", "26% of sent"},
		{"Data submitted", "11", "9% of sent"},
		{"Reported", "46", "39% of sent"},
		{"Training started", "34", "83% of training sent"},
		{"Training completed", "29", "85% of started"},
		{"Repeat offenders", "6", ""},
	}
	if len(d.Results) != len(want) {
		t.Fatalf("want %d cards, got %+v", len(want), d.Results)
	}
	for i, w := range want {
		l := d.Results[i]
		if l.Label != w[0] || l.Value != w[1] || l.Sub != w[2] {
			t.Fatalf("card %d = %q %q %q, want %q %q %q", i, l.Label, l.Value, l.Sub, w[0], w[1], w[2])
		}
	}
	if !d.HasSends || !d.HasPhishing || !d.HasTraining || len(d.Outcome) != 3 {
		t.Fatalf("the sample is a mixed program with sends: %+v", d)
	}
}
