package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// goneEnrollment matches the enrollments of people who left a program: exited,
// or completed and missing from its groups since cutoff or before
func goneEnrollment(cutoff time.Time) clause.Expression {
	status := clause.Column{Name: "status"}
	return clause.Or(
		clause.Eq{Column: status, Value: model.ProgramEnrollmentExited},
		clause.And(
			clause.Eq{Column: status, Value: model.ProgramEnrollmentCompleted},
			clause.Neq{Column: clause.Column{Name: "missing_since"}, Value: nil},
			clause.Lte{Column: clause.Column{Name: "missing_since"}, Value: cutoff.UTC()},
		),
	)
}

// GetGoneRecipients returns which of the given people left the program, see
// goneEnrollment. The ids are read in batches.
func (r *Program) GetGoneRecipients(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	cutoff time.Time,
) ([]*uuid.UUID, error) {
	out := []*uuid.UUID{}
	for start := 0; start < len(recipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(recipientIDs))
		ids, err := pluckUUIDs(r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs[start:end])}).
			Where(goneEnrollment(cutoff)), "recipient_id")
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return out, nil
}

// GetGoneWithUnsentSends returns up to limit people who left the program, see
// goneEnrollment, and still have a send of it that is neither sent nor
// cancelled. The sends are read through the unsent campaign index, the person
// column is not read through its index, which holds every campaign. The two
// statuses a person who left can have are named apart from goneEnrollment, so
// the enrollments are read by program and status, not every enrollment of the
// program.
func (r *Program) GetGoneWithUnsentSends(
	ctx context.Context,
	programID *uuid.UUID,
	cutoff time.Time,
	limit int,
) ([]*uuid.UUID, error) {
	gone := r.DB.Model(&database.ProgramEnrollment{}).
		Select("recipient_id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.IN{Column: clause.Column{Name: "status"}, Values: []any{
			model.ProgramEnrollmentExited,
			model.ProgramEnrollmentCompleted,
		}}).
		Where(goneEnrollment(cutoff))
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Distinct("recipient_id").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(untakenSends()).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(clause.Column{Name: "recipient_id"}), gone}}).
		Limit(limit).
		Pluck("recipient_id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// GetCampaignStepID returns the step a hidden campaign of a program sends, or
// gorm.ErrRecordNotFound when the campaign is not linked to a step
func (r *Program) GetCampaignStepID(ctx context.Context, campaignID *uuid.UUID) (*uuid.UUID, error) {
	var link database.ProgramCampaign
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Limit(1).
		Find(&link)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected == 0 || link.ProgramStepID == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return link.ProgramStepID, nil
}

// CancelAllUnsent marks every send of a program that is neither sent nor
// cancelled as cancelled, at most ProgramWriteChunk rows per write with
// PauseBetweenWrites between two writes, so other writers get a turn. The rows
// are found through the unsent campaign index. A close runs it after it let
// go of the lifecycle lock, so it must not be called while that lock or a
// transaction is held.
func (r *Program) CancelAllUnsent(ctx context.Context, programID *uuid.UUID) error {
	for {
		batch := r.DB.
			Model(&database.CampaignRecipient{}).
			Select("id").
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "campaign_id"}, r.programCampaignIDs(programID)}}).
			Where(untakenSends()).
			Limit(ProgramWriteChunk)
		now := utils.NowRFC3339UTC()
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "id"}, batch}}).
			Updates(map[string]any{
				"cancelled_at": now,
				"updated_at":   now,
			})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if res.RowsAffected < ProgramWriteChunk {
			return nil
		}
		if err := PauseBetweenWrites(ctx); err != nil {
			return err
		}
	}
}

// StopEnrollments moves the active enrollments of a program to stopped
// and clears every pending send time, used when the program closes. Each
// write changes at most ProgramWriteChunk rows with PauseAfterWrite between
// two writes. It must not be called while the lifecycle lock or a transaction
// is held.
func (r *Program) StopEnrollments(ctx context.Context, programID *uuid.UUID) error {
	now := utils.NowRFC3339UTC()
	byProgram := programIDEq(clause.Column{Name: "program_id"}, programID)
	if err := r.updateEnrollmentsPaced(ctx, clause.And(
		byProgram,
		clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive},
	), map[string]any{
		"status":        model.ProgramEnrollmentStopped,
		"next_due_at":   nil,
		"missing_since": nil,
		"updated_at":    now,
	}); err != nil {
		return err
	}
	if err := PauseBetweenWrites(ctx); err != nil {
		return err
	}
	return r.updateEnrollmentsPaced(ctx, clause.And(
		byProgram,
		clause.Neq{Column: clause.Column{Name: "next_due_at"}, Value: nil},
	), map[string]any{
		"next_due_at": nil,
		"updated_at":  now,
	})
}

