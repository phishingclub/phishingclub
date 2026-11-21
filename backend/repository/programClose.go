package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The writes in this file run when an anonymous program closes, after its
// hidden campaigns are closed and their rows anonymized. They remove what
// could still link a person to outcomes: the pseudonym shared by every send
// of a person, the department and title kept on each send, the sends that
// were never sent and the exact enrollment dates. Each write is paced, must
// not run while the lifecycle lock or a transaction is held, and can run
// again: a close that stops half way runs every step again.

// pseudonymRotateBatch is how many pseudonyms one transaction of
// RotateSendPseudonyms rewrites. A pseudonym has one send per template and
// round, each with a few events, so a batch writes a few hundred rows.
const pseudonymRotateBatch = 50

// closeWriteTries is how many times one chunk write of a program close is
// tried when SQLite reports the database as busy
const closeWriteTries = 5

// isBusy reports whether err is a busy or locked error of SQLite. A
// transaction that read before its first write gets SQLITE_BUSY_SNAPSHOT at
// once when another writer committed in between, without waiting out the busy
// timeout, so such a write is worth trying again.
func isBusy(err error) bool {
	var e sqlite3.Error
	if errors.As(err, &e) {
		return e.Code == sqlite3.ErrBusy || e.Code == sqlite3.ErrLocked
	}
	return false
}

// retryBusy runs write and runs it again, up to closeWriteTries times in all,
// while it fails with a busy error, see isBusy. It waits PauseAfterWrite
// between two tries, so the other writers get their turn. It must not be
// called while a transaction or the program lifecycle lock is held.
func retryBusy(ctx context.Context, write func() error) error {
	for try := 1; ; try++ {
		err := write()
		if err == nil || try >= closeWriteTries || !isBusy(err) {
			return err
		}
		if err := PauseAfterWrite(ctx, MaxBusyWait); err != nil {
			return err
		}
	}
}

// DeleteUnsentSends deletes the sends of a program that were never sent, and
// their events. Results count sent rows only, so no count changes. The rows
// of each hidden campaign are deleted in chunks of at most ProgramWriteChunk,
// each chunk in one transaction with PauseAfterWrite between two chunks. A
// deleted row leaves the read, so a call that stops half way can run again.
func (r *Program) DeleteUnsentSends(ctx context.Context, programID *uuid.UUID) error {
	campaignIDs, err := r.GetCampaignIDs(ctx, programID)
	if err != nil {
		return err
	}
	for _, campaignID := range campaignIDs {
		if err := r.deleteUnsentSendsOf(ctx, campaignID); err != nil {
			return err
		}
	}
	return nil
}

