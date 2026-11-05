package model

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/vo"
)

// legacyDailySlots is the day walk the scheduler used before time zones were
// stored. Campaigns without a zone must keep getting exactly these windows.
func legacyDailySlots(startAt, endAt time.Time, weekDays []int, dayStart, dayEnd vo.CampaignTimeConstraint) []DailySlot {
	slots := []DailySlot{}
	for d := startAt; d.Before(endAt) || d.Equal(endAt); d = d.AddDate(0, 0, 1) {
		if slices.Contains(weekDays, int(d.Weekday())) {
			slots = append(slots, DailySlot{
				Start: d.Truncate(24 * time.Hour).Add(dayStart.Minutes()),
				End:   d.Truncate(24 * time.Hour).Add(dayEnd.Minutes()),
			})
		}
	}
	return slots
}

func hm(m int) vo.CampaignTimeConstraint {
	return *vo.NewCampaignTimeConstraintMust(fmt.Sprintf("%02d:%02d", m/60, m%60))
}

func TestDailySlots_NoTimeZoneMatchesLegacy(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5000; i++ {
		start := base.Add(time.Duration(r.Intn(400*24*60)) * time.Minute)
		end := start.Add(time.Duration(r.Intn(40*24*60)) * time.Minute)
		weekDays := vo.NewCampaignWeekDaysMust(1 + r.Intn(127)).AsSlice()
		a := r.Intn(24*60 - 1)
		b := a + 1 + r.Intn(24*60-1-a)
		got, err := DailySlots(start, end, weekDays, hm(a), hm(b), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := legacyDailySlots(start, end, weekDays, hm(a), hm(b))
		if len(got) != len(want) {
			t.Fatalf("case %d: got %d slots, want %d", i, len(got), len(want))
		}
		for j := range want {
			if !got[j].Start.Equal(want[j].Start) || !got[j].End.Equal(want[j].End) {
				t.Fatalf("case %d slot %d: got %v-%v want %v-%v", i, j, got[j].Start, got[j].End, want[j].Start, want[j].End)
			}
		}
	}
}

// every slot must be on a selected local week day at exactly the local hours,
// and every selected local date in the range must have a slot
func TestDailySlots_TimeZoneKeepsLocalHoursAcrossClockChanges(t *testing.T) {
	zones := []string{
		"UTC", "Europe/Copenhagen", "Europe/London", "America/New_York", "America/Los_Angeles",
		"America/Sao_Paulo", "America/St_Johns", "Asia/Tokyo", "Asia/Kolkata", "Asia/Kathmandu",
		"Australia/Sydney", "Australia/Adelaide", "Australia/Lord_Howe", "Pacific/Auckland",
		"Pacific/Chatham", "Pacific/Kiritimati", "Pacific/Pago_Pago", "America/Santiago",
	}
	ranges := [][2]string{
		{"2026-11-02", "2026-11-20"}, {"2027-03-08", "2027-04-09"}, {"2027-09-20", "2027-11-12"},
		{"2026-12-28", "2027-01-08"}, {"2027-03-28", "2027-03-28"}, {"2027-04-01", "2027-04-10"},
	}
	hours := [][2]int{{9 * 60, 17 * 60}, {8 * 60, 12 * 60}, {0, 30}, {22 * 60, 23*60 + 59}, {12 * 60, 13 * 60}}
	weekDaySets := []int{62, 12, 65, 127, 18}
	for _, zone := range zones {
		tz := vo.NewCampaignTimeZoneMust(zone)
		loc := tz.Location()
		for _, rg := range ranges {
			for _, h := range hours {
				for _, wdBits := range weekDaySets {
					wd := vo.NewCampaignWeekDaysMust(wdBits).AsSlice()
					d0, _ := time.Parse("2006-01-02", rg[0])
					d1, _ := time.Parse("2006-01-02", rg[1])
					// the frontend sends the first and last local day as instants
					startAt := time.Date(d0.Year(), d0.Month(), d0.Day(), 12, 0, 0, 0, loc)
					endAt := time.Date(d1.Year(), d1.Month(), d1.Day(), 12, 0, 0, 0, loc)
					slots, err := DailySlots(startAt, endAt, wd, hm(h[0]), hm(h[1]), tz)
					if err != nil {
						// only Santiago changes its clock at midnight, so only hours
						// next to midnight can fall in a clock change in these cases
						if zone != "America/Santiago" || (h[0] != 0 && h[1] != 23*60+59) {
							t.Fatalf("%s %v %v %d: unexpected error %v", zone, rg, h, wdBits, err)
						}
						continue
					}
					wantDays := 0
					for d := d0; !d.After(d1); d = d.AddDate(0, 0, 1) {
						if slices.Contains(wd, int(d.Weekday())) {
							wantDays++
						}
					}
					if len(slots) != wantDays {
						t.Fatalf("%s %v %v %d: got %d slots want %d", zone, rg, h, wdBits, len(slots), wantDays)
					}
					for _, s := range slots {
						ls, le := s.Start.In(loc), s.End.In(loc)
						if ls.Hour()*60+ls.Minute() != h[0] || le.Hour()*60+le.Minute() != h[1] {
							t.Fatalf("%s: slot %v-%v is not at local %v", zone, ls, le, h)
						}
						if ls.YearDay() != le.YearDay() || !slices.Contains(wd, int(ls.Weekday())) {
							t.Fatalf("%s: slot %v-%v is not on a selected local day", zone, ls, le)
						}
						if !s.End.After(s.Start) {
							t.Fatalf("%s: empty slot %v", zone, s)
						}
					}
				}
			}
		}
	}
}

func TestDailySlots_RejectsHoursInAClockChange(t *testing.T) {
	cases := []struct {
		zone, date, start, end string
	}{
		// the clock jumps from 02:00 to 03:00, 02:30 does not exist
		{"Europe/Copenhagen", "2027-03-28", "02:30", "04:00"},
		// the clock goes back from 03:00 to 02:00, 02:30 happens twice
		{"Europe/Copenhagen", "2027-10-31", "01:00", "02:30"},
		{"America/Los_Angeles", "2027-03-14", "02:30", "03:30"},
		{"America/Los_Angeles", "2027-11-07", "01:30", "05:00"},
		// the clock jumps at midnight, 00:30 does not exist
		{"America/Santiago", "2027-09-05", "00:30", "08:00"},
	}
	for _, c := range cases {
		tz := vo.NewCampaignTimeZoneMust(c.zone)
		d, _ := time.Parse("2006-01-02", c.date)
		at := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, tz.Location())
		_, err := DailySlots(at, at, []int{int(d.Weekday())}, *vo.NewCampaignTimeConstraintMust(c.start), *vo.NewCampaignTimeConstraintMust(c.end), tz)
		if err == nil {
			t.Fatalf("%s %s %s-%s: expected an error", c.zone, c.date, c.start, c.end)
		}
	}
	// the same hours on a day without a clock change are fine
	tz := vo.NewCampaignTimeZoneMust("Europe/Copenhagen")
	at := time.Date(2027, 3, 29, 12, 0, 0, 0, tz.Location())
	slots, err := DailySlots(at, at, []int{1}, *vo.NewCampaignTimeConstraintMust("02:30"), *vo.NewCampaignTimeConstraintMust("04:00"), tz)
	if err != nil || len(slots) != 1 {
		t.Fatalf("expected one slot, got %v %v", slots, err)
	}
}

