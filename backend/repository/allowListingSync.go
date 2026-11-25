package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AllowListingSync is the repository of allow listing sync targets, their
// entries and pins, and the reads the desired set is built from
type AllowListingSync struct {
	DB *gorm.DB
}

func alsCol(table string, name string) clause.Column {
	return clause.Column{Table: table, Name: name}
}

func alsID(table string, id *uuid.UUID) clause.Eq {
	return clause.Eq{Column: alsCol(table, "id"), Value: id.String()}
}

// dayBefore is the day before t as YYYY-MM-DD. Times are not stored in one
// format, but every format starts with the date, so a compare against the
// date of the day before keeps every row that may be after t. The exact
// compare is done in Go on the loaded rows.
func dayBefore(t time.Time) string {
	return t.UTC().Add(-24 * time.Hour).Format("2006-01-02")
}

func toAllowListingTarget(row *database.AllowListingTarget) *model.AllowListingTarget {
	return &model.AllowListingTarget{
		ID:                  *row.ID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		CompanyID:           row.CompanyID,
		Enabled:             row.Enabled,
		TokenPrefix:         row.TokenPrefix,
		LeadMinutes:         row.LeadMinutes,
		GraceMinutes:        row.GraceMinutes,
		IntervalMinutes:     row.IntervalMinutes,
		MaxReportAgeMinutes: model.AllowListingMaxReportAgeMinutes(row.IntervalMinutes),
		ReportGapMinutes:    row.ReportGapMinutes,
		RunnerDomain:        row.RunnerDomain,
		PlanID:              row.PlanID,
		PlanIssuedAt:        row.PlanIssuedAt,
		PlanJSON:            row.PlanJSON,
		RecentPlansJSON:     row.RecentPlansJSON,
		LastPlanAt:          row.LastPlanAt,
		LastReportAt:        row.LastReportAt,
		LastError:           row.LastError,
		LastNote:            row.LastNote,
		FailureCount:        row.FailureCount,
		DriftCount:          row.DriftCount,
		DeleteRequestedAt:   row.DeleteRequestedAt,
	}
}

// GetTargetByCompanyID returns the target of a company, a nil company is the
// global target. The token hash is returned too.
func (r *AllowListingSync) GetTargetByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) (*model.AllowListingTarget, string, error) {
	var row database.AllowListingTarget
	q := whereCompany(r.DB.WithContext(ctx), database.ALLOW_LISTING_TARGET_TABLE, companyID)
	if err := q.First(&row).Error; err != nil {
		return nil, "", errs.Wrap(err)
	}
	return toAllowListingTarget(&row), row.TokenHash, nil
}

// GetTargetByID returns a target and its token hash
func (r *AllowListingSync) GetTargetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.AllowListingTarget, string, error) {
	var row database.AllowListingTarget
	err := r.DB.WithContext(ctx).
		Where(alsID(database.ALLOW_LISTING_TARGET_TABLE, id)).
		First(&row).Error
	if err != nil {
		return nil, "", errs.Wrap(err)
	}
	return toAllowListingTarget(&row), row.TokenHash, nil
}

// InsertTarget adds a target with its token hash
func (r *AllowListingSync) InsertTarget(
	ctx context.Context,
	companyID *uuid.UUID,
	settings *model.AllowListingTargetSettings,
	tokenHash string,
	tokenPrefix string,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := map[string]any{
		"id":               id,
		"token_hash":       tokenHash,
		"token_prefix":     tokenPrefix,
		"enabled":          settings.Enabled,
		"lead_minutes":     settings.LeadMinutes,
		"grace_minutes":    settings.GraceMinutes,
		"interval_minutes": settings.IntervalMinutes,
	}
	if companyID != nil {
		row["company_id"] = companyID.String()
	} else {
		row["company_id"] = nil
	}
	AddTimestamps(row)
	err := r.DB.WithContext(ctx).Model(&database.AllowListingTarget{}).Create(row).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &id, nil
}

func (r *AllowListingSync) updateTarget(ctx context.Context, id *uuid.UUID, row map[string]any) error {
	AddUpdatedAt(row)
	err := r.DB.WithContext(ctx).
		Model(&database.AllowListingTarget{}).
		Where(alsID(database.ALLOW_LISTING_TARGET_TABLE, id)).
		Updates(row).Error
	return errs.Wrap(err)
}

