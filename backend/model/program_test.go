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

// TestProgramValidateBounds checks the timing bounds are the ones the program
// form offers: one day to three months between templates, a spread up to
// three months and a wait for new members up to one month
func TestProgramValidateBounds(t *testing.T) {
	cases := []struct {
		name     string
		set      func(p *Program, v int)
		min, max int
	}{
		{"stepIntervalMinutes", func(p *Program, v int) { p.StepIntervalMinutes = nullable.NewNullableWithValue(v) }, 24 * 60, 129600},
		{"spreadMinutes", func(p *Program, v int) {
			p.StepIntervalMinutes = nullable.NewNullableWithValue(ProgramMaxIntervalMinutes)
			p.SpreadMinutes = nullable.NewNullableWithValue(v)
		}, 0, 129600},
		{"newMemberDelayMinutes", func(p *Program, v int) { p.NewMemberDelayMinutes = nullable.NewNullableWithValue(v) }, 0, 43200},
	}
	for _, c := range cases {
		for _, v := range []int{c.min, c.max} {
			p := validProgram()
			c.set(p, v)
			if err := p.Validate(); err != nil {
				t.Fatalf("%s at %d should pass: %v", c.name, v, err)
			}
		}
		for _, v := range []int{c.min - 1, c.max + 1} {
			p := validProgram()
			c.set(p, v)
			if err := p.Validate(); err == nil {
				t.Fatalf("%s at %d should fail", c.name, v)
			}
		}
	}
	// a one minute interval would make a hidden campaign per template every minute
	p := validProgram()
	p.StepIntervalMinutes = nullable.NewNullableWithValue(1)
	if err := p.Validate(); err == nil {
		t.Fatal("a one minute interval should fail")
	}
}

// TestProgramValidateCreateSpreadAgainstDefault checks a create that sets only
// the spread is checked against the default time between templates
func TestProgramValidateCreateSpreadAgainstDefault(t *testing.T) {
	create := func(spread int, interval *int) error {
		p := validProgram()
		p.Steps = []*ProgramStep{step(nil)}
		group := uuid.New()
		p.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&group})
		p.SpreadMinutes = nullable.NewNullableWithValue(spread)
		if interval != nil {
			p.StepIntervalMinutes = nullable.NewNullableWithValue(*interval)
		}
		return p.ValidateCreate()
	}
	if err := create(ProgramDefaultIntervalMinutes, nil); err != nil {
		t.Fatalf("a spread equal to the default interval should pass: %v", err)
	}
	if err := create(ProgramDefaultIntervalMinutes+1, nil); err == nil {
		t.Fatal("a spread longer than the default interval should fail")
	}
	long := ProgramMaxIntervalMinutes
	if err := create(ProgramDefaultIntervalMinutes+1, &long); err != nil {
		t.Fatalf("a spread within a sent interval should pass: %v", err)
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
	p.StepIntervalMinutes = nullable.NewNullableWithValue(1440)
	p.SpreadMinutes = nullable.NewNullableWithValue(1440)
	if err := p.Validate(); err != nil {
		t.Fatalf("a spread equal to the interval should pass: %v", err)
	}
	p.SpreadMinutes = nullable.NewNullableWithValue(1441)
	if err := p.Validate(); err == nil {
		t.Fatal("a spread longer than the interval should fail")
	}
}

// TestProgramValidateMessagesSayTemplates checks the timing errors name sends
// the way the program UI does, as templates and not emails
func TestProgramValidateMessagesSayTemplates(t *testing.T) {
	cases := []func(p *Program){
		func(p *Program) { p.StepIntervalMinutes = nullable.NewNullableWithValue(0) },
		func(p *Program) { p.StepIntervalMinutes = nullable.NewNullableWithValue(ProgramMaxIntervalMinutes + 1) },
		func(p *Program) {
			p.StepIntervalMinutes = nullable.NewNullableWithValue(1440)
			p.SpreadMinutes = nullable.NewNullableWithValue(1441)
		},
		func(p *Program) { p.NewMemberDelayMinutes = nullable.NewNullableWithValue(-1) },
		func(p *Program) {
			p.NewMemberDelayMinutes = nullable.NewNullableWithValue(ProgramMaxNewMemberDelayMinutes + 1)
		},
	}
	for i, set := range cases {
		p := validProgram()
		set(p)
		err := p.Validate()
		if err == nil {
			t.Fatalf("case %d should fail", i)
		}
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "email") || !strings.Contains(msg, "template") {
			t.Fatalf("case %d message %q should name templates, not emails", i, err.Error())
		}
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

// windowProgram is a program sending every day between start and end in zone
func windowProgram(zone, start, end string) *Program {
	p := validProgram()
	p.TimeZone = nullable.NewNullableWithValue(zone)
	p.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(127))
	p.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust(start))
	p.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust(end))
	return p
}

