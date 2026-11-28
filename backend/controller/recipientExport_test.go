package controller

import (
	"fmt"
	"testing"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestRecipientExportColumnsLineUp checks that the header and the row used by
// the single recipient export and the company export have the same length and
// that each value sits under its own header
func TestRecipientExportColumnsLineUp(t *testing.T) {
	for _, groupCount := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("groups %d", groupCount), func(t *testing.T) {
			groups := []*model.RecipientGroup{}
			for i := 0; i < groupCount; i++ {
				groups = append(groups, &model.RecipientGroup{
					Name: nullable.NewNullableWithValue(*vo.NewString127Must(fmt.Sprintf("group%d", i+1))),
				})
			}
			recp := &model.Recipient{
				Email:     nullable.NewNullableWithValue(*vo.NewEmailMust("a@example.com")),
				FirstName: nullable.NewNullableWithValue(*vo.NewOptionalString127Must("Ann")),
				LastName:  nullable.NewNullableWithValue(*vo.NewOptionalString127Must("Berg")),
				Misc:      nullable.NewNullableWithValue(*vo.NewOptionalString127Must("VIP")),
				Language:  nullable.NewNullableWithValue("da"),
			}
			headers := recipientExportHeaders(groupCount)
			row := recipientExportRow(recp, groups)
			if len(headers) != len(row) {
				t.Fatalf("header has %d columns, row has %d", len(headers), len(row))
			}
			want := map[string]string{
				"Email":      "a@example.com",
				"First name": "Ann",
				"Last name":  "Berg",
				"Misc":       "VIP",
				"Language":   "da",
			}
			for i := 0; i < groupCount; i++ {
				want[fmt.Sprintf("Group %d", i+1)] = fmt.Sprintf("group%d", i+1)
			}
			for i, h := range headers {
				if v, ok := want[h]; ok && row[i] != v {
					t.Errorf("column %q holds %q, want %q", h, row[i], v)
				}
			}
		})
	}
}
