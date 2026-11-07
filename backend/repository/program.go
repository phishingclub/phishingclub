package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var allowedProgramColumns = assignTableToColumns(database.PROGRAM_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
})

// ProgramOption controls preloading of a program read.
type ProgramOption struct {
	*vo.QueryArgs
	WithSteps           bool
	WithRecipientGroups bool
}

// Program is the repository for programs, their steps, assigned groups,
// enrollments and the link to their hidden campaigns.
type Program struct {
	DB *gorm.DB
}

// Insert stores a new program row and returns its id. Steps and groups are
// stored through ReplaceSteps and ReplaceRecipientGroups.
func (r *Program) Insert(
	ctx context.Context,
	program *model.Program,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := program.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.WithContext(ctx).Model(&database.Program{}).Create(row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// UpdateByID updates the storable fields of a program.
func (r *Program) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	program *model.Program,
) error {
	row := program.ToDBMap()
	AddUpdatedAt(row)
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id = ?", id).
		Updates(row)
	return errs.Wrap(res.Error)
}

// SetFields updates arbitrary columns on a program, used for lifecycle changes
// like pause, resume and close.
func (r *Program) SetFields(
	ctx context.Context,
	id *uuid.UUID,
	fields map[string]any,
) error {
	AddUpdatedAt(fields)
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id = ?", id).
		Updates(fields)
	return errs.Wrap(res.Error)
}

// DeleteByID removes a program and its steps, groups, enrollments and campaign
// links. The hidden campaigns are deleted by the program service before this.
func (r *Program) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("program_id = ?", id).Delete(&database.ProgramStep{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where("program_id = ?", id).Delete(&database.ProgramRecipientGroup{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where("program_id = ?", id).Delete(&database.ProgramEnrollment{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where("program_id = ?", id).Delete(&database.ProgramCampaign{}).Error; err != nil {
			return errs.Wrap(err)
		}
		if err := tx.Where("id = ?", id).Delete(&database.Program{}).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
}

// GetByID loads a program and optionally its steps and assigned groups.
func (r *Program) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *ProgramOption,
) (*model.Program, error) {
	var row database.Program
	res := r.DB.WithContext(ctx).Where("id = ?", id).First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	program, err := ToProgram(&row)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if options != nil && options.WithSteps {
		steps, err := r.GetSteps(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		program.Steps = steps
	}
	if options != nil && options.WithRecipientGroups {
		groups, err := r.getRecipientGroups(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		program.RecipientGroups = groups
		ids := make([]*uuid.UUID, 0, len(groups))
		for _, g := range groups {
			gid := g.ID.MustGet()
			ids = append(ids, &gid)
		}
		program.RecipientGroupIDs = nullable.NewNullableWithValue(ids)
	}
	return program, nil
}

// GetAll lists programs, newest first, scoped to a company.
func (r *Program) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ProgramOption,
) (*model.Result[model.Program], error) {
	result := model.NewEmptyResult[model.Program]()
	db := whereCompany(r.DB.WithContext(ctx), database.PROGRAM_TABLE, companyID)
	db, err := useQuery(db, database.PROGRAM_TABLE, options.QueryArgs, allowedProgramColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	var rows []database.Program
	res := db.Find(&rows)
	if res.Error != nil {
		return result, errs.Wrap(res.Error)
	}
	hasNextPage, err := useHasNextPage(db, database.PROGRAM_TABLE, options.QueryArgs, allowedProgramColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage
	for i := range rows {
		p, err := ToProgram(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		result.Rows = append(result.Rows, p)
	}
	return result, nil
}

// GetActivePrograms returns programs that have started and are neither paused
// nor closed, each with its steps and assigned group ids loaded, for the
// scheduler.
func (r *Program) GetActivePrograms(ctx context.Context, now time.Time) ([]*model.Program, error) {
	var rows []database.Program
	res := r.DB.WithContext(ctx).
		Where(r.runningPrograms(now)).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	programs := make([]*model.Program, 0, len(rows))
	for i := range rows {
		p, err := ToProgram(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		steps, err := r.GetSteps(ctx, rows[i].ID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		p.Steps = steps
		ids, err := r.GetRecipientGroupIDs(ctx, rows[i].ID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		p.RecipientGroupIDs = nullable.NewNullableWithValue(ids)
		programs = append(programs, p)
	}
	return programs, nil
}

// ReplaceSteps makes the program's steps match the given list. A step that
// carries the id of an existing step keeps that id, so the hidden campaigns and
// the delivery history linked to it stay valid. Steps without a known id are
// added and steps left out are removed.
func (r *Program) ReplaceSteps(
	ctx context.Context,
	programID *uuid.UUID,
	steps []*model.ProgramStep,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing := []database.ProgramStep{}
		if err := tx.Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
			Find(&existing).Error; err != nil {
			return errs.Wrap(err)
		}
		// a step keeps its id only while it keeps its template. A new template is
		// a new step, so its sends go into a new hidden campaign
		knownTemplate := map[uuid.UUID]uuid.UUID{}
		for _, e := range existing {
			knownTemplate[*e.ID] = *e.CampaignTemplateID
		}
		// move every position out of the way first, the position index is unique
		for i, e := range existing {
			if err := tx.Model(&database.ProgramStep{}).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: e.ID}).
				Update("position", -(i + 1)).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		kept := map[uuid.UUID]bool{}
		now := time.Now()
		for i, s := range steps {
			templateID := s.CampaignTemplateID.MustGet()
			position := i
			if s.Position.IsSpecified() && !s.Position.IsNull() {
				position = s.Position.MustGet()
			}
			sameStep := false
			if s.ID.IsSpecified() && !s.ID.IsNull() {
				t, ok := knownTemplate[s.ID.MustGet()]
				sameStep = ok && t == templateID && !kept[s.ID.MustGet()]
			}
			if sameStep {
				id := s.ID.MustGet()
				kept[id] = true
				if err := tx.Model(&database.ProgramStep{}).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
					Updates(map[string]any{
						"position":   position,
						"updated_at": now,
					}).Error; err != nil {
					return errs.Wrap(err)
				}
				continue
			}
			id := uuid.New()
			row := database.ProgramStep{
				ID:                 &id,
				CreatedAt:          &now,
				UpdatedAt:          &now,
				ProgramID:          programID,
				CampaignTemplateID: &templateID,
				Position:           position,
			}
			if err := tx.Create(&row).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		for _, e := range existing {
			if kept[*e.ID] {
				continue
			}
			if err := tx.Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: e.ID}).
				Delete(&database.ProgramStep{}).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		return nil
	})
}

// GetSteps returns a program's steps ordered by position.
func (r *Program) GetSteps(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*model.ProgramStep, error) {
	var rows []database.ProgramStep
	res := r.DB.WithContext(ctx).
		Where("program_id = ?", programID).
		Order("position ASC").
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	steps := make([]*model.ProgramStep, 0, len(rows))
	for i := range rows {
		steps = append(steps, ToProgramStep(&rows[i]))
	}
	return steps, nil
}

// ReplaceRecipientGroups deletes a program's assigned groups and inserts the
// given ones.
func (r *Program) ReplaceRecipientGroups(
	ctx context.Context,
	programID *uuid.UUID,
	groupIDs []*uuid.UUID,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("program_id = ?", programID).Delete(&database.ProgramRecipientGroup{}).Error; err != nil {
			return errs.Wrap(err)
		}
		for _, gid := range groupIDs {
			row := database.ProgramRecipientGroup{
				ProgramID:        programID,
				RecipientGroupID: gid,
			}
			if err := tx.Create(&row).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		return nil
	})
}

// GetRecipientGroupIDs returns the ids of a program's assigned groups.
func (r *Program) GetRecipientGroupIDs(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*uuid.UUID, error) {
	var rows []database.ProgramRecipientGroup
	res := r.DB.WithContext(ctx).
		Where("program_id = ?", programID).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	ids := make([]*uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].RecipientGroupID)
	}
	return ids, nil
}

func (r *Program) getRecipientGroups(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*model.RecipientGroup, error) {
	var rows []database.RecipientGroup
	res := r.DB.WithContext(ctx).
		Joins("JOIN program_recipient_groups ON program_recipient_groups.recipient_group_id = recipient_groups.id").
		Where("program_recipient_groups.program_id = ?", programID).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	groups := make([]*model.RecipientGroup, 0, len(rows))
	for i := range rows {
		g, err := ToRecipientGroup(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// InsertStepCampaign inserts a hidden, program managed campaign row from the
// model. It sets no recipient groups, unlike the campaign repository Insert.
func (r *Program) InsertStepCampaign(
	ctx context.Context,
	campaign *model.Campaign,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := campaign.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	res := r.DB.WithContext(ctx).Model(&database.Campaign{}).Create(row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetProgramCampaigns returns the link rows for a step and loop iteration,
// newest first. There is more than one when a closed step campaign was
// followed by a new one.
func (r *Program) GetProgramCampaigns(
	ctx context.Context,
	programID *uuid.UUID,
	stepID *uuid.UUID,
	loopIndex int,
) ([]*database.ProgramCampaign, error) {
	rows := []*database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "program_step_id"}, Value: stepID}).
		Where(clause.Eq{Column: clause.Column{Name: "loop_index"}, Value: loopIndex}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}, Desc: true}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// ProgramPersonSend is the state of one person's send in a hidden campaign
type ProgramPersonSend struct {
	ID          uuid.UUID
	SentAt      *time.Time
	CancelledAt *time.Time
}

// GetPersonSend returns a person's send in a campaign, matched by recipient id
// or pseudonym, or nil when there is none
func (r *Program) GetPersonSend(
	ctx context.Context,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) (*ProgramPersonSend, error) {
	rows := []ProgramPersonSend{}
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "sent_at", "cancelled_at").
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Where(r.personMatch(recipientID, anonymizedID)).
		Limit(1).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// ReviveCancelledSend puts a cancelled, never sent row back in the send queue
// at a new time. The unique campaign and recipient index allows one row per
// person per campaign, so a returning person reuses it.
func (r *Program) ReviveCancelledSend(
	ctx context.Context,
	campaignRecipientID *uuid.UUID,
	sendAt time.Time,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: campaignRecipientID}).
		Where(clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Updates(map[string]any{
			"cancelled_at":    nil,
			"send_at":         utils.RFC3339UTC(sendAt),
			"last_attempt_at": nil,
			"updated_at":      utils.NowRFC3339UTC(),
		})
	return errs.Wrap(res.Error)
}

// ProgramNewestSend is the newest planned send of a person's scheduled steps
type ProgramNewestSend struct {
	SendAt time.Time
	StepID uuid.UUID
}

// GetNewestScheduledSend returns the latest planned send time of a person over
// the program's steps, nil when there is none. Cancelled sends do not count. A
// send of a step removed since then still counts.
func (r *Program) GetNewestScheduledSend(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
) (*ProgramNewestSend, error) {
	rows := []struct {
		CampaignID uuid.UUID
		SendAt     *time.Time
	}{}
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("campaign_id", "send_at").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil}).
		Where(r.personMatch(recipientID, anonymizedID)).
		Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	var newest *ProgramNewestSend
	var newestCampaign uuid.UUID
	for _, row := range rows {
		if row.SendAt == nil {
			continue
		}
		if newest == nil || row.SendAt.After(newest.SendAt) {
			newest = &ProgramNewestSend{SendAt: *row.SendAt}
			newestCampaign = row.CampaignID
		}
	}
	if newest == nil {
		return nil, nil
	}
	link := database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: newestCampaign}).
		Limit(1).
		Find(&link)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected > 0 && link.ProgramStepID != nil {
		newest.StepID = *link.ProgramStepID
	}
	return newest, nil
}

// ProgramHeldSend is a send of a program that is not sent and not cancelled.
type ProgramHeldSend struct {
	ID          uuid.UUID
	CampaignID  uuid.UUID
	RecipientID *uuid.UUID
	SendAt      *time.Time
}

// GetHeldSends returns the sends of a program's open hidden campaigns that are
// neither sent nor cancelled
func (r *Program) GetHeldSends(
	ctx context.Context,
	programID *uuid.UUID,
) ([]ProgramHeldSend, error) {
	openCampaigns := r.programCampaignIDs(programID).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	rows := []ProgramHeldSend{}
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "recipient_id", "send_at").
		Where("campaign_id IN (?)", openCampaigns).
		Where(clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// SetSendAt moves the given sends that are not sent yet to a new send time, in
// batches
func (r *Program) SetSendAt(
	ctx context.Context,
	campaignRecipientIDs []*uuid.UUID,
	sendAt time.Time,
) error {
	for start := 0; start < len(campaignRecipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(campaignRecipientIDs))
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(campaignRecipientIDs[start:end])}).
			Where(clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
			Updates(map[string]any{
				"send_at":    utils.RFC3339UTC(sendAt),
				"updated_at": utils.NowRFC3339UTC(),
			})
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// CountEnrollments returns how many people were ever enrolled in a program,
// whatever their status
func (r *Program) CountEnrollments(ctx context.Context, programID *uuid.UUID) (int64, error) {
	var count int64
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Count(&count)
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return count, nil
}

// GetAnonymousProgramIDs returns the ids of every anonymous program
func (r *Program) GetAnonymousProgramIDs(ctx context.Context) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "is_anonymous"}, Value: true}).
		Pluck("id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// RemoveIdentityOfSendsBefore removes the recipient link and releases the lure
// code of the sent rows of a program's hidden campaigns sent before the given
// time, the same way a closed anonymous campaign is finalized. Only rows that
// carry a pseudonym are changed, so the results stay countable by it, and
// rows not yet sent are never touched.
func (r *Program) RemoveIdentityOfSendsBefore(
	ctx context.Context,
	programID *uuid.UUID,
	before time.Time,
) (int64, error) {
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		// sent_at is stored as RFC3339 UTC text, so it is compared as such
		Where(clause.Lt{Column: clause.Column{Name: "sent_at"}, Value: utils.RFC3339UTC(before)}).
		Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil}).
		Where(clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
		Updates(map[string]any{
			"recipient_id": nil,
			"lure_code":    nil,
			"updated_at":   utils.NowRFC3339UTC(),
		})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return res.RowsAffected, nil
}

// CountActiveEnrollments returns how many people are active in a program
func (r *Program) CountActiveEnrollments(ctx context.Context, programID *uuid.UUID) (int64, error) {
	var count int64
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}).
		Count(&count)
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return count, nil
}