// TestProgramWindowClockChange checks a window bound that does not exist or
// exists twice on a sending day is refused, like Daily Slots, and that the
// end of day and normal hours are kept
func TestProgramWindowClockChange(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Copenhagen"); err != nil {
		t.Skip("no zone data")
	}
	// Copenhagen jumps from 02:00 to 03:00 in spring and back from 03:00 to
	// 02:00 in autumn, so 02:30 is missing once and doubled once a year
	for _, c := range []struct {
		start, end string
		ok         bool
	}{
		{"09:00", "17:00", true},
		{"00:00", "23:59", true},
		{"02:30", "17:00", false},
		{"01:00", "02:30", false},
	} {
		err := windowProgram("Europe/Copenhagen", c.start, c.end).Validate()
		if (err == nil) != c.ok {
			t.Fatalf("%s to %s: accepted %v, want %v (%v)", c.start, c.end, err == nil, c.ok, err)
		}
		if err != nil && !strings.Contains(err.Error(), "clock change") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// UTC has no clock change
	if err := windowProgram("UTC", "02:30", "17:00").Validate(); err != nil {
		t.Fatalf("UTC: %v", err)
	}
	// a sending day without a clock change is fine: Copenhagen changes on
	// Sundays, so a Monday to Friday window at 02:30 is allowed
	weekdays := windowProgram("Europe/Copenhagen", "02:30", "17:00")
	weekdays.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(0b0111110))
	if err := weekdays.Validate(); err != nil {
		t.Fatalf("weekdays: %v", err)
	}
	// an update that only changes the zone is checked with the stored window
	existing := windowProgram("UTC", "02:30", "17:00")
	update := validProgram()
	update.TimeZone = nullable.NewNullableWithValue("Europe/Copenhagen")
	if err := update.ValidateUpdate(existing, time.Now()); err == nil {
		t.Fatal("a zone change that puts the window in a clock change must be refused")
	}
	// an update that sends only the start time is checked with the stored days
	// and end time
	stored := windowProgram("Europe/Copenhagen", "09:00", "17:00")
	startOnly := &Program{
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("02:30")),
	}
	if err := startOnly.ValidateUpdate(stored, time.Now()); err == nil {
		t.Fatal("a start time inside a clock change must be refused when only the start is sent")
	}
	// clearing only one window field would leave a half window
	clearStart := &Program{ConstraintStartTime: nullable.NewNullNullable[vo.CampaignTimeConstraint]()}
	if err := clearStart.ValidateUpdate(stored, time.Now()); err == nil {
		t.Fatal("clearing only the start time must be refused")
	}
	clearAll := &Program{
		ConstraintWeekDays:  nullable.NewNullNullable[vo.CampaignWeekDays](),
		ConstraintStartTime: nullable.NewNullNullable[vo.CampaignTimeConstraint](),
		ConstraintEndTime:   nullable.NewNullNullable[vo.CampaignTimeConstraint](),
	}
	if err := clearAll.ValidateUpdate(stored, time.Now()); err != nil {
		t.Fatalf("clearing the whole window must be allowed: %v", err)
	}
}

