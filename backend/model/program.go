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

// the bounds of the program timing in minutes, the same the create and edit
// form offers. A month is 30 days.
const (
	// ProgramMinIntervalMinutes is the shortest time between templates, one day
	ProgramMinIntervalMinutes = 24 * 60
	// ProgramMaxIntervalMinutes is the longest time between templates, three months
	ProgramMaxIntervalMinutes = 3 * 30 * 24 * 60
	// ProgramMaxSpreadMinutes is the longest spread, three months
	ProgramMaxSpreadMinutes = 3 * 30 * 24 * 60
	// ProgramMaxNewMemberDelayMinutes is the longest wait before the first
	// template to a new member, one month
	ProgramMaxNewMemberDelayMinutes = 30 * 24 * 60
)

// ProgramDefaultIntervalMinutes is the time between rounds when a program sets
// none, two weeks in minutes
const ProgramDefaultIntervalMinutes = 14 * 24 * 60

// ProgramMaxSteps is the most steps a program may have
const ProgramMaxSteps = 200

// ProgramMaxRecipientGroups is the most recipient groups a program may use
const ProgramMaxRecipientGroups = 50

// ProgramMaxAllowDeny and ProgramMaxWebhooks bound the lists a program can set
const (
	ProgramMaxAllowDeny = 50
	ProgramMaxWebhooks  = 20
)

// ValidateProgramFilterPages applies the same page rules a campaign has: an
// evasion page needs a deny page and so does an allow or deny list.
func ValidateProgramFilterPages(hasDenyPage bool, hasEvasionPage bool, allowDenyCount int) error {
	if hasEvasionPage && !hasDenyPage {
		return validate.WrapErrorWithField(errors.New("The evasion page needs a deny page"), "evasionPageID")
	}
	if allowDenyCount > 0 && !hasDenyPage {
		return validate.WrapErrorWithField(errors.New("Filtering needs a deny page"), "denyPageID")
	}
	return nil
}

// ValidateProgramRecipientGroupCount checks a list of group ids is not longer
// than ProgramMaxRecipientGroups
func ValidateProgramRecipientGroupCount(ids []*uuid.UUID) error {
	if len(ids) > ProgramMaxRecipientGroups {
		return validate.WrapErrorWithField(errors.Errorf("A program can have at most %d groups", ProgramMaxRecipientGroups), "recipientGroupIDs")
	}
	for _, id := range ids {
		if id == nil {
			return validate.WrapErrorWithField(errors.New("group entry cannot be null"), "recipientGroupIDs")
		}
	}
	return nil
}

// ValidateProgramDataSaving refuses saving submitted data on an anonymous
// program, the same rule an anonymous campaign has
func ValidateProgramDataSaving(anonymous bool, saveSubmittedData bool) error {
	if anonymous && saveSubmittedData {
		return validate.WrapErrorWithField(errors.New("submitted data cannot be saved on an anonymous program"), "saveSubmittedData")
	}
	return nil
}

// ProgramAllowedOrderModes are the order modes a program may use.
var ProgramAllowedOrderModes = []string{ProgramOrderSequential, ProgramOrderShuffle}

// ProgramAllowedLoopModes are the loop modes a program may use.
var ProgramAllowedLoopModes = []string{ProgramLoopComplete, ProgramLoopLoop}

// ProgramAllowedTemplateMixBy are the recipient fields a program may mix
// templates by. Empty is off.
var ProgramAllowedTemplateMixBy = []string{"", TemplateMixDepartment, TemplateMixPosition, TemplateMixCity}

// IsValidProgramTemplateMixBy reports whether v is in ProgramAllowedTemplateMixBy
func IsValidProgramTemplateMixBy(v string) bool {
	for _, allowed := range ProgramAllowedTemplateMixBy {
		if v == allowed {
			return true
		}
	}
	return false
}

