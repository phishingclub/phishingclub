package service

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// Program is the service for programs: their lifecycle, the enrollment scheduler
// that feeds hidden campaigns and reporting.
type Program struct {
	Common
	ProgramRepository           *repository.Program
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
	RecipientRepository         *repository.Recipient
	RecipientGroupRepository    *repository.RecipientGroup
	CampaignTemplateService     *CampaignTemplate
	// used to check the scope of program bot filtering, pages, webhooks and
	// the target company of a clone
	AllowDenyRepository *repository.AllowDeny
	WebhookRepository   *repository.Webhook
	PageRepository      *repository.Page
	CompanyRepository   *repository.Company
	// CampaignService is reused for the send side helpers (lure allocation,
	// scheduled insert, lure/training snapshot, anonymous snapshot).
	CampaignService *Campaign
	// SystemSession authorizes the background scheduler, mirroring how the
	// campaign send batch runs.
	SystemSession *model.Session

	lastMembershipAt time.Time
	lastRetentionAt  time.Time
	// closeFailureAuditAt is when a failed end date close of a program was last
	// audited. Only the scheduler uses it, under the lifecycle lock.
	closeFailureAuditAt map[uuid.UUID]time.Time
	// lifecycleMu keeps a scheduler tick and a close or delete from running at
	// the same time, so no send is added to a program that is being stopped
	lifecycleMu ctxMutex
}

// ctxMutex is a lock whose wait can be given up when a context ends. The zero
// value is an unlocked lock.
type ctxMutex struct {
	once sync.Once
	ch   chan struct{}
}

func (m *ctxMutex) init() {
	m.once.Do(func() { m.ch = make(chan struct{}, 1) })
}

// Lock waits for the lock
func (m *ctxMutex) Lock() {
	m.init()
	m.ch <- struct{}{}
}

// LockContext waits for the lock until ctx ends and returns the context error
// when the lock was not taken
func (m *ctxMutex) LockContext(ctx context.Context) error {
	m.init()
	select {
	case m.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Unlock releases the lock
func (m *ctxMutex) Unlock() {
	<-m.ch
}

// rng returns a random source for one scheduling decision. It is seeded from the
// shared source, which is safe to use from the scheduler and request handlers.
func (s *Program) rng() *rand.Rand {
	return rand.New(rand.NewSource(rand.Int63()))
}

// Create stores a new program with its steps and assigned groups.
func (s *Program) Create(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Program.Create", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if err := program.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	if len(program.Steps) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("Add at least one template"), "steps")
	}
	if !program.RecipientGroupIDs.IsSpecified() || program.RecipientGroupIDs.IsNull() ||
		len(program.RecipientGroupIDs.MustGet()) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("a program must have at least one recipient group"), "recipientGroupIDs")
	}
	if err := s.validateScope(ctx, session, optionalID(program.CompanyID), program.Steps, program.RecipientGroupIDs.MustGet()); err != nil {
		return nil, err
	}
	if err := s.validateProgramSettings(ctx, optionalID(program.CompanyID), nil, program); err != nil {
		return nil, err
	}
	now := time.Now()
	applyStartRequest(program, nil, now)
	if err := program.ValidateEndAt(program.StartAtTime(), true, now); err != nil {
		return nil, err
	}
	id, err := s.ProgramRepository.Insert(ctx, program)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.ProgramRepository.ReplaceSteps(ctx, id, program.Steps); err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.ProgramRepository.ReplaceRecipientGroups(ctx, id, program.RecipientGroupIDs.MustGet()); err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.saveProgramSettings(ctx, id, program); err != nil {
		return nil, err
	}
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)
	return id, nil
}

