package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// programSettingsSpecified reports whether a request touches the bot filtering,
// page or webhook settings.
func programSettingsSpecified(p *model.Program) bool {
	return p.AllowDenyIDs.IsSpecified() || p.DenyPageID.IsSpecified() ||
		p.EvasionPageID.IsSpecified() || p.Webhooks.IsSpecified()
}

// programCampaignSettingsSpecified reports whether a request touches anything
// the hidden campaigns copy from the program
func programCampaignSettingsSpecified(p *model.Program) bool {
	return programSettingsSpecified(p) || p.SaveSubmittedData.IsSpecified()
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
	if len(allowDenyIDs) > model.ProgramMaxAllowDeny {
		return validate.WrapErrorWithField(errors.New("too many allow or deny lists"), "allowDenyIDs")
	}
	if err := model.ValidateProgramFilterPages(denyPageID != nil, evasionPageID != nil, len(allowDenyIDs)); err != nil {
		return err
	}
	// only what the request sets is checked for scope, stored values were
	// checked when they were set
	if incoming.AllowDenyIDs.IsSpecified() {
		isAllowList := false
		for i, id := range allowDenyIDs {
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
		// the count, null entries and fields were checked by model Validate
		for _, wh := range nullableWebhooks(incoming.Webhooks) {
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
		// an anonymous program never saves data, checked again here so a
		// hidden campaign can not get it from a stale row
		SaveSubmittedData: program.SaveSubmittedDataOn() && !program.IsAnonymousOn(),
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

// applyStepCampaignSettings copies the program bot filtering, pages, webhooks
// and data saving to a new hidden campaign. If that fails the campaign is removed, so
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