// Program is a continuous, self running sequence of phishing and training content.
type Program struct {
	ID        nullable.Nullable[uuid.UUID]   `json:"id"`
	CreatedAt *time.Time                     `json:"createdAt"`
	UpdatedAt *time.Time                     `json:"updatedAt"`
	Name      nullable.Nullable[vo.String64] `json:"name"`
	CompanyID nullable.Nullable[uuid.UUID]   `json:"companyID"`

	ClosedAt nullable.Nullable[time.Time] `json:"closedAt"`
	PausedAt nullable.Nullable[time.Time] `json:"pausedAt"`
	// ClosingAt and DeletingAt are set while a close or a delete is under way,
	// see database.Program. Only the service writes them.
	ClosingAt  *time.Time `json:"closingAt"`
	DeletingAt *time.Time `json:"deletingAt"`
	// ReplanPendingAt and ReplanOldEndAt mark an edit whose people are not all
	// planned again yet, see database.Program. Only the service writes them.
	ReplanPendingAt *time.Time `json:"-"`
	ReplanOldEndAt  *time.Time `json:"-"`

	OrderMode nullable.Nullable[string] `json:"orderMode"`
	LoopMode  nullable.Nullable[string] `json:"loopMode"`
	// TemplateMixBy is the recipient field templates are mixed by in random
	// order, one of ProgramAllowedTemplateMixBy. Empty is off. It is stored
	// for sequential order too but only used in random order.
	TemplateMixBy nullable.Nullable[string] `json:"templateMixBy"`

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
	// AnonymizeAt and DataAnonymizeAt schedule the anonymize actions, see
	// programAnonymize.go. AnonymizedAt and DataAnonymizedAt are read only and
	// tell when they ran.
	AnonymizeAt     nullable.Nullable[time.Time] `json:"anonymizeAt"`
	DataAnonymizeAt nullable.Nullable[time.Time] `json:"dataAnonymizeAt"`
	AnonymizedAt    *time.Time                   `json:"anonymizedAt"`
	// AnonymizingAt is set while an Anonymize All has begun and not finished
	AnonymizingAt    *time.Time `json:"anonymizingAt"`
	DataAnonymizedAt *time.Time `json:"dataAnonymizedAt"`
	// DataAnonymizingAt is set while an Anonymize Data run by hand has not
	// finished. It is read only.
	DataAnonymizingAt *time.Time `json:"dataAnonymizingAt"`
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
	// SaveSubmittedData keeps what people submit on the hidden campaigns, the
	// same as the campaign setting. Never on for an anonymous program.
	SaveSubmittedData nullable.Nullable[bool] `json:"saveSubmittedData"`

	// automated report email settings, see programReportSchedule.go
	ReportEnabled  nullable.Nullable[bool]   `json:"reportEnabled"`
	ReportInterval nullable.Nullable[string] `json:"reportInterval"`
	ReportPeriod   nullable.Nullable[string] `json:"reportPeriod"`
	// ReportLastPeriodEnd is the end of the last period a report was sent for,
	// or the baseline set when reports were turned on. Read only.
	ReportLastPeriodEnd *time.Time `json:"reportLastPeriodEnd"`
	// ReportNextAt is when the next automated report is sent, set on reads
	ReportNextAt *time.Time `json:"reportNextAt"`
	// ReportBaseline is stored as the last reported end on a write, set by
	// ApplyReportBaseline and never read from a request
	ReportBaseline *time.Time `json:"-"`

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
	// Stopped is the number of people still active when the program closed
	Stopped int64 `json:"stopped"`
	// EnrolledTotal is the number of people the program ever enrolled, also
	// those who completed, were removed or were still active at the close
	EnrolledTotal int64 `json:"enrolledTotal"`
	// NextSendAt is the earliest upcoming send of an active person
	NextSendAt *time.Time `json:"nextSendAt"`
	// Names names the templates, groups, lists, pages, webhooks and company
	// the program refers to, filled on a single program read
	Names *ProgramNames `json:"names,omitempty"`
	// Resuming is set while a resume runs in the background, AnonymizeRunning
	// while an Anonymize All or Anonymize Data runs, filled on a program read
	Resuming         bool `json:"resuming"`
	AnonymizeRunning bool `json:"anonymizeRunning"`
	// Enrolling is set while the members are enrolled in the background
	// after Start
	Enrolling bool `json:"enrolling"`
	// ActionFailure is the last resume or Anonymize Data that failed in the
	// background, filled on a program read. It is kept in memory until the
	// action runs again.
	ActionFailure *ProgramActionFailure `json:"actionFailure,omitempty"`
}

