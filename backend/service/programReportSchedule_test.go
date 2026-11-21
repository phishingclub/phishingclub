package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
)

// a program without a company is sent with the global config, which is never
// enabled. Its missing fields must fail validation instead of reaching the
// MustGet calls that follow it.
func TestProgramReportDeliveryConfigRequiresFields(t *testing.T) {
	global := &model.CompanyReportConfig{Enabled: false}
	if err := programReportDeliveryConfig(global, global).Validate(); err == nil {
		t.Fatalf("an empty global config must not validate")
	}
	groupID := uuid.New()
	company := &model.CompanyReportConfig{
		Enabled:          true,
		RecipientGroupID: nullable.NewNullableWithValue(groupID),
	}
	if err := programReportDeliveryConfig(company, global).Validate(); err == nil {
		t.Fatalf("a config without smtp and sender must not validate")
	}
}

// TestProgramReportDuePeriodAnonymousSettles checks an anonymous program sends
// the report of a month once the month has settled and is counted, a named
// program right after the month ends
func TestProgramReportDuePeriodAnonymousSettles(t *testing.T) {
	started := time.Date(2026, time.March, 3, 9, 0, 0, 0, time.UTC)
	program := func(anonymous bool) *model.Program {
		return &model.Program{
			IsAnonymous:    nullable.NewNullableWithValue(anonymous),
			ReportEnabled:  nullable.NewNullableWithValue(true),
			ReportInterval: nullable.NewNullableWithValue(model.ProgramReportMonthly),
			ReportPeriod:   nullable.NewNullableWithValue(model.ProgramReportPeriodLast),
			TimeZone:       nullable.NewNullableWithValue("UTC"),
			StartedAt:      &started,
			ClosedAt:       nullable.NewNullNullable[time.Time](),
		}
	}
	october := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	november := october.AddDate(0, 1, 0)
	early := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	if from, to, ok := programReportDuePeriod(program(false), early); !ok || !from.Equal(october) || !to.Equal(november) {
		t.Fatalf("named October report: %v %v %v", from, to, ok)
	}
	anon := program(true)
	if _, to, ok := programReportDuePeriod(anon, early); ok && to.After(october) {
		t.Fatalf("anonymous October report must wait for October to settle, got to %v", to)
	}
	settled := time.Date(2026, time.November, 4, 0, 30, 0, 0, time.UTC)
	if from, to, ok := programReportDuePeriod(anon, settled); !ok || !from.Equal(october) || !to.Equal(november) {
		t.Fatalf("anonymous October report once settled: %v %v %v", from, to, ok)
	}
	if !programAnonymousCountedTo(settled, time.UTC).Equal(november) {
		t.Fatalf("October is counted once settled")
	}
}

// TestProgramReportEmailUsesConfig checks the automated program report email
// takes its subject and body from the report delivery config, as a campaign
// report does, with the program defaults when a field is empty or does not
// render for a program, and the body escaped
func TestProgramReportEmailUsesConfig(t *testing.T) {
	s := &Program{Common: Common{Logger: zap.NewNop().Sugar()}}
	d := programReportEmailData{
		ProgramName: "Q <1>", CampaignName: "Q <1>", CompanyName: "Acme & Co",
		Period: "1 Oct 2026 to 31 Oct 2026", ReportDate: "2026-11-01",
	}
	config := &model.CompanyReportConfig{
		EmailSubject: nullable.NewNullableWithValue("Results {{.CompanyName}}: {{.CampaignName}}"),
		EmailBody:    nullable.NewNullableWithValue("<p>{{.ProgramName}} for {{.CompanyName}}, {{.Period}}</p>"),
	}
	subject, body := s.programReportEmail(config, d)
	if subject != "Results Acme & Co: Q <1>" {
		t.Fatalf("subject = %q", subject)
	}
	if body != "<p>Q &lt;1&gt; for Acme &amp; Co, 1 Oct 2026 to 31 Oct 2026</p>" {
		t.Fatalf("body = %q", body)
	}
	// empty fields use the program defaults
	subject, body = s.programReportEmail(&model.CompanyReportConfig{EmailSubject: nullable.NewNullableWithValue("  ")}, d)
	if subject != "Program report: Q <1>, 1 Oct 2026 to 31 Oct 2026" {
		t.Fatalf("default subject = %q", subject)
	}
	if body != "<p>The program report for Q &lt;1&gt; covering 1 Oct 2026 to 31 Oct 2026 is attached.</p>" {
		t.Fatalf("default body = %q", body)
	}
	// a campaign only variable does not render for a program, the default is used
	subject, body = s.programReportEmail(&model.CompanyReportConfig{
		EmailSubject: nullable.NewNullableWithValue("Click rate {{.ClickRate}}"),
		EmailBody:    nullable.NewNullableWithValue("{{if .IsTraining}}x{{end}}"),
	}, d)
	if subject != "Program report: Q <1>, 1 Oct 2026 to 31 Oct 2026" || body == "" || body == "x" {
		t.Fatalf("a variable a program has not must fall back: %q %q", subject, body)
	}
}