// InsertProgramCampaign creates the link between a step iteration and a hidden
// campaign.
func (r *Program) InsertProgramCampaign(
	ctx context.Context,
	programID *uuid.UUID,
	stepID *uuid.UUID,
	loopIndex int,
	campaignID *uuid.UUID,
) error {
	now := time.Now()
	id := uuid.New()
	row := database.ProgramCampaign{
		ID:            &id,
		CreatedAt:     &now,
		ProgramID:     programID,
		ProgramStepID: stepID,
		LoopIndex:     loopIndex,
		CampaignID:    campaignID,
	}
	res := r.DB.WithContext(ctx).Create(&row)
	return errs.Wrap(res.Error)
}

// GetCampaignIDs returns the ids of the hidden campaigns a program owns. It reads
// campaigns.program_id rather than the links, so it still finds every campaign
// after a delete that stopped half way.
func (r *Program) GetCampaignIDs(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Pluck("id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// DeleteProgramCampaignByCampaignID removes the link between a hidden campaign
// and its program step
func (r *Program) DeleteProgramCampaignByCampaignID(
	ctx context.Context,
	campaignID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}).
		Delete(&database.ProgramCampaign{})
	return errs.Wrap(res.Error)
}

// SetEnrollmentPseudonymIfEmpty stores a pseudonym on an enrollment that has none
func (r *Program) SetEnrollmentPseudonymIfEmpty(
	ctx context.Context,
	enrollmentID *uuid.UUID,
	pseudonym uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: enrollmentID}).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
		Update("anonymized_id", pseudonym)
	return errs.Wrap(res.Error)
}

