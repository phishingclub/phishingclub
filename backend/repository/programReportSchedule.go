package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm/clause"
)

// applyProgramReportRow fills the automated report settings of a program read
// from its row.
func applyProgramReportRow(program *model.Program, row *database.Program) {
	program.ReportEnabled = nullable.NewNullableWithValue(row.ReportEnabled)
	program.ReportInterval = nullable.NewNullableWithValue(row.ReportInterval)
	program.ReportPeriod = nullable.NewNullableWithValue(row.ReportPeriod)
	program.ReportLastPeriodEnd = row.ReportLastPeriodEnd
}

// GetReportEnabledIDs returns the ids of the programs that have started and
// have automated reports on, closed ones included. A program that is being
// deleted is left out, and so is one that is being closed: its last report
// goes out once the close has finished, as the period it closed in. A closed
// program whose last report is sent is left out: the period it closed in
// ends at or after the close, see model.Program.ReportDuePeriod. The times
// are stored in more than one text layout, so they are compared as julian
// days.
func (r *Program) GetReportEnabledIDs(ctx context.Context) ([]*uuid.UUID, error) {
	closedAt := clause.Column{Name: "closed_at"}
	lastEnd := clause.Column{Name: "report_last_period_end"}
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "report_enabled"}, Value: true}).
		Where(clause.Neq{Column: clause.Column{Name: "started_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "closing_at"}, Value: nil}).
		Where(clause.Or(
			clause.Eq{Column: closedAt, Value: nil},
			clause.Eq{Column: lastEnd, Value: nil},
			clause.Expr{SQL: "julianday(?) < julianday(?)", Vars: []any{lastEnd, closedAt}},
		)), "id")
}

// ClaimReportPeriod moves the last reported period end of a program from prev
// to end, only when it still is prev. A nil value is no end. It reports
// whether this call moved it, so two runs never send the same period. A claim
// is refused for a program that is being deleted or closed, so a report that
// was rendered before the delete or close started is not sent.
func (r *Program) ClaimReportPeriod(
	ctx context.Context,
	id *uuid.UUID,
	prev *time.Time,
	end *time.Time,
) (bool, error) {
	return r.moveReportPeriod(ctx, id, prev, end, true)
}

// GiveBackReportPeriod moves the last reported period end of a program back
// from end to prev after a send failed, only when it still is end. It is
// allowed while a close runs, so the period is sent once the close has
// finished.
func (r *Program) GiveBackReportPeriod(
	ctx context.Context,
	id *uuid.UUID,
	end *time.Time,
	prev *time.Time,
) (bool, error) {
	return r.moveReportPeriod(ctx, id, end, prev, false)
}

// moveReportPeriod moves the last reported period end of a program from prev
// to end, only when it still is prev. With onlyOpen the program must not be
// being deleted or closed.
func (r *Program) moveReportPeriod(
	ctx context.Context,
	id *uuid.UUID,
	prev *time.Time,
	end *time.Time,
	onlyOpen bool,
) (bool, error) {
	col := clause.Column{Name: "report_last_period_end"}
	db := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id})
	if onlyOpen {
		db = db.
			Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
			Where(clause.Eq{Column: clause.Column{Name: "closing_at"}, Value: nil})
	}
	if prev == nil {
		db = db.Where(clause.Eq{Column: col, Value: nil})
	} else {
		db = db.Where(clause.Eq{Column: col, Value: prev.UTC()})
	}
	var value any
	if end != nil {
		value = end.UTC()
	}
	// a claim keeps when it was made and the end before it, a give back
	// clears both
	fields := map[string]any{
		"report_last_period_end": value,
		"report_claimed_at":      nil,
		"report_claim_prev_end":  nil,
	}
	if onlyOpen {
		fields["report_claimed_at"] = time.Now().UTC()
		if prev != nil {
			fields["report_claim_prev_end"] = prev.UTC()
		}
	}
	res := db.UpdateColumns(fields)
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	return res.RowsAffected == 1, nil
}

// FinishReportClaim marks the claimed period end of a program as sent, only
// when the period end still is end
func (r *Program) FinishReportClaim(ctx context.Context, id *uuid.UUID, end *time.Time) error {
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Name: "report_last_period_end"}, Value: end.UTC()}).
		UpdateColumns(map[string]any{
			"report_claimed_at":     nil,
			"report_claim_prev_end": nil,
		})
	return errs.Wrap(res.Error)
}

// GiveBackStaleReportClaims gives back every claimed report period that was
// claimed before the given time and never marked as sent, as when the
// process stopped between the claim and the send. The period end moves back
// to the end before the claim, so the next pass sends the period. It returns
// how many periods were given back.
func (r *Program) GiveBackStaleReportClaims(ctx context.Context, before time.Time) (int64, error) {
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Neq{Column: clause.Column{Name: "report_claimed_at"}, Value: nil}).
		Where(clause.Lt{Column: clause.Column{Name: "report_claimed_at"}, Value: before.UTC()}).
		UpdateColumns(map[string]any{
			"report_last_period_end": clause.Expr{SQL: "?", Vars: []any{clause.Column{Name: "report_claim_prev_end"}}},
			"report_claimed_at":      nil,
			"report_claim_prev_end":  nil,
		})
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return res.RowsAffected, nil
}
