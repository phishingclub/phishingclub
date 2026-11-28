package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// ProgramCloneDropped is an item a clone left out because the target company
// may not use it.
type ProgramCloneDropped struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProgramCloneResult is the new program id and what was left out.
type ProgramCloneResult struct {
	ID      *uuid.UUID            `json:"id"`
	Dropped []ProgramCloneDropped `json:"dropped"`
}

// programCloneSource is what a clone reads from the source program, with the
// owner company of each referenced item.
type programCloneSource struct {
	Program   *model.Program
	Templates map[uuid.UUID]programCloneItem
	Groups    map[uuid.UUID]programCloneItem
	AllowDeny map[uuid.UUID]programCloneItem
	Pages     map[uuid.UUID]programCloneItem
	Webhooks  map[uuid.UUID]programCloneItem
}

type programCloneItem struct {
	Name    string
	Company nullable.Nullable[uuid.UUID]
}

// BuildProgramClone maps a source program to a new draft program for the
// target company. Items the target may not use are dropped and listed: a
// different company never gets the groups, and company owned templates,
// filters, pages and webhooks only stay when they belong to the target.
func BuildProgramClone(
	src *programCloneSource,
	name vo.String64,
	targetCompanyID *uuid.UUID,
) (*model.Program, []ProgramCloneDropped) {
	p := src.Program
	dropped := []ProgramCloneDropped{}
	sourceCompanyID := optionalID(p.CompanyID)
	sameCompany := (sourceCompanyID == nil && targetCompanyID == nil) ||
		(sourceCompanyID != nil && targetCompanyID != nil && *sourceCompanyID == *targetCompanyID)
	keep := func(kind string, items map[uuid.UUID]programCloneItem, id uuid.UUID) bool {
		item, ok := items[id]
		if ok && sameScope(targetCompanyID, item.Company) {
			return true
		}
		dropped = append(dropped, ProgramCloneDropped{Kind: kind, ID: id.String(), Name: item.Name})
		return false
	}

	out := &model.Program{
		Name:                  nullable.NewNullableWithValue(name),
		OrderMode:             p.OrderMode,
		LoopMode:              p.LoopMode,
		TemplateMixBy:         p.TemplateMixBy,
		StepIntervalMinutes:   p.StepIntervalMinutes,
		SpreadMinutes:         p.SpreadMinutes,
		NewMemberDelayMinutes: p.NewMemberDelayMinutes,
		TimeZone:              p.TimeZone,
		ConstraintWeekDays:    p.ConstraintWeekDays,
		ConstraintStartTime:   p.ConstraintStartTime,
		ConstraintEndTime:     p.ConstraintEndTime,
		IsAnonymous:           p.IsAnonymous,
		SaveSubmittedData:     p.SaveSubmittedData,
		BlackoutDates:         p.BlackoutDates,
		ReportEnabled:         p.ReportEnabled,
		ReportInterval:        p.ReportInterval,
		ReportPeriod:          p.ReportPeriod,
		// a clone is always a draft
		StartAt: nullable.NewNullNullable[time.Time](),
	}
	if targetCompanyID != nil {
		out.CompanyID = nullable.NewNullableWithValue(*targetCompanyID)
	} else {
		out.CompanyID = nullable.NewNullNullable[uuid.UUID]()
	}

	for _, st := range p.Steps {
		templateID := st.CampaignTemplateID.MustGet()
		if !keep("template", src.Templates, templateID) {
			continue
		}
		out.Steps = append(out.Steps, &model.ProgramStep{
			CampaignTemplateID: nullable.NewNullableWithValue(templateID),
			Position:           nullable.NewNullableWithValue(len(out.Steps)),
		})
	}

	groupIDs := []*uuid.UUID{}
	for _, gid := range nullableIDs(p.RecipientGroupIDs) {
		if gid == nil {
			continue
		}
		if !sameCompany {
			item := src.Groups[*gid]
			dropped = append(dropped, ProgramCloneDropped{Kind: "group", ID: gid.String(), Name: item.Name})
			continue
		}
		if keep("group", src.Groups, *gid) {
			id := *gid
			groupIDs = append(groupIDs, &id)
		}
	}
	out.RecipientGroupIDs = nullable.NewNullableWithValue(groupIDs)

	denyPage := optionalID(p.DenyPageID)
	if denyPage != nil && !keep("denyPage", src.Pages, *denyPage) {
		denyPage = nil
	}
	evasionPage := optionalID(p.EvasionPageID)
	if evasionPage != nil && !keep("evasionPage", src.Pages, *evasionPage) {
		evasionPage = nil
	}
	allowDenyIDs := []*uuid.UUID{}
	for _, id := range nullableIDs(p.AllowDenyIDs) {
		if id != nil && keep("allowDeny", src.AllowDeny, *id) {
			v := *id
			allowDenyIDs = append(allowDenyIDs, &v)
		}
	}
	// the page rules must still hold after dropping: no evasion page and no
	// filters without a deny page
	if denyPage == nil {
		if evasionPage != nil {
			dropped = append(dropped, ProgramCloneDropped{Kind: "evasionPage", ID: evasionPage.String(), Name: src.Pages[*evasionPage].Name})
			evasionPage = nil
		}
		for _, id := range allowDenyIDs {
			dropped = append(dropped, ProgramCloneDropped{Kind: "allowDeny", ID: id.String(), Name: src.AllowDeny[*id].Name})
		}
		allowDenyIDs = []*uuid.UUID{}
	}
	out.DenyPageID = nullable.NewNullNullable[uuid.UUID]()
	if denyPage != nil {
		out.DenyPageID.Set(*denyPage)
	}
	out.EvasionPageID = nullable.NewNullNullable[uuid.UUID]()
	if evasionPage != nil {
		out.EvasionPageID.Set(*evasionPage)
	}
	out.AllowDenyIDs = nullable.NewNullableWithValue(allowDenyIDs)

	webhooks := []*model.CampaignWebhook{}
	for _, wh := range nullableWebhooks(p.Webhooks) {
		if wh == nil {
			continue
		}
		id, err := wh.WebhookID.Get()
		if err != nil || !keep("webhook", src.Webhooks, id) {
			continue
		}
		webhooks = append(webhooks, &model.CampaignWebhook{
			WebhookID:          nullable.NewNullableWithValue(id),
			WebhookIncludeData: nullable.NewNullableWithValue(wh.GetWebhookIncludeDataOrDefault()),
			WebhookEvents:      nullable.NewNullableWithValue(wh.GetWebhookEventsOrDefault()),
		})
	}
	out.Webhooks = nullable.NewNullableWithValue(webhooks)

	// the field and the default are kept. The routes are values of the
	// source company's recipients, so another company does not get them.
	out.LanguageRouteBy = nullable.NewNullableWithValue(p.LanguageRouteField())
	out.LanguageDefault = nullable.NewNullableWithValue("")
	if v, err := p.LanguageDefault.Get(); err == nil {
		out.LanguageDefault.Set(v)
	}
	routes := map[string]string{}
	if v, err := p.LanguageRoutes.Get(); err == nil && v != nil {
		routes = v
	}
	if sameCompany {
		copied := make(map[string]string, len(routes))
		for key, code := range routes {
			copied[key] = code
		}
		out.LanguageRoutes = nullable.NewNullableWithValue(copied)
	} else {
		out.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{})
		if len(routes) > 0 {
			dropped = append(dropped, ProgramCloneDropped{
				Kind: "languageRoutes",
				Name: routeValuesName(len(routes)),
			})
		}
	}
	return out, dropped
}

