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

// windowProgram builds a program with a Monday only 09:00-17:00 delivery window.
func windowProgram(t *testing.T) *model.Program {
	t.Helper()
	// weekday bitmask: monday = 2
	return &model.Program{
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(2)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
	}
}

func TestNextWindowSlotNoWindow(t *testing.T) {
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(&model.Program{}, now); !got.Equal(now) {
		t.Fatalf("no window should return now, got %v", got)
	}
}

func TestNextWindowSlotBeforeWindowSameDay(t *testing.T) {
	// 2024-01-01 is a Monday, 08:00 is before the 09:00 window opening
	now := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	want := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(windowProgram(t), now); !got.Equal(want) {
		t.Fatalf("expected slot at %v, got %v", want, got)
	}
}

func TestNextWindowSlotAfterWindowSkipsToNextAllowedDay(t *testing.T) {
	// Monday 18:00 is past the window, only Monday is allowed
	now := time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC)
	want := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(windowProgram(t), now); !got.Equal(want) {
		t.Fatalf("expected next Monday slot at %v, got %v", want, got)
	}
}

func TestNextWindowSlotWithinWindowIsNow(t *testing.T) {
	now := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(windowProgram(t), now); !got.Equal(now) {
		t.Fatalf("within window should return now, got %v", got)
	}
}

func TestWindowUsesProgramTimeZone(t *testing.T) {
	p := windowProgram(t)
	p.TimeZone = nullable.NewNullableWithValue("Europe/Copenhagen")
	// Monday 07:30 UTC is 08:30 in Copenhagen (winter), before the 09:00 opening
	now := time.Date(2024, 1, 1, 7, 30, 0, 0, time.UTC)
	want := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, now); !got.Equal(want) {
		t.Fatalf("expected 09:00 Copenhagen (%v), got %v", want, got)
	}
	// Sunday 23:30 UTC is already Monday 00:30 in Copenhagen
	sunday := time.Date(2023, 12, 31, 23, 30, 0, 0, time.UTC)
	if got := programNextWindowSlot(p, sunday); !got.Equal(want) {
		t.Fatalf("weekday must be read in the program zone, got %v", got)
	}
}

func TestPickSendTimeSpreadsInsideWindow(t *testing.T) {
	p := windowProgram(t)
	// a Monday only window and a one week spread leave exactly one 8 hour span
	p.StepIntervalMinutes = nullable.NewNullableWithValue(7 * 24 * 60)
	p.SpreadMinutes = nullable.NewNullableWithValue(7 * 24 * 60)
	roundStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(1))
	open := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	close := time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC)
	distinct := map[time.Time]bool{}
	for i := 0; i < 200; i++ {
		got := programPickSendTime(p, roundStart, roundStart, rng)
		if got.Before(open) || !got.Before(close) {
			t.Fatalf("send time %v outside the window", got)
		}
		distinct[got.Truncate(time.Hour)] = true
	}
	if len(distinct) < 6 {
		t.Fatalf("send times are not spread over the window, %d distinct hours", len(distinct))
	}
}

func TestPickSendTimeWithoutSpreadIsNextSlot(t *testing.T) {
	p := windowProgram(t)
	now := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	got := programPickSendTime(p, now, now, rand.New(rand.NewSource(1)))
	if want := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("no spread should use the next window opening %v, got %v", want, got)
	}
}

func TestNextRoundStartAfterLongPauseStartsNow(t *testing.T) {
	p := &model.Program{
		StepIntervalMinutes: nullable.NewNullableWithValue(60),
		SpreadMinutes:       nullable.NewNullableWithValue(30),
	}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := programNextRoundStart(p, start, start.Add(10*time.Minute)); !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("next round should start one interval later, got %v", got)
	}
	late := start.Add(48 * time.Hour)
	if got := programNextRoundStart(p, start, late); !got.Equal(late) {
		t.Fatalf("an overdue round should start now, got %v", got)
	}
}

func TestPickStepOrder(t *testing.T) {
	mk := func(pos int) *model.ProgramStep {
		return &model.ProgramStep{Position: nullable.NewNullableWithValue(pos)}
	}
	steps := []*model.ProgramStep{mk(2), mk(0), mk(1)}
	seq := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderSequential)}
	if got := programPickStep(seq, steps, nil); got != steps[1] {
		t.Fatalf("sequential should take the lowest position")
	}
	shuffle := &model.Program{OrderMode: nullable.NewNullableWithValue(model.ProgramOrderShuffle)}
	rng := rand.New(rand.NewSource(7))
	seen := map[*model.ProgramStep]bool{}
	for i := 0; i < 50; i++ {
		seen[programPickStep(shuffle, steps, rng)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("shuffle should reach every step, got %d", len(seen))
	}
	if programPickStep(shuffle, nil, rng) != nil {
		t.Fatalf("no candidates should give nil")
	}
}

func TestSameScope(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	global := nullable.NewNullNullable[uuid.UUID]()
	ownedByA := nullable.NewNullableWithValue(a)
	if !sameScope(nil, global) || !sameScope(&a, global) {
		t.Fatalf("a global resource is usable by every program")
	}
	if !sameScope(&a, ownedByA) {
		t.Fatalf("a company resource is usable by that company")
	}
	if sameScope(&b, ownedByA) {
		t.Fatalf("a company resource must not be usable by another company")
	}
	if sameScope(nil, ownedByA) {
		t.Fatalf("a company resource must not be usable by a global program")
	}
}