// deleteUnsentSendsOf deletes the unsent rows of one hidden campaign and their
// events, see DeleteUnsentSends. The events of a person in the campaign
// belong to an unsent row only while no sent row of the campaign names the
// person. Every read is done before the transaction, so its first statement
// is a write: a transaction that reads first gets a busy error at once when
// another writer commits before its first write.
func (r *Program) deleteUnsentSendsOf(ctx context.Context, campaignID *uuid.UUID) error {
	byCampaign := clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID}
	// the rows are found through the campaign index, not through the sent_at
	// index that holds the unsent rows of every campaign
	// a row the sender took may have gone out before sent_at was written, so
	// it is kept with its events
	unsent := clause.And(
		clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}},
		clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "last_attempt_at"})}},
	)
	for {
		var rows []struct {
			ID           *uuid.UUID
			RecipientID  *uuid.UUID
			AnonymizedID *uuid.UUID
		}
		if err := r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select("id", "recipient_id", "anonymized_id").
			Where(byCampaign).
			Where(unsent).
			Limit(ProgramWriteChunk).
			Scan(&rows).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]any, 0, len(rows))
		pseudonyms := map[uuid.UUID]bool{}
		recipients := map[uuid.UUID]bool{}
		for _, row := range rows {
			ids = append(ids, *row.ID)
			switch {
			case row.AnonymizedID != nil:
				pseudonyms[*row.AnonymizedID] = true
			case row.RecipientID != nil:
				recipients[*row.RecipientID] = true
			}
		}
		lonePseudonyms, err := r.withoutSentRow(ctx, campaignID, "anonymized_id", pseudonyms)
		if err != nil {
			return err
		}
		loneRecipients, err := r.withoutSentRow(ctx, campaignID, "recipient_id", recipients)
		if err != nil {
			return err
		}
		start := time.Now()
		err = retryBusy(ctx, func() error {
			return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				// the events are found through the person or pseudonym index
				for column, people := range map[string][]any{
					"anonymized_id": lonePseudonyms,
					"recipient_id":  loneRecipients,
				} {
					if len(people) == 0 {
						continue
					}
					if err := tx.
						Where(clause.IN{Column: clause.Column{Name: column}, Values: people}).
						Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), campaignID}}).
						Delete(&database.CampaignEvent{}).Error; err != nil {
						return err
					}
				}
				return tx.
					Where(clause.IN{Column: clause.Column{Name: "id"}, Values: ids}).
					Where(unsent).
					Delete(&database.CampaignRecipient{}).Error
			})
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if len(rows) < ProgramWriteChunk {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// withoutSentRow returns the given people, pseudonyms or recipients by
// column, who have no sent row in the campaign. It is one read for all of
// them, through the index of the column: the campaign and the sent time are
// not read through an index, as the campaign and sent time index holds every
// sent row of the campaign.
func (r *Program) withoutSentRow(
	ctx context.Context,
	campaignID *uuid.UUID,
	column string,
	people map[uuid.UUID]bool,
) ([]any, error) {
	if len(people) == 0 {
		return nil, nil
	}
	values := make([]any, 0, len(people))
	for id := range people {
		values = append(values, id)
	}
	sent, err := pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Distinct().
		Where(clause.IN{Column: clause.Column{Name: column}, Values: values}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), campaignID}}).
		Where(notIndexedNeq("sent_at")), column)
	if err != nil {
		return nil, err
	}
	for _, id := range sent {
		if id != nil {
			delete(people, *id)
		}
	}
	out := make([]any, 0, len(people))
	for id := range people {
		out = append(out, id)
	}
	return out, nil
}

// GetClosePseudonyms returns every pseudonym on the sends and events of the
// hidden campaigns of a program, each once and sorted
func (r *Program) GetClosePseudonyms(ctx context.Context, programID *uuid.UUID) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]bool{}
	for _, table := range []any{&database.CampaignRecipient{}, &database.CampaignEvent{}} {
		ids, err := pluckUUIDs(r.DB.WithContext(ctx).
			Model(table).
			Distinct().
			Where(r.inProgramCampaigns(programID, true)).
			Where(clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}), "anonymized_id")
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if id != nil {
				seen[*id] = true
			}
		}
	}
	out := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}