// routeValuesName names a count of route values, like "3 values"
func routeValuesName(n int) string {
	if n == 1 {
		return "1 value"
	}
	return strconv.Itoa(n) + " values"
}

// droppedLanguageRoutes is the number of language routes a clone left out
func droppedLanguageRoutes(src *model.Program, clone *model.Program) int {
	source, err := src.LanguageRoutes.Get()
	if err != nil {
		return 0
	}
	kept, _ := clone.LanguageRoutes.Get()
	return len(source) - len(kept)
}

// loadCloneSource reads the source program and the owner of everything it uses.
func (s *Program) loadCloneSource(ctx context.Context, session *model.Session, id *uuid.UUID) (*programCloneSource, error) {
	program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
		WithLanguageRoutes:  true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.fillProgramSettings(ctx, program); err != nil {
		return nil, err
	}
	src := &programCloneSource{
		Program:   program,
		Templates: map[uuid.UUID]programCloneItem{},
		Groups:    map[uuid.UUID]programCloneItem{},
		AllowDeny: map[uuid.UUID]programCloneItem{},
		Pages:     map[uuid.UUID]programCloneItem{},
		Webhooks:  map[uuid.UUID]programCloneItem{},
	}
	// a missing item has no entry and is dropped by the clone
	for _, st := range program.Steps {
		tid := st.CampaignTemplateID.MustGet()
		t, err := s.CampaignTemplateService.GetByID(ctx, session, &tid, &repository.CampaignTemplateOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		src.Templates[tid] = programCloneItem{Name: t.Name.MustGet().String(), Company: t.CompanyID}
	}
	for _, g := range program.RecipientGroups {
		src.Groups[g.ID.MustGet()] = programCloneItem{Name: g.Name.MustGet().String(), Company: g.CompanyID}
	}
	for _, aid := range nullableIDs(program.AllowDenyIDs) {
		a, err := s.AllowDenyRepository.GetByID(ctx, aid, &repository.AllowDenyOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		src.AllowDeny[*aid] = programCloneItem{Name: a.Name.MustGet().String(), Company: a.CompanyID}
	}
	for _, pid := range []*uuid.UUID{optionalID(program.DenyPageID), optionalID(program.EvasionPageID)} {
		if pid == nil {
			continue
		}
		p, err := s.PageRepository.GetByID(ctx, pid, &repository.PageOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		src.Pages[*pid] = programCloneItem{Name: p.Name.MustGet().String(), Company: p.CompanyID}
	}
	for _, wh := range nullableWebhooks(program.Webhooks) {
		wid := wh.WebhookID.MustGet()
		w, err := s.WebhookRepository.GetByID(ctx, &wid)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		src.Webhooks[wid] = programCloneItem{Name: w.Name.MustGet().String(), Company: w.CompanyID}
	}
	return src, nil
}

// Clone copies a program as a new draft, to the same or another company. It
// never copies enrollments, pseudonyms, campaigns or the start. Everything the
// copy uses is checked again against the target company before it is stored.
func (s *Program) Clone(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	name vo.String64,
	targetCompanyID *uuid.UUID,
) (_ *ProgramCloneResult, err error) {
	ae := NewAuditEvent("Program.Clone", session)
	defer s.auditDone(ae, &err)
	ae.Details["sourceId"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	unlockCompany, err := refuseDeletingCompany(ctx, s.CompanyRepository, targetCompanyID)
	if err != nil {
		return nil, err
	}
	defer unlockCompany()
	if targetCompanyID != nil {
		if _, err := s.CompanyRepository.GetByID(ctx, targetCompanyID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, validate.WrapErrorWithField(errors.New("company not found"), "companyID")
			}
			return nil, errs.Wrap(err)
		}
	}
	src, err := s.loadCloneSource(ctx, session, id)
	if err != nil {
		return nil, err
	}
	// a program that is being deleted is about to lose what the copy reads
	if src.Program.DeletingAt != nil {
		return nil, errs.NewValidationError(errors.New("the program is being deleted"))
	}
	clone, dropped := BuildProgramClone(src, name, targetCompanyID)
	clone.ApplyReportBaseline(nil, time.Now())
	if err := clone.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	if len(clone.Steps) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("None of the templates can be used by that company"), "companyID")
	}
	if err := s.checkNameIsUnique(ctx, name, targetCompanyID, nil); err != nil {
		return nil, err
	}
	// the clone is checked like any new program, against the target company
	groupIDs := nullableIDs(clone.RecipientGroupIDs)
	// a group that is being deleted is refused in validateScope, the read
	// locks of the groups keep a delete mark from passing before the copy is
	// stored, see deleteMarkLock
	defer groupDeleteLock.read(groupIDs...)()
	if err := s.validateScope(ctx, session, targetCompanyID, clone.Steps, groupIDs); err != nil {
		return nil, err
	}
	if err := s.validateProgramSettings(ctx, targetCompanyID, nil, clone); err != nil {
		return nil, err
	}
	// a copy in the same company keeps its routes, so every language they
	// pick must be sendable by every step template, the same as at create. A
	// copy to another company has no routes and is checked when it starts.
	if sameCompanyScope(optionalID(src.Program.CompanyID), targetCompanyID) {
		if routeBy := clone.LanguageRouteField(); routeBy != "" {
			_, fallback, routes, err := s.programRouting(ctx, clone)
			if err != nil {
				return nil, err
			}
			if err := s.checkProgramLanguages(ctx, targetCompanyID, routeBy, fallback, routes, clone.Steps, nullableIDs(clone.RecipientGroupIDs)); err != nil {
				return nil, err
			}
		}
	}
	// the draft, its steps, groups and settings are stored in one transaction
	newID, err := s.ProgramRepository.Insert(ctx, clone, programChildren(clone))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = newID.String()
	ae.Details["sourceCompanyId"] = companyIDString(optionalID(src.Program.CompanyID))
	ae.Details["targetCompanyId"] = companyIDString(targetCompanyID)
	ae.Details["dropped"] = len(dropped)
	if n := droppedLanguageRoutes(src.Program, clone); n > 0 {
		ae.Details["droppedLanguageRoutes"] = n
	}
	s.AuditLogAuthorized(ae)
	return &ProgramCloneResult{ID: newID, Dropped: dropped}, nil
}

func companyIDString(id *uuid.UUID) string {
	if id == nil {
		return "global"
	}
	return id.String()
}
