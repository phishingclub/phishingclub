package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
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

// programMaxAllowDeny and programMaxWebhooks bound the lists a program can set
const (
	programMaxAllowDeny = 50
	programMaxWebhooks  = 20
)

// programSettingsSpecified reports whether a request touches the bot filtering,
// page or webhook settings.
func programSettingsSpecified(p *model.Program) bool {
	return p.AllowDenyIDs.IsSpecified() || p.DenyPageID.IsSpecified() ||
		p.EvasionPageID.IsSpecified() || p.Webhooks.IsSpecified()
}

// nullableIDs returns the ids of a specified list, empty for null.
func nullableIDs(v nullable.Nullable[[]*uuid.UUID]) []*uuid.UUID {
	if ids, err := v.Get(); err == nil {
		return ids
	}
	return []*uuid.UUID{}
}

// nullableWebhooks returns the webhooks of a specified list, empty for null.
func nullableWebhooks(v nullable.Nullable[[]*model.CampaignWebhook]) []*model.CampaignWebhook {
	if whs, err := v.Get(); err == nil {
		return whs
	}
	return []*model.CampaignWebhook{}
}

// optionalID returns the id of a nullable, nil when unset or null.
func optionalID(v nullable.Nullable[uuid.UUID]) *uuid.UUID {
	if id, err := v.Get(); err == nil {
		return &id
	}
	return nil
}

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

// validateProgramSettings checks the bot filtering, pages and webhooks of a
// create or update against the program company. On update, settings that are
// not in the request are read from the stored program so the page rules hold
// for the merged result.
func (s *Program) validateProgramSettings(
	ctx context.Context,
	companyID *uuid.UUID,
	existingID *uuid.UUID,
	incoming *model.Program,
) error {
	if existingID != nil && !programSettingsSpecified(incoming) {
		return nil
	}
	allowDenyIDs := nullableIDs(incoming.AllowDenyIDs)
	denyPageID := optionalID(incoming.DenyPageID)
	evasionPageID := optionalID(incoming.EvasionPageID)
	if existingID != nil {
		existing, err := s.ProgramRepository.GetByID(ctx, existingID, &repository.ProgramOption{})
		if err != nil {
			return errs.Wrap(err)
		}
		if !incoming.AllowDenyIDs.IsSpecified() {
			ids, err := s.ProgramRepository.GetAllowDenyIDs(ctx, existingID)
			if err != nil {
				return errs.Wrap(err)
			}
			allowDenyIDs = ids
		}
		if !incoming.DenyPageID.IsSpecified() {
			denyPageID = optionalID(existing.DenyPageID)
		}
		if !incoming.EvasionPageID.IsSpecified() {
			evasionPageID = optionalID(existing.EvasionPageID)
		}
	}
	if len(allowDenyIDs) > programMaxAllowDeny {
		return validate.WrapErrorWithField(errors.New("too many allow or deny lists"), "allowDenyIDs")
	}
	if err := ValidateProgramFilterPages(denyPageID != nil, evasionPageID != nil, len(allowDenyIDs)); err != nil {
		return err
	}
	// only what the request sets is checked for scope, stored values were
	// checked when they were set
	if incoming.AllowDenyIDs.IsSpecified() {
		isAllowList := false
		for i, id := range allowDenyIDs {
			if id == nil {
				return validate.WrapErrorWithField(errors.New("Filter list not found"), "allowDenyIDs")
			}
			entry, err := s.AllowDenyRepository.GetByID(ctx, id, &repository.AllowDenyOption{})
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validate.WrapErrorWithField(errors.New("Filter list not found"), "allowDenyIDs")
			}
			if err != nil {
				return errs.Wrap(err)
			}
			if !sameScope(companyID, entry.CompanyID) {
				return validate.WrapErrorWithField(errors.New("a filter belongs to another company"), "allowDenyIDs")
			}
			allowed := entry.Allowed.IsSpecified() && !entry.Allowed.IsNull() && entry.Allowed.MustGet()
			if i == 0 {
				isAllowList = allowed
			} else if allowed != isAllowList {
				return validate.WrapErrorWithField(errors.New("Use allow lists or deny lists, not both"), "allowDenyIDs")
			}
		}
	}
	for _, page := range []struct {
		field string
		id    *uuid.UUID
		set   bool
	}{
		{"denyPageID", denyPageID, incoming.DenyPageID.IsSpecified()},
		{"evasionPageID", evasionPageID, incoming.EvasionPageID.IsSpecified()},
	} {
		if !page.set || page.id == nil {
			continue
		}
		p, err := s.PageRepository.GetByID(ctx, page.id, &repository.PageOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("page not found"), page.field)
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if !sameScope(companyID, p.CompanyID) {
			return validate.WrapErrorWithField(errors.New("the page belongs to another company"), page.field)
		}
	}
	if incoming.Webhooks.IsSpecified() {
		webhooks := nullableWebhooks(incoming.Webhooks)
		if len(webhooks) > programMaxWebhooks {
			return validate.WrapErrorWithField(errors.New("too many webhooks"), "webhooks")
		}
		for _, wh := range webhooks {
			if wh == nil {
				return validate.WrapErrorWithField(errors.New("webhook entry cannot be null"), "webhooks")
			}
			if err := wh.Validate(); err != nil {
				return err
			}
			webhookID := wh.WebhookID.MustGet()
			w, err := s.WebhookRepository.GetByID(ctx, &webhookID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return validate.WrapErrorWithField(errors.New("webhook not found"), "webhooks")
			}
			if err != nil {
				return errs.Wrap(err)
			}
			if !sameScope(companyID, w.CompanyID) {
				return validate.WrapErrorWithField(errors.New("a webhook belongs to another company"), "webhooks")
			}
		}
	}
	return nil
}