// StopEnrollments clears every pending send time of a program's enrollments
// and moves the active ones to stopped, used when the program closes
func (r *Program) StopEnrollments(
	ctx context.Context,
	programID *uuid.UUID,
) error {
	now := utils.NowRFC3339UTC()
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}).
		Updates(map[string]any{
			"status":        model.ProgramEnrollmentStopped,
			"next_due_at":   nil,
			"missing_since": nil,
			"updated_at":    now,
		})
	if res.Error != nil {
		return errs.Wrap(res.Error)
	}
	res = r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Neq{Column: clause.Column{Name: "next_due_at"}, Value: nil}).
		Updates(map[string]any{
			"next_due_at": nil,
			"updated_at":  now,
		})
	return errs.Wrap(res.Error)
}

// ClearEnrollmentPseudonyms removes the pseudonym from every enrollment of a
// program
func (r *Program) ClearEnrollmentPseudonyms(
	ctx context.Context,
	programID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Update("anonymized_id", nil)
	return errs.Wrap(res.Error)
}

// DeleteEnrollmentsByRecipientID removes every program enrollment of a recipient
func (r *Program) DeleteEnrollmentsByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Delete(&database.ProgramEnrollment{})
	return errs.Wrap(res.Error)
}

// GetNamesByRecipientGroupID returns the names of the programs that the
// recipient group is assigned to
func (r *Program) GetNamesByRecipientGroupID(
	ctx context.Context,
	groupID *uuid.UUID,
) ([]string, error) {
	names := []string{}
	assigned := r.DB.Model(&database.ProgramRecipientGroup{}).
		Select("program_id").
		Where(clause.Eq{Column: clause.Column{Name: "recipient_group_id"}, Value: groupID})
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id IN (?)", assigned).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "name"}}).
		Pluck("name", &names)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return names, nil
}