// UpdateByID updates a program while it runs. Steps keep their ids across an
// edit, so recipients keep their history and only future rounds see the change.
func (s *Program) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Program,
) error {
	ae := NewAuditEvent("Program.UpdateById", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := incoming.Validate(); err != nil {
		return errs.Wrap(err)
	}
	// the closed check and the write must not race a close or a tick
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	existing, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if existing.IsClosed() {
		return errs.NewValidationError(errors.New("a closed program cannot be changed"))
	}
	// a program stays with the company it was created for
	incoming.CompanyID = existing.CompanyID
	// the start can change only until the program has started
	now := time.Now()
	applyStartRequest(incoming, existing, now)
	endChanged, err := validateEndAtUpdate(incoming, existing, now)
	if err != nil {
		return err
	}
	// the spread may not outgrow the interval, also when only one of them changes
	merged := &model.Program{StepIntervalMinutes: existing.StepIntervalMinutes, SpreadMinutes: existing.SpreadMinutes}
	if incoming.StepIntervalMinutes.IsSpecified() && !incoming.StepIntervalMinutes.IsNull() {
		merged.StepIntervalMinutes = incoming.StepIntervalMinutes
	}
	if incoming.SpreadMinutes.IsSpecified() && !incoming.SpreadMinutes.IsNull() {
		merged.SpreadMinutes = incoming.SpreadMinutes
	}
	if merged.SpreadOrDefault() > merged.IntervalOrDefault() {
		return validate.WrapErrorWithField(errors.New("The spread cannot be longer than the time between emails"), "spreadMinutes")
	}
	// anonymity decides how every result is stored, it is fixed once people are
	// enrolled. The enrollments are counted too, as the start mark is stored
	// after the first people are enrolled
	if incoming.IsAnonymous.IsSpecified() && !incoming.IsAnonymous.IsNull() &&
		incoming.IsAnonymousOn() != existing.IsAnonymousOn() {
		enrolled := int64(0)
		if existing.StartedAt == nil {
			enrolled, err = s.ProgramRepository.CountEnrollments(ctx, id)
			if err != nil {
				return errs.Wrap(err)
			}
		}
		if existing.StartedAt != nil || enrolled > 0 {
			return validate.WrapErrorWithField(errors.New("anonymous cannot be changed after the program has started"), "isAnonymous")
		}
	}
	editsSteps := len(incoming.Steps) > 0
	editsGroups := incoming.RecipientGroupIDs.IsSpecified() && !incoming.RecipientGroupIDs.IsNull()
	if editsGroups && len(incoming.RecipientGroupIDs.MustGet()) == 0 {
		return validate.WrapErrorWithField(errors.New("a program must have at least one recipient group"), "recipientGroupIDs")
	}
	groupIDs := []*uuid.UUID{}
	if editsGroups {
		groupIDs = incoming.RecipientGroupIDs.MustGet()
	}
	if err := s.validateScope(ctx, session, optionalID(existing.CompanyID), incoming.Steps, groupIDs); err != nil {
		return err
	}
	if err := s.validateProgramSettings(ctx, optionalID(existing.CompanyID), id, incoming); err != nil {
		return err
	}
	ae.Details["changed"] = s.programChangedFields(ctx, id, incoming)
	if err := s.ProgramRepository.UpdateByID(ctx, id, incoming); err != nil {
		return errs.Wrap(err)
	}
	if programSettingsSpecified(incoming) {
		if err := s.saveProgramSettings(ctx, id, incoming); err != nil {
			return err
		}
		if err := s.syncProgramCampaignSettings(ctx, id); err != nil {
			return err
		}
	}
	if editsSteps {
		if err := s.ProgramRepository.ReplaceSteps(ctx, id, incoming.Steps); err != nil {
			return errs.Wrap(err)
		}
	}
	if editsGroups {
		if err := s.ProgramRepository.ReplaceRecipientGroups(ctx, id, groupIDs); err != nil {
			return errs.Wrap(err)
		}
	}
	// a new end also drops the sends already planned at or after it
	if endChanged {
		if end := incoming.EndAtTime(); end != nil {
			if err := s.ProgramRepository.CancelUnsentFrom(ctx, id, *end); err != nil {
				return errs.Wrap(err)
			}
		}
	}
	// the change is stored, a failed replan only delays when it takes effect
	if err := s.replanAfterEdit(ctx, existing, id, editsSteps); err != nil {
		s.Logger.Errorw("failed to plan program people again after an edit", "programID", id.String(), "error", err)
	}
	ae.Details["id"] = id.String()
	s.AuditLogAuthorized(ae)
	return nil
}

// validateEndAtUpdate checks the end date of an update against the start the
// program will have. It reports whether the request sets a new end.
func validateEndAtUpdate(incoming *model.Program, existing *model.Program, now time.Time) (bool, error) {
	start := existing.StartAtTime()
	if incoming.StartAt.IsSpecified() {
		start = incoming.StartAtTime()
	}
	merged := &model.Program{EndAt: existing.EndAt}
	changed := false
	if incoming.EndAt.IsSpecified() {
		merged.EndAt = incoming.EndAt
		before := existing.EndAtTime()
		after := incoming.EndAtTime()
		changed = after != nil && (before == nil || !before.Equal(*after))
	}
	return changed, merged.ValidateEndAt(start, changed, now)
}

// sameScope reports whether a resource owned by ownerID may be used by a program
// of companyID. Global resources are usable everywhere, company resources only
// by that company.
func sameScope(companyID *uuid.UUID, owner nullable.Nullable[uuid.UUID]) bool {
	if !owner.IsSpecified() || owner.IsNull() {
		return true
	}
	return companyID != nil && *companyID == owner.MustGet()
}

// validateScope checks that the templates and groups a program uses belong to
// its company or are global, so one client's program never phishes another
// client's people. A step list needs at least one template that is not
// training.
func (s *Program) validateScope(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	steps []*model.ProgramStep,
	groupIDs []*uuid.UUID,
) error {
	simulations := 0
	for _, st := range steps {
		templateID := st.CampaignTemplateID.MustGet()
		t, err := s.CampaignTemplateService.GetByID(ctx, session, &templateID, &repository.CampaignTemplateOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("A template does not exist"), "steps")
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !sameScope(companyID, t.CompanyID) {
			return validate.WrapErrorWithField(errors.New("A template belongs to another company"), "steps")
		}
		if v, err := t.IsTraining.Get(); err != nil || !v {
			simulations++
		}
	}
	if len(steps) > 0 && simulations == 0 {
		return validate.WrapErrorWithField(errors.New("Add at least one template that is not training"), "steps")
	}
	for _, groupID := range groupIDs {
		g, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("a selected group does not exist"), "recipientGroupIDs")
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !sameScope(companyID, g.CompanyID) {
			return validate.WrapErrorWithField(errors.New("a selected group belongs to another company"), "recipientGroupIDs")
		}
	}
	return nil
}

