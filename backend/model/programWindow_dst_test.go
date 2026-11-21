package model

import (
	"testing"
	"time"
)

func TestProgramWallTimeEndOfDayAcrossMidnightGap(t *testing.T) {
	for _, zone := range []string{"America/Santiago", "America/Havana", "Asia/Beirut", "Africa/Cairo", "Europe/Copenhagen", "UTC"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 2*366; i++ {
			y, mo, d := day.AddDate(0, 0, i).Date()
			end, ok := ProgramWallTime(y, mo, d, 24*60, loc)
			if !ok {
				t.Fatalf("%s %d-%d-%d: end refused", zone, y, mo, d)
			}
			ny, nmo, nd := time.Date(y, mo, d+1, 0, 0, 0, 0, time.UTC).Date()
			ly, lmo, ld := end.In(loc).Date()
			if ly != ny || lmo != nmo || ld != nd {
				t.Fatalf("%s %d-%d-%d: end %v not on next day", zone, y, mo, d, end.In(loc))
			}
			if before := end.Add(-time.Minute).In(loc); before.Day() == nd && before.Month() == nmo {
				t.Fatalf("%s %d-%d-%d: end %v is not the first instant", zone, y, mo, d, end.In(loc))
			}
		}
	}
}
