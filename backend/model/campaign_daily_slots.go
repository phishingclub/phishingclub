package model

import (
	"fmt"
	"slices"
	"time"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// MaxDailySlotRange is the longest Daily Slots delivery range accepted. It keeps
// the per day work of building the windows small.
const MaxDailySlotRange = 3 * 366 * 24 * time.Hour

// ValidateDailySlotRange checks that the delivery dates are in order and not
// further apart than MaxDailySlotRange
func ValidateDailySlotRange(startAt, endAt time.Time) error {
	if endAt.Before(startAt) {
		return validate.WrapErrorWithField(errors.New("the delivery end must be after the start"), "sendEndAt")
	}
	if endAt.Sub(startAt) > MaxDailySlotRange {
		return validate.WrapErrorWithField(errors.New("the delivery dates can be at most 3 years apart"), "sendEndAt")
	}
	return nil
}

// DailySlot is the send window of one Daily Slots delivery day
type DailySlot struct {
	Start time.Time
	End   time.Time
}

// DailySlots returns the send window of every delivery day of a Daily Slots
// schedule between startAt and endAt.
//
// Without a time zone the week days and hours are UTC. The days are walked from
// startAt in steps of one day while not after endAt, and each window is the UTC
// day of that step plus the hours. The times are read in UTC whatever location
// they arrive with. Campaigns saved before the time zone was
// stored run this way, so it must not change.
//
// With a time zone the week days and hours are local to it. Every local date
// from the date of startAt to the date of endAt that is on a selected week day
// gets the window start to end on that date in the zone, so the local hours stay
// the same across clock changes. A start or end hour that does not exist (the
// clock jumps forward) or exists twice (the clock goes back) on a delivery day
// is rejected, as the window would be a guess.
func DailySlots(
	startAt, endAt time.Time,
	weekDays []int,
	dayStart, dayEnd vo.CampaignTimeConstraint,
	tz *vo.CampaignTimeZone,
) ([]DailySlot, error) {
	slots := []DailySlot{}
	if tz == nil {
		// days are cut at UTC midnight, so the week day is read in UTC too
		startAt, endAt = startAt.UTC(), endAt.UTC()
		for d := startAt; !d.After(endAt); d = d.AddDate(0, 0, 1) {
			if slices.Contains(weekDays, int(d.Weekday())) {
				day := d.Truncate(24 * time.Hour)
				slots = append(slots, DailySlot{
					Start: day.Add(dayStart.Minutes()),
					End:   day.Add(dayEnd.Minutes()),
				})
			}
		}
		return slots, nil
	}
	loc := tz.Location()
	sh, sm := dayStart.HourMinute()
	eh, em := dayEnd.HourMinute()
	// walk calendar dates in UTC, which has no clock changes, and build each
	// window in the zone
	sy, smo, sd := startAt.In(loc).Date()
	ey, emo, ed := endAt.In(loc).Date()
	first := time.Date(sy, smo, sd, 0, 0, 0, 0, time.UTC)
	last := time.Date(ey, emo, ed, 0, 0, 0, 0, time.UTC)
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		if !slices.Contains(weekDays, int(day.Weekday())) {
			continue
		}
		y, mo, d := day.Date()
		start, okStart := exactWallTime(y, mo, d, sh, sm, loc)
		end, okEnd := exactWallTime(y, mo, d, eh, em, loc)
		if !okStart || !okEnd || !end.After(start) {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf(
					"the daily hours %s to %s do not exist or occur twice on %s in %s because of a clock change",
					dayStart.String(), dayEnd.String(), day.Format("2006-01-02"), tz.String(),
				),
				"constraintStartTime",
			)
		}
		slots = append(slots, DailySlot{Start: start, End: end})
	}
	return slots, nil
}

// exactWallTime returns the instant of a wall clock time in loc, and false when
// that wall time does not exist or exists twice there
func exactWallTime(y int, mo time.Month, d, h, mi int, loc *time.Location) (time.Time, bool) {
	// the wall time read as if it was UTC; the real instant is this minus the
	// zone offset in effect at that instant. Offsets are within 14h of UTC, so
	// every offset that can apply is in effect somewhere in the 36h around it.
	wall := time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
	found := []time.Time{}
	seen := map[int]bool{}
	for k := -6; k <= 6; k++ {
		_, offset := wall.Add(time.Duration(k) * 6 * time.Hour).In(loc).Zone()
		if seen[offset] {
			continue
		}
		seen[offset] = true
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		ly, lmo, ld := local.Date()
		if ly == y && lmo == mo && ld == d && local.Hour() == h && local.Minute() == mi {
			found = append(found, candidate)
		}
	}
	if len(found) != 1 {
		return time.Time{}, false
	}
	return found[0], true
}

// ValidateDailySlotWeekDays checks that every selected week day has a delivery
// day between startAt and endAt and that the windows can be built
func ValidateDailySlotWeekDays(
	startAt, endAt time.Time,
	weekDays vo.CampaignWeekDays,
	dayStart, dayEnd vo.CampaignTimeConstraint,
	tz *vo.CampaignTimeZone,
) error {
	if tz == nil {
		if !weekDays.IsWithin(&startAt, &endAt) {
			return validate.WrapErrorWithField(
				fmt.Errorf(
					"constraint week days must be within the start (%s) and end date (%s)",
					startAt.Format("2006-01-02"),
					endAt.Format("2006-01-02"),
				),
				"ConstraintWeekDays",
			)
		}
		return nil
	}
	slots, err := DailySlots(startAt, endAt, weekDays.AsSlice(), dayStart, dayEnd, tz)
	if err != nil {
		return err
	}
	covered := map[int]bool{}
	for _, s := range slots {
		covered[int(s.Start.In(tz.Location()).Weekday())] = true
	}
	for _, wd := range weekDays.AsSlice() {
		if !covered[wd] {
			return validate.WrapErrorWithField(
				errors.New("every selected week day must fall inside the delivery dates"),
				"ConstraintWeekDays",
			)
		}
	}
	return nil
}

// DailySlotTimeZone returns the Daily Slots time zone, or nil when the week days
// and hours are UTC
func (c *Campaign) DailySlotTimeZone() *vo.CampaignTimeZone {
	if v, err := c.ConstraintTimeZone.Get(); err == nil {
		return &v
	}
	return nil
}
