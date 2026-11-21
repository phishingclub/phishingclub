package service

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func TestMinNextSendIsHalfAnInterval(t *testing.T) {
	p := &model.Program{StepIntervalMinutes: nullable.NewNullableWithValue(7 * 24 * 60)}
	if got := programMinNextSend(p, nil); !got.IsZero() {
		t.Fatalf("no last send should set no bound, got %v", got)
	}
	last := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	want := last.Add(84 * time.Hour)
	if got := programMinNextSend(p, &last); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestPickSendTimeAfterKeepsGap(t *testing.T) {
	// one week interval with a full week spread: without the bound the pick
	// could land right after the round start
	p := &model.Program{
		StepIntervalMinutes: nullable.NewNullableWithValue(7 * 24 * 60),
		SpreadMinutes:       nullable.NewNullableWithValue(7 * 24 * 60),
	}
	roundStart := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	lastSend := time.Date(2024, 1, 7, 23, 0, 0, 0, time.UTC)
	notBefore := programMinNextSend(p, &lastSend)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		got := programPickSendTimeAfter(p, roundStart, roundStart, notBefore, rng)
		if got.Before(notBefore) {
			t.Fatalf("pick %v is before the minimum gap %v", got, notBefore)
		}
		if !got.Before(roundStart.Add(7 * 24 * time.Hour)) {
			t.Fatalf("pick %v is past the spread", got)
		}
	}
}

