package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// program order modes
const (
	ProgramOrderSequential = "sequential"
	ProgramOrderShuffle    = "shuffle"
)

// program loop modes
const (
	ProgramLoopComplete = "complete"
	ProgramLoopLoop     = "loop"
)

// program enrollment statuses
const (
	ProgramEnrollmentActive    = "active"
	ProgramEnrollmentExited    = "exited"
	ProgramEnrollmentCompleted = "completed"
	// ProgramEnrollmentStopped is a person who was still active when the
	// program closed
	ProgramEnrollmentStopped = "stopped"
)

// ProgramMaxMinutes is the longest interval, spread or delay a program may use,
// three years in minutes
const ProgramMaxMinutes = 3 * 365 * 24 * 60

// ProgramDefaultIntervalMinutes is the time between rounds when a program sets
// none, two weeks in minutes
const ProgramDefaultIntervalMinutes = 14 * 24 * 60

// ProgramMaxSteps is the most steps a program may have
const ProgramMaxSteps = 200

// ProgramMaxRecipientGroups is the most recipient groups a program may use
const ProgramMaxRecipientGroups = 50

// ValidateProgramRecipientGroupCount checks a list of group ids is not longer
// than ProgramMaxRecipientGroups
func ValidateProgramRecipientGroupCount(ids []*uuid.UUID) error {
	if len(ids) > ProgramMaxRecipientGroups {
		return validate.WrapErrorWithField(errors.Errorf("A program can have at most %d groups", ProgramMaxRecipientGroups), "recipientGroupIDs")
	}
	return nil
}

// ProgramAllowedOrderModes are the order modes a program may use.
var ProgramAllowedOrderModes = []string{ProgramOrderSequential, ProgramOrderShuffle}

// ProgramAllowedLoopModes are the loop modes a program may use.
var ProgramAllowedLoopModes = []string{ProgramLoopComplete, ProgramLoopLoop}

// Program is a continuous, self running sequence of phishing and training content.
type Program struct {
	ID        nullable.Nullable[uuid.UUID]   `json:"id"`
	CreatedAt *time.Time                     `json:"createdAt"`
	UpdatedAt *time.Time                     `json:"updatedAt"`
	Name      nullable.Nullable[vo.String64] `json:"name"`
	CompanyID nullable.Nullable[uuid.UUID]   `json:"companyID"`

	ClosedAt nullable.Nullable[time.Time] `json:"closedAt"`
	PausedAt nullable.Nullable[time.Time] `json:"pausedAt"`

	OrderMode nullable.Nullable[string] `json:"orderMode"`
	LoopMode  nullable.Nullable[string] `json:"loopMode"`

	StepIntervalMinutes   nullable.Nullable[int]    `json:"stepIntervalMinutes"`
	SpreadMinutes         nullable.Nullable[int]    `json:"spreadMinutes"`
	NewMemberDelayMinutes nullable.Nullable[int]    `json:"newMemberDelayMinutes"`
	TimeZone              nullable.Nullable[string] `json:"timeZone"`
	// StartedAt is set by the scheduler on the first enrollment pass
	StartedAt *time.Time `json:"startedAt"`
	// StartAt is when the program may start, null for a draft
	StartAt nullable.Nullable[time.Time] `json:"startAt"`
	// EndAt is when the program closes on its own, null runs until closed
	EndAt nullable.Nullable[time.Time] `json:"endAt"`
	// EndedAutomatically is set on reads of a program closed by its end date
	EndedAutomatically bool `json:"endedAutomatically"`
	// Draft on create or update keeps the program from starting, start_at stays null
	Draft *bool `json:"draft,omitempty"`
	// BlackoutDates are day ranges on which nothing is sent
	BlackoutDates nullable.Nullable[[]ProgramBlackout] `json:"blackoutDates"`
	// Status is draft, scheduled, active, paused or closed, set on reads
	Status string `json:"status"`

	ConstraintWeekDays  nullable.Nullable[vo.CampaignWeekDays]       `json:"constraintWeekDays"`
	ConstraintStartTime nullable.Nullable[vo.CampaignTimeConstraint] `json:"constraintStartTime"`
	ConstraintEndTime   nullable.Nullable[vo.CampaignTimeConstraint] `json:"constraintEndTime"`

	// IsAnonymous hides who did what. Every count then waits until the sends
	// and actions are at least three whole days old, small groups are withheld
	// and reports cover whole calendar months. It protects against casual
	// viewing, not against an admin who controls the recipients and groups,
	// who can still narrow a group down to learn about one person.
	IsAnonymous nullable.Nullable[bool] `json:"isAnonymous"`

	// bot filtering and webhooks, copied to every hidden campaign. A specified
	// null or empty list clears them on update, unspecified keeps them.
	AllowDenyIDs  nullable.Nullable[[]*uuid.UUID]       `json:"allowDenyIDs"`
	DenyPageID    nullable.Nullable[uuid.UUID]          `json:"denyPageID"`
	EvasionPageID nullable.Nullable[uuid.UUID]          `json:"evasionPageID"`
	Webhooks      nullable.Nullable[[]*CampaignWebhook] `json:"webhooks"`

	Steps             []*ProgramStep                  `json:"steps"`
	RecipientGroups   []*RecipientGroup               `json:"recipientGroups"`
	RecipientGroupIDs nullable.Nullable[[]*uuid.UUID] `json:"recipientGroupIDs,omitempty"`
	Company           *Company                        `json:"company"`

	// Warnings lists problems that stop the program from running as set up,
	// filled on a single program read
	Warnings []string `json:"warnings,omitempty"`
	// NeedsAttention is set on a list read when the program has a problem
	NeedsAttention bool `json:"needsAttention"`
	// AttentionReason says in plain words why the program needs attention
	AttentionReason string `json:"attentionReason,omitempty"`
	// SimulationCount is the number of simulations, set on a list read
	SimulationCount int `json:"simulationCount"`
	// Enrolled is the number of people currently active in the program
	Enrolled int64 `json:"enrolled"`
	// NextSendAt is the earliest upcoming send of an active person
	NextSendAt *time.Time `json:"nextSendAt"`
}