func TestValidateDailySlotWeekDays_TimeZone(t *testing.T) {
	tz := vo.NewCampaignTimeZoneMust("America/Los_Angeles")
	loc := tz.Location()
	// Mon 9 Nov to Fri 13 Nov 2026, 09:00-17:00 local, crosses UTC midnight
	startAt := time.Date(2026, 11, 9, 9, 0, 0, 0, loc)
	endAt := time.Date(2026, 11, 13, 17, 0, 0, 0, loc)
	wd := *vo.NewCampaignWeekDaysMust(62)
	if err := ValidateDailySlotWeekDays(startAt, endAt, wd, hm(9*60), hm(17*60), tz); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	// Saturday is not in the range
	if err := ValidateDailySlotWeekDays(startAt, endAt, *vo.NewCampaignWeekDaysMust(62 | 64), hm(9*60), hm(17*60), tz); err == nil {
		t.Fatalf("expected an error for a week day outside the range")
	}
}

func TestValidateDailySlotRange(t *testing.T) {
	start := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	if err := ValidateDailySlotRange(start, start.AddDate(3, 0, 0)); err != nil {
		t.Fatalf("3 years should pass: %v", err)
	}
	if err := ValidateDailySlotRange(start, start.AddDate(3, 0, 5)); err == nil {
		t.Fatal("more than 3 years should fail")
	}
	if err := ValidateDailySlotRange(start, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("year 9999 should fail")
	}
	if err := ValidateDailySlotRange(start, start.Add(-time.Hour)); err == nil {
		t.Fatal("end before start should fail")
	}
}