// GetIDsByCompanyID returns the ids of the programs that belong to a company
func (r *Program) GetIDsByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: companyColumn(database.PROGRAM_TABLE), Value: companyID}).
		Pluck("id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// GetNamesByCampaignTemplateID returns the names of the programs that have a
// step using the campaign template
func (r *Program) GetNamesByCampaignTemplateID(
	ctx context.Context,
	templateID *uuid.UUID,
) ([]string, error) {
	names := []string{}
	stepsUsing := r.DB.Model(&database.ProgramStep{}).
		Select("program_id").
		Where(clause.Eq{Column: clause.Column{Name: "campaign_template_id"}, Value: templateID})
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id IN (?)", stepsUsing).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "name"}}).
		Pluck("name", &names)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return names, nil
}

// ProgramDelivery is one step a recipient has been given in a loop, with the
// template the hidden campaign of the step sends
type ProgramDelivery struct {
	RecipientID uuid.UUID
	StepID      uuid.UUID
	LoopIndex   int
	TemplateID  *uuid.UUID
}

// deliveredSends selects the campaign recipient rows that count as a delivery:
// not cancelled, and either sent or still waiting in a campaign that is open.
// A send that will never go out does not use up its step.
func (r *Program) deliveredSends() *gorm.DB {
	openCampaigns := r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
	return r.DB.Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil}).
		Where(r.DB.
			Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
			Or("campaign_id IN (?)", openCampaigns))
}