// RotateSendPseudonyms gives every person of a closed anonymous program a new
// random pseudonym for each calendar month in loc, so no pseudonym holds more
// than one month of a person. For each given pseudonym its sends get a new
// pseudonym per month of their sent time, and the events of the pseudonym in
// a campaign take the pseudonym of its send in that campaign, so an event
// stays on its send and in the month of the send. The department and title
// kept on the sends are cleared in the same write. An event with no send of
// its pseudonym in its campaign counts nowhere and loses its pseudonym. A
// row never sent gets a pseudonym of its own. Every rewritten row gets the
// same update time at, one time for the whole close, so the update times do
// not group the rows of a person.
//
// The anonymous results count people per month and sum the months, so every
// count stays the same. All rows of a pseudonym are written in one
// transaction, at most pseudonymRotateBatch pseudonyms per transaction with
// PauseAfterWrite between two. A pseudonym that was already rotated holds one
// month of one person, so rotating it again keeps every count: a call that
// stops half way can run again from the start.
func (r *Program) RotateSendPseudonyms(
	ctx context.Context,
	programID *uuid.UUID,
	pseudonyms []uuid.UUID,
	loc *time.Location,
	at time.Time,
) error {
	if loc == nil {
		loc = time.UTC
	}
	stamp := utils.RFC3339UTC(at)
	for len(pseudonyms) > 0 {
		n := min(len(pseudonyms), pseudonymRotateBatch)
		batch := pseudonyms[:n]
		pseudonyms = pseudonyms[n:]
		plan, err := r.planPseudonymBatch(ctx, programID, batch, loc)
		if err != nil {
			return err
		}
		start := time.Now()
		err = retryBusy(ctx, func() error {
			return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return r.writePseudonymBatch(tx, programID, plan, stamp)
			})
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if len(pseudonyms) == 0 {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
	return nil
}

// pseudonymPlan is what one batch of RotateSendPseudonyms writes
type pseudonymPlan struct {
	// batch are the old pseudonyms
	batch []any
	// sendsTo maps a new pseudonym to the ids of the sends that take it
	sendsTo map[uuid.UUID][]any
	// eventsTo maps an old pseudonym in a campaign to the new pseudonym its
	// events take
	eventsTo map[campaignPseudonym]uuid.UUID
}

// campaignPseudonym is a pseudonym in one campaign
type campaignPseudonym struct {
	campaign  uuid.UUID
	pseudonym uuid.UUID
}

// planPseudonymBatch reads the sends of some pseudonyms and draws their new
// pseudonyms, see RotateSendPseudonyms. It runs before the write transaction,
// so the transaction starts with a write.
func (r *Program) planPseudonymBatch(
	ctx context.Context,
	programID *uuid.UUID,
	batch []uuid.UUID,
	loc *time.Location,
) (*pseudonymPlan, error) {
	values := make([]any, 0, len(batch))
	for _, id := range batch {
		values = append(values, id)
	}
	// the rows are found through the pseudonym index, the campaign column is
	// not read through its index
	var sends []struct {
		ID           *uuid.UUID
		CampaignID   *uuid.UUID
		AnonymizedID *uuid.UUID
		SentAt       *time.Time
	}
	if err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("id", "campaign_id", "anonymized_id", "sent_at").
		Where(clause.IN{Column: clause.Column{Name: "anonymized_id"}, Values: values}).
		Where(r.inProgramCampaigns(programID, false)).
		Scan(&sends).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	type personMonth struct {
		pseudonym uuid.UUID
		month     string
	}
	plan := &pseudonymPlan{
		batch:    values,
		sendsTo:  map[uuid.UUID][]any{},
		eventsTo: map[campaignPseudonym]uuid.UUID{},
	}
	newOf := map[personMonth]uuid.UUID{}
	// the send of a pseudonym in a campaign that events follow: the first
	// sent, as there is one row per person and campaign
	firstSent := map[campaignPseudonym]time.Time{}
	for _, s := range sends {
		if s.ID == nil || s.CampaignID == nil || s.AnonymizedID == nil {
			continue
		}
		next := uuid.New()
		if s.SentAt != nil {
			local := s.SentAt.In(loc)
			key := personMonth{*s.AnonymizedID, local.Format("2006-01")}
			if v, ok := newOf[key]; ok {
				next = v
			} else {
				newOf[key] = next
			}
			cp := campaignPseudonym{*s.CampaignID, *s.AnonymizedID}
			if at, ok := firstSent[cp]; !ok || s.SentAt.Before(at) {
				firstSent[cp] = *s.SentAt
				plan.eventsTo[cp] = next
			}
		}
		plan.sendsTo[next] = append(plan.sendsTo[next], *s.ID)
	}
	return plan, nil
}

// writePseudonymBatch writes one planned batch in a transaction, see
// RotateSendPseudonyms. Every statement is a write.
func (r *Program) writePseudonymBatch(tx *gorm.DB, programID *uuid.UUID, plan *pseudonymPlan, stamp string) error {
	pseudonymColumn := clause.Column{Name: "anonymized_id"}
	for next, ids := range plan.sendsTo {
		if err := tx.Model(&database.CampaignRecipient{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: ids}).
			Updates(map[string]any{
				"anonymized_id": next.String(),
				"department":    "",
				"position":      "",
				"updated_at":    stamp,
			}).Error; err != nil {
			return err
		}
	}
	for cp, next := range plan.eventsTo {
		if err := tx.Model(&database.CampaignEvent{}).
			Where(clause.Eq{Column: pseudonymColumn, Value: cp.pseudonym}).
			Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "campaign_id"}), cp.campaign}}).
			Updates(map[string]any{
				"anonymized_id": next.String(),
				"updated_at":    stamp,
			}).Error; err != nil {
			return err
		}
	}
	// what is left on an old pseudonym is an event with no send of the
	// pseudonym in its campaign
	return tx.Model(&database.CampaignEvent{}).
		Where(clause.IN{Column: pseudonymColumn, Values: plan.batch}).
		Where(r.inProgramCampaigns(programID, false)).
		Updates(map[string]any{
			"anonymized_id": nil,
			"updated_at":    stamp,
		}).Error
}

