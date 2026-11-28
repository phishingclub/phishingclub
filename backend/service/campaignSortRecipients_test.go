package service

import (
	"strings"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// sortTestRecipient makes a recipient where every field holds the given value,
// first and last name hold the given names
func sortTestRecipient(value, first, last string) *model.Recipient {
	opt := func(s string) nullable.Nullable[vo.OptionalString127] {
		return nullable.NewNullableWithValue(*vo.NewOptionalString127Must(s))
	}
	return &model.Recipient{
		Email:           nullable.NewNullableWithValue(*vo.NewEmailMust(value + "@example.com")),
		Phone:           opt(value),
		ExtraIdentifier: opt(value),
		FirstName:       opt(first),
		LastName:        opt(last),
		Position:        opt(value),
		Department:      opt(value),
		City:            opt(value),
		Country:         opt(value),
		Misc:            opt(value),
		Language:        nullable.NewNullableWithValue(value),
	}
}

// sortOutcome reads the order of the recipients by their misc value
func sortOutcome(recipients []*model.Recipient) string {
	out := []string{}
	for _, r := range recipients {
		out = append(out, r.Misc.MustGet().String())
	}
	return strings.Join(out, ",")
}

// TestSortRecipientsEveryField sorts by every field the sort field value
// allows, ascending and descending
func TestSortRecipientsEveryField(t *testing.T) {
	for _, field := range vo.CampaignSortFields {
		if _, err := vo.NewCampaignSortField(field); err != nil {
			t.Fatalf("field %q not allowed: %v", field, err)
		}
		for _, order := range []string{"asc", "desc"} {
			t.Run(field+" "+order, func(t *testing.T) {
				var recipients []*model.Recipient
				if field == "name" {
					// first name decides, last name breaks a tie
					recipients = []*model.Recipient{
						sortTestRecipient("c", "Bo", "Zed"),
						sortTestRecipient("a", "anna", "Berg"),
						sortTestRecipient("b", "Bo", "Alm"),
					}
				} else {
					recipients = []*model.Recipient{
						sortTestRecipient("c", "x", "x"),
						sortTestRecipient("a", "x", "x"),
						sortTestRecipient("b", "x", "x"),
					}
				}
				got, known := sortRecipients(recipients, order, field)
				if !known {
					t.Fatalf("field %q reported as not known", field)
				}
				want := "a,b,c"
				if order == "desc" {
					want = "c,b,a"
				}
				if sortOutcome(got) != want {
					t.Errorf("order %s, want %s", sortOutcome(got), want)
				}
			})
		}
	}
}

// TestSortRecipientsUnknownField leaves the order as is and does not panic
func TestSortRecipientsUnknownField(t *testing.T) {
	for _, order := range []string{"asc", "desc"} {
		recipients := []*model.Recipient{
			sortTestRecipient("c", "x", "x"),
			sortTestRecipient("a", "x", "x"),
			sortTestRecipient("b", "x", "x"),
		}
		got, known := sortRecipients(recipients, order, "first_name")
		if known {
			t.Errorf("%s: unknown field reported as known", order)
		}
		if sortOutcome(got) != "c,a,b" {
			t.Errorf("%s: order changed to %s", order, sortOutcome(got))
		}
	}
}

// TestSortRecipientsMissingValue does not fail on recipients without a value
func TestSortRecipientsMissingValue(t *testing.T) {
	recipients := []*model.Recipient{
		sortTestRecipient("b", "x", "x"),
		{Misc: nullable.NewNullableWithValue(*vo.NewOptionalString127Must("none"))},
		sortTestRecipient("a", "x", "x"),
	}
	for _, field := range vo.CampaignSortFields {
		for _, order := range []string{"asc", "desc"} {
			if _, known := sortRecipients(recipients, order, field); !known {
				t.Errorf("%s %s reported as not known", field, order)
			}
		}
	}
}
