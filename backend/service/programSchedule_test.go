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
