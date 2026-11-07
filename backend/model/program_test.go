package model

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func validProgram() *Program {
	return &Program{Name: nullable.NewNullableWithValue(*vo.NewString64Must("p"))}
}

func step(position *int) *ProgramStep {
	s := &ProgramStep{CampaignTemplateID: nullable.NewNullableWithValue(uuid.New())}
	if position != nil {
		s.Position = nullable.NewNullableWithValue(*position)
	}
	return s
}

func intp(v int) *int { return &v }

func TestProgramValidateUpperBounds(t *testing.T) {
	cases := []struct {
		name string
		set  func(p *Program, v int)
	}{
		{"stepIntervalMinutes", func(p *Program, v int) { p.StepIntervalMinutes = nullable.NewNullableWithValue(v) }},
		{"spreadMinutes", func(p *Program, v int) { p.SpreadMinutes = nullable.NewNullableWithValue(v) }},
		{"newMemberDelayMinutes", func(p *Program, v int) { p.NewMemberDelayMinutes = nullable.NewNullableWithValue(v) }},
	}
	for _, c := range cases {
		p := validProgram()
		c.set(p, ProgramMaxMinutes)
		if err := p.Validate(); err != nil {
			t.Fatalf("%s at the limit should pass: %v", c.name, err)
		}
		p = validProgram()
		c.set(p, ProgramMaxMinutes+1)
		if err := p.Validate(); err == nil {
			t.Fatalf("%s over the limit should fail", c.name)
		}
	}
}

func TestProgramValidateSteps(t *testing.T) {
	p := validProgram()
	for i := 0; i < ProgramMaxSteps; i++ {
		p.Steps = append(p.Steps, step(nil))
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("max steps should pass: %v", err)
	}
	p.Steps = append(p.Steps, step(nil))
	if err := p.Validate(); err == nil {
		t.Fatal("too many steps should fail")
	}

	p = validProgram()
	p.Steps = []*ProgramStep{step(intp(1)), step(intp(1))}
	if err := p.Validate(); err == nil {
		t.Fatal("duplicate positions should fail")
	}
	// the first step has no position and is stored at index 0
	p.Steps = []*ProgramStep{step(nil), step(intp(0))}
	if err := p.Validate(); err == nil {
		t.Fatal("a position that collides with an index should fail")
	}
	p.Steps = []*ProgramStep{step(intp(-1))}
	if err := p.Validate(); err == nil {
		t.Fatal("a negative position should fail")
	}
	p.Steps = []*ProgramStep{step(intp(0)), step(intp(1)), step(intp(5))}
	if err := p.Validate(); err != nil {
		t.Fatalf("distinct positions should pass: %v", err)
	}
}

func TestProgramFlagHelpers(t *testing.T) {
	p := &Program{}
	if p.IsAnonymousOn() {
		t.Fatal("unset flag is off")
	}
	p.IsAnonymous = nullable.NewNullNullable[bool]()
	if p.IsAnonymousOn() {
		t.Fatal("null flag is off")
	}
	p.IsAnonymous = nullable.NewNullableWithValue(true)
	if !p.IsAnonymousOn() {
		t.Fatal("set flag is on")
	}
}

func TestProgramValidateSpreadUpToInterval(t *testing.T) {
	p := validProgram()
	p.StepIntervalMinutes = nullable.NewNullableWithValue(60)
	p.SpreadMinutes = nullable.NewNullableWithValue(60)
	if err := p.Validate(); err != nil {
		t.Fatalf("a spread equal to the interval should pass: %v", err)
	}
	p.SpreadMinutes = nullable.NewNullableWithValue(61)
	if err := p.Validate(); err == nil {
		t.Fatal("a spread longer than the interval should fail")
	}
}

func TestProgramValidateEndAt(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	withEnd := func(end time.Time) *Program {
		p := validProgram()
		p.EndAt = nullable.NewNullableWithValue(end)
		return p
	}
	// no end runs until closed
	if err := validProgram().ValidateEndAt(&start, true, now); err != nil {
		t.Fatalf("no end should pass: %v", err)
	}
	if err := withEnd(now.AddDate(1, 0, 0)).ValidateEndAt(&start, true, now); err != nil {
		t.Fatalf("an end a year ahead should pass: %v", err)
	}
	err := withEnd(now.Add(-time.Minute)).ValidateEndAt(nil, true, now)
	if err == nil || !strings.Contains(err.Error(), "The end must be in the future") {
		t.Fatalf("a past end must fail, got %v", err)
	}
	err = withEnd(start.Add(-time.Minute)).ValidateEndAt(&start, true, now)
	if err == nil || !strings.Contains(err.Error(), "The end must be after the start") {
		t.Fatalf("an end before the start must fail, got %v", err)
	}
	if err := withEnd(start).ValidateEndAt(&start, true, now); err == nil {
		t.Fatalf("an end equal to the start must fail")
	}
	// an unchanged end that has passed does not block other edits
	if err := withEnd(now.Add(-time.Minute)).ValidateEndAt(nil, false, now); err != nil {
		t.Fatalf("an unchanged end should pass: %v", err)
	}
	// a draft has no start, only the future check applies
	if err := withEnd(now.Add(time.Minute)).ValidateEndAt(nil, true, now); err != nil {
		t.Fatalf("a draft with a future end should pass: %v", err)
	}
}

func TestProgramEndToDBMapAndStatus(t *testing.T) {
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	p := validProgram()
	p.EndAt = nullable.NewNullableWithValue(end)
	if m := p.ToDBMap(); m["end_at"] != end {
		t.Fatalf("end_at not mapped: %v", m["end_at"])
	}
	p.EndAt = nullable.NewNullNullable[time.Time]()
	if m := p.ToDBMap(); m["end_at"] != nil {
		t.Fatalf("a null end must clear end_at")
	} else if _, ok := m["end_at"]; !ok {
		t.Fatalf("a null end must be written")
	}
	p = validProgram()
	if _, ok := p.ToDBMap()["end_at"]; ok {
		t.Fatalf("an unspecified end must not be written")
	}
	p.EndAt = nullable.NewNullableWithValue(end)
	if p.IsEndedBy(end.Add(-time.Second)) || !p.IsEndedBy(end) {
		t.Fatalf("IsEndedBy wrong around the end")
	}
}