// UpdateTargetSettings saves the settings of a target
func (r *AllowListingSync) UpdateTargetSettings(
	ctx context.Context,
	id *uuid.UUID,
	settings *model.AllowListingTargetSettings,
) error {
	return r.updateTarget(ctx, id, map[string]any{
		"enabled":          settings.Enabled,
		"lead_minutes":     settings.LeadMinutes,
		"grace_minutes":    settings.GraceMinutes,
		"interval_minutes": settings.IntervalMinutes,
	})
}

// UpdateTargetToken replaces the token hash and prefix
func (r *AllowListingSync) UpdateTargetToken(ctx context.Context, id *uuid.UUID, hash string, prefix string) error {
	return r.updateTarget(ctx, id, map[string]any{"token_hash": hash, "token_prefix": prefix})
}

// SetTargetPlan stores the plan handed to the runner
func (r *AllowListingSync) SetTargetPlan(
	ctx context.Context,
	id *uuid.UUID,
	planID uuid.UUID,
	planJSON string,
	recentPlansJSON string,
	at time.Time,
) error {
	return r.updateTarget(ctx, id, map[string]any{
		"plan_id":           planID.String(),
		"plan_issued_at":    at.UTC(),
		"plan_json":         planJSON,
		"recent_plans_json": recentPlansJSON,
		"last_plan_at":      at.UTC(),
	})
}

// SetTargetDeleteRequested marks the target to be deleted once its owned
// values are removed, nil clears it
func (r *AllowListingSync) SetTargetDeleteRequested(ctx context.Context, id *uuid.UUID, at *time.Time) error {
	var v any
	if at != nil {
		v = at.UTC()
	}
	return r.updateTarget(ctx, id, map[string]any{"delete_requested_at": v})
}

// SetTargetRunnerDomain stores the domain the runbook calls
func (r *AllowListingSync) SetTargetRunnerDomain(ctx context.Context, id *uuid.UUID, domain string) error {
	return r.updateTarget(ctx, id, map[string]any{"runner_domain": domain})
}

// RecordTargetFailure stores an error the runner or a report gave
func (r *AllowListingSync) RecordTargetFailure(ctx context.Context, id *uuid.UUID, message string) error {
	return r.updateTarget(ctx, id, map[string]any{
		"last_error":    message,
		"failure_count": gorm.Expr("failure_count + 1"),
	})
}

// AllowListingEntryChanges are the ledger changes of one report
type AllowListingEntryChanges struct {
	Insert []*database.AllowListingEntry
	Delete []uuid.UUID
	Seen   []uuid.UUID
}

// ApplyReport writes the ledger changes of a report and the report state of
// the target in one transaction. Nothing is read inside it.
func (r *AllowListingSync) ApplyReport(
	ctx context.Context,
	targetID *uuid.UUID,
	changes *AllowListingEntryChanges,
	at time.Time,
	lastError string,
	lastNote string,
	drift int,
	gapMinutes int,
) error {
	at = at.UTC()
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		table := database.ALLOW_LISTING_ENTRY_TABLE
		if len(changes.Delete) > 0 {
			err := tx.
				Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
				Where(clause.IN{Column: alsCol(table, "id"), Values: uuidSliceValues(changes.Delete)}).
				Delete(&database.AllowListingEntry{}).Error
			if err != nil {
				return err
			}
		}
		if len(changes.Seen) > 0 {
			err := tx.Model(&database.AllowListingEntry{}).
				Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
				Where(clause.IN{Column: alsCol(table, "id"), Values: uuidSliceValues(changes.Seen)}).
				Updates(map[string]any{"last_seen_at": at, "updated_at": at}).Error
			if err != nil {
				return err
			}
		}
		for _, e := range changes.Insert {
			id := uuid.New()
			e.ID = &id
			e.TargetID = targetID
			e.CreatedAt = &at
			e.UpdatedAt = &at
			e.AddedAt = &at
			e.LastSeenAt = &at
			if err := tx.Create(e).Error; err != nil {
				return err
			}
		}
		row := map[string]any{
			"last_report_at": at,
			"last_error":     lastError,
			"last_note":      lastNote,
			"updated_at":     at,
		}
		if gapMinutes > 0 {
			row["report_gap_minutes"] = gapMinutes
		}
		if lastError == "" {
			row["failure_count"] = 0
		} else {
			row["failure_count"] = gorm.Expr("failure_count + 1")
		}
		if drift > 0 {
			row["drift_count"] = gorm.Expr("drift_count + ?", drift)
		}
		return tx.Model(&database.AllowListingTarget{}).
			Where(alsID(database.ALLOW_LISTING_TARGET_TABLE, targetID)).
			Updates(row).Error
	}))
}