// GetDeliveredStepIDs returns the steps a recipient has been given in a loop.
// A cancelled send is not a delivery, so the step is offered again. The
// pseudonym, when given, also matches sends whose recipient link was removed.
func (r *Program) GetDeliveredStepIDs(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
	loopIndex int,
) (map[uuid.UUID]bool, error) {
	deliveries, err := r.GetLoopDeliveries(ctx, programID, recipientID, anonymizedID, loopIndex)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(deliveries))
	for _, d := range deliveries {
		out[d.StepID] = true
	}
	return out, nil
}

// GetLoopDeliveries returns the steps a recipient has been given in a loop,
// each with the template its hidden campaign sends. It matches sends the same
// way as GetDeliveredStepIDs.
func (r *Program) GetLoopDeliveries(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
	loopIndex int,
) ([]ProgramDelivery, error) {
	sentTo := r.deliveredSends().
		Select("campaign_id").
		Where(r.personMatch(recipientID, anonymizedID))
	links := []database.ProgramCampaign{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "loop_index"}, Value: loopIndex}).
		Where("campaign_id IN (?)", sentTo).
		Find(&links)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	templates, err := r.campaignTemplates(ctx, links)
	if err != nil {
		return nil, err
	}
	out := make([]ProgramDelivery, 0, len(links))
	for _, l := range links {
		d := ProgramDelivery{StepID: *l.ProgramStepID, LoopIndex: l.LoopIndex}
		if recipientID != nil {
			d.RecipientID = *recipientID
		}
		if t, ok := templates[*l.CampaignID]; ok {
			d.TemplateID = &t
		}
		out = append(out, d)
	}
	return out, nil
}

// campaignTemplates returns the template of each linked hidden campaign that
// has one
func (r *Program) campaignTemplates(
	ctx context.Context,
	links []database.ProgramCampaign,
) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	if len(links) == 0 {
		return out, nil
	}
	ids := make([]*uuid.UUID, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.CampaignID)
	}
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		rows := []struct {
			ID                 uuid.UUID
			CampaignTemplateID *uuid.UUID
		}{}
		if err := r.DB.WithContext(ctx).
			Model(&database.Campaign{}).
			Select("id", "campaign_template_id").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for _, row := range rows {
			if row.CampaignTemplateID != nil {
				out[row.ID] = *row.CampaignTemplateID
			}
		}
	}
	return out, nil
}

// personMatch matches campaign recipient rows of a person by recipient id, or
// by pseudonym when one is given
func (r *Program) personMatch(recipientID *uuid.UUID, anonymizedID *uuid.UUID) *gorm.DB {
	byRecipient := clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}
	if anonymizedID == nil {
		return r.DB.Where(byRecipient)
	}
	return r.DB.Where(byRecipient).
		Or(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: anonymizedID})
}