// updateEnrollmentsPaced sets fields on the enrollments that match where, at
// most ProgramWriteChunk rows per write with PauseAfterWrite between two
// writes. The fields must make a row stop matching where, or the loop would
// find the same rows again.
func (r *Program) updateEnrollmentsPaced(ctx context.Context, where clause.Expression, fields map[string]any) error {
	for {
		batch := r.DB.Session(&gorm.Session{NewDB: true}).
			Model(&database.ProgramEnrollment{}).
			Select("rowid").
			Where(where).
			Limit(ProgramWriteChunk)
		start := time.Now()
		res := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "rowid"}, batch}}).
			Updates(fields)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if res.RowsAffected < ProgramWriteChunk {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// SetFieldsAndCampaignPause writes fields on a program and pauses or resumes
// its hidden campaigns in one transaction, so a failure leaves both as they
// were and a retry finds the program as before. A pause marks the open hidden
// campaigns that are not paused, a resume clears the mark on every hidden
// campaign. The campaigns are found through the program column, the paused
// and closed columns are not read through their indexes, which hold every
// campaign. Empty fields change only the campaigns.
func (r *Program) SetFieldsAndCampaignPause(
	ctx context.Context,
	programID *uuid.UUID,
	fields map[string]any,
	paused bool,
) error {
	if len(fields) > 0 {
		AddUpdatedAt(fields)
	}
	now := utils.NowRFC3339UTC()
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(fields) > 0 {
			if err := tx.Model(&database.Program{}).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: programID}).
				Updates(fields).Error; err != nil {
				return err
			}
		}
		campaigns := tx.Model(&database.Campaign{}).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID))
		if paused {
			return campaigns.
				Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "paused_at"})}}).
				Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "closed_at"})}}).
				Updates(map[string]any{"paused_at": now, "updated_at": now}).Error
		}
		return campaigns.
			Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "paused_at"})}}).
			Updates(map[string]any{"paused_at": nil, "updated_at": now}).Error
	}))
}

// ReconcileCampaignPause makes the pause mark of every hidden campaign match
// its program: the open hidden campaigns of a program that is not closed and
// is paused or past its end date are paused, and the hidden campaigns of a
// program that is not paused and not past its end date are resumed. A program
// past its end date sends nothing, its campaigns stay paused until the close
// finishes, or until the end date is moved later. It returns how many
// campaigns it changed. Each statement reads the programs and writes the
// campaigns at once, so a pause or resume that runs at the same time is not
// undone.
func (r *Program) ReconcileCampaignPause(ctx context.Context) (int64, error) {
	now := utils.NowRFC3339UTC()
	endAt := clause.Column{Name: "end_at"}
	at := time.Now().UTC()
	ended := clause.And(
		clause.Neq{Column: endAt, Value: nil},
		clause.Lte{Column: endAt, Value: at},
	)
	notEnded := clause.Or(
		clause.Eq{Column: endAt, Value: nil},
		clause.Gt{Column: endAt, Value: at},
	)
	pausedPrograms := r.DB.Model(&database.Program{}).
		Select("id").
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "paused_at"}, Value: nil},
			ended,
		)).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	// found through the program column, the paused and closed columns hold
	// every campaign
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "program_id"}, pausedPrograms}}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "paused_at"})}}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "closed_at"})}}).
		Updates(map[string]any{"paused_at": now, "updated_at": now})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	changed := res.RowsAffected
	runningPrograms := r.DB.Model(&database.Program{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "paused_at"}, Value: nil}).
		Where(notEnded)
	// found through the paused column, few campaigns are paused, while most
	// programs are not
	res = r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Neq{Column: clause.Column{Name: "paused_at"}, Value: nil}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(clause.Column{Name: "program_id"}), runningPrograms}}).
		Updates(map[string]any{"paused_at": nil, "updated_at": now})
	if res.Error != nil {
		return changed, errs.Wrap(res.Error)
	}
	return changed + res.RowsAffected, nil
}

// RemovedStepSend is a send that is neither sent nor cancelled in an open
// hidden campaign whose step is no longer on its program
type RemovedStepSend struct {
	ProgramID uuid.UUID
	StepID    uuid.UUID
	ProgramStepUnsentSend
}