func TestPickSendTimeAfterGapBeyondSpreadUsesNextSlot(t *testing.T) {
	p := windowProgram(t)
	p.StepIntervalMinutes = nullable.NewNullableWithValue(7 * 24 * 60)
	p.SpreadMinutes = nullable.NewNullableWithValue(60)
	// Monday 2024-01-08 09:00 round start, gap ends on Tuesday 12:00
	roundStart := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	notBefore := time.Date(2024, 1, 9, 12, 0, 0, 0, time.UTC)
	got := programPickSendTimeAfter(p, roundStart, roundStart, notBefore, rand.New(rand.NewSource(1)))
	// only Mondays are allowed, so the next opening is the Monday after
	want := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBlackoutSkipsDaysWithoutWindow(t *testing.T) {
	p := &model.Program{
		BlackoutDates: nullable.NewNullableWithValue([]model.ProgramBlackout{{From: "2024-12-24", To: "2024-12-26"}}),
	}
	now := time.Date(2024, 12, 24, 10, 0, 0, 0, time.UTC)
	want := time.Date(2024, 12, 27, 0, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, now); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// a day outside the blackout is not changed
	free := time.Date(2024, 12, 23, 10, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, free); !got.Equal(free) {
		t.Fatalf("expected %v, got %v", free, got)
	}
}

func TestBlackoutSkipsWindowDay(t *testing.T) {
	p := windowProgram(t)
	// Monday 2024-01-08 is blacked out, so Monday 2024-01-15 is next
	p.BlackoutDates = nullable.NewNullableWithValue([]model.ProgramBlackout{{From: "2024-01-08", To: "2024-01-08"}})
	now := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	want := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, now); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBlackoutLongerThanFirstSearch(t *testing.T) {
	p := &model.Program{
		BlackoutDates: nullable.NewNullableWithValue([]model.ProgramBlackout{{From: "2024-07-01", To: "2024-08-31"}}),
	}
	now := time.Date(2024, 7, 2, 10, 0, 0, 0, time.UTC)
	want := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, now); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBlackoutPickNeverLandsOnBlockedDay(t *testing.T) {
	p := &model.Program{
		StepIntervalMinutes: nullable.NewNullableWithValue(7 * 24 * 60),
		SpreadMinutes:       nullable.NewNullableWithValue(7 * 24 * 60),
		BlackoutDates:       nullable.NewNullableWithValue([]model.ProgramBlackout{{From: "2024-01-09", To: "2024-01-12"}}),
	}
	roundStart := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 300; i++ {
		got := programPickSendTime(p, roundStart, roundStart, rng)
		if p.IsBlackoutDay(got.Format(model.ProgramDateLayout)) {
			t.Fatalf("pick %v is on a blackout day", got)
		}
	}
}

func TestYearlyBlackoutRepeats(t *testing.T) {
	p := &model.Program{
		BlackoutDates: nullable.NewNullableWithValue([]model.ProgramBlackout{
			{From: "2024-12-25", To: "2024-12-25", Yearly: true},
		}),
	}
	for _, year := range []int{2024, 2026, 2031} {
		now := time.Date(year, 12, 25, 10, 0, 0, 0, time.UTC)
		want := time.Date(year, 12, 26, 0, 0, 0, 0, time.UTC)
		if got := programNextWindowSlot(p, now); !got.Equal(want) {
			t.Fatalf("%d: expected %v, got %v", year, want, got)
		}
	}
	// the same day of a year before the range was made is skipped too
	if !p.IsBlackoutDay("2020-12-25") || p.IsBlackoutDay("2027-12-24") {
		t.Fatalf("a yearly range must match on month and day only")
	}
	// a range that is not yearly does not repeat
	p.BlackoutDates = nullable.NewNullableWithValue([]model.ProgramBlackout{{From: "2024-12-25", To: "2024-12-25"}})
	if p.IsBlackoutDay("2026-12-25") {
		t.Fatalf("a one time range must not repeat")
	}
}

func TestYearlyBlackoutAcrossNewYear(t *testing.T) {
	p := &model.Program{
		BlackoutDates: nullable.NewNullableWithValue([]model.ProgramBlackout{
			{From: "2024-12-24", To: "2025-01-01", Yearly: true},
		}),
	}
	for _, d := range []string{"2026-12-24", "2026-12-31", "2027-01-01", "2030-12-28"} {
		if !p.IsBlackoutDay(d) {
			t.Errorf("%s should be skipped", d)
		}
	}
	for _, d := range []string{"2026-12-23", "2027-01-02", "2027-06-15"} {
		if p.IsBlackoutDay(d) {
			t.Errorf("%s should not be skipped", d)
		}
	}
	// a send on Dec 27 moves past the new year to Jan 2
	now := time.Date(2026, 12, 27, 10, 0, 0, 0, time.UTC)
	want := time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, now); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// with a Monday only window, Monday Dec 28 2026 is skipped and Monday
	// Jan 4 2027 is next
	w := windowProgram(t)
	w.BlackoutDates = p.BlackoutDates
	got := programNextWindowSlot(w, time.Date(2026, 12, 22, 10, 0, 0, 0, time.UTC))
	if want := time.Date(2027, 1, 4, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// picks over a spread across the holidays never land on a skipped day
	w.StepIntervalMinutes = nullable.NewNullableWithValue(21 * 24 * 60)
	w.SpreadMinutes = nullable.NewNullableWithValue(21 * 24 * 60)
	roundStart := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 200; i++ {
		got := programPickSendTime(w, roundStart, roundStart, rng)
		if w.IsBlackoutDay(got.Format(model.ProgramDateLayout)) {
			t.Fatalf("pick %v is on a skipped day", got)
		}
	}
}

func TestBlackoutValidation(t *testing.T) {
	name := nullable.NewNullableWithValue(*vo.NewString64Must("p"))
	cases := []struct {
		ranges []model.ProgramBlackout
		ok     bool
	}{
		{[]model.ProgramBlackout{{From: "2024-01-01", To: "2024-01-02"}}, true},
		{[]model.ProgramBlackout{{From: "2024-01-02", To: "2024-01-02"}}, true},
		{[]model.ProgramBlackout{{From: "2024-01-03", To: "2024-01-02"}}, false},
		{[]model.ProgramBlackout{{From: "2024-1-3", To: "2024-01-04"}}, false},
		{[]model.ProgramBlackout{{From: "", To: "2024-01-04"}}, false},
		{[]model.ProgramBlackout{{From: "2024-12-24", To: "2025-01-01", Yearly: true}}, true},
		{[]model.ProgramBlackout{{From: "2024-01-01", To: "2024-12-30", Yearly: true}}, true},
		// a yearly range that covers every day of the year is refused
		{[]model.ProgramBlackout{{From: "2024-01-01", To: "2024-12-31", Yearly: true}}, false},
		{[]model.ProgramBlackout{{From: "2024-06-01", To: "2025-06-01", Yearly: true}}, false},
		{[]model.ProgramBlackout{{From: "2024-12-26", To: "2024-12-25", Yearly: true}}, false},
		// shorter than a year only because the next year has Feb 29, every other
		// year it covers every day
		{[]model.ProgramBlackout{{From: "2023-03-01", To: "2024-02-28", Yearly: true}}, false},
		// Feb 28 stays free every year
		{[]model.ProgramBlackout{{From: "2023-03-01", To: "2024-02-27", Yearly: true}}, true},
		// a one off range covers at most a year
		{[]model.ProgramBlackout{{From: "2027-01-01", To: "2027-12-31"}}, true},
		{[]model.ProgramBlackout{{From: "2028-01-01", To: "2028-12-31"}}, true},
		{[]model.ProgramBlackout{{From: "2027-01-01", To: "2028-01-01"}}, false},
		{[]model.ProgramBlackout{{From: "2027-01-01", To: "2029-12-31"}}, false},
	}
	for i, c := range cases {
		p := &model.Program{Name: name, BlackoutDates: nullable.NewNullableWithValue(c.ranges)}
		if err := p.Validate(); (err == nil) != c.ok {
			t.Errorf("case %d: err = %v, want ok = %v", i, err, c.ok)
		}
	}
	tooMany := make([]model.ProgramBlackout, model.ProgramBlackoutMax+1)
	for i := range tooMany {
		tooMany[i] = model.ProgramBlackout{From: "2024-01-01", To: "2024-01-01"}
	}
	p := &model.Program{Name: name, BlackoutDates: nullable.NewNullableWithValue(tooMany)}
	if err := p.Validate(); err == nil {
		t.Fatal("more than the max blackout ranges should fail")
	}
}

func TestLoopOpenersExcludeLastStep(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	steps := []*model.ProgramStep{
		{ID: nullable.NewNullableWithValue(a), Position: nullable.NewNullableWithValue(0)},
		{ID: nullable.NewNullableWithValue(b), Position: nullable.NewNullableWithValue(1)},
		{ID: nullable.NewNullableWithValue(c), Position: nullable.NewNullableWithValue(2)},
	}
	shuffle := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderShuffle)}
	preferred, fallback := programLoopOpeners(shuffle, steps, &b)
	if len(preferred) != 2 || len(fallback) != 1 || fallback[0].ID.MustGet() != b {
		t.Fatalf("expected last step in fallback only, got %d preferred %d fallback", len(preferred), len(fallback))
	}
	for _, p := range preferred {
		if p.ID.MustGet() == b {
			t.Fatal("last step must not be preferred")
		}
	}
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 100; i++ {
		if programPickStep(shuffle, preferred, rng).ID.MustGet() == b {
			t.Fatal("a new loop opened with the step sent last")
		}
	}
	// one step only: it is still sent
	preferred, fallback = programLoopOpeners(shuffle, steps[:1], &a)
	if len(preferred) != 1 || fallback != nil {
		t.Fatal("a single step must stay preferred")
	}
	// sequential order keeps everything
	sequential := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderSequential)}
	preferred, fallback = programLoopOpeners(sequential, steps, &b)
	if len(preferred) != 3 || fallback != nil {
		t.Fatal("sequential order must not exclude a step")
	}
	// no earlier send keeps everything
	preferred, fallback = programLoopOpeners(shuffle, steps, nil)
	if len(preferred) != 3 || fallback != nil {
		t.Fatal("first loop must not exclude a step")
	}
}

