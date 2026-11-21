package service

import "testing"

// TestReadReportedCSVRowPickedDate checks a row with no date in a picked date
// column fails, also where the date is optional, and that a file without a
// date column still reads rows without a date
func TestReadReportedCSVRowPickedDate(t *testing.T) {
	cases := []struct {
		name         string
		fields       []string
		dateColumn   int
		dateRequired bool
		ok           bool
	}{
		{"optional date picked and empty", []string{"bob@example.com", ""}, 1, false, false},
		{"optional date picked and blank", []string{"bob@example.com", "   "}, 1, false, false},
		{"optional date picked and missing", []string{"bob@example.com"}, 1, false, false},
		{"optional date picked and set", []string{"bob@example.com", "2026-02-01"}, 1, false, true},
		{"no date column", []string{"bob@example.com", ""}, -1, false, true},
		{"required date empty", []string{"bob@example.com", ""}, 1, true, false},
		{"required date set", []string{"bob@example.com", "2026-02-01"}, 1, true, true},
	}
	for _, c := range cases {
		result := &ReportedCSVResult{}
		cell, ok := readReportedCSVRow(result, ReportedCSVRow{Line: 2, Fields: c.fields}, 0, c.dateColumn, c.dateRequired)
		if ok != c.ok {
			t.Fatalf("%s: ok = %v, want %v", c.name, ok, c.ok)
		}
		if !ok && (result.Failed != 1 || len(result.Errors) != 1 || result.Errors[0].Line != 2) {
			t.Fatalf("%s: a failed row must be recorded with its line: %+v", c.name, result)
		}
		if ok && cell.email != "bob@example.com" {
			t.Fatalf("%s: email = %q", c.name, cell.email)
		}
	}
}
