package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func newTestTemplate(redirect string) *CampaignTemplate {
	urlID := uuid.New()
	stateID := uuid.New()
	return &CampaignTemplate{
		Name:                        nullable.NewNullableWithValue(*vo.NewString64Must("t")),
		URLIdentifierID:             nullable.NewNullableWithValue(&urlID),
		StateIdentifierID:           nullable.NewNullableWithValue(&stateID),
		LandingPageID:               nullable.NewNullableWithValue(uuid.New()),
		AfterLandingPageRedirectURL: nullable.NewNullableWithValue(*vo.NewOptionalString255Must(redirect)),
	}
}

func TestCampaignTemplateRedirectURL(t *testing.T) {
	good := []string{
		"",
		"/thanks",
		"/a/b?x=1#y",
		"https://intranet/done",
		"http://localhost:8080/x",
		"https://example.com/done",
		"http://10.0.0.1/",
	}
	for _, u := range good {
		if err := newTestTemplate(u).Validate(); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	bad := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html,x",
		"ftp://example.com/x",
		"//evil.test",
		"/\\evil.test",
		"thanks",
		"https://",
	}
	for _, u := range bad {
		if err := newTestTemplate(u).Validate(); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

// a stored value from before the check does not block an unrelated edit, but
// a changed value is still checked
func TestCampaignTemplateValidateUpdateKeepsStoredRedirect(t *testing.T) {
	legacy := "intranet-thanks"
	if err := newTestTemplate(legacy).ValidateUpdate(legacy); err != nil {
		t.Fatalf("unchanged stored redirect blocked update: %v", err)
	}
	if err := newTestTemplate("javascript:alert(1)").ValidateUpdate(legacy); err == nil {
		t.Fatal("changed javascript redirect accepted on update")
	}
	if err := newTestTemplate("/thanks").ValidateUpdate(legacy); err != nil {
		t.Fatalf("changed relative redirect rejected on update: %v", err)
	}
	// other rules still run on update
	tpl := newTestTemplate(legacy)
	tpl.Name = nullable.Nullable[vo.String64]{}
	if err := tpl.ValidateUpdate(legacy); err == nil {
		t.Fatal("missing name accepted on update")
	}
}