// saveProgramSettings stores the allow or deny lists and webhooks of a request.
// The page ids are stored with the program row.
func (s *Program) saveProgramSettings(ctx context.Context, programID *uuid.UUID, incoming *model.Program) error {
	if incoming.AllowDenyIDs.IsSpecified() {
		if err := s.ProgramRepository.ReplaceAllowDenies(ctx, programID, nullableIDs(incoming.AllowDenyIDs)); err != nil {
			return errs.Wrap(err)
		}
	}
	if incoming.Webhooks.IsSpecified() {
		if err := s.ProgramRepository.ReplaceWebhooks(ctx, programID, nullableWebhooks(incoming.Webhooks)); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// loadProgramCampaignSettings reads what a program copies to its campaigns.
func (s *Program) loadProgramCampaignSettings(ctx context.Context, programID *uuid.UUID) (*repository.ProgramCampaignSettings, error) {
	program, err := s.ProgramRepository.GetByID(ctx, programID, &repository.ProgramOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	allowDenyIDs, err := s.ProgramRepository.GetAllowDenyIDs(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	webhooks, err := s.ProgramRepository.GetWebhooks(ctx, programID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &repository.ProgramCampaignSettings{
		DenyPageID:    optionalID(program.DenyPageID),
		EvasionPageID: optionalID(program.EvasionPageID),
		AllowDenyIDs:  allowDenyIDs,
		Webhooks:      webhooks,
	}, nil
}

// fillProgramSettings sets the allow or deny list ids and webhooks on a program
// read for the API.
func (s *Program) fillProgramSettings(ctx context.Context, program *model.Program) error {
	id, err := program.ID.Get()
	if err != nil {
		return nil
	}
	allowDenyIDs, err := s.ProgramRepository.GetAllowDenyIDs(ctx, &id)
	if err != nil {
		return errs.Wrap(err)
	}
	webhooks, err := s.ProgramRepository.GetWebhooks(ctx, &id)
	if err != nil {
		return errs.Wrap(err)
	}
	program.AllowDenyIDs = nullable.NewNullableWithValue(allowDenyIDs)
	program.Webhooks = nullable.NewNullableWithValue(webhooks)
	return nil
}

// applyStepCampaignSettings copies the program bot filtering, pages and
// webhooks to a new hidden campaign. If that fails the campaign is removed, so
// no campaign ever sends without the filters. The next tick creates it again.
func (s *Program) applyStepCampaignSettings(
	ctx context.Context,
	session *model.Session,
	programID *uuid.UUID,
	campaignID *uuid.UUID,
) error {
	settings, err := s.loadProgramCampaignSettings(ctx, programID)
	if err == nil {
		err = s.ProgramRepository.ApplyCampaignSettings(ctx, []*uuid.UUID{campaignID}, settings)
	}
	if err == nil {
		return nil
	}
	s.Logger.Errorw("failed to copy program settings to campaign", "error", err, "campaignID", campaignID.String())
	if dErr := s.ProgramRepository.DeleteProgramCampaignByCampaignID(ctx, campaignID); dErr != nil {
		s.Logger.Errorw("failed to unlink program campaign", "error", dErr)
	}
	if dErr := s.CampaignService.deleteByID(ctx, session, campaignID, true); dErr != nil {
		s.Logger.Errorw("failed to remove program campaign", "error", dErr)
	}
	return errs.Wrap(err)
}

// syncProgramCampaignSettings copies the current program settings to every
// hidden campaign of the program that is not closed. The caller holds the
// lifecycle lock so a tick does not create a campaign with the old settings
// meanwhile.
func (s *Program) syncProgramCampaignSettings(ctx context.Context, programID *uuid.UUID) error {
	settings, err := s.loadProgramCampaignSettings(ctx, programID)
	if err != nil {
		return errs.Wrap(err)
	}
	campaignIDs, err := s.ProgramRepository.GetOpenCampaignIDs(ctx, programID)
	if err != nil {
		return errs.Wrap(err)
	}
	return errs.Wrap(s.ProgramRepository.ApplyCampaignSettings(ctx, campaignIDs, settings))
}

// programChangedFields returns the json names of the fields an update changes,
// for the audit log.
func (s *Program) programChangedFields(ctx context.Context, id *uuid.UUID, incoming *model.Program) []string {
	existing, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
		WithSteps:           true,
		WithRecipientGroups: true,
	})
	if err != nil {
		return nil
	}
	if err := s.fillProgramSettings(ctx, existing); err != nil {
		return nil
	}
	return ProgramChangedFields(existing, incoming)
}

// ProgramChangedFields compares the fields set in an update with the stored
// program and returns the json names of those that differ.
func ProgramChangedFields(existing *model.Program, incoming *model.Program) []string {
	changed := []string{}
	ev := reflect.ValueOf(existing).Elem()
	iv := reflect.ValueOf(incoming).Elem()
	t := iv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "id" || name == "companyID" {
			continue
		}
		in := iv.Field(i)
		// nullable values are maps, an empty map is a field not in the request
		if in.Kind() != reflect.Map || in.Len() == 0 {
			continue
		}
		if !sameJSON(normalizeForCompare(in.Interface()), normalizeForCompare(ev.Field(i).Interface())) {
			changed = append(changed, name)
		}
	}
	if len(incoming.Steps) > 0 && !sameJSON(stepSignature(incoming.Steps), stepSignature(existing.Steps)) {
		changed = append(changed, "steps")
	}
	return changed
}

// normalizeForCompare sorts id lists so a new order of the same ids is no change
func normalizeForCompare(v any) any {
	if ids, ok := v.(nullable.Nullable[[]*uuid.UUID]); ok {
		out := []string{}
		if list, err := ids.Get(); err == nil {
			for _, id := range list {
				if id != nil {
					out = append(out, id.String())
				}
			}
		}
		slices.Sort(out)
		return out
	}
	if whs, ok := v.(nullable.Nullable[[]*model.CampaignWebhook]); ok {
		out := []string{}
		if list, err := whs.Get(); err == nil {
			for _, wh := range list {
				if wh == nil {
					continue
				}
				b, _ := json.Marshal([]any{wh.WebhookID, wh.GetWebhookIncludeDataOrDefault(), wh.GetWebhookEventsOrDefault()})
				out = append(out, string(b))
			}
		}
		slices.Sort(out)
		return out
	}
	return v
}

func stepSignature(steps []*model.ProgramStep) []string {
	out := []string{}
	for _, st := range steps {
		id := ""
		if v, err := st.CampaignTemplateID.Get(); err == nil {
			id = v.String()
		}
		out = append(out, id)
	}
	return out
}

func sameJSON(a any, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

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
		StepIntervalMinutes:   p.StepIntervalMinutes,
		SpreadMinutes:         p.SpreadMinutes,
		NewMemberDelayMinutes: p.NewMemberDelayMinutes,
		TimeZone:              p.TimeZone,
		ConstraintWeekDays:    p.ConstraintWeekDays,
		ConstraintStartTime:   p.ConstraintStartTime,
		ConstraintEndTime:     p.ConstraintEndTime,
		IsAnonymous:           p.IsAnonymous,
		BlackoutDates:         p.BlackoutDates,
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
	return out, dropped
}

// loadCloneSource reads the source program and the owner of everything it uses.
func (s *Program) loadCloneSource(ctx context.Context, session *model.Session, id *uuid.UUID) (*programCloneSource, error) {
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
) (*ProgramCloneResult, error) {
	ae := NewAuditEvent("Program.Clone", session)
	ae.Details["sourceID"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
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
	clone, dropped := BuildProgramClone(src, name, targetCompanyID)
	if err := clone.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	if len(clone.Steps) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("None of the templates can be used by that company"), "companyID")
	}
	isUnique, err := s.ProgramRepository.IsNameUnique(ctx, name.String(), targetCompanyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if !isUnique {
		return nil, validate.WrapErrorWithField(errors.New("A program with this name already exists"), "name")
	}
	// the clone is checked like any new program, against the target company
	groupIDs := nullableIDs(clone.RecipientGroupIDs)
	if err := s.validateScope(ctx, session, targetCompanyID, clone.Steps, groupIDs); err != nil {
		return nil, err
	}
	if err := s.validateProgramSettings(ctx, targetCompanyID, nil, clone); err != nil {
		return nil, err
	}
	newID, err := s.ProgramRepository.Insert(ctx, clone)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := s.ProgramRepository.ReplaceSteps(ctx, newID, clone.Steps); err != nil {
		return nil, errs.Wrap(err)
	}
	if len(groupIDs) > 0 {
		if err := s.ProgramRepository.ReplaceRecipientGroups(ctx, newID, groupIDs); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	if err := s.saveProgramSettings(ctx, newID, clone); err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = newID.String()
	ae.Details["sourceCompanyID"] = companyIDString(optionalID(src.Program.CompanyID))
	ae.Details["targetCompanyID"] = companyIDString(targetCompanyID)
	ae.Details["dropped"] = len(dropped)
	s.AuditLogAuthorized(ae)
	return &ProgramCloneResult{ID: newID, Dropped: dropped}, nil
}

func companyIDString(id *uuid.UUID) string {
	if id == nil {
		return "global"
	}
	return id.String()
}