func TestApplyStartRequest(t *testing.T) {
	now := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	yes, no := true, false
	future := now.Add(48 * time.Hour)
	past := now.Add(-48 * time.Hour)

	// create without a start starts now
	p := &model.Program{}
	applyStartRequest(p, nil, now)
	if got := p.StartAtTime(); got == nil || !got.Equal(now) {
		t.Fatalf("create without start: %v", got)
	}
	// create as draft has no start, also with a time given
	p = &model.Program{Draft: &yes, StartAt: nullable.NewNullableWithValue(future)}
	applyStartRequest(p, nil, now)
	if !p.StartAt.IsSpecified() || !p.StartAt.IsNull() {
		t.Fatal("draft must store a null start")
	}
	// a future start is kept, a past one is now
	p = &model.Program{StartAt: nullable.NewNullableWithValue(future)}
	applyStartRequest(p, nil, now)
	if !p.StartAtTime().Equal(future) {
		t.Fatal("future start changed")
	}
	p = &model.Program{StartAt: nullable.NewNullableWithValue(past)}
	applyStartRequest(p, nil, now)
	if !p.StartAtTime().Equal(now) {
		t.Fatal("past start should be now")
	}
	// update of a started program never changes the start
	started := now.Add(-time.Hour)
	existing := &model.Program{StartedAt: &started}
	p = &model.Program{Draft: &yes}
	applyStartRequest(p, existing, now)
	if p.StartAt.IsSpecified() {
		t.Fatal("started program start must not change")
	}
	// update of a draft without start fields leaves it alone
	draftProgram := &model.Program{}
	p = &model.Program{}
	applyStartRequest(p, draftProgram, now)
	if p.StartAt.IsSpecified() {
		t.Fatal("update without start fields must not change the start")
	}
	// update with draft false starts now
	p = &model.Program{Draft: &no}
	applyStartRequest(p, draftProgram, now)
	if got := p.StartAtTime(); got == nil || !got.Equal(now) {
		t.Fatal("draft false should start now")
	}
	// update with an explicit null start makes it a draft again
	p = &model.Program{StartAt: nullable.NewNullNullable[time.Time]()}
	applyStartRequest(p, draftProgram, now)
	if !p.StartAt.IsSpecified() || !p.StartAt.IsNull() {
		t.Fatal("null start should make a draft")
	}
}