// BlurEnrollments removes from the enrollments of a closed anonymous program
// what could tie a person to the time of a send: the enrollment time is moved
// to the start of its month in loc, and the exit time, the missing time, the
// progress, the round count and the failure count are cleared. The status is
// kept and the update time is set to at, one time for the whole close. The
// rows are walked in recipient order through the program and recipient
// index, at most ProgramWriteChunk per transaction with PauseAfterWrite
// between two. Every write gives the same result when run again.
func (r *Program) BlurEnrollments(ctx context.Context, programID *uuid.UUID, loc *time.Location, at time.Time) error {
	if loc == nil {
		loc = time.UTC
	}
	rowidColumn := clause.Column{Name: "rowid"}
	recipientColumn := clause.Column{Name: "recipient_id"}
	stamp := utils.RFC3339UTC(at)
	var after *uuid.UUID
	for {
		var rows []struct {
			RowID       int64      `gorm:"column:rowid"`
			RecipientID *uuid.UUID `gorm:"column:recipient_id"`
			CreatedAt   *time.Time `gorm:"column:created_at"`
		}
		db := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Select("rowid", "recipient_id", "created_at").
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			// the enrollment of a deleted recipient is blurred already
			Where(clause.Neq{Column: recipientColumn, Value: nil})
		if after != nil {
			db = db.Where(clause.Gt{Column: recipientColumn, Value: *after})
		}
		if err := db.
			Order(clause.OrderByColumn{Column: recipientColumn}).
			Limit(ProgramWriteChunk).
			Scan(&rows).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		after = rows[len(rows)-1].RecipientID
		byMonth := map[time.Time][]any{}
		for _, row := range rows {
			month := time.Time{}
			if row.CreatedAt != nil {
				local := row.CreatedAt.In(loc)
				month = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc).UTC()
			}
			byMonth[month] = append(byMonth[month], row.RowID)
		}
		start := time.Now()
		err := retryBusy(ctx, func() error {
			return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				for month, ids := range byMonth {
					fields := map[string]any{
						"exited_at":        nil,
						"missing_since":    nil,
						"position":         0,
						"loop_count":       0,
						"advance_failures": 0,
						"updated_at":       stamp,
					}
					if !month.IsZero() {
						fields["created_at"] = month
					}
					if err := tx.Model(&database.ProgramEnrollment{}).
						Where(clause.IN{Column: rowidColumn, Values: ids}).
						Updates(fields).Error; err != nil {
						return err
					}
				}
				return nil
			})
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if len(rows) < ProgramWriteChunk || after == nil {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}