// GetDeliveries returns every step given to the recipients of a program. When
// recipientIDs is not nil only those recipients' deliveries are returned.
func (r *Program) GetDeliveries(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) ([]ProgramDelivery, error) {
	links := []database.ProgramCampaign{}
	if err := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID}).
		Find(&links).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	type sent struct {
		CampaignID  uuid.UUID
		RecipientID uuid.UUID
	}
	sends := []sent{}
	if recipientIDs != nil && len(recipientIDs) == 0 {
		return []ProgramDelivery{}, nil
	}
	db := r.deliveredSends().
		WithContext(ctx).
		Select("campaign_id", "recipient_id").
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil})
	if recipientIDs != nil {
		db = db.Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: idValues(recipientIDs)})
	}
	if err := db.Scan(&sends).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	templates, err := r.campaignTemplates(ctx, links)
	if err != nil {
		return nil, err
	}
	byCampaign := map[uuid.UUID]database.ProgramCampaign{}
	for _, l := range links {
		byCampaign[*l.CampaignID] = l
	}
	out := make([]ProgramDelivery, 0, len(sends))
	for _, sd := range sends {
		l, ok := byCampaign[sd.CampaignID]
		if !ok {
			continue
		}
		d := ProgramDelivery{RecipientID: sd.RecipientID, StepID: *l.ProgramStepID, LoopIndex: l.LoopIndex}
		if t, ok := templates[sd.CampaignID]; ok {
			d.TemplateID = &t
		}
		out = append(out, d)
	}
	return out, nil
}

// ProgramListSummary is what the program list shows for one program
type ProgramListSummary struct {
	Enrolled        int64
	NextDueAt       *time.Time
	StepTemplateIDs []uuid.UUID
	// Simulations counts the steps
	Simulations int
}

// sqliteTimeLayouts are the layouts SQLite returns a time in when it comes
// from an aggregate, which has no declared column type to convert from
var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

func parseSQLiteTime(v string) (time.Time, bool) {
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// GetListSummaries returns the active enrollment count, the earliest next send
// and the step templates of each given program in three queries in total.
func (r *Program) GetListSummaries(
	ctx context.Context,
	programIDs []*uuid.UUID,
) (map[uuid.UUID]*ProgramListSummary, error) {
	out := map[uuid.UUID]*ProgramListSummary{}
	if len(programIDs) == 0 {
		return out, nil
	}
	for _, id := range programIDs {
		out[*id] = &ProgramListSummary{}
	}
	inPrograms := clause.IN{Column: clause.Column{Name: "program_id"}, Values: uuidValues(programIDs)}
	isActive := clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}

	var counts []struct {
		ProgramID uuid.UUID
		Enrolled  int64
	}
	err := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("program_id, COUNT(*) AS enrolled").
		Where(inPrograms).
		Where(isActive).
		Group("program_id").
		Scan(&counts).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, c := range counts {
		if row, ok := out[c.ProgramID]; ok {
			row.Enrolled = c.Enrolled
		}
	}

	var nexts []struct {
		ProgramID uuid.UUID
		NextDueAt *string
	}
	err = r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("program_id, MIN(next_due_at) AS next_due_at").
		Where(inPrograms).
		Where(isActive).
		Where(clause.Neq{Column: clause.Column{Name: "next_due_at"}, Value: nil}).
		Group("program_id").
		Scan(&nexts).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, n := range nexts {
		row, ok := out[n.ProgramID]
		if !ok || n.NextDueAt == nil {
			continue
		}
		if t, ok := parseSQLiteTime(*n.NextDueAt); ok {
			row.NextDueAt = &t
		}
	}

	var steps []database.ProgramStep
	err = r.DB.WithContext(ctx).
		Where(inPrograms).
		Find(&steps).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, st := range steps {
		row, ok := out[*st.ProgramID]
		if !ok {
			continue
		}
		if st.CampaignTemplateID != nil {
			row.StepTemplateIDs = append(row.StepTemplateIDs, *st.CampaignTemplateID)
		}
		row.Simulations++
	}
	return out, nil
}

// GetTemplateUsability returns for each existing template id whether it can be
// sent. A template that is missing from the result no longer exists.
func (r *Program) GetTemplateUsability(
	ctx context.Context,
	templateIDs []uuid.UUID,
) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(templateIDs) == 0 {
		return out, nil
	}
	values := make([]any, 0, len(templateIDs))
	for _, id := range templateIDs {
		values = append(values, id)
	}
	var rows []database.CampaignTemplate
	err := r.DB.WithContext(ctx).
		Select("id", "is_usable").
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: values}).
		Find(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, t := range rows {
		out[*t.ID] = t.IsUsable
	}
	return out, nil
}

// runningPrograms is the condition for a program the scheduler runs: its start
// time has passed and it is neither paused nor closed. A draft has no start.
func (r *Program) runningPrograms(now time.Time) clause.Expression {
	return clause.And(
		clause.Eq{Column: clause.Column{Name: "paused_at"}, Value: nil},
		clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil},
		clause.Neq{Column: clause.Column{Name: "start_at"}, Value: nil},
		clause.Lte{Column: clause.Column{Name: "start_at"}, Value: now.UTC()},
	)
}

// programCampaignIDs selects the ids of the hidden campaigns a program owns
func (r *Program) programCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "program_id"}, Value: programID})
}

