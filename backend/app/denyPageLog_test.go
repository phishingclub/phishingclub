package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestDenyPageLogFieldsAnonymous checks the deny page debug log names the
// recipient only on a campaign known not to be anonymous
func TestDenyPageLogFieldsAnonymous(t *testing.T) {
	recipient := &model.Recipient{Email: nullable.NewNullableWithValue(*vo.NewEmailMust("alice@example.com"))}
	cases := map[string]struct {
		anonymous nullable.Nullable[bool]
		named     bool
	}{
		"normal":    {nullable.NewNullableWithValue(false), true},
		"anonymous": {nullable.NewNullableWithValue(true), false},
		"unknown":   {nullable.Nullable[bool]{}, false},
	}
	for name, c := range cases {
		campaign := &model.Campaign{IsAnonymous: c.anonymous}
		text := fmt.Sprint(denyPageLogFields(campaign, recipient, "page", "id"))
		if strings.Contains(text, "alice@example.com") != c.named || strings.Contains(text, "recipientEmail") != c.named {
			t.Fatalf("%s: fields %s", name, text)
		}
	}
	if text := fmt.Sprint(denyPageLogFields(nil, recipient, "page", "id")); strings.Contains(text, "alice") {
		t.Fatalf("no campaign: fields %s", text)
	}
}