// MarkUndesired sets undesired_since on the entries in set that do not have
// it and clears it on the entries in clear
func (r *AllowListingSync) MarkUndesired(
	ctx context.Context,
	targetID *uuid.UUID,
	set []uuid.UUID,
	clearIDs []uuid.UUID,
	at time.Time,
) error {
	if len(set) == 0 && len(clearIDs) == 0 {
		return nil
	}
	table := database.ALLOW_LISTING_ENTRY_TABLE
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(set) > 0 {
			err := tx.Model(&database.AllowListingEntry{}).
				Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
				Where(clause.IN{Column: alsCol(table, "id"), Values: uuidSliceValues(set)}).
				Where(clause.Eq{Column: alsCol(table, "undesired_since"), Value: nil}).
				Updates(map[string]any{"undesired_since": at.UTC(), "updated_at": at.UTC()}).Error
			if err != nil {
				return err
			}
		}
		if len(clearIDs) > 0 {
			err := tx.Model(&database.AllowListingEntry{}).
				Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
				Where(clause.IN{Column: alsCol(table, "id"), Values: uuidSliceValues(clearIDs)}).
				Updates(map[string]any{"undesired_since": nil, "updated_at": at.UTC()}).Error
			if err != nil {
				return err
			}
		}
		return nil
	}))
}

// ListEntries returns the ledger of a target. It holds only values that are
// in the tenant, so it is bounded by the report limits.
func (r *AllowListingSync) ListEntries(ctx context.Context, targetID *uuid.UUID) ([]*database.AllowListingEntry, error) {
	var rows []*database.AllowListingEntry
	table := database.ALLOW_LISTING_ENTRY_TABLE
	err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: alsCol(table, "kind")},
			{Column: alsCol(table, "value")},
		}}).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// ListPins returns the pins of a target, at most model.AllowListingSyncMaxPins
func (r *AllowListingSync) ListPins(ctx context.Context, targetID *uuid.UUID) ([]*database.AllowListingPin, error) {
	var rows []*database.AllowListingPin
	table := database.ALLOW_LISTING_PIN_TABLE
	err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: alsCol(table, "created_at")}}}).
		Limit(model.AllowListingSyncMaxPins).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// DeleteExpiredPins removes the pins of a target that expired before now.
// Expiry times are compared in Go, they are not stored in one format.
func (r *AllowListingSync) DeleteExpiredPins(ctx context.Context, targetID *uuid.UUID, now time.Time) error {
	var rows []*database.AllowListingPin
	table := database.ALLOW_LISTING_PIN_TABLE
	err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
		Where(clause.Neq{Column: alsCol(table, "expires_at"), Value: nil}).
		Find(&rows).Error
	if err != nil {
		return errs.Wrap(err)
	}
	ids := []uuid.UUID{}
	for _, row := range rows {
		if row.ID != nil && row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
			ids = append(ids, *row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return errs.Wrap(r.DB.WithContext(ctx).
		Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
		Where(clause.IN{Column: alsCol(table, "id"), Values: uuidSliceValues(ids)}).
		Delete(&database.AllowListingPin{}).Error)
}

// CountPins counts the pins of a target
func (r *AllowListingSync) CountPins(ctx context.Context, targetID *uuid.UUID) (int64, error) {
	var n int64
	err := r.DB.WithContext(ctx).
		Model(&database.AllowListingPin{}).
		Where(clause.Eq{Column: alsCol(database.ALLOW_LISTING_PIN_TABLE, "target_id"), Value: targetID.String()}).
		Count(&n).Error
	return n, errs.Wrap(err)
}

// InsertPin adds a pin
func (r *AllowListingSync) InsertPin(ctx context.Context, pin *database.AllowListingPin) (*uuid.UUID, error) {
	id := uuid.New()
	now := time.Now().UTC()
	pin.ID = &id
	pin.CreatedAt = &now
	pin.UpdatedAt = &now
	if err := r.DB.WithContext(ctx).Create(pin).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	return &id, nil
}

// DeletePin removes a pin of a target. A pin of another target is not found.
func (r *AllowListingSync) DeletePin(ctx context.Context, targetID *uuid.UUID, id *uuid.UUID) error {
	table := database.ALLOW_LISTING_PIN_TABLE
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: alsCol(table, "target_id"), Value: targetID.String()}).
		Where(alsID(table, id)).
		Delete(&database.AllowListingPin{})
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	if res.RowsAffected == 0 {
		return errs.Wrap(gorm.ErrRecordNotFound)
	}
	return nil
}

