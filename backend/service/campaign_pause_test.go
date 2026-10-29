package service

import (
	"testing"
	"time"

	"github.com/phishingclub/phishingclub/vo"
)

// spreadSendTimes must place every recipient inside the window, in order, with
// the first at the start and the last at the end when there is no jitter. Resume
// relies on this to pace the remaining recipients over the new window instead of
// firing them all at once.
func TestSpreadSendTimes(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC)

	t.Run("single recipient sends at the start", func(t *testing.T) {
		got := spreadSendTimes(1, start, end, 0, 0)
		if len(got) != 1 {
			t.Fatalf("want 1 time, got %d", len(got))
		}
		if !got[0].Equal(start) {
			t.Fatalf("want %s, got %s", start, got[0])
		}
	})

	t.Run("even spread stays in order and inside the window", func(t *testing.T) {
		const n = 5
		got := spreadSendTimes(n, start, end, 0, 0)
		if len(got) != n {
			t.Fatalf("want %d times, got %d", n, len(got))
		}
		if !got[0].Equal(start) {
			t.Fatalf("first: want %s, got %s", start, got[0])
		}
		if !got[n-1].Equal(end) {
			t.Fatalf("last: want %s, got %s", end, got[n-1])
		}
		for i := 1; i < n; i++ {
			if got[i].Before(got[i-1]) {
				t.Fatalf("time %d (%s) is before previous (%s)", i, got[i], got[i-1])
			}
			if got[i].Before(start) || got[i].After(end) {
				t.Fatalf("time %d (%s) outside window", i, got[i])
			}
		}
	})

	t.Run("jitter never escapes the window", func(t *testing.T) {
		got := spreadSendTimes(20, start, end, -30, 30)
		for i, ts := range got {
			if ts.Before(start) || ts.After(end) {
				t.Fatalf("jittered time %d (%s) outside window [%s, %s]", i, ts, start, end)
			}
		}
	})
}

// constrainedSendTimes is the Daily Slots schedule used by resume: every send
// must land on a selected week day and inside the daily time window, in order.
func TestConstrainedSendTimes(t *testing.T) {
	// a Monday
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 9, 23, 59, 0, 0, time.UTC) // the Friday
	weekDays := vo.NewCampaignWeekDaysMust(62).AsSlice()  // Mon..Fri
	dayStart := vo.NewCampaignTimeConstraintMust("09:00")
	dayEnd := vo.NewCampaignTimeConstraintMust("17:00")

	got := constrainedSendTimes(10, start, end, weekDays, *dayStart, *dayEnd, 0, 0)
	if len(got) != 10 {
		t.Fatalf("want 10 times, got %d", len(got))
	}
	for i, ts := range got {
		wd := int(ts.Weekday())
		if wd < 1 || wd > 5 {
			t.Fatalf("time %d (%s) is not on a weekday Mon-Fri", i, ts)
		}
		mins := ts.Hour()*60 + ts.Minute()
		if mins < 9*60 || mins > 17*60 {
			t.Fatalf("time %d (%s) is outside the 09:00-17:00 daily window", i, ts)
		}
		if i > 0 && ts.Before(got[i-1]) {
			t.Fatalf("time %d (%s) is before the previous (%s)", i, ts, got[i-1])
		}
	}
}

// a degenerate window (no matching week day in range) must not divide by zero or
// loop forever; it falls back to an even spread inside the window.
func TestConstrainedSendTimes_DegenerateFallsBack(t *testing.T) {
	// a Saturday to a Sunday, but only Monday selected
	start := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 4, 23, 59, 0, 0, time.UTC)
	weekDays := vo.NewCampaignWeekDaysMust(2).AsSlice() // Monday only
	dayStart := vo.NewCampaignTimeConstraintMust("09:00")
	dayEnd := vo.NewCampaignTimeConstraintMust("17:00")

	got := constrainedSendTimes(5, start, end, weekDays, *dayStart, *dayEnd, 0, 0)
	if len(got) != 5 {
		t.Fatalf("want 5 times from the fallback, got %d", len(got))
	}
	for i, ts := range got {
		if ts.Before(start) || ts.After(end) {
			t.Fatalf("fallback time %d (%s) outside [%s, %s]", i, ts, start, end)
		}
	}
}

// a mid day start can shorten the first day so the window does not fit everyone;
// the shortfall must still land inside the daily window, never at end of day.
func TestConstrainedSendTimes_MidDayStartStaysInWindow(t *testing.T) {
	start := time.Date(2026, 1, 5, 15, 0, 0, 0, time.UTC) // Monday 15:00
	end := time.Date(2026, 1, 5, 23, 59, 0, 0, time.UTC)  // same Monday, end of day
	weekDays := vo.NewCampaignWeekDaysMust(2).AsSlice()   // Monday only
	dayStart := vo.NewCampaignTimeConstraintMust("09:00")
	dayEnd := vo.NewCampaignTimeConstraintMust("17:00")

	got := constrainedSendTimes(50, start, end, weekDays, *dayStart, *dayEnd, 0, 0)
	if len(got) != 50 {
		t.Fatalf("want 50 times, got %d", len(got))
	}
	for i, ts := range got {
		if int(ts.Weekday()) != 1 {
			t.Fatalf("time %d (%s) not on Monday", i, ts)
		}
		mins := ts.Hour()*60 + ts.Minute()
		if mins < 15*60 || mins > 17*60 {
			t.Fatalf("time %d (%s) outside the usable 15:00-17:00 window (would include an end of day 23:59 bug)", i, ts)
		}
	}
}
