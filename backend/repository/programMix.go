package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm/clause"
)

// ProgramMixCounts is the number of sends per step for each value of the
// field templates are mixed by: value, then step id, then sends
type ProgramMixCounts map[string]map[uuid.UUID]int

// programMixQuery holds the fixed SQL text that reads one mix field. The text
// is written out whole for each field, nothing is put together from parts.
type programMixQuery struct {
	selectSends string
	groupSends  string
	recipient   string
}

// programMixQueries are the only fields a program may mix by. A send whose
// person link was removed, which an anonymous program does after a while,
// falls back on the department and position kept on the send. City has no
// such copy, so such a send counts with the empty value.
var programMixQueries = map[string]programMixQuery{
	model.TemplateMixDepartment: {
		selectSends: "program_campaigns.program_step_id AS step_id, COALESCE(NULLIF(recipients.department, ''), NULLIF(campaign_recipients.department, ''), '') AS mix_value, COUNT(*) AS sends",
		groupSends:  "program_campaigns.program_step_id, COALESCE(NULLIF(recipients.department, ''), NULLIF(campaign_recipients.department, ''), '')",
		recipient:   "id, department AS value",
	},
	model.TemplateMixPosition: {
		selectSends: "program_campaigns.program_step_id AS step_id, COALESCE(NULLIF(recipients.position, ''), NULLIF(campaign_recipients.position, ''), '') AS mix_value, COUNT(*) AS sends",
		groupSends:  "program_campaigns.program_step_id, COALESCE(NULLIF(recipients.position, ''), NULLIF(campaign_recipients.position, ''), '')",
		recipient:   "id, position AS value",
	},
	model.TemplateMixCity: {
		selectSends: "program_campaigns.program_step_id AS step_id, COALESCE(recipients.city, '') AS mix_value, COUNT(*) AS sends",
		groupSends:  "program_campaigns.program_step_id, COALESCE(recipients.city, '')",
		recipient:   "id, city AS value",
	},
}

// GetMixCounts returns, for each value of the mix field, how many sends of
// each step of the program are planned or sent in [from, to]. Cancelled sends
// do not count. It is one read for the whole program. An unknown field
// returns nothing.
func (r *Program) GetMixCounts(
	ctx context.Context,
	programID *uuid.UUID,
	mixBy string,
	from time.Time,
	to time.Time,
) (ProgramMixCounts, error) {
	out := ProgramMixCounts{}
	q, ok := programMixQueries[mixBy]
	if !ok {
		return out, nil
	}
	rows := []struct {
		StepID   *uuid.UUID
		MixValue string
		Sends    int
	}{}
	res := r.DB.WithContext(ctx).
		Table(database.CAMPAIGN_RECIPIENT_TABLE_NAME).
		Select(q.selectSends).
		Joins("JOIN program_campaigns ON program_campaigns.campaign_id = campaign_recipients.campaign_id").
		Joins("LEFT JOIN recipients ON recipients.id = campaign_recipients.recipient_id").
		Where(programIDEq(clause.Column{Table: database.PROGRAM_CAMPAIGN_TABLE, Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "cancelled_at"}, Value: nil}).
		// send_at is stored as RFC3339 UTC text, so it is compared as such
		Where(clause.Gte{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "send_at"}, Value: utils.RFC3339UTC(from)}).
		Where(clause.Lte{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "send_at"}, Value: utils.RFC3339UTC(to)}).
		Group(q.groupSends).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	for _, row := range rows {
		if row.StepID == nil {
			continue
		}
		if out[row.MixValue] == nil {
			out[row.MixValue] = map[uuid.UUID]int{}
		}
		out[row.MixValue][*row.StepID] += row.Sends
	}
	return out, nil
}

// GetRecipientMixValues returns the value of the mix field of each given
// recipient, in one read. A recipient that is gone is left out. An unknown
// field returns nothing.
func (r *Program) GetRecipientMixValues(
	ctx context.Context,
	recipientIDs []*uuid.UUID,
	mixBy string,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	q, ok := programMixQueries[mixBy]
	if !ok || len(recipientIDs) == 0 {
		return out, nil
	}
	for start := 0; start < len(recipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(recipientIDs))
		part := []struct {
			ID    uuid.UUID
			Value *string
		}{}
		res := r.DB.WithContext(ctx).
			Model(&database.Recipient{}).
			Select(q.recipient).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(recipientIDs[start:end])}).
			Scan(&part)
		if res.Error != nil {
			return nil, errs.Wrap(res.Error)
		}
		for _, row := range part {
			v := ""
			if row.Value != nil {
				v = *row.Value
			}
			out[row.ID] = v
		}
	}
	return out, nil
}
