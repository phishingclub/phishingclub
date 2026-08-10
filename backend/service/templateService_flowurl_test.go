package service

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/utils"
)

// decryptState pulls the state parameter out of a built flow url and decrypts it back to
// the page type it encodes, so the test can assert the url points at the right stage.
func decryptState(t *testing.T, rawURL string, stateKey string, secret string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse url %q: %v", rawURL, err)
	}
	state := parsed.Query().Get(stateKey)
	if state == "" {
		t.Fatalf("url %q has no %q state param", rawURL, stateKey)
	}
	page, err := utils.Decrypt(state, secret)
	if err != nil {
		t.Fatalf("failed to decrypt state %q: %v", state, err)
	}
	return page
}

func TestBuildFlowPageURLs(t *testing.T) {
	campaignID := uuid.New()
	secret := utils.UUIDToSecret(&campaignID)
	crid := uuid.New().String()

	base := FlowPageURLParams{
		BaseURL:             "https://example.test",
		URLPath:             "/login",
		URLIdentifier:       "id",
		StateIdentifier:     "state",
		CampaignRecipientID: crid,
		CampaignID:          campaignID,
	}

	t.Run("all stages present", func(t *testing.T) {
		p := base
		p.HasBeforePage = true
		p.HasAfterPage = true
		before, landing, after := BuildFlowPageURLs(p)

		for name, u := range map[string]string{"before": before, "landing": landing, "after": after} {
			if u == "" {
				t.Fatalf("%s url is empty, expected a value", name)
			}
			parsed, err := url.Parse(u)
			if err != nil {
				t.Fatalf("%s url %q did not parse: %v", name, u, err)
			}
			if got := parsed.Query().Get("id"); got != crid {
				t.Errorf("%s url id param = %q, want %q", name, got, crid)
			}
			if parsed.Path != "/login" {
				t.Errorf("%s url path = %q, want /login", name, parsed.Path)
			}
		}

		if got := decryptState(t, before, "state", secret); got != data.PAGE_TYPE_BEFORE {
			t.Errorf("before state = %q, want %q", got, data.PAGE_TYPE_BEFORE)
		}
		if got := decryptState(t, landing, "state", secret); got != data.PAGE_TYPE_LANDING {
			t.Errorf("landing state = %q, want %q", got, data.PAGE_TYPE_LANDING)
		}
		if got := decryptState(t, after, "state", secret); got != data.PAGE_TYPE_AFTER {
			t.Errorf("after state = %q, want %q", got, data.PAGE_TYPE_AFTER)
		}
	})

	t.Run("no before or after stage renders empty", func(t *testing.T) {
		before, landing, after := BuildFlowPageURLs(base)
		if before != "" {
			t.Errorf("before url = %q, want empty when stage absent", before)
		}
		if after != "" {
			t.Errorf("after url = %q, want empty when stage absent", after)
		}
		if landing == "" {
			t.Fatal("landing url is empty, want a value since landing is always present")
		}
		if got := decryptState(t, landing, "state", secret); got != data.PAGE_TYPE_LANDING {
			t.Errorf("landing state = %q, want %q", got, data.PAGE_TYPE_LANDING)
		}
	})
}