// Validate checks the program request has a valid state.
func (p *Program) Validate() error {
	if err := validate.NullableFieldRequired("name", p.Name); err != nil {
		return err
	}
	if p.OrderMode.IsSpecified() && !p.OrderMode.IsNull() {
		if err := validate.ErrorIfNotContains(ProgramAllowedOrderModes, p.OrderMode.MustGet()); err != nil {
			return validate.WrapErrorWithField(err, "orderMode")
		}
	}
	if p.LoopMode.IsSpecified() && !p.LoopMode.IsNull() {
		if err := validate.ErrorIfNotContains(ProgramAllowedLoopModes, p.LoopMode.MustGet()); err != nil {
			return validate.WrapErrorWithField(err, "loopMode")
		}
	}
	if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() {
		if p.StepIntervalMinutes.MustGet() < 1 {
			return validate.WrapErrorWithField(errors.New("The time between emails must be at least one minute"), "stepIntervalMinutes")
		}
		if p.StepIntervalMinutes.MustGet() > ProgramMaxMinutes {
			return validate.WrapErrorWithField(errors.New("The time between emails can be at most three years"), "stepIntervalMinutes")
		}
	}
	// a delivery window must set all three of weekdays, start and end together,
	// and the end must be after the start.
	if (p.ConstraintWeekDays.IsSpecified() && !p.ConstraintWeekDays.IsNull()) ||
		(p.ConstraintStartTime.IsSpecified() && !p.ConstraintStartTime.IsNull()) ||
		(p.ConstraintEndTime.IsSpecified() && !p.ConstraintEndTime.IsNull()) {
		if err := validate.NullableFieldRequired("constraintWeekDays", p.ConstraintWeekDays); err != nil {
			return err
		}
		if p.ConstraintWeekDays.MustGet().Count() == 0 {
			return validate.WrapErrorWithField(errors.New("Pick at least one sending day"), "constraintWeekDays")
		}
		if err := validate.NullableFieldRequired("constraintStartTime", p.ConstraintStartTime); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("constraintEndTime", p.ConstraintEndTime); err != nil {
			return err
		}
		start := p.ConstraintStartTime.MustGet()
		end := p.ConstraintEndTime.MustGet()
		if !end.IsAfter(start) {
			return validate.WrapErrorWithField(errors.New("end time must be after start time"), "constraintEndTime")
		}
	}
	if p.SpreadMinutes.IsSpecified() && !p.SpreadMinutes.IsNull() {
		spread := p.SpreadMinutes.MustGet()
		if spread < 0 {
			return validate.WrapErrorWithField(errors.New("spread cannot be negative"), "spreadMinutes")
		}
		if spread > ProgramMaxMinutes {
			return validate.WrapErrorWithField(errors.New("spread can be at most three years"), "spreadMinutes")
		}
		if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() &&
			spread > p.StepIntervalMinutes.MustGet() {
			return validate.WrapErrorWithField(errors.New("The spread cannot be longer than the time between emails"), "spreadMinutes")
		}
	}
	if p.NewMemberDelayMinutes.IsSpecified() && !p.NewMemberDelayMinutes.IsNull() {
		delay := p.NewMemberDelayMinutes.MustGet()
		if delay < 0 {
			return validate.WrapErrorWithField(errors.New("First email to new members cannot be negative"), "newMemberDelayMinutes")
		}
		if delay > ProgramMaxMinutes {
			return validate.WrapErrorWithField(errors.New("First email to new members can be at most three years"), "newMemberDelayMinutes")
		}
	}
	if p.TimeZone.IsSpecified() && !p.TimeZone.IsNull() {
		// the same zones a campaign accepts, so the server zone "Local" and other
		// names a browser does not know are refused
		if _, err := vo.NewCampaignTimeZone(p.TimeZone.MustGet()); err != nil {
			return validate.WrapErrorWithField(errors.New("unknown time zone"), "timeZone")
		}
	}
	if p.RecipientGroupIDs.IsSpecified() && !p.RecipientGroupIDs.IsNull() {
		if err := ValidateProgramRecipientGroupCount(p.RecipientGroupIDs.MustGet()); err != nil {
			return err
		}
	}
	if err := p.validateSchedule(); err != nil {
		return err
	}
	// steps, when present, must each be valid, with distinct positions. A step
	// without a position is stored at its
	// index, so that index takes part in the check. Positions are never
	// negative, the store moves old steps to negative positions while it saves.
	if len(p.Steps) > ProgramMaxSteps {
		return validate.WrapErrorWithField(errors.Errorf("A program can have at most %d templates", ProgramMaxSteps), "steps")
	}
	positions := map[int]bool{}
	for i, s := range p.Steps {
		if err := s.Validate(); err != nil {
			return err
		}
		position := i
		if s.Position.IsSpecified() && !s.Position.IsNull() {
			position = s.Position.MustGet()
		}
		if position < 0 {
			return validate.WrapErrorWithField(errors.New("Template order is invalid"), "steps")
		}
		if positions[position] {
			return validate.WrapErrorWithField(errors.New("Template order is invalid"), "steps")
		}
		positions[position] = true
	}
	return nil
}