// CancelUnsentForProgramRecipients marks the not yet sent step sends of the
// given recipients as cancelled, so program leavers receive no further mail. The
// campaign set is chosen with a subquery on program_campaigns, and the recipient
// ids are chunked to stay well under the SQLite bind parameter ceiling.
func (r *Program) CancelUnsentForProgramRecipients(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) error {
	if len(recipientIDs) == 0 {
		return nil
	}
	const chunkSize = 800
	now := utils.NowRFC3339UTC()
	for start := 0; start < len(recipientIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(recipientIDs) {
			end = len(recipientIDs)
		}
		res := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
			Where("recipient_id IN ?", recipientIDs[start:end]).
			Where("sent_at IS NULL AND cancelled_at IS NULL").
			Update("cancelled_at", now)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// CancelUnsentFrom marks the not yet sent sends of a program that are planned
// at or after the given time as cancelled, used when the program gets an end
// date so nothing goes out after it
func (r *Program) CancelUnsentFrom(
	ctx context.Context,
	programID *uuid.UUID,
	at time.Time,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where("campaign_id IN (?)", r.programCampaignIDs(programID)).
		Where(clause.Eq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "cancelled_at"}, Value: nil}).
		Where(clause.Gte{Column: clause.Column{Name: "send_at"}, Value: utils.RFC3339UTC(at)}).
		Update("cancelled_at", utils.NowRFC3339UTC())
	return errs.Wrap(res.Error)
}

// GetIDsDueToEnd returns the ids of the programs that are not closed and whose
// end date has passed
func (r *Program) GetIDsDueToEnd(ctx context.Context, now time.Time) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Neq{Column: clause.Column{Name: "end_at"}, Value: nil}).
		Where(clause.Lte{Column: clause.Column{Name: "end_at"}, Value: now.UTC()}).
		Pluck("id", &ids)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// programEnrollmentBatch is how many enrollments one insert or update writes
const programEnrollmentBatch = 200

// InsertEnrollments stores new enrollments in batches
func (r *Program) InsertEnrollments(
	ctx context.Context,
	enrollments []*database.ProgramEnrollment,
) error {
	if len(enrollments) == 0 {
		return nil
	}
	now := time.Now()
	for _, enrollment := range enrollments {
		if enrollment.ID == nil {
			id := uuid.New()
			enrollment.ID = &id
		}
		enrollment.CreatedAt = &now
		enrollment.UpdatedAt = &now
		// times are stored in UTC so text compares in the due scan hold
		for _, t := range []**time.Time{&enrollment.RoundStartAt, &enrollment.NextDueAt, &enrollment.LastSendAt, &enrollment.MissingSince} {
			if *t != nil {
				u := (*t).UTC()
				*t = &u
			}
		}
	}
	res := r.DB.WithContext(ctx).CreateInBatches(enrollments, programEnrollmentBatch)
	return errs.Wrap(res.Error)
}

// UpdateEnrollmentsFields sets the same columns on many enrollments, in batches
func (r *Program) UpdateEnrollmentsFields(
	ctx context.Context,
	ids []*uuid.UUID,
	fields map[string]any,
) error {
	for k, v := range fields {
		if t, ok := v.(time.Time); ok {
			fields[k] = t.UTC()
		}
	}
	AddUpdatedAt(fields)
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		values := make([]any, 0, end-start)
		for _, id := range ids[start:end] {
			values = append(values, id)
		}
		res := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: values}).
			Updates(fields)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// UpdateEnrollmentFields updates columns on an enrollment.
func (r *Program) UpdateEnrollmentFields(
	ctx context.Context,
	id *uuid.UUID,
	fields map[string]any,
) error {
	// times are stored in UTC so text compares in the due scan hold
	for k, v := range fields {
		if t, ok := v.(time.Time); ok {
			fields[k] = t.UTC()
		}
	}
	AddUpdatedAt(fields)
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where("id = ?", id).
		Updates(fields)
	return errs.Wrap(res.Error)
}

// GetEnrollmentByProgramRecipient returns the enrollment for a recipient in a
// program, or gorm.ErrRecordNotFound.
func (r *Program) GetEnrollmentByProgramRecipient(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
) (*database.ProgramEnrollment, error) {
	var row database.ProgramEnrollment
	res := r.DB.WithContext(ctx).
		Where("program_id = ? AND recipient_id = ?", programID, recipientID).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &row, nil
}

