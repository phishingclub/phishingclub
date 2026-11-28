package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/repository"
)

// TestCampaignUpdateStoresLanguageRouting checks a campaign update stores the
// language field, default and routes with normalized keys, keeps the routes
// when they are left out, clears them when routing is turned off and refuses
// routes that do not validate
func TestCampaignUpdateStoresLanguageRouting(t *testing.T) {
	f := newLanguagePoolFixture(t)
	// routes are checked against the pool, so the template has the languages
	a := f.languageTemplate("en", "da", "de")
	campaignID, campaign, groupID := f.selfManagedWithRecipients([]uuid.UUID{a})
	routes := func() map[string]string {
		t.Helper()
		got, err := f.svc.CampaignRepository.GetLanguageRoutesByCampaignID(f.ctx, &campaignID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	incoming := selfManagedUpdate(campaign, groupID)
	incoming.LanguageRouteBy = nullable.NewNullableWithValue("country")
	incoming.LanguageDefault = nullable.NewNullableWithValue("en")
	incoming.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{" Denmark ": "da", "GERMANY": "de"})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, incoming); err != nil {
		t.Fatalf("update with routing: %v", err)
	}
	stored, err := f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{WithLanguageRoutes: true})
	if err != nil {
		t.Fatal(err)
	}
	got := stored.LanguageRoutes.MustGet()
	if stored.LanguageRouteField() != "country" || stored.LanguageDefault.MustGet() != "en" ||
		len(got) != 2 || got["denmark"] != "da" || got["germany"] != "de" {
		t.Fatalf("stored routing: %q %q %v", stored.LanguageRouteField(), stored.LanguageDefault.MustGet(), got)
	}

	// an update that leaves the routing out keeps it
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, selfManagedUpdate(campaign, groupID)); err != nil {
		t.Fatalf("update without routing: %v", err)
	}
	if len(routes()) != 2 {
		t.Fatalf("routes after an update without them: %v", routes())
	}

	// routes that do not validate are refused and change nothing
	bad := selfManagedUpdate(campaign, groupID)
	bad.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"denmark": "da", "DENMARK": "de"})
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, bad); !isValidationError(err) {
		t.Fatalf("duplicate keys: want validation error, got %v", err)
	}
	bad = selfManagedUpdate(campaign, groupID)
	bad.LanguageRouteBy = nullable.NewNullableWithValue("email")
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, bad); !isValidationError(err) {
		t.Fatalf("unknown field: want validation error, got %v", err)
	}
	if len(routes()) != 2 {
		t.Fatalf("refused update changed the routes: %v", routes())
	}

	// turning routing off clears the default and the routes
	off := selfManagedUpdate(campaign, groupID)
	off.LanguageRouteBy = nullable.NewNullableWithValue("")
	if err := f.svc.UpdateByID(f.ctx, f.session, &campaignID, off); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	stored, err = f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.LanguageRouteField() != "" || stored.LanguageDefault.MustGet() != "" || len(routes()) != 0 {
		t.Fatalf("routing left after turning it off: %q %q %v", stored.LanguageRouteField(), stored.LanguageDefault.MustGet(), routes())
	}
}