func TestProgramStatus(t *testing.T) {
	now := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	p := &model.Program{StartAt: nullable.NewNullNullable[time.Time]()}
	if s := p.StatusAt(now); s != model.ProgramStatusDraft {
		t.Fatalf("got %s", s)
	}
	p.StartAt = nullable.NewNullableWithValue(now.Add(time.Hour))
	if s := p.StatusAt(now); s != model.ProgramStatusScheduled {
		t.Fatalf("got %s", s)
	}
	p.StartAt = nullable.NewNullableWithValue(now)
	if s := p.StatusAt(now); s != model.ProgramStatusActive {
		t.Fatalf("got %s", s)
	}
	p.PausedAt = nullable.NewNullableWithValue(now)
	if s := p.StatusAt(now); s != model.ProgramStatusPaused {
		t.Fatalf("got %s", s)
	}
	p.ClosedAt = nullable.NewNullableWithValue(now)
	if s := p.StatusAt(now); s != model.ProgramStatusClosed {
		t.Fatalf("got %s", s)
	}
}

// TestLongBlackoutHoldsUntilItEnds checks a send is held until skip ranges
// that run longer than one look ahead have ended, and never lands on a skip
// day: Monday to Friday 09:00 to 17:00 with 2027-01-01 to 2029-12-31 skipped,
// as one range saved before the one year cap and as three ranges of a year
func TestLongBlackoutHoldsUntilItEnds(t *testing.T) {
	window := func(ranges []model.ProgramBlackout) *model.Program {
		return &model.Program{
			// weekday bitmask: monday 2 to friday 32
			ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(2 | 4 | 8 | 16 | 32)),
			ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
			ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
			BlackoutDates:       nullable.NewNullableWithValue(ranges),
		}
	}
	programs := map[string]*model.Program{
		"one range": window([]model.ProgramBlackout{{From: "2027-01-01", To: "2029-12-31"}}),
		"three ranges": window([]model.ProgramBlackout{
			{From: "2027-01-01", To: "2027-12-31"},
			{From: "2028-01-01", To: "2028-12-31"},
			{From: "2029-01-01", To: "2029-12-31"},
		}),
	}
	// 2030-01-01 is a Tuesday
	want := time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)
	for name, p := range programs {
		for _, now := range []time.Time{
			time.Date(2026, 12, 31, 18, 0, 0, 0, time.UTC),
			time.Date(2027, 1, 4, 10, 0, 0, 0, time.UTC),
			time.Date(2028, 6, 1, 10, 0, 0, 0, time.UTC),
		} {
			got := programNextWindowSlot(p, now)
			if !got.Equal(want) {
				t.Fatalf("%s from %v: expected %v, got %v", name, now, want, got)
			}
		}
		rng := rand.New(rand.NewSource(7))
		roundStart := time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 50; i++ {
			got := programPickSendTime(p, roundStart, time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC), rng)
			if p.IsBlackoutDay(got.Format(model.ProgramDateLayout)) {
				t.Fatalf("%s: pick %v is on a skip day", name, got)
			}
		}
	}
}

// TestProgramSendDayLookAheadMatches checks the save check of the skip ranges
// looks as far ahead as the scheduler looks for a window opening
func TestProgramSendDayLookAheadMatches(t *testing.T) {
	if programSlotMaxDays*programSlotMaxSearches != model.ProgramSendDayLookAheadDays {
		t.Fatalf("look ahead %d days, the save check %d", programSlotMaxDays*programSlotMaxSearches, model.ProgramSendDayLookAheadDays)
	}
}
