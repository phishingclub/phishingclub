package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func newTestCampaign(start, end, anonymize time.Time) *Campaign {
	group := uuid.New()
	return &Campaign{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("c")),
		SortField:         nullable.NewNullableWithValue(*vo.NewCampaignSortFieldMust("email")),
		SortOrder:         nullable.NewNullableWithValue(*vo.NewCampaignSendingOrderMust("asc")),
		SendStartAt:       nullable.NewNullableWithValue(start),
		SendEndAt:         nullable.NewNullableWithValue(end),
		AnonymizeAt:       nullable.NewNullableWithValue(anonymize),
		TemplateID:        nullable.NewNullableWithValue(uuid.New()),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&group}),
	}
}

func TestCampaignAnonymizeAfterEnd(t *testing.T) {
	start := time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	before := start.Add(24 * time.Hour)
	after := end.Add(time.Hour)

	// create always enforces the rule
	if err := newTestCampaign(start, end, before).Validate(); err == nil {
		t.Fatal("anonymize before end accepted on create")
	}
	if err := newTestCampaign(start, end, after).Validate(); err != nil {
		t.Fatalf("valid campaign rejected: %v", err)
	}

	// a stored campaign with anonymize before end stays editable when the
	// update leaves the dates as they are
	c := newTestCampaign(start, end, before)
	stored := c.AnonymizeDates()
	c.Name.Set(*vo.NewString64Must("renamed"))
	if err := c.ValidateUpdate(stored); err != nil {
		t.Fatalf("unrelated edit blocked: %v", err)
	}
	// the client resends the same dates with seconds dropped
	c = newTestCampaign(start, end, before.Add(30*time.Second))
	stored = c.AnonymizeDates()
	c.AnonymizeAt.Set(before)
	if err := c.ValidateUpdate(stored); err != nil {
		t.Fatalf("same minute counted as a change: %v", err)
	}

	// changing anonymize at or the end date runs the rule
	c = newTestCampaign(start, end, before)
	stored = c.AnonymizeDates()
	c.AnonymizeAt.Set(before.Add(time.Hour))
	if err := c.ValidateUpdate(stored); err == nil {
		t.Fatal("changed anonymize at before end accepted")
	}
	c = newTestCampaign(start, end, before)
	stored = c.AnonymizeDates()
	c.SendEndAt.Set(end.Add(time.Hour))
	if err := c.ValidateUpdate(stored); err == nil {
		t.Fatal("changed end date after anonymize at accepted")
	}
	// fixing the dates is accepted
	c = newTestCampaign(start, end, before)
	stored = c.AnonymizeDates()
	c.AnonymizeAt.Set(after)
	if err := c.ValidateUpdate(stored); err != nil {
		t.Fatalf("fixed dates rejected: %v", err)
	}
}