// DeleteTarget removes a target with its entries and pins
func (r *AllowListingSync) DeleteTarget(ctx context.Context, id *uuid.UUID) error {
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where(clause.Eq{Column: alsCol(database.ALLOW_LISTING_ENTRY_TABLE, "target_id"), Value: id.String()}).
			Delete(&database.AllowListingEntry{}).Error; err != nil {
			return err
		}
		if err := tx.
			Where(clause.Eq{Column: alsCol(database.ALLOW_LISTING_PIN_TABLE, "target_id"), Value: id.String()}).
			Delete(&database.AllowListingPin{}).Error; err != nil {
			return err
		}
		return tx.
			Where(alsID(database.ALLOW_LISTING_TARGET_TABLE, id)).
			Delete(&database.AllowListingTarget{}).Error
	}))
}

// CountingCampaigns returns the campaigns of a scope that are open or closed
// after closedAfter, without the hidden campaigns of programs. A nil company
// is the global scope and returns global campaigns only. Rows closed a little
// before closedAfter can be returned, the caller compares the exact time.
func (r *AllowListingSync) CountingCampaigns(
	ctx context.Context,
	companyID *uuid.UUID,
	closedAfter time.Time,
) ([]*database.Campaign, error) {
	table := database.CAMPAIGN_TABLE
	closed := alsCol(table, "closed_at")
	var rows []*database.Campaign
	q := r.DB.WithContext(ctx).
		Select(
			"id", "name", "company_id", "created_at", "closed_at", "send_start_at",
			"send_end_at", "schedule_at", "campaign_template_id", "program_id",
		)
	q = whereCompany(q, table, companyID)
	err := q.
		Where(clause.Eq{Column: alsCol(table, "program_id"), Value: nil}).
		Where(clause.Or(
			clause.Eq{Column: closed, Value: nil},
			clause.Gte{Column: closed, Value: dayBefore(closedAfter)},
		)).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// TemplatePools returns the template ids of each campaign in pool order
func (r *AllowListingSync) TemplatePools(
	ctx context.Context,
	campaignIDs []uuid.UUID,
) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(campaignIDs) == 0 {
		return out, nil
	}
	table := database.CAMPAIGN_TEMPLATE_POOL_TABLE
	var rows []database.CampaignTemplatePool
	err := r.DB.WithContext(ctx).
		Where(clause.IN{Column: alsCol(table, "campaign_id"), Values: uuidSliceValues(campaignIDs)}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: alsCol(table, "position")}}}).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range rows {
		if row.CampaignID != nil && row.CampaignTemplateID != nil {
			out[*row.CampaignID] = append(out[*row.CampaignID], *row.CampaignTemplateID)
		}
	}
	return out, nil
}

// CountingPrograms returns the programs of a scope that have a start and are
// open or closed after closedAfter. Programs being deleted are left out. A
// nil company is the global scope and returns global programs only.
func (r *AllowListingSync) CountingPrograms(
	ctx context.Context,
	companyID *uuid.UUID,
	closedAfter time.Time,
) ([]*database.Program, error) {
	table := database.PROGRAM_TABLE
	closed := alsCol(table, "closed_at")
	var rows []*database.Program
	q := r.DB.WithContext(ctx).
		Select("id", "name", "company_id", "created_at", "closed_at", "paused_at", "start_at", "end_at")
	q = whereCompany(q, table, companyID)
	err := q.
		Where(clause.Eq{Column: alsCol(table, "deleting_at"), Value: nil}).
		Where(clause.Neq{Column: alsCol(table, "start_at"), Value: nil}).
		Where(clause.Or(
			clause.Eq{Column: closed, Value: nil},
			clause.Gte{Column: closed, Value: dayBefore(closedAfter)},
		)).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// ProgramStepTemplates returns the template ids of the steps of each program
// in step order
func (r *AllowListingSync) ProgramStepTemplates(
	ctx context.Context,
	programIDs []uuid.UUID,
) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(programIDs) == 0 {
		return out, nil
	}
	table := database.PROGRAM_STEP_TABLE
	var rows []database.ProgramStep
	err := r.DB.WithContext(ctx).
		Where(clause.IN{Column: alsCol(table, "program_id"), Values: uuidSliceValues(programIDs)}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: alsCol(table, "program_id")},
			{Column: alsCol(table, "position")},
		}}).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, row := range rows {
		if row.ProgramID != nil && row.CampaignTemplateID != nil {
			out[*row.ProgramID] = append(out[*row.ProgramID], *row.CampaignTemplateID)
		}
	}
	return out, nil
}

func uuidSliceValues(ids []uuid.UUID) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