// ProgramActionFailure is an action that ran in the background and failed.
// Action is resume or anonymizeData.
type ProgramActionFailure struct {
	Action string    `json:"action"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// ProgramNamedRef is the id and name of something a program refers to
type ProgramNamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProgramNames names what a program refers to, so a page shows the names
// without reading each one. Something that no longer exists is left out.
type ProgramNames struct {
	// Templates maps a step template id to its name
	Templates map[string]string `json:"templates"`
	Groups    []ProgramNamedRef `json:"groups"`
	AllowDeny []ProgramNamedRef `json:"allowDeny"`
	// AllowDenyAllowed is true when the lists allow only what they hold, false
	// when they block it, null without lists
	AllowDenyAllowed *bool             `json:"allowDenyAllowed"`
	DenyPage         *ProgramNamedRef  `json:"denyPage"`
	EvasionPage      *ProgramNamedRef  `json:"evasionPage"`
	Webhooks         []ProgramNamedRef `json:"webhooks"`
	Company          *ProgramNamedRef  `json:"company"`
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
	if v, err := p.TemplateMixBy.Get(); err == nil && !IsValidProgramTemplateMixBy(v) {
		return validate.WrapErrorWithField(errors.New("unknown field to mix templates by"), "templateMixBy")
	}
	if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() {
		if p.StepIntervalMinutes.MustGet() < ProgramMinIntervalMinutes {
			return validate.WrapErrorWithField(errors.New("The time between templates must be at least one day"), "stepIntervalMinutes")
		}
		if p.StepIntervalMinutes.MustGet() > ProgramMaxIntervalMinutes {
			return validate.WrapErrorWithField(errors.New("The time between templates can be at most three months"), "stepIntervalMinutes")
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
		if spread > ProgramMaxSpreadMinutes {
			return validate.WrapErrorWithField(errors.New("The spread can be at most three months"), "spreadMinutes")
		}
		if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() &&
			spread > p.StepIntervalMinutes.MustGet() {
			return validate.WrapErrorWithField(errors.New("The spread cannot be longer than the time between templates"), "spreadMinutes")
		}
	}
	if p.NewMemberDelayMinutes.IsSpecified() && !p.NewMemberDelayMinutes.IsNull() {
		delay := p.NewMemberDelayMinutes.MustGet()
		if delay < 0 {
			return validate.WrapErrorWithField(errors.New("First template to new members cannot be negative"), "newMemberDelayMinutes")
		}
		if delay > ProgramMaxNewMemberDelayMinutes {
			return validate.WrapErrorWithField(errors.New("First template to new members can be at most one month"), "newMemberDelayMinutes")
		}
	}
	if p.TimeZone.IsSpecified() && !p.TimeZone.IsNull() {
		// the same zones a campaign accepts, so the server zone "Local" and other
		// names a browser does not know are refused
		if _, err := vo.NewCampaignTimeZone(p.TimeZone.MustGet()); err != nil {
			return validate.WrapErrorWithField(errors.New("unknown time zone"), "timeZone")
		}
	}
	if err := ValidateProgramDataSaving(p.IsAnonymousOn(), p.SaveSubmittedDataOn()); err != nil {
		return err
	}
	if p.RecipientGroupIDs.IsSpecified() && !p.RecipientGroupIDs.IsNull() {
		if err := ValidateProgramRecipientGroupCount(p.RecipientGroupIDs.MustGet()); err != nil {
			return err
		}
	}
	if err := p.validateSettingLists(); err != nil {
		return err
	}
	if err := p.validateWindowClock(time.Now()); err != nil {
		return err
	}
	if err := p.validateSchedule(); err != nil {
		return err
	}
	if err := p.validateReportSchedule(); err != nil {
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
		if s == nil {
			return validate.WrapErrorWithField(errors.New("template entry cannot be null"), "steps")
		}
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

// validateSettingLists checks the sizes and entries of the allow or deny list
// and webhook lists of a request
func (p *Program) validateSettingLists() error {
	if ids, err := p.AllowDenyIDs.Get(); err == nil {
		if len(ids) > ProgramMaxAllowDeny {
			return validate.WrapErrorWithField(errors.New("too many allow or deny lists"), "allowDenyIDs")
		}
		for _, id := range ids {
			if id == nil {
				return validate.WrapErrorWithField(errors.New("Filter list not found"), "allowDenyIDs")
			}
		}
	}
	if webhooks, err := p.Webhooks.Get(); err == nil {
		if len(webhooks) > ProgramMaxWebhooks {
			return validate.WrapErrorWithField(errors.New("too many webhooks"), "webhooks")
		}
		for _, wh := range webhooks {
			if wh == nil {
				return validate.WrapErrorWithField(errors.New("webhook entry cannot be null"), "webhooks")
			}
			if err := wh.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateCreate checks a new program: the Validate rules, a spread no
// longer than the time between templates it will have, also when that is the
// default, at least one template and at least one recipient group
func (p *Program) ValidateCreate() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.SpreadOrDefault() > p.IntervalOrDefault() {
		return validate.WrapErrorWithField(errors.New("The spread cannot be longer than the time between templates"), "spreadMinutes")
	}
	if len(p.Steps) == 0 {
		return validate.WrapErrorWithField(errors.New("Add at least one template"), "steps")
	}
	if ids, err := p.RecipientGroupIDs.Get(); err != nil || len(ids) == 0 {
		return validate.WrapErrorWithField(errors.New("a program must have at least one recipient group"), "recipientGroupIDs")
	}
	return nil
}

// ValidateUpdate checks the rules an update must keep together with the
// stored program: the spread may not outgrow the interval when only one of
// them changes, a group list that is sent may not be empty and the window
// bounds must exist on every sending day in the zone the program will have.
// It runs after Validate.
func (p *Program) ValidateUpdate(existing *Program, now time.Time) error {
	merged := &Program{
		StepIntervalMinutes: existing.StepIntervalMinutes,
		SpreadMinutes:       existing.SpreadMinutes,
		TimeZone:            existing.TimeZone,
		ConstraintWeekDays:  existing.ConstraintWeekDays,
		ConstraintStartTime: existing.ConstraintStartTime,
		ConstraintEndTime:   existing.ConstraintEndTime,
	}
	if p.StepIntervalMinutes.IsSpecified() && !p.StepIntervalMinutes.IsNull() {
		merged.StepIntervalMinutes = p.StepIntervalMinutes
	}
	if p.SpreadMinutes.IsSpecified() && !p.SpreadMinutes.IsNull() {
		merged.SpreadMinutes = p.SpreadMinutes
	}
	if merged.SpreadOrDefault() > merged.IntervalOrDefault() {
		return validate.WrapErrorWithField(errors.New("The spread cannot be longer than the time between templates"), "spreadMinutes")
	}
	if p.RecipientGroupIDs.IsSpecified() && !p.RecipientGroupIDs.IsNull() && len(p.RecipientGroupIDs.MustGet()) == 0 {
		return validate.WrapErrorWithField(errors.New("a program must have at least one recipient group"), "recipientGroupIDs")
	}
	windowChanged := p.ConstraintWeekDays.IsSpecified() || p.ConstraintStartTime.IsSpecified() || p.ConstraintEndTime.IsSpecified()
	// each window field is stored on its own, so an update that sends only some
	// of them keeps the stored rest
	if p.ConstraintWeekDays.IsSpecified() {
		merged.ConstraintWeekDays = p.ConstraintWeekDays
	}
	if p.ConstraintStartTime.IsSpecified() {
		merged.ConstraintStartTime = p.ConstraintStartTime
	}
	if p.ConstraintEndTime.IsSpecified() {
		merged.ConstraintEndTime = p.ConstraintEndTime
	}
	// a window is stored whole or not at all, a half window would be ignored
	// by the scheduler while its stale columns stay
	if windowChanged {
		set := 0
		if !merged.ConstraintWeekDays.IsNull() && merged.ConstraintWeekDays.IsSpecified() {
			set++
		}
		if !merged.ConstraintStartTime.IsNull() && merged.ConstraintStartTime.IsSpecified() {
			set++
		}
		if !merged.ConstraintEndTime.IsNull() && merged.ConstraintEndTime.IsSpecified() {
			set++
		}
		if set != 0 && set != 3 {
			return validate.WrapErrorWithField(errors.New("set the sending days, start and end together"), "constraintStartTime")
		}
	}
	zoneChanged := p.TimeZone.IsSpecified() && !p.TimeZone.IsNull()
	if zoneChanged {
		merged.TimeZone = p.TimeZone
	}
	merged.BlackoutDates = existing.BlackoutDates
	if p.BlackoutDates.IsSpecified() {
		merged.BlackoutDates = p.BlackoutDates
	}
	if windowChanged || zoneChanged || p.BlackoutDates.IsSpecified() {
		if err := merged.validateSendDay(now); err != nil {
			return err
		}
	}
	if windowChanged || zoneChanged {
		return merged.validateWindowClock(now)
	}
	return nil
}

// SetReadState sets the fields a read derives from the time: the status and
// when the next automated report is sent
func (p *Program) SetReadState(now time.Time) {
	p.Status = p.StatusAt(now)
	p.ReportNextAt = p.ReportNextAtTime(now)
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
	if p.TemplateMixBy.IsSpecified() {
		m["template_mix_by"] = ""
		if v, err := p.TemplateMixBy.Get(); err == nil {
			m["template_mix_by"] = v
		}
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
	p.anonymizeToDBMap(m)
	p.reportScheduleToDBMap(m)
	if p.IsAnonymous.IsSpecified() && !p.IsAnonymous.IsNull() {
		m["is_anonymous"] = p.IsAnonymous.MustGet()
	}
	if p.SaveSubmittedData.IsSpecified() && !p.SaveSubmittedData.IsNull() {
		m["save_submitted_data"] = p.SaveSubmittedData.MustGet()
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

// TemplateMixInEffect returns the recipient field templates are mixed by, or
// empty when mixing is off. Only random order mixes, sequential order sends
// everyone the same template at each step.
func (p *Program) TemplateMixInEffect() string {
	if p.OrderModeOrDefault() != ProgramOrderShuffle {
		return ""
	}
	if v, err := p.TemplateMixBy.Get(); err == nil {
		return v
	}
	return ""
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

// SaveSubmittedDataOn reports whether the program saves submitted data, false
// when unset
func (p *Program) SaveSubmittedDataOn() bool {
	v, err := p.SaveSubmittedData.Get()
	return err == nil && v
}