// TestProgramValidateCreateAndUpdate checks the create and update rules that
// need no stored data
func TestProgramValidateCreateAndUpdate(t *testing.T) {
	group := uuid.New()
	p := validProgram()
	if err := p.ValidateCreate(); err == nil {
		t.Fatal("a create without templates must fail")
	}
	p.Steps = []*ProgramStep{step(nil)}
	if err := p.ValidateCreate(); err == nil {
		t.Fatal("a create without groups must fail")
	}
	p.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{&group})
	if err := p.ValidateCreate(); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	existing := validProgram()
	existing.StepIntervalMinutes = nullable.NewNullableWithValue(60)
	update := validProgram()
	update.SpreadMinutes = nullable.NewNullableWithValue(120)
	if err := update.ValidateUpdate(existing, time.Now()); err == nil {
		t.Fatal("a spread over the stored interval must fail")
	}
	update = validProgram()
	update.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{})
	if err := update.ValidateUpdate(existing, time.Now()); err == nil {
		t.Fatal("an empty group list must fail")
	}
	tooMany := validProgram()
	tooMany.Webhooks = nullable.NewNullableWithValue(make([]*CampaignWebhook, ProgramMaxWebhooks+1))
	if err := tooMany.Validate(); err == nil {
		t.Fatal("too many webhooks must fail")
	}
	nilEntry := validProgram()
	nilEntry.AllowDenyIDs = nullable.NewNullableWithValue([]*uuid.UUID{nil})
	if err := nilEntry.Validate(); err == nil {
		t.Fatal("a null filter entry must fail")
	}
}

// TestProgramTemplateMixBy checks the mix field is held to its allowlist, is
// stored when sent, and only takes effect in random order
func TestProgramTemplateMixBy(t *testing.T) {
	for _, v := range []string{"", TemplateMixDepartment, TemplateMixPosition, TemplateMixCity} {
		p := validProgram()
		p.TemplateMixBy = nullable.NewNullableWithValue(v)
		if err := p.Validate(); err != nil {
			t.Fatalf("%q should be allowed: %v", v, err)
		}
	}
	for _, v := range []string{TemplateMixCountry, TemplateMixMisc, TemplateMixNone, "email", "department; DROP TABLE programs", "Department"} {
		p := validProgram()
		p.TemplateMixBy = nullable.NewNullableWithValue(v)
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "templateMixBy") {
			t.Fatalf("%q should be refused on templateMixBy, got %v", v, err)
		}
	}
	p := validProgram()
	if _, ok := p.ToDBMap()["template_mix_by"]; ok {
		t.Fatal("a mix field not in the request must not be written")
	}
	p.TemplateMixBy = nullable.NewNullableWithValue(TemplateMixCity)
	if p.ToDBMap()["template_mix_by"] != TemplateMixCity {
		t.Fatal("the mix field must be written")
	}
	p.TemplateMixBy = nullable.NewNullNullable[string]()
	if v, ok := p.ToDBMap()["template_mix_by"]; !ok || v != "" {
		t.Fatal("a null mix field turns mixing off")
	}
	p.TemplateMixBy = nullable.NewNullableWithValue(TemplateMixDepartment)
	if p.TemplateMixInEffect() != TemplateMixDepartment {
		t.Fatal("random order is the default and mixes")
	}
	p.OrderMode = nullable.NewNullableWithValue(ProgramOrderSequential)
	if p.TemplateMixInEffect() != "" {
		t.Fatal("same order must not mix")
	}
}

// TestProgramValidateLanguageRouting checks Validate holds the language
// field to its own allowlist apart from the mix fields, needs a default, and
// stores the route keys normalized
func TestProgramValidateLanguageRouting(t *testing.T) {
	for _, v := range []string{LanguageRouteCountry, LanguageRouteMisc, LanguageRouteDepartment} {
		p := validProgram()
		p.LanguageRouteBy = nullable.NewNullableWithValue(v)
		p.LanguageDefault = nullable.NewNullableWithValue("en")
		if err := p.Validate(); err != nil {
			t.Fatalf("%q should be allowed to pick languages: %v", v, err)
		}
	}
	for _, v := range []string{"email", "Country", "country; DROP TABLE programs"} {
		p := validProgram()
		p.LanguageRouteBy = nullable.NewNullableWithValue(v)
		p.LanguageDefault = nullable.NewNullableWithValue("en")
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "languageRouteBy") {
			t.Fatalf("%q should be refused on languageRouteBy, got %v", v, err)
		}
	}
	p := validProgram()
	p.LanguageRouteBy = nullable.NewNullableWithValue(LanguageRouteCountry)
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "languageDefault") {
		t.Fatalf("a default is required, got %v", err)
	}
	p.LanguageDefault = nullable.NewNullableWithValue(" EN ")
	p.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{"  Germany ": "DE"})
	if err := p.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.LanguageDefault.MustGet() != "en" || p.LanguageRoutes.MustGet()["germany"] != "de" {
		t.Fatalf("default and routes must be normalized, got %q %v", p.LanguageDefault.MustGet(), p.LanguageRoutes.MustGet())
	}
	m := p.ToDBMap()
	if m["language_route_by"] != LanguageRouteCountry || m["language_default"] != "en" {
		t.Fatalf("the routing must be written, got %v", m)
	}
	if _, ok := m["language_routes"]; ok {
		t.Fatal("routes are child rows, not a column")
	}
}

