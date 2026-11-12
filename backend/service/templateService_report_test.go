package service

import (
	"context"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

const randomRecipientContent = `{{.RandomRecipient.FirstName}} {{.RandomRecipient.LastName}} {{.RandomRecipient.Email}}`

// The template service has no recipient repository here, so any attempt to
// read a real recipient panics and fails the test.

func TestReportPageRenderUsesFakeRandomRecipient(t *testing.T) {
	ts := &Template{}
	page := &model.Page{
		Content: nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(randomRecipientContent)),
	}
	buf, err := ts.CreatePageMockForReport(context.Background(), page, nil, "example.test", "/")
	if err != nil {
		t.Fatalf("render page: %v", err)
	}
	if got := buf.String(); got != "John Smith john.smith@example.test" {
		t.Fatalf("unexpected page output %q", got)
	}
}

func TestReportEmailRenderUsesFakeRandomRecipient(t *testing.T) {
	ts := &Template{}
	email := model.NewEmailExample()
	email.Content = nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(randomRecipientContent))
	out, err := ts.CreateMailBodyForReport(email, "example.test", "/")
	if err != nil {
		t.Fatalf("render email: %v", err)
	}
	if !strings.Contains(out, "John Smith john.smith@example.test") {
		t.Fatalf("unexpected email output %q", out)
	}
}

func TestReportRendersUsePlainRecipientEmail(t *testing.T) {
	ts := &Template{}
	content := `{{.Email}}`
	page := &model.Page{Content: nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(content))}
	buf, err := ts.CreatePageMockForReport(context.Background(), page, nil, "example.test", "/")
	if err != nil || buf.String() != "rick@company.test" {
		t.Fatalf("page: got %q %v", buf.String(), err)
	}
	email := model.NewEmailExample()
	email.Content = nullable.NewNullableWithValue(*vo.NewOptionalString1MBMust(content))
	out, err := ts.CreateMailBodyForReport(email, "example.test", "/")
	if err != nil || out != "rick@company.test" {
		t.Fatalf("email: got %q %v", out, err)
	}
}

func TestReportEmailSubjectRender(t *testing.T) {
	ts := &Template{}
	email := model.NewEmailExample()
	email.MailHeaderSubject = nullable.NewNullableWithValue(*vo.NewOptionalString255Must(`Hi {{.FirstName}}, {{.RandomRecipient.FirstName}} shared a file`))
	out, err := ts.CreateMailSubjectForReport(email, "example.test", "/")
	if err != nil {
		t.Fatalf("render subject: %v", err)
	}
	want := "Hi " + reportExampleRecipient().FirstName.MustGet().String() + ", John shared a file"
	if out != want {
		t.Fatalf("got subject %q, want %q", out, want)
	}
}