// ToDBMap converts the storable program fields to a column map. Steps and
// recipient groups are stored through their own tables, not here.
func (p *Program) ToDBMap() map[string]any {
	m := map[string]any{}
	if p.Name.IsSpecified() {
		name := p.Name.MustGet()
		m["name"] = name.String()
	}
	if p.CompanyID.IsSpecified() {
		if p.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = p.CompanyID.MustGet()
		}
	}
	if p.OrderMode.IsSpecified() && !p.OrderMode.IsNull() {
		m["order_mode"] = p.OrderMode.MustGet()
	}
	if p.LoopMode.IsSpecified() && !p.LoopMode.IsNull() {
		m["loop_mode"] = p.LoopMode.MustGet()
	}
	if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() {
		m["step_interval_minutes"] = p.StepIntervalMinutes.MustGet()
	}
	if p.ConstraintWeekDays.IsSpecified() {
		if p.ConstraintWeekDays.IsNull() {
			m["constraint_week_days"] = nil
		} else {
			wd := p.ConstraintWeekDays.MustGet()
			m["constraint_week_days"] = wd.Int()
		}
	}
	if p.ConstraintStartTime.IsSpecified() {
		if p.ConstraintStartTime.IsNull() {
			m["constraint_start_time"] = nil
		} else {
			t := p.ConstraintStartTime.MustGet()
			m["constraint_start_time"] = t.String()
		}
	}
	if p.ConstraintEndTime.IsSpecified() {
		if p.ConstraintEndTime.IsNull() {
			m["constraint_end_time"] = nil
		} else {
			t := p.ConstraintEndTime.MustGet()
			m["constraint_end_time"] = t.String()
		}
	}
	if p.SpreadMinutes.IsSpecified() && !p.SpreadMinutes.IsNull() {
		m["spread_minutes"] = p.SpreadMinutes.MustGet()
	}
	if p.NewMemberDelayMinutes.IsSpecified() && !p.NewMemberDelayMinutes.IsNull() {
		m["new_member_delay_minutes"] = p.NewMemberDelayMinutes.MustGet()
	}
	if p.TimeZone.IsSpecified() && !p.TimeZone.IsNull() {
		m["time_zone"] = p.TimeZone.MustGet()
	}
	p.scheduleToDBMap(m)
	if p.IsAnonymous.IsSpecified() && !p.IsAnonymous.IsNull() {
		m["is_anonymous"] = p.IsAnonymous.MustGet()
	}
	if p.DenyPageID.IsSpecified() {
		m["deny_page_id"] = nil
		if v, err := p.DenyPageID.Get(); err == nil {
			m["deny_page_id"] = v
		}
	}
	if p.EvasionPageID.IsSpecified() {
		m["evasion_page_id"] = nil
		if v, err := p.EvasionPageID.Get(); err == nil {
			m["evasion_page_id"] = v
		}
	}
	return m
}

