package model

import (
	"testing"
	"time"
)

// TestProgramWallTimeStartOfDayMatchesEndOfDay checks minute 0 of a day is the
// same instant as the end of the day before, also in zones where a clock
// change skips midnight, so a window from midnight starts where a window to
// midnight ends
func TestProgramWallTimeStartOfDayMatchesEndOfDay(t *testing.T) {
	for _, zone := range []string{"America/Santiago", "America/Havana", "Asia/Beirut", "Africa/Cairo", "Europe/Copenhagen", "UTC"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 1; i < 2*366; i++ {
			y, mo, d := day.AddDate(0, 0, i).Date()
			start, ok := ProgramWallTime(y, mo, d, 0, loc)
			if !ok {
				t.Fatalf("%s %d-%d-%d: start of day refused", zone, y, mo, d)
			}
			py, pmo, pd := day.AddDate(0, 0, i-1).Date()
			end, _ := ProgramWallTime(py, pmo, pd, 24*60, loc)
			if !start.Equal(end) {
				t.Fatalf("%s %d-%d-%d: start %v differs from the end of the day before %v", zone, y, mo, d, start.In(loc), end.In(loc))
			}
			if ly, lmo, ld := start.In(loc).Date(); ly != y || lmo != mo || ld != d {
				t.Fatalf("%s %d-%d-%d: start %v is not on the day", zone, y, mo, d, start.In(loc))
			}
		}
	}
}
