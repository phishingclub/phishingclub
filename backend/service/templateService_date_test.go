package service

import (
	"testing"
	"time"
)

func TestFormatDate(t *testing.T) {
	// 5 Jan 2026 07:08:09 in the morning, so leading zeros and am show up
	morning := time.Date(2026, time.January, 5, 7, 8, 9, 0, time.UTC)
	// 25 Dec 2026 19:30:45 in the evening
	evening := time.Date(2026, time.December, 25, 19, 30, 45, 0, time.UTC)

	cases := []struct {
		name   string
		t      time.Time
		format string
		want   string
	}{
		{"full date", morning, "Y-m-d H:i:s", "2026-01-05 07:08:09"},
		{"short year", morning, "y/n/j", "26/1/5"},
		{"short month name", morning, "M", "Jan"},
		{"full month name", morning, "F", "January"},
		{"month names in a sentence", evening, "d F Y", "25 December 2026"},
		{"short month with am", morning, "M j, g:i a", "Jan 5, 7:08 am"},
		{"24 hour without leading zero", morning, "G", "7"},
		{"24 hour without leading zero evening", evening, "G:i", "19:30"},
		{"12 hour", evening, "h:i A", "07:30 PM"},
		{"12 hour no zero", evening, "g a", "7 pm"},
		{"punctuation kept", morning, "(Y) [m]", "(2026) [01]"},
		{"go layout digits are literal", morning, "1 2 3", "1 2 3"},
		{"empty", morning, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatDate(c.t, c.format)
			if got != c.want {
				t.Fatalf("formatDate(%q) = %q, want %q", c.format, got, c.want)
			}
		})
	}
}
