package service

import (
	"bytes"
	"strings"
	"testing"
	"time"

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

func TestRatioText(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		in   *float64
		want string
	}{
		{nil, "-"}, {f(0.5), "0.5"}, {f(1), "1"}, {f(0.25), "0.25"}, {f(2.333), "2.33"},
	}
	for _, c := range cases {
		if got := ratioText(c.in); got != c.want {
			t.Fatalf("ratioText = %q, want %q", got, c.want)
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
		"Status":                     "Active",
		"Anonymous":                  "Yes",
		"Templates":                  "2: Invoice, Parcel",
		"Template order":             "Same order for everyone",
		"After the last template":    "Start over",
		"Time between emails":        "1 month",
		"Spread":                     "2 days",
		"Sending days":               "Any day",
		"Sending hours":              "Any time",
		"Skip dates":                 "24 Dec 2026 to 26 Dec 2026, 1 May 2026",
		"First email to new members": "After 1 week",
		"Start":                      "5 Jan 2026 09:00",
		"End":                        "Runs until closed",
		"Groups":                     "Sales",
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

// TestProgramReportSections checks the headings, the key results order and
// that training rows show only for a program with training
func TestProgramReportSections(t *testing.T) {
	program := &model.Program{
		Name:   nullable.NewNullableWithValue(*vo.NewString64Must("Year one")),
		Status: "active",
	}
	facts := &programFacts{templateNames: map[string]string{"t1": "Invoice"}}
	for _, p := range people(6) {
		facts.sends = append(facts.sends, send(p, 10, "clicked"))
	}
	results := buildProgramResults(facts, nil, time.UTC, riskNow, nil, nil)
	d := programReportFromResults(program, &programReportSetup{}, results, results, facts, riskNow, nil, nil)
	for _, l := range d.Funnel {
		if strings.HasPrefix(l.Label, "Training") || l.Label == "Opened" {
			t.Fatalf("unexpected funnel row %q", l.Label)
		}
	}
	wantOrder := []string{
		"Report rate (30 days)",
		"Reported, no click",
		"Phish prone (12 months)",
		"Median time to report",
		"Reports per click",
		"Repeat clickers (2 or more of last 6 emails)",
	}
	if len(d.Headline) != len(wantOrder) {
		t.Fatalf("expected %d key results, got %+v", len(wantOrder), d.Headline)
	}
	for i, l := range d.Headline {
		if l.Label != wantOrder[i] {
			t.Fatalf("key result %d = %q, want %q", i, l.Label, wantOrder[i])
		}
	}
	d = programReportFromResults(program, &programReportSetup{HasTraining: true}, results, results, facts, riskNow, nil, nil)
	if last := d.Funnel[len(d.Funnel)-1]; last.Label != "Training completed" {
		t.Fatalf("expected training rows for a program with training, got %+v", d.Funnel)
	}
	var buf bytes.Buffer
	if err := programReportTemplate.Execute(&buf, d); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, h := range []string{"Program Details", "Key Results", "Results in This Period", "Reporting", "Results by Template", "Trend by Month"} {
		if !strings.Contains(out, h) {
			t.Fatalf("missing heading %q", h)
		}
	}
	for _, banned := range []string{"imulation", "Withheld", "Configuration", "Funnel", "Resilience"} {
		if strings.Contains(out, banned) {
			t.Fatalf("report still says %q", banned)
		}
	}
}