// IsCampaignStepRemoved reports whether a hidden campaign sends a step that
// its program no longer has. An edit stores the new steps first and cancels
// the sends of a removed step after, so the sender checks it before it sends.
func (r *Program) IsCampaignStepRemoved(ctx context.Context, campaignID *uuid.UUID) (bool, error) {
	steps := r.DB.Model(&database.ProgramStep{}).Select("id")
	var n int64
	err := r.DB.WithContext(ctx).
		Model(&database.ProgramCampaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Where(clause.Expr{SQL: "? NOT IN (?)", Vars: []any{clause.Column{Name: "program_step_id"}, steps}}).
		Count(&n).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return n > 0, nil
}

// programRemovedStepLinkLimit is the most campaign links of removed steps one
// read of GetUnsentSendsOfRemovedSteps looks at
const programRemovedStepLinkLimit = 1000

// GetUnsentSendsOfRemovedSteps returns up to limit sends that are neither sent
// nor cancelled in open hidden campaigns whose step was removed from its
// program. The links are read first, only those whose campaign still holds
// such a send and whose program is not being closed or deleted, in a fixed
// order, so links with nothing left never fill the read. Then the sends of
// their campaigns are read through the unsent campaign index.
func (r *Program) GetUnsentSendsOfRemovedSteps(ctx context.Context, limit int) ([]RemovedStepSend, error) {
	steps := r.DB.Model(&database.ProgramStep{}).Select("id")
	openCampaigns := r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Neq{Column: clause.Column{Name: "program_id"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	// a program being closed or deleted cancels every send itself
	stopping := r.DB.Model(&database.Program{}).
		Select("id").
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "closing_at"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "deleting_at"}, Value: nil},
		))
	linkCampaign := clause.Column{Table: database.PROGRAM_CAMPAIGN_TABLE, Name: "campaign_id"}
	unsent := r.DB.Model(&database.CampaignRecipient{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "sent_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "cancelled_at"}, Value: nil}).
		// a row the sender took is never cancelled, so it does not keep a link
		// in the read
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "last_attempt_at"}, Value: nil}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"}, linkCampaign}})
	links := []database.ProgramCampaign{}
	if err := r.DB.WithContext(ctx).
		Model(&database.ProgramCampaign{}).
		Where(clause.Expr{SQL: "? NOT IN (?)", Vars: []any{clause.Column{Name: "program_step_id"}, steps}}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{linkCampaign, openCampaigns}}).
		Where(clause.Expr{SQL: "? NOT IN (?)", Vars: []any{clause.Column{Table: database.PROGRAM_CAMPAIGN_TABLE, Name: "program_id"}, stopping}}).
		Where(clause.Expr{SQL: "EXISTS (?)", Vars: []any{unsent}}).
		Order(clause.OrderByColumn{Column: clause.Column{Table: database.PROGRAM_CAMPAIGN_TABLE, Name: "id"}}).
		Limit(programRemovedStepLinkLimit).
		Find(&links).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	out := []RemovedStepSend{}
	if len(links) == 0 {
		return out, nil
	}
	byCampaign := make(map[uuid.UUID]database.ProgramCampaign, len(links))
	campaignIDs := make([]*uuid.UUID, 0, len(links))
	for _, l := range links {
		if l.CampaignID == nil || l.ProgramID == nil || l.ProgramStepID == nil {
			continue
		}
		byCampaign[*l.CampaignID] = l
		campaignIDs = append(campaignIDs, l.CampaignID)
	}
	type row struct {
		ID           uuid.UUID
		CampaignID   uuid.UUID
		RecipientID  *uuid.UUID
		AnonymizedID *uuid.UUID
		SendAt       *time.Time
	}
	rows := []row{}
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "recipient_id", "anonymized_id", "send_at").
		Where(clause.IN{Column: clause.Column{Name: "campaign_id"}, Values: uuidValues(campaignIDs)}).
		Where(untakenSends()).
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for _, rw := range rows {
		l, ok := byCampaign[rw.CampaignID]
		if !ok {
			continue
		}
		out = append(out, RemovedStepSend{
			ProgramID: *l.ProgramID,
			StepID:    *l.ProgramStepID,
			ProgramStepUnsentSend: ProgramStepUnsentSend{
				ID:           rw.ID,
				RecipientID:  rw.RecipientID,
				AnonymizedID: rw.AnonymizedID,
				SendAt:       rw.SendAt,
			},
		})
	}
	return out, nil
}

// GetIDsBeingDeleted returns up to limit programs marked as being deleted,
// the oldest mark first
func (r *Program) GetIDsBeingDeleted(ctx context.Context, limit int) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Neq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "deleting_at"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Limit(limit), "id")
}

