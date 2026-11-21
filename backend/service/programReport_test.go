package service

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func TestDurationText(t *testing.T) {
	const day = 24 * 60
	cases := map[int]string{
		0:          "-",
		1:          "1 minute",
		90:         "90 minutes",
		60:         "1 hour",
		day:        "1 day",
		3 * day:    "3 days",
		7 * day:    "1 week",
		14 * day:   "2 weeks",
		30 * day:   "1 month",
		90 * day:   "3 months",
		365 * day:  "1 year",
		10 * day:   "10 days",
		day + 30:   "1470 minutes",
		2 * 60:     "2 hours",
		210 * day:  "7 months",
		730 * day:  "2 years",
		1095 * day: "3 years",
	}
	for minutes, want := range cases {
		if got := durationText(minutes); got != want {
			t.Fatalf("durationText(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestProgramReportDetails(t *testing.T) {
	start := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	program := &model.Program{
		Status:                "active",
		IsAnonymous:           nullable.NewNullableWithValue(true),
		OrderMode:             nullable.NewNullableWithValue(model.ProgramOrderSequential),
		LoopMode:              nullable.NewNullableWithValue(model.ProgramLoopLoop),
		StepIntervalMinutes:   nullable.NewNullableWithValue(30 * 24 * 60),
		SpreadMinutes:         nullable.NewNullableWithValue(2 * 24 * 60),
		NewMemberDelayMinutes: nullable.NewNullableWithValue(7 * 24 * 60),
		StartAt:               nullable.NewNullableWithValue(start),
		BlackoutDates: nullable.NewNullableWithValue([]model.ProgramBlackout{
			{From: "2026-12-24", To: "2026-12-26"},
			{From: "2026-05-01", To: "2026-05-01"},
		}),
		Steps: []*model.ProgramStep{{}, {}},
	}
	setup := &programReportSetup{TemplateNames: []string{"Invoice", "Parcel"}, GroupNames: []string{"Sales"}}
	got := map[string]string{}
	labels := []string{}
	for _, l := range programReportDetails(program, setup) {
		got[l.Label] = l.Value
		labels = append(labels, l.Label)
	}
	want := map[string]string{
		"Status":                        "Active",
		"Anonymous":                     "Yes",
		"Templates":                     "2: Invoice, Parcel",
		"Template order":                "Same order for everyone",
		"After the last template":       "Start over",
		"Time between templates":        "1 month",
		"Spread sends over":             "2 days",
		"Sending days":                  "Any day",
		"Sending hours":                 "Any time",
		"Skip dates":                    "24 Dec 2026 to 26 Dec 2026, 1 May 2026",
		"First template to new members": "After 1 week",
		"Start":                         "5 Jan 2026 09:00",
		"End":                           "Runs until closed",
		"Groups":                        "Sales",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, l := range labels {
		if strings.Contains(strings.ToLower(l), "time zone") || strings.Contains(strings.ToLower(l), "simulation") {
			t.Fatalf("unexpected label %q", l)
		}
	}
	program.EndAt = nullable.NewNullableWithValue(start.AddDate(1, 0, 0))
	program.IsAnonymous = nullable.NewNullableWithValue(false)
	for _, l := range programReportDetails(program, setup) {
		if l.Label == "End" && l.Value != "5 Jan 2027 09:00" {
			t.Fatalf("End = %q", l.Value)
		}
		if l.Label == "Anonymous" && l.Value != "No" {
			t.Fatalf("Anonymous = %q", l.Value)
		}
	}
}

// TestProgramReportSections checks the report shows the figures and names of
// the program page, with training cards only for a program with training
func TestProgramReportSections(t *testing.T) {
	program := &model.Program{
		Name:   nullable.NewNullableWithValue(*vo.NewString64Must("Year one")),
		Status: "active",
	}
	facts := &programFacts{templateNames: map[string]string{"t1": "Invoice"}}
	for _, p := range people(6) {
		facts.sends = append(facts.sends, send(p, 10, "clicked"))
	}
	r := buildProgramResults(facts, programResultsInput{loc: time.UTC, now: resultsNow, recipients: 8, repeatMonths: 12})
	d := programReportFromResults(program, &programReportSetup{}, r, resultsNow, nil, nil)
	if d.HasTraining || !d.HasSends {
		t.Fatalf("flags: training %v, sends %v", d.HasTraining, d.HasSends)
	}
	if got := d.Templates[0]; len(got.Cells) != 5 || got.Cells[2] != "6" || got.Pcts[2] != "100%" {
		t.Fatalf("template row: %+v", got)
	}
	wantOrder := []string{"Recipients", "Sent", "Clicked link", "Data submitted", "Reported", "Repeat offenders"}
	if len(d.Results) != len(wantOrder) {
		t.Fatalf("expected %d results, got %+v", len(wantOrder), d.Results)
	}
	for i, l := range d.Results {
		if l.Label != wantOrder[i] {
			t.Fatalf("result %d = %q, want %q", i, l.Label, wantOrder[i])
		}
	}
	if d.Results[1].Value != "6" || d.Results[1].Sub != "" {
		t.Fatalf("sent card: %+v", d.Results[1])
	}
	if d.Results[2].Value != "6" || d.Results[2].Sub != "100% of sent" {
		t.Fatalf("clicked card: %+v", d.Results[2])
	}
	if d.Outcome[0].Label != "Clicked link" || d.Outcome[0].Rate != 100 {
		t.Fatalf("outcome: %+v", d.Outcome[0])
	}
	facts.sends = append(facts.sends, &programSend{Person: uuid.New(), SentAt: at(5), Training: true})
	r = results(facts, time.UTC, resultsNow, nil, nil)
	d = programReportFromResults(program, &programReportSetup{}, r, resultsNow, nil, nil)
	if !d.HasTraining {
		t.Fatalf("expected the training flag")
	}
	if l := d.Results[len(d.Results)-2]; l.Label != "Training completed" {
		t.Fatalf("expected training cards before repeat offenders, got %+v", d.Results)
	}
	var buf bytes.Buffer
	if err := programReportTemplate.Execute(&buf, d); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, h := range []string{"Program Details", "Results", "Clicked link", "Data submitted", "Repeat offenders", "Results by Template", "Trend by Month", "Training completed"} {
		if !strings.Contains(out, h) {
			t.Fatalf("missing heading %q", h)
		}
	}
	for _, banned := range []string{"imulation", "Withheld", "Configuration", "Funnel", "Resilience", "ifficulty", "Hard to spot",
		"Coverage", "Phish prone", "Repeat clickers", "Reports per click", "Median time", "30 days", "Send number", "mail"} {
		if strings.Contains(out, banned) {
			t.Fatalf("report still says %q", banned)
		}
	}
}

// TestProgramReportTablesBounded checks a long program keeps the month table
// to the last months, the template table to the most sent templates and the
// details to the first names, each with a note of how many there are
func TestProgramReportTablesBounded(t *testing.T) {
	program := &model.Program{
		Name:   nullable.NewNullableWithValue(*vo.NewString64Must("Long run")),
		Status: "active",
		Steps:  make([]*model.ProgramStep, 40),
	}
	r := &ProgramResults{Summary: &ProgramSummary{}}
	for i := 0; i < 30; i++ {
		sent := 100 - i
		key := time.Date(2024, time.Month(1+i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		r.Months = append(r.Months, &ProgramResultRow{Key: key, Name: key, Sent: intPtr(10)})
		r.Templates = append(r.Templates, &ProgramResultRow{Key: key, Name: "template " + key, Sent: &sent})
	}
	d := programReportFromResults(program, &programReportSetup{}, r, resultsNow, nil, nil)
	if len(d.Months) != programReportTableMonths || d.MonthsTotal != 30 {
		t.Fatalf("expected the last %d of 30 months, got %d of %d", programReportTableMonths, len(d.Months), d.MonthsTotal)
	}
	if d.Months[len(d.Months)-1].Cells[0] != r.Months[29].Name || d.Months[0].Cells[0] != r.Months[18].Name {
		t.Fatalf("expected the latest months, got %v to %v", d.Months[0].Cells[0], d.Months[len(d.Months)-1].Cells[0])
	}
	if len(d.Templates) != programReportTableTemplates || d.TemplatesTotal != 30 || d.Templates[0].Cells[0] != r.Templates[0].Name {
		t.Fatalf("expected the %d most sent of 30 templates, got %d of %d", programReportTableTemplates, len(d.Templates), d.TemplatesTotal)
	}
	var buf bytes.Buffer
	if err := programReportTemplate.Execute(&buf, d); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, note := range []string{"The last 12 of 30 months of the period are shown.", "The 15 most sent of 30 templates are shown."} {
		if !strings.Contains(out, note) {
			t.Fatalf("missing note %q", note)
		}
	}
	names := []string{}
	for i := 0; i < 8; i++ {
		names = append(names, "T"+strconv.Itoa(i))
	}
	for _, l := range programReportDetails(program, &programReportSetup{TemplateNames: names}) {
		if l.Label == "Templates" && l.Value != "40: T0, T1, T2, T3, T4 and 3 more" {
			t.Fatalf("Templates = %q", l.Value)
		}
	}
}

// TestProgramReportShareBase checks the share under a card is of the people
// sent to and never passes 100%, also when fewer people are enrolled than
// were sent to, as after a recipient is deleted
func TestProgramReportShareBase(t *testing.T) {
	program := &model.Program{
		Name:   nullable.NewNullableWithValue(*vo.NewString64Must("Base")),
		Status: "active",
	}
	r := &ProgramResults{Summary: &ProgramSummary{
		Recipients: 2, Sent: intPtr(4), ClickedLink: intPtr(4), SubmittedData: intPtr(1), Reported: intPtr(2),
		HasTraining: true, TrainingStarted: intPtr(2), TrainingCompleted: intPtr(3),
	}}
	d := programReportFromResults(program, &programReportSetup{}, r, resultsNow, nil, nil)
	want := map[string]string{
		"Sent": "", "Clicked link": "100% of sent", "Data submitted": "25% of sent", "Reported": "50% of sent",
		"Training started": "", "Training completed": "100% of started", "Repeat offenders": "",
	}
	for _, l := range d.Results {
		if w, ok := want[l.Label]; ok && l.Sub != w {
			t.Fatalf("%s sub = %q, want %q", l.Label, l.Sub, w)
		}
	}
	for _, l := range d.Outcome {
		if l.Rate > 100 {
			t.Fatalf("%s rate %v passes 100", l.Label, l.Rate)
		}
	}
}
