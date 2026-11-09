package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
)

func TestNormalizeTemplatePool(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	// only templateID gives a pool of one
	c := &Campaign{TemplateID: nullable.NewNullableWithValue(a)}
	c.NormalizeTemplatePool()
	if pool := c.TemplatePool(); len(pool) != 1 || pool[0] != a {
		t.Fatalf("pool from templateID: got %v", pool)
	}

	// only templateIDs sets templateID to its first entry
	c = &Campaign{TemplateIDs: nullable.NewNullableWithValue([]*uuid.UUID{&b, &a})}
	c.NormalizeTemplatePool()
	if v := c.TemplateID.MustGet(); v != b {
		t.Fatalf("templateID: got %s, want %s", v, b)
	}
	if err := c.ValidateTemplatePool(); err != nil {
		t.Fatalf("valid pool refused: %v", err)
	}

	// a templateID that is not the first template is refused, not rewritten
	c = &Campaign{
		TemplateID:  nullable.NewNullableWithValue(a),
		TemplateIDs: nullable.NewNullableWithValue([]*uuid.UUID{&b, &a}),
	}
	c.NormalizeTemplatePool()
	if v := c.TemplateID.MustGet(); v != a {
		t.Fatalf("templateID rewritten: got %s, want %s", v, a)
	}
	if err := c.ValidateTemplatePool(); err == nil {
		t.Fatal("mismatched templateID accepted")
	}
}

func TestValidateTemplatePool(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	pool := func(ids ...uuid.UUID) *Campaign {
		refs := make([]*uuid.UUID, len(ids))
		for i := range ids {
			refs[i] = &ids[i]
		}
		c := &Campaign{TemplateIDs: nullable.NewNullableWithValue(refs)}
		c.NormalizeTemplatePool()
		return c
	}
	if err := pool(a, b, a).ValidateTemplatePool(); err == nil {
		t.Fatal("duplicate template accepted")
	}
	empty := &Campaign{TemplateIDs: nullable.NewNullableWithValue([]*uuid.UUID{})}
	if err := empty.ValidateTemplatePool(); err == nil {
		t.Fatal("empty pool accepted")
	}
	big := make([]uuid.UUID, MaxCampaignTemplates+1)
	for i := range big {
		big[i] = uuid.New()
	}
	if err := pool(big...).ValidateTemplatePool(); err == nil {
		t.Fatal("pool over the limit accepted")
	}
	if err := pool(big[:MaxCampaignTemplates]...).ValidateTemplatePool(); err != nil {
		t.Fatalf("pool at the limit refused: %v", err)
	}
	// templateID must be the first entry
	c := pool(a, b)
	c.TemplateID.Set(b)
	if err := c.ValidateTemplatePool(); err == nil {
		t.Fatal("templateID not first accepted")
	}
}

func TestEffectiveTemplateIDFallsBackToCampaign(t *testing.T) {
	campaignTemplate, own := uuid.New(), uuid.New()
	campaign := &Campaign{TemplateID: nullable.NewNullableWithValue(campaignTemplate)}

	legacy := &CampaignRecipient{}
	legacy.TemplateID.SetNull()
	if got, err := legacy.EffectiveTemplateID(campaign); err != nil || got != campaignTemplate {
		t.Fatalf("null template: got %s %v, want campaign template", got, err)
	}
	stamped := &CampaignRecipient{TemplateID: nullable.NewNullableWithValue(own)}
	if got, err := stamped.EffectiveTemplateID(campaign); err != nil || got != own {
		t.Fatalf("stamped template: got %s %v, want own", got, err)
	}
	if _, err := legacy.EffectiveTemplateID(&Campaign{}); err == nil {
		t.Fatal("no template anywhere should error")
	}
}

// TestCampaignCloseNow checks CloseNow brings a later close time forward, keeps
// an earlier one and refuses a closed campaign
func TestCampaignCloseNow(t *testing.T) {
	later := &Campaign{}
	later.CloseAt.Set(time.Now().Add(72 * time.Hour))
	if err := later.CloseNow(); err != nil || later.CloseAt.MustGet().After(time.Now()) {
		t.Fatalf("a later close time was not brought forward: %v", err)
	}
	earlier := &Campaign{}
	past := time.Now().Add(-time.Hour).UTC()
	earlier.CloseAt.Set(past)
	if err := earlier.CloseNow(); err != nil || !earlier.CloseAt.MustGet().Equal(past) {
		t.Fatalf("an earlier close time was changed: %v", err)
	}
	closed := &Campaign{}
	closed.ClosedAt.Set(time.Now())
	if err := closed.CloseNow(); err == nil {
		t.Fatal("a closed campaign was not refused")
	}
}