// DeleteByID removes a program with its hidden campaigns and all their results.
func (s *Program) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Program.DeleteById", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	ae.Details["id"] = id.String()
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	if _, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{}); err != nil {
		return errs.Wrap(err)
	}
	// the hidden campaigns are only reachable through the program, so they and
	// their results are deleted with it, the same as deleting a campaign.
	// pausing first keeps the scheduler from adding a send while deleting, and
	// pausing the hidden campaigns keeps the sender from sending what is queued
	// when the delete stops half way
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{"paused_at": time.Now().UTC()}); err != nil {
		s.Logger.Errorw("failed to pause program before delete", "error", err)
		return errs.Wrap(err)
	}
	if err := s.setCampaignsPaused(ctx, id, true); err != nil {
		s.Logger.Errorw("failed to pause program campaigns before delete", "error", err)
		return err
	}
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get program campaigns on delete", "error", err)
		return errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		if err := s.ProgramRepository.DeleteProgramCampaignByCampaignID(ctx, campaignID); err != nil {
			s.Logger.Errorw("failed to unlink program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
		if err := s.CampaignService.deleteByID(ctx, session, campaignID, true); err != nil {
			s.Logger.Errorw("failed to delete program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
	}
	if err := s.ProgramRepository.DeleteSettings(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	if err := s.ProgramRepository.DeleteByID(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// GetByID returns a program with its steps and groups.
func (s *Program) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Program, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetById", session)
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.fillProgramSettings(ctx, program); err != nil {
		return nil, err
	}
	program.Warnings = s.healthWarnings(ctx, session, program)
	summaries, err := s.ProgramRepository.GetListSummaries(ctx, []*uuid.UUID{id})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if sum := summaries[*id]; sum != nil {
		program.Enrolled = sum.Enrolled
		if !program.IsClosed() {
			program.NextSendAt = nextSendBeforeEnd(program, sum.NextDueAt)
		}
	}
	return program, nil
}

// CountAudience returns how many distinct, active people are in the given groups,
// for the create and edit forms. Groups from another company are rejected.
func (s *Program) CountAudience(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	groupIDs []*uuid.UUID,
) (int, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.CountAudience", session)
		s.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	if err := model.ValidateProgramRecipientGroupCount(groupIDs); err != nil {
		return 0, err
	}
	if err := s.validateScope(ctx, session, companyID, nil, groupIDs); err != nil {
		return 0, err
	}
	members, err := s.resolveMembership(ctx, &model.Program{
		RecipientGroupIDs: nullable.NewNullableWithValue(groupIDs),
	})
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return len(members), nil
}

// healthWarnings lists what stops a program from running as set up, so the
// operator sees it instead of a step being skipped in silence.
func (s *Program) healthWarnings(ctx context.Context, session *model.Session, program *model.Program) []string {
	warnings := []string{}
	if program.IsClosed() {
		return warnings
	}
	warnings = append(warnings, s.templateWarnings(ctx, session, program)...)
	if s.audienceIsEmpty(ctx, program) {
		warnings = append(warnings, "The groups are empty, so nobody is enrolled.")
	}
	return warnings
}

// audienceIsEmpty reports whether the program has nobody to send to. It counts
// instead of resolving every member: active enrollments first, then the group
// member counts only when nobody is enrolled.
func (s *Program) audienceIsEmpty(ctx context.Context, program *model.Program) bool {
	programID := program.ID.MustGet()
	active, err := s.ProgramRepository.CountActiveEnrollments(ctx, &programID)
	if err != nil || active > 0 {
		return false
	}
	if !program.RecipientGroupIDs.IsSpecified() || program.RecipientGroupIDs.IsNull() {
		return true
	}
	for _, groupID := range program.RecipientGroupIDs.MustGet() {
		count, err := s.RecipientGroupRepository.GetRecipientCount(ctx, groupID)
		if err != nil || count > 0 {
			return false
		}
	}
	return true
}

// templateWarnings lists the steps whose template is gone or cannot be sent
func (s *Program) templateWarnings(ctx context.Context, session *model.Session, program *model.Program) []string {
	warnings := []string{}
	for _, st := range program.Steps {
		templateID := st.CampaignTemplateID.MustGet()
		t, err := s.CampaignTemplateService.GetByID(ctx, session, &templateID, &repository.CampaignTemplateOption{})
		if err != nil {
			warnings = append(warnings, "A template in this program no longer exists")
			continue
		}
		usable := t.IsUsable.IsSpecified() && !t.IsUsable.IsNull() && t.IsUsable.MustGet()
		if !usable {
			name := "A template"
			if v, err := t.Name.Get(); err == nil {
				name = "Template '" + v.String() + "'"
			}
			warnings = append(warnings, name+" is missing a domain, email or page, so it is not sent")
		}
	}
	return warnings
}

// GetAll lists programs for a company.
func (s *Program) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.ProgramOption,
) (*model.Result[model.Program], error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetAll", session)
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	result, err := s.ProgramRepository.GetAll(ctx, companyID, options)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	programIDs := make([]*uuid.UUID, 0, len(result.Rows))
	for _, p := range result.Rows {
		id := p.ID.MustGet()
		programIDs = append(programIDs, &id)
	}
	summaries, err := s.ProgramRepository.GetListSummaries(ctx, programIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	templateIDs := []uuid.UUID{}
	for _, sum := range summaries {
		templateIDs = append(templateIDs, sum.StepTemplateIDs...)
	}
	usable, err := s.ProgramRepository.GetTemplateUsability(ctx, templateIDs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, p := range result.Rows {
		sum := summaries[p.ID.MustGet()]
		if sum == nil {
			continue
		}
		p.Enrolled = sum.Enrolled
		p.SimulationCount = sum.Simulations
		if p.IsClosed() {
			continue
		}
		p.NextSendAt = nextSendBeforeEnd(p, sum.NextDueAt)
		brokenTemplate := false
		for _, tid := range sum.StepTemplateIDs {
			if !usable[tid] {
				brokenTemplate = true
			}
		}
		switch {
		case brokenTemplate:
			p.AttentionReason = "A template cannot be sent. Check its domain, sender and pages."
		case p.StartedAt != nil && sum.Enrolled == 0:
			p.AttentionReason = "The groups are empty, so nobody is enrolled."
		}
		p.NeedsAttention = p.AttentionReason != ""
	}
	return result, nil
}

// nextSendBeforeEnd returns the next send, nil when it falls at or after the
// end date as the program closes before it
func nextSendBeforeEnd(program *model.Program, next *time.Time) *time.Time {
	if next != nil && program.IsEndedBy(*next) {
		return nil
	}
	return next
}

// applyStartRequest turns the startAt and draft fields of a create or update
// request into the start_at to store. A draft has no start. On create, no
// start time means now. A start in the past means now. Once a program has
// started its start time no longer changes.
func applyStartRequest(incoming *model.Program, existing *model.Program, now time.Time) {
	now = now.UTC()
	draft := incoming.Draft != nil && *incoming.Draft
	notDraft := incoming.Draft != nil && !*incoming.Draft
	if existing != nil && existing.StartedAt != nil {
		incoming.StartAt = nullable.Nullable[time.Time]{}
		return
	}
	if draft {
		incoming.StartAt = nullable.NewNullNullable[time.Time]()
		return
	}
	hasStart := incoming.StartAt.IsSpecified() && !incoming.StartAt.IsNull()
	if !hasStart {
		// create starts now unless it is a draft. An update leaves the start
		// alone unless it asks to start or clears it
		switch {
		case existing == nil || notDraft:
			incoming.StartAt = nullable.NewNullableWithValue(now)
		case incoming.StartAt.IsSpecified():
			incoming.StartAt = nullable.NewNullNullable[time.Time]()
		}
		return
	}
	if incoming.StartAt.MustGet().Before(now) {
		incoming.StartAt = nullable.NewNullableWithValue(now)
	}
}

// Start starts a draft or scheduled program now.
func (s *Program) Start(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	ae := NewAuditEvent("Program.Start", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{WithRecipientGroups: true})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errors.New("a closed program cannot be started"))
	}
	// a program cloned to another company has no groups until they are chosen
	if groups, err := program.RecipientGroupIDs.Get(); err != nil || len(groups) == 0 {
		return errs.NewValidationError(errors.New("choose at least one recipient group before starting"))
	}
	now := time.Now().UTC()
	if start := program.StartAtTime(); start != nil && !start.After(now) {
		return errs.NewValidationError(errors.New("the program has already started"))
	}
	if end := program.EndAtTime(); end != nil && !end.After(now) {
		return errs.NewValidationError(errors.New("The end has passed. Change or clear the end first."))
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{"start_at": now}); err != nil {
		return errs.Wrap(err)
	}
	// enroll on the next tick instead of waiting for the membership interval
	s.lastMembershipAt = time.Time{}
	s.AuditLogAuthorized(ae)
	return nil
}

// Pause halts enrollment and advancement and allows step edits.
func (s *Program) Pause(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	ae := NewAuditEvent("Program.Pause", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errors.New("a closed program cannot be paused"))
	}
	now := time.Now()
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{"paused_at": now}); err != nil {
		return errs.Wrap(err)
	}
	// hold sends that are already queued, the campaign pause gates the sender
	if err := s.setCampaignsPaused(ctx, id, true); err != nil {
		return err
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// Resume clears the pause so the scheduler picks the program up again.
func (s *Program) Resume(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	ae := NewAuditEvent("Program.Resume", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errors.New("a closed program cannot be resumed"))
	}
	now := time.Now()
	// sends held during the pause are checked before the campaigns send again:
	// people who left the groups get none, the rest wait for an open window slot
	if program.StartedAt != nil {
		s.resolveMembershipOnResume(ctx, id, now)
	}
	if err := s.respreadHeldSends(ctx, program, now); err != nil {
		return err
	}
	if end := program.EndAtTime(); end != nil {
		if err := s.ProgramRepository.CancelUnsentFrom(ctx, id, *end); err != nil {
			return errs.Wrap(err)
		}
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{"paused_at": nil}); err != nil {
		return errs.Wrap(err)
	}
	if err := s.setCampaignsPaused(ctx, id, false); err != nil {
		return err
	}
	// recipients whose send came due during the pause start a fresh round now,
	// drawn over the spread, instead of all being sent at once
	program.PausedAt = nullable.NewNullNullable[time.Time]()
	enrollments, err := s.ProgramRepository.GetEnrollmentsByProgramID(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, e := range enrollments {
		if e.Status != model.ProgramEnrollmentActive || e.NextDueAt == nil || e.NextDueAt.After(now) {
			continue
		}
		due := programPickSendTimeAfter(program, now, now, programMinNextSend(program, e.LastSendAt), s.rng())
		if err := s.ProgramRepository.UpdateEnrollmentFields(ctx, e.ID, map[string]any{
			"round_start_at": now,
			"next_due_at":    due,
		}); err != nil {
			return errs.Wrap(err)
		}
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// setCampaignsPaused pauses or resumes the sending of a program's hidden campaigns
func (s *Program) setCampaignsPaused(ctx context.Context, programID *uuid.UUID, paused bool) error {
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, programID)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		if paused {
			err = s.CampaignRepository.SetPausedByID(ctx, campaignID)
		} else {
			err = s.CampaignRepository.ClearPausedByID(ctx, campaignID)
		}
		if err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// Close ends a program for good. Sending stops, pending sends are cancelled and
// the hidden campaigns are closed, while all results stay readable. A closed
// program cannot be resumed or edited, only deleted.
func (s *Program) Close(ctx context.Context, session *model.Session, id *uuid.UUID) error {
	ae := NewAuditEvent("Program.Close", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := s.lifecycleMu.LockContext(ctx); err != nil {
		return errs.Wrap(err)
	}
	defer s.lifecycleMu.Unlock()
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{})
	if err != nil {
		return errs.Wrap(err)
	}
	if program.IsClosed() {
		return errs.NewValidationError(errors.New("program is already closed"))
	}
	if err := s.closeProgram(ctx, session, id, "program closed"); err != nil {
		return err
	}
	s.AuditLogAuthorized(ae)
	return nil
}

// closeProgram closes a program that is not closed: sending stops, pending
// sends are cancelled and the hidden campaigns are closed, while all results
// stay. The caller checks access and holds the lifecycle lock. Every step can
// run again, and closed_at is written last, so a close that stops half way
// leaves a paused program that a new close finishes.
func (s *Program) closeProgram(ctx context.Context, session *model.Session, id *uuid.UUID, reason string) error {
	// pause it first so the scheduler adds nothing if a later step fails
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"paused_at": time.Now().UTC(),
	}); err != nil {
		return errs.Wrap(err)
	}
	campaignIDs, err := s.ProgramRepository.GetCampaignIDs(ctx, id)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, campaignID := range campaignIDs {
		campaign, err := s.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{
			IncludeProgramManaged: true,
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if !campaign.IsActive() {
			continue
		}
		err = s.CampaignService.closeCampaign(ctx, session, campaignID, campaign, reason)
		if err != nil && !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			s.Logger.Errorw("failed to close program campaign", "error", err, "campaignID", campaignID.String())
			return errs.Wrap(err)
		}
	}
	// the pseudonyms link people to their results, a closed program needs no link
	if err := s.ProgramRepository.ClearEnrollmentPseudonyms(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	// active people move to stopped, so counts and next sends end with the program
	if err := s.ProgramRepository.StopEnrollments(ctx, id); err != nil {
		return errs.Wrap(err)
	}
	if err := s.ProgramRepository.SetFields(ctx, id, map[string]any{
		"closed_at": time.Now().UTC(),
		"paused_at": nil,
	}); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// enrollmentPseudonym returns the pseudonym of an enrollment, creating it on
// first use
func (s *Program) enrollmentPseudonym(
	ctx context.Context,
	e *database.ProgramEnrollment,
) (uuid.UUID, error) {
	if e.AnonymizedID != nil {
		return *e.AnonymizedID, nil
	}
	if err := s.ProgramRepository.SetEnrollmentPseudonymIfEmpty(ctx, e.ID, uuid.New()); err != nil {
		return uuid.UUID{}, errs.Wrap(err)
	}
	// read it back so a concurrent writer and this call agree on one pseudonym
	stored, err := s.ProgramRepository.GetEnrollmentByProgramRecipient(ctx, e.ProgramID, e.RecipientID)
	if err != nil {
		return uuid.UUID{}, errs.Wrap(err)
	}
	if stored.AnonymizedID == nil {
		return uuid.UUID{}, errs.Wrap(errors.New("program pseudonym was not stored"))
	}
	e.AnonymizedID = stored.AnonymizedID
	return *stored.AnonymizedID, nil
}

// ProgramResultStats is the lifetime funnel of a program in people. On an
// anonymous program the outcome counts are null and Suppressed is set when the
// group is too small, so a count can not point at one person.
type ProgramResultStats struct {
	Recipients          int64  `json:"recipients"`
	EmailsSent          int64  `json:"emailsSent"`
	TrackingPixelLoaded *int64 `json:"trackingPixelLoaded"`
	WebsiteLoaded       *int64 `json:"clickedLink"`
	SubmittedData       *int64 `json:"submittedData"`
	Reported            *int64 `json:"reported"`
	TrainingStarted     *int64 `json:"trainingStarted"`
	TrainingCompleted   *int64 `json:"trainingCompleted"`
	Suppressed          bool   `json:"suppressed"`
}

// programResultStats applies the small group rule to the raw funnel. A named
// program keeps every count. An anonymous program withholds all outcomes when
// fewer than programMinGroup people were emailed, and any outcome that
// withholdCount hides out of the people emailed: 1 to programMinGroup - 1
// people, or a count that leaves fewer than programMinGroup of them out.
func programResultStats(raw *model.CampaignResultView, anonymous bool) *ProgramResultStats {
	out := &ProgramResultStats{Recipients: raw.Recipients, EmailsSent: raw.EmailsSent}
	withholdAll := anonymous && raw.EmailsSent < programMinGroup
	count := func(v int64) *int64 {
		if withholdAll || (anonymous && withholdCount(int(raw.EmailsSent), int(v))) {
			out.Suppressed = true
			return nil
		}
		return &v
	}
	out.TrackingPixelLoaded = count(raw.TrackingPixelLoaded)
	out.WebsiteLoaded = count(raw.WebsiteLoaded)
	out.SubmittedData = count(raw.SubmittedData)
	out.Reported = count(raw.Reported)
	out.TrainingStarted = count(raw.TrainingStarted)
	out.TrainingCompleted = count(raw.TrainingCompleted)
	return out
}

// GetResultStats returns the program's result funnel across its hidden campaigns.
func (s *Program) GetResultStats(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*ProgramResultStats, error) {
	ae := NewAuditEvent("Program.GetResultStats", session)
	program, err := s.authorizedProgram(ctx, session, id, ae, nil)
	if err != nil {
		return nil, err
	}
	if program.IsAnonymousOn() {
		// an anonymous funnel counts only settled sends and outcomes, the same
		// as every other count of the program
		facts, err := s.loadProgramFacts(ctx, session, program, nil)
		if err != nil {
			return nil, err
		}
		raw := programFunnel(facts.countable(time.Now(), program.Location()))
		return programResultStats(raw, true), nil
	}
	raw, err := s.CampaignRepository.GetProgramResultStats(ctx, id)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return programResultStats(raw, false), nil
}

// programFunnel counts the people of the facts at each step of the funnel.
// The phishing steps read phishing sends, the training steps training sends.
func programFunnel(facts *programFacts) *model.CampaignResultView {
	var recipients, sent, read, clicked, submitted, reported, trainingStarted, trainingDone programTally
	for _, sd := range facts.sends {
		recipients.add(sd.Person)
		if sd.Training {
			if sd.TrainingStartedAt != nil {
				trainingStarted.add(sd.Person)
			}
			if sd.TrainingCompletedAt != nil {
				trainingDone.add(sd.Person)
			}
			continue
		}
		sent.add(sd.Person)
		if sd.ReadAt != nil {
			read.add(sd.Person)
		}
		if sd.ClickedAt != nil {
			clicked.add(sd.Person)
		}
		if sd.SubmittedAt != nil {
			submitted.add(sd.Person)
		}
		if sd.ReportedAt != nil {
			reported.add(sd.Person)
		}
	}
	return &model.CampaignResultView{
		Recipients:          int64(recipients.persons()),
		EmailsSent:          int64(sent.persons()),
		TrackingPixelLoaded: int64(read.persons()),
		WebsiteLoaded:       int64(clicked.persons()),
		SubmittedData:       int64(submitted.persons()),
		Reported:            int64(reported.persons()),
		TrainingStarted:     int64(trainingStarted.persons()),
		TrainingCompleted:   int64(trainingDone.persons()),
	}
}
