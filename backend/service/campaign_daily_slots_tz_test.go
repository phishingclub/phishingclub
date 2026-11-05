package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// with a time zone the send start and end become the first and last delivery
// day's local hours, also when the hours cross UTC midnight and a clock change
// falls inside the range
func TestUpdateSchedulesCampaignStartAndEndDates_TimeZone(t *testing.T) {
	tz := vo.NewCampaignTimeZoneMust("America/Los_Angeles")
	loc := tz.Location()
	c := &model.Campaign{}
	// Sat 6 Mar to Sat 20 Mar 2027, clocks go forward Sun 14 Mar
	c.SendStartAt = nullable.NewNullableWithValue(time.Date(2027, 3, 6, 9, 0, 0, 0, loc))
	c.SendEndAt = nullable.NewNullableWithValue(time.Date(2027, 3, 20, 17, 0, 0, 0, loc))
	c.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(62))
	c.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00"))
	c.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00"))
	c.ConstraintTimeZone = nullable.NewNullableWithValue(*tz)
	if err := (&Campaign{}).updateSchedulesCampaignStartAndEndDates(c); err != nil {
		t.Fatal(err)
	}
	if got, want := c.SendStartAt.MustGet(), time.Date(2027, 3, 8, 9, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("start: got %v want %v", got.In(loc), want)
	}
	if got, want := c.SendEndAt.MustGet(), time.Date(2027, 3, 19, 17, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("end: got %v want %v", got.In(loc), want)
	}
	// no selected day in range is an error, not a zero date
	c.SendStartAt = nullable.NewNullableWithValue(time.Date(2027, 3, 6, 9, 0, 0, 0, loc))
	c.SendEndAt = nullable.NewNullableWithValue(time.Date(2027, 3, 7, 17, 0, 0, 0, loc))
	if err := (&Campaign{}).updateSchedulesCampaignStartAndEndDates(c); err == nil {
		t.Fatal("expected an error when no selected day is in range")
	}
}

// resume with a time zone places every send on a selected local day inside the
// local hours, here 09:00-17:00 Los Angeles which crosses UTC midnight
func TestResumeCampaignByID_DailySlotsTimeZone(t *testing.T) {
	ctx := context.Background()
	svc, db := newPauseResumeService(t)
	session := authorizedSession(t)
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)
	id := seedCampaign(t, db, &past, &future, nil, nil)
	crIDs := []string{}
	for i := 0; i < 40; i++ {
		crIDs = append(crIDs, seedRecipient(t, db, id, past.Add(time.Duration(i)*time.Minute), false).String())
	}
	mustPause(t, svc, db, session, id)

	tz := vo.NewCampaignTimeZoneMust("America/Los_Angeles")
	loc := tz.Location()
	y, m, d := time.Now().In(loc).AddDate(0, 0, 1).Date()
	start := time.Date(y, m, d, 9, 0, 0, 0, loc)
	ey, em, ed := start.AddDate(0, 0, 13).Date()
	end := time.Date(ey, em, ed, 17, 0, 0, 0, loc)
	req := &model.Campaign{
		SendStartAt:         nullable.NewNullableWithValue(start),
		SendEndAt:           nullable.NewNullableWithValue(end),
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(62)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
		ConstraintTimeZone:  nullable.NewNullableWithValue(*tz),
	}
	if err := svc.ResumeCampaignByID(ctx, session, &id, req); err != nil {
		t.Fatalf("resume daily slots: %v", err)
	}
	for _, crID := range crIDs {
		var row database.CampaignRecipient
		if err := db.Where("id = ?", crID).First(&row).Error; err != nil {
			t.Fatalf("read recipient: %v", err)
		}
		if row.SendAt == nil {
			t.Fatalf("recipient %s has no send time", crID)
		}
		ts := row.SendAt.In(loc)
		if wd := int(ts.Weekday()); wd < 1 || wd > 5 {
			t.Fatalf("recipient %s scheduled on a weekend (%s)", crID, ts)
		}
		mins := ts.Hour()*60 + ts.Minute()
		if mins < 9*60 || mins > 17*60 {
			t.Fatalf("recipient %s scheduled outside 09:00-17:00 local (%s)", crID, ts)
		}
	}
}

// resume never places a send after the end of the resume window, even when the
// last delivery day's hours run past it
func TestConstrainedSendTimes_ClampsToEnd(t *testing.T) {
	tz := vo.NewCampaignTimeZoneMust("America/Los_Angeles")
	loc := tz.Location()
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, loc)
	end := time.Date(2026, 11, 18, 10, 0, 0, 0, loc) // Wed 10:00, hours run to 17:00
	slots, err := model.DailySlots(start, end, []int{1, 2, 3}, *vo.NewCampaignTimeConstraintMust("09:00"), *vo.NewCampaignTimeConstraintMust("17:00"), tz)
	if err != nil {
		t.Fatal(err)
	}
	for _, ts := range constrainedSendTimes(50, start, end, slots, 30, 30) {
		if ts.After(end) || ts.Before(start) {
			t.Fatalf("send %v outside [%v, %v]", ts.In(loc), start, end)
		}
	}
}

// the day and hour errors are the plain ones, also with a time zone
func TestUpdateSchedulesCampaignStartAndEndDates_Errors(t *testing.T) {
	tz := vo.NewCampaignTimeZoneMust("Europe/Copenhagen")
	loc := tz.Location()
	c := &model.Campaign{}
	c.SendStartAt = nullable.NewNullableWithValue(time.Date(2026, 12, 1, 9, 0, 0, 0, loc))
	c.SendEndAt = nullable.NewNullableWithValue(time.Date(2026, 12, 4, 17, 0, 0, 0, loc))
	c.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(62))
	c.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00"))
	c.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00"))
	c.ConstraintTimeZone = nullable.NewNullableWithValue(*tz)
	err := (&Campaign{}).updateSchedulesCampaignStartAndEndDates(c)
	if err == nil || !strings.Contains(err.Error(), "end time must be after start time") {
		t.Fatalf("want the end after start error, got %v", err)
	}
}