// GetEnrollmentsByProgramID returns all enrollments of a program.
func (r *Program) GetEnrollmentsByProgramID(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*database.ProgramEnrollment, error) {
	var rows []*database.ProgramEnrollment
	res := r.DB.WithContext(ctx).
		Where("program_id = ?", programID).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// GetDueEnrollments returns active enrollments due at or before now, bounded by
// limit so a large program drains over several ticks.
func (r *Program) GetDueEnrollments(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]*database.ProgramEnrollment, error) {
	var rows []*database.ProgramEnrollment
	// draft, scheduled, paused and closed programs do not advance, so their due
	// rows must not take the places of other programs in the batch
	running := r.DB.Model(&database.Program{}).
		Select("id").
		Where(r.runningPrograms(now))
	res := r.DB.WithContext(ctx).
		Where("status = ? AND next_due_at IS NOT NULL AND next_due_at <= ?", model.ProgramEnrollmentActive, now.UTC()).
		// a person missing from the groups waits until they are back or exited
		Where(clause.Eq{Column: clause.Column{Name: "missing_since"}, Value: nil}).
		Where("program_id IN (?)", running).
		Order("next_due_at ASC").
		Limit(limit).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// ToProgram maps a program row to the model, without steps or groups.
func ToProgram(row *database.Program) (*model.Program, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	} else {
		companyID.SetNull()
	}
	closedAt := nullable.NewNullNullable[time.Time]()
	if row.ClosedAt != nil {
		closedAt.Set(*row.ClosedAt)
	} else {
		closedAt.SetNull()
	}
	pausedAt := nullable.NewNullNullable[time.Time]()
	if row.PausedAt != nil {
		pausedAt.Set(*row.PausedAt)
	} else {
		pausedAt.SetNull()
	}
	weekDays := nullable.NewNullNullable[vo.CampaignWeekDays]()
	if row.ConstraintWeekDays != nil {
		weekDays.Set(*vo.NewCampaignWeekDaysMust(*row.ConstraintWeekDays))
	} else {
		weekDays.SetNull()
	}
	startTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintStartTime != nil {
		startTime.Set(*vo.NewCampaignTimeConstraintMust(*row.ConstraintStartTime))
	} else {
		startTime.SetNull()
	}
	endTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintEndTime != nil {
		endTime.Set(*vo.NewCampaignTimeConstraintMust(*row.ConstraintEndTime))
	} else {
		endTime.SetNull()
	}
	startAt := nullable.NewNullNullable[time.Time]()
	if row.StartAt != nil {
		startAt.Set(*row.StartAt)
	} else {
		startAt.SetNull()
	}
	program := &model.Program{
		ID:                    id,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		Name:                  name,
		CompanyID:             companyID,
		ClosedAt:              closedAt,
		PausedAt:              pausedAt,
		OrderMode:             nullable.NewNullableWithValue(row.OrderMode),
		LoopMode:              nullable.NewNullableWithValue(row.LoopMode),
		StepIntervalMinutes:   nullable.NewNullableWithValue(row.StepIntervalMinutes),
		SpreadMinutes:         nullable.NewNullableWithValue(row.SpreadMinutes),
		NewMemberDelayMinutes: nullable.NewNullableWithValue(row.NewMemberDelayMinutes),
		TimeZone:              nullable.NewNullableWithValue(row.TimeZone),
		StartedAt:             row.StartedAt,
		ConstraintWeekDays:    weekDays,
		ConstraintStartTime:   startTime,
		ConstraintEndTime:     endTime,
		IsAnonymous:           nullable.NewNullableWithValue(row.IsAnonymous),
	}
	program.StartAt = startAt
	endAt := nullable.NewNullNullable[time.Time]()
	if row.EndAt != nil {
		endAt.Set(*row.EndAt)
	}
	program.EndAt = endAt
	program.EndedAutomatically = row.ClosedAt != nil && row.EndAt != nil && !row.ClosedAt.Before(*row.EndAt)
	program.BlackoutDates = nullable.NewNullableWithValue(model.ParseProgramBlackouts(row.BlackoutDates))
	applyProgramSettingsRow(program, row)
	program.Status = program.StatusAt(time.Now())
	return program, nil
}

// ToProgramStep maps a step row to the model.
func ToProgramStep(row *database.ProgramStep) *model.ProgramStep {
	step := &model.ProgramStep{
		ID:                 nullable.NewNullableWithValue(*row.ID),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		ProgramID:          nullable.NewNullableWithValue(*row.ProgramID),
		CampaignTemplateID: nullable.NewNullableWithValue(*row.CampaignTemplateID),
		Position:           nullable.NewNullableWithValue(row.Position),
	}
	return step
}

// uuidValues turns ids into values for an IN clause
func uuidValues(ids []*uuid.UUID) []any {
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, *id)
	}
	return values
}