// TestProgramValidateNullEntries checks a null entry in a list of a request
// is a validation error, not a crash
func TestProgramValidateNullEntries(t *testing.T) {
	cases := map[string]func(p *Program){
		"step":      func(p *Program) { p.Steps = []*ProgramStep{step(nil), nil} },
		"group":     func(p *Program) { p.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{nil}) },
		"allowDeny": func(p *Program) { p.AllowDenyIDs = nullable.NewNullableWithValue([]*uuid.UUID{nil}) },
		"webhook":   func(p *Program) { p.Webhooks = nullable.NewNullableWithValue([]*CampaignWebhook{nil}) },
	}
	for name, set := range cases {
		p := validProgram()
		set(p)
		if err := p.Validate(); err == nil {
			t.Fatalf("%s: a null entry must be refused", name)
		}
		if err := p.ValidateCreate(); err == nil {
			t.Fatalf("%s: a null entry must be refused on create", name)
		}
	}
	if err := ValidateProgramRecipientGroupCount([]*uuid.UUID{nil}); err == nil {
		t.Fatalf("a null group must be refused")
	}
	var nilStep *ProgramStep
	if err := nilStep.Validate(); err == nil {
		t.Fatalf("a null step must be refused")
	}
}

// TestProgramSkipRangesLeaveASendDay checks skip ranges that cover every day
// together, or every sending day, are refused on save, on create and on an
// update that changes only the ranges or only the window
func TestProgramSkipRangesLeaveASendDay(t *testing.T) {
	halves := []ProgramBlackout{
		{From: "2026-01-01", To: "2026-06-30", Yearly: true},
		{From: "2026-07-01", To: "2026-12-31", Yearly: true},
	}
	p := validProgram()
	p.BlackoutDates = nullable.NewNullableWithValue(halves)
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "no day to send on") {
		t.Fatalf("two yearly halves must be refused, got %v", err)
	}
	// one free day a year is enough
	open := []ProgramBlackout{
		{From: "2026-01-01", To: "2026-06-30", Yearly: true},
		{From: "2026-07-02", To: "2026-12-31", Yearly: true},
	}
	p.BlackoutDates = nullable.NewNullableWithValue(open)
	if err := p.Validate(); err != nil {
		t.Fatalf("a free day a year must be allowed, got %v", err)
	}
	// the free day must also be a sending day: Jul 1 2026 is a Wednesday, a
	// window of Mondays only still meets it on a Monday in a later year
	mondays := windowProgram("UTC", "09:00", "17:00")
	mondays.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(1 << 1))
	mondays.BlackoutDates = nullable.NewNullableWithValue(open)
	if err := mondays.Validate(); err != nil {
		t.Fatalf("Jul 1 falls on a Monday in a later year, got %v", err)
	}
	// a window of weekends with the yearly halves leaves no day either
	now := time.Now().UTC()
	weekend := windowProgram("UTC", "09:00", "17:00")
	weekend.ConstraintWeekDays = nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(1 | 1<<6))
	weekend.BlackoutDates = nullable.NewNullableWithValue(halves)
	if err := weekend.Validate(); err == nil {
		t.Fatal("yearly halves with a window must be refused")
	}
	// an update that sends only the window checks the stored ranges
	stored := validProgram()
	stored.BlackoutDates = nullable.NewNullableWithValue(open)
	update := &Program{ConstraintWeekDays: nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(1 << 3))}
	update.ConstraintStartTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00"))
	update.ConstraintEndTime = nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00"))
	if err := update.ValidateUpdate(stored, now); err != nil {
		t.Fatalf("Jul 1 is a Wednesday in some year, got %v", err)
	}
	// an update that sends only the ranges checks the stored window
	storedWindow := windowProgram("UTC", "09:00", "17:00")
	only := &Program{BlackoutDates: nullable.NewNullableWithValue(halves)}
	if err := only.ValidateUpdate(storedWindow, now); err == nil || !strings.Contains(err.Error(), "no day to send on") {
		t.Fatalf("an update to ranges that cover every day must be refused, got %v", err)
	}
}
