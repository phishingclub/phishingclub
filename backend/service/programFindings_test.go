package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
)

func TestProgramReportPeriodNamed(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	p := &model.Program{TimeZone: nullable.NewNullableWithValue("Europe/Copenhagen")}
	// date only values are read in the program zone and the last day is whole
	from, to, err := programReportPeriod(p, "2026-03-10", "2026-03-10")
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 3, 10, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 3, 11, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %v to %v", from, to)
	}
	// a time snaps to its day in the program zone: late in the UTC day is the
	// next day in Copenhagen
	from, to, err = programReportPeriod(p, "2026-03-10T23:30:00Z", "2026-03-12T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 3, 11, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 3, 13, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %v to %v", from, to)
	}
	// the last day across the spring forward change is still a whole day
	_, to, err = programReportPeriod(p, "", "2026-03-29")
	if err != nil {
		t.Fatal(err)
	}
	if !to.Equal(time.Date(2026, 3, 30, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %v", to)
	}
	if f, tt, err := programReportPeriod(p, "", ""); err != nil || f != nil || tt != nil {
		t.Fatal("open bounds stay open")
	}
	if _, _, err := programReportPeriod(p, "10/03/2026", ""); err == nil {
		t.Fatal("a bad date must fail")
	}
}

func TestProgramReportPeriodAnonymousWholeMonths(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skip("no zone data")
	}
	p := &model.Program{
		TimeZone:    nullable.NewNullableWithValue("Europe/Copenhagen"),
		IsAnonymous: nullable.NewNullableWithValue(true),
	}
	from, to, err := programReportPeriod(p, "2026-10-01", "2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	if !from.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, loc)) || !to.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("got %v to %v", from, to)
	}
	// a month start given as a time in the program zone is fine
	if _, _, err := programReportPeriod(p, "2026-09-01T00:00:00+02:00", "2026-11-01T00:00:00+01:00"); err != nil {
		t.Fatal(err)
	}
	if f, tt, err := programReportPeriod(p, "", ""); err != nil || f != nil || tt != nil {
		t.Fatal("all time must be allowed")
	}
	bad := [][2]string{
		{"2026-10-02", "2026-11-01"},
		{"2026-10-01", "2026-10-31"},
		{"2026-10-01", ""},
		{"", "2026-11-01"},
		{"2026-11-01", "2026-10-01"},
		{"2026-10-01", "2026-10-01"},
		// midnight UTC is not the month start in Copenhagen
		{"2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"},
	}
	for _, b := range bad {
		_, _, err := programReportPeriod(p, b[0], b[1])
		if err == nil || !strings.Contains(err.Error(), "whole calendar months") {
			t.Fatalf("%v: want whole month error, got %v", b, err)
		}
	}
}

func TestReportedDayEnd(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone data")
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	got := reportedDayEnd(time.Date(2026, 5, 20, 8, 15, 3, 0, time.UTC), loc, now)
	want := time.Date(2026, 5, 21, 0, 0, 0, 0, loc).Add(-time.Nanosecond)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// today never goes past now
	if got := reportedDayEnd(now, loc, now); !got.Equal(now) {
		t.Fatalf("expected now, got %v", got)
	}
}

func TestProgramResultStatsSuppression(t *testing.T) {
	raw := &model.CampaignResultView{
		Recipients: 20, EmailsSent: 20, TrackingPixelLoaded: 12, WebsiteLoaded: 3,
		SubmittedData: 0, Reported: 7, TrainingStarted: 5, TrainingCompleted: 1,
	}
	named := programResultStats(raw, false)
	if named.Suppressed || named.WebsiteLoaded == nil || *named.WebsiteLoaded != 3 ||
		named.TrainingCompleted == nil || *named.TrainingCompleted != 1 {
		t.Fatalf("named program keeps every count: %+v", named)
	}
	anon := programResultStats(raw, true)
	if !anon.Suppressed {
		t.Fatal("small counts must mark the stats suppressed")
	}
	if anon.WebsiteLoaded != nil || anon.TrainingCompleted != nil {
		t.Fatal("small outcome counts must be null")
	}
	if anon.TrackingPixelLoaded == nil || *anon.TrackingPixelLoaded != 12 ||
		anon.Reported == nil || *anon.Reported != 7 || anon.SubmittedData == nil || *anon.SubmittedData != 0 {
		t.Fatalf("large and zero counts stay: %+v", anon)
	}
	small := programResultStats(&model.CampaignResultView{Recipients: 4, EmailsSent: 4, Reported: 0}, true)
	if !small.Suppressed || small.Reported != nil || small.TrackingPixelLoaded != nil {
		t.Fatalf("a small group withholds every outcome: %+v", small)
	}
}

func TestCtxMutexHonoursContext(t *testing.T) {
	var m ctxMutex
	m.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.LockContext(ctx); err == nil {
		t.Fatal("a held lock must give up when the context ends")
	}
	m.Unlock()
	if err := m.LockContext(context.Background()); err != nil {
		t.Fatalf("a free lock must be taken: %v", err)
	}
	m.Unlock()
}