// OrderModeOrDefault returns the order mode, defaulting to shuffle.
func (p *Program) OrderModeOrDefault() string {
	if p.OrderMode.IsSpecified() && !p.OrderMode.IsNull() {
		return p.OrderMode.MustGet()
	}
	return ProgramOrderShuffle
}

// Location returns the time zone the delivery window is read in, UTC when unset
// or unknown.
func (p *Program) Location() *time.Location {
	if p.TimeZone.IsSpecified() && !p.TimeZone.IsNull() {
		if loc, err := time.LoadLocation(p.TimeZone.MustGet()); err == nil {
			return loc
		}
	}
	return time.UTC
}

// IntervalOrDefault returns the time between a recipient's rounds.
func (p *Program) IntervalOrDefault() time.Duration {
	if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() && p.StepIntervalMinutes.MustGet() > 0 {
		return time.Duration(p.StepIntervalMinutes.MustGet()) * time.Minute
	}
	return ProgramDefaultIntervalMinutes * time.Minute
}

// SpreadOrDefault returns how far into a round a send may land.
func (p *Program) SpreadOrDefault() time.Duration {
	if p.SpreadMinutes.IsSpecified() && !p.SpreadMinutes.IsNull() && p.SpreadMinutes.MustGet() > 0 {
		return time.Duration(p.SpreadMinutes.MustGet()) * time.Minute
	}
	return 0
}

// LoopModeOrDefault returns the loop mode, defaulting to complete.
func (p *Program) LoopModeOrDefault() string {
	if p.LoopMode.IsSpecified() && !p.LoopMode.IsNull() {
		return p.LoopMode.MustGet()
	}
	return ProgramLoopComplete
}

// IsClosed returns true if the program has been closed
func (p *Program) IsClosed() bool {
	return p.ClosedAt.IsSpecified() && !p.ClosedAt.IsNull()
}

// IsAnonymousOn reports whether the program is anonymous, false when unset
func (p *Program) IsAnonymousOn() bool {
	v, err := p.IsAnonymous.Get()
	return err == nil && v
}