// GetProgramsWithStaleCampaignSettings returns up to limit programs, not
// closed nor being closed or deleted, with an open hidden campaign whose
// pages, data saving, allow or deny lists or webhooks differ from the
// program. A campaign saves data when the program does and is not anonymous.
func (r *Program) GetProgramsWithStaleCampaignSettings(ctx context.Context, limit int) ([]*uuid.UUID, error) {
	campaign := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	program := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	col := func(table, name string) clause.Column {
		return clause.Column{Table: table, Name: name}
	}
	eq := func(a, b clause.Column) clause.Expression {
		return clause.Expr{SQL: "? = ?", Vars: []any{a, b}}
	}
	campaignAllowDeny := func(allowDenyID clause.Column) *gorm.DB {
		return r.DB.Model(&database.CampaignAllowDeny{}).
			Select("1").
			Where(eq(col(database.CAMPAIGN_ALLOW_DENY_TABLE, "campaign_id"), campaign("id"))).
			Where(eq(col(database.CAMPAIGN_ALLOW_DENY_TABLE, "allow_deny_id"), allowDenyID))
	}
	programAllowDeny := func(allowDenyID clause.Column) *gorm.DB {
		return r.DB.Model(&database.ProgramAllowDeny{}).
			Select("1").
			Where(eq(col(database.PROGRAM_ALLOW_DENY_TABLE, "program_id"), program("id"))).
			Where(eq(col(database.PROGRAM_ALLOW_DENY_TABLE, "allow_deny_id"), allowDenyID))
	}
	// a program list missing on the campaign, and a campaign list not on the
	// program
	missingAllowDeny := r.DB.Model(&database.ProgramAllowDeny{}).
		Select("1").
		Where(eq(col(database.PROGRAM_ALLOW_DENY_TABLE, "program_id"), program("id"))).
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{campaignAllowDeny(col(database.PROGRAM_ALLOW_DENY_TABLE, "allow_deny_id"))}})
	extraAllowDeny := r.DB.Model(&database.CampaignAllowDeny{}).
		Select("1").
		Where(eq(col(database.CAMPAIGN_ALLOW_DENY_TABLE, "campaign_id"), campaign("id"))).
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{programAllowDeny(col(database.CAMPAIGN_ALLOW_DENY_TABLE, "allow_deny_id"))}})
	campaignWebhooks := (&database.CampaignWebhook{}).TableName()
	sameWebhook := func(other string) []clause.Expression {
		return []clause.Expression{
			eq(col(campaignWebhooks, "webhook_id"), col(other, "webhook_id")),
			eq(col(campaignWebhooks, "webhook_include_data"), col(other, "webhook_include_data")),
			eq(col(campaignWebhooks, "webhook_events"), col(other, "webhook_events")),
		}
	}
	missingWebhook := r.DB.Model(&database.ProgramWebhook{}).
		Select("1").
		Where(eq(col(database.PROGRAM_WEBHOOK_TABLE, "program_id"), program("id"))).
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.DB.Model(&database.CampaignWebhook{}).
			Select("1").
			Where(eq(col(campaignWebhooks, "campaign_id"), campaign("id"))).
			Where(clause.And(sameWebhook(database.PROGRAM_WEBHOOK_TABLE)...))}})
	programWebhook := r.DB.Model(&database.ProgramWebhook{}).
		Select("1").
		Where(eq(col(database.PROGRAM_WEBHOOK_TABLE, "program_id"), program("id"))).
		Where(clause.And(sameWebhook(database.PROGRAM_WEBHOOK_TABLE)...))
	extraWebhook := r.DB.Model(&database.CampaignWebhook{}).
		Select("1").
		Where(eq(col(campaignWebhooks, "campaign_id"), campaign("id"))).
		Where(clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{programWebhook}})
	ids := []uuid.UUID{}
	err := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Clauses(clause.Select{Distinct: true, Columns: []clause.Column{campaign("program_id")}}).
		Joins("JOIN ? ON ? = ?", clause.Table{Name: database.PROGRAM_TABLE}, program("id"), campaign("program_id")).
		Where(clause.Eq{Column: campaign("closed_at"), Value: nil}).
		Where(clause.Eq{Column: program("closed_at"), Value: nil}).
		Where(clause.Eq{Column: program("closing_at"), Value: nil}).
		Where(clause.Eq{Column: program("deleting_at"), Value: nil}).
		Where(clause.Or(
			clause.Expr{SQL: "? IS NOT ?", Vars: []any{campaign("deny_page_id"), program("deny_page_id")}},
			clause.Expr{SQL: "? IS NOT ?", Vars: []any{campaign("evasion_page_id"), program("evasion_page_id")}},
			clause.Expr{SQL: "? <> (? AND NOT ?)", Vars: []any{campaign("save_submitted_data"), program("save_submitted_data"), program("is_anonymous")}},
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{missingAllowDeny}},
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{extraAllowDeny}},
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{missingWebhook}},
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{extraWebhook}},
		)).
		Order(clause.OrderByColumn{Column: campaign("program_id")}).
		Limit(limit).
		Scan(&ids).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}
