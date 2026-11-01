package vo

import "testing"

// Count must include Saturday (bit 6). The old loop stopped at bit 5, so a
// Saturday-only selection counted as zero and was wrongly rejected as "no day
// selected" on campaign create and resume.
func TestCampaignWeekDaysCount(t *testing.T) {
	cases := []struct {
		bits int
		want int
	}{
		{1 << 0, 1}, // Sunday
		{1 << 6, 1}, // Saturday (the regression)
		{62, 5},     // Mon..Fri
		{127, 7},    // all seven days
	}
	for _, c := range cases {
		got := NewCampaignWeekDaysMust(c.bits).Count()
		if got != c.want {
			t.Fatalf("Count(bits=%d) = %d, want %d", c.bits, got, c.want)
		}
		if n := len(NewCampaignWeekDaysMust(c.bits).AsSlice()); n != c.want {
			t.Fatalf("AsSlice len(bits=%d) = %d, want %d (Count and AsSlice must agree)", c.bits, n, c.want)
		}
	}
}
