package service

import (
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
	buf, err := ts.CreatePageMockForReport(page, nil, "example.test", "/")
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
