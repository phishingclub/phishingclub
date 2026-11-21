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

// programAnonymizeBatch is the most programs one run of the anonymize tasks
// reads. The rest are found on the next run.
const programAnonymizeBatch = 50

// enrollmentPseudonymChunk is how many enrollments one transaction of
// AssignEnrollmentPseudonyms writes, one write per row
const enrollmentPseudonymChunk = 250

// GetIDsToAnonymize returns the closed programs to anonymize: those whose
// anonymize date has passed and those whose Anonymize All began and did not
// finish. An anonymous program is anonymized when it closes and is left out,
// so is a program being deleted.
func (r *Program) GetIDsToAnonymize(ctx context.Context, now time.Time, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	due := clause.And(
		clause.Neq{Column: clause.Column{Name: "anonymize_at"}, Value: nil},
		clause.Lte{Column: clause.Column{Name: "anonymize_at"}, Value: now.UTC()},
	)
	begun := clause.Neq{Column: clause.Column{Name: "anonymizing_at"}, Value: nil}
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Or(due, begun)).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_at"}, Value: nil}).
		Where(clause.Neq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "is_anonymous"}, Value: false}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Where(notInIDs(skip)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}}).
		Limit(programAnonymizeBatch), "id")
}

// GetIDsToDataAnonymize returns the programs whose data anonymize date has
// passed and whose data was not anonymized since that date, so a run before
// the date, by hand or for an earlier date, does not stand in for it, and the
// programs with a run by hand that did not finish. A program that is
// anonymized or being deleted is left out.
func (r *Program) GetIDsToDataAnonymize(ctx context.Context, now time.Time, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	at := clause.Column{Name: "data_anonymize_at"}
	ran := clause.Column{Name: "data_anonymized_at"}
	due := clause.And(
		clause.Neq{Column: at, Value: nil},
		clause.Lte{Column: at, Value: now.UTC()},
		clause.Or(
			clause.Eq{Column: ran, Value: nil},
			clause.Expr{SQL: "julianday(?) < julianday(?)", Vars: []any{ran, at}},
		),
	)
	// a run by hand that did not finish
	begun := clause.Neq{Column: clause.Column{Name: "data_anonymizing_at"}, Value: nil}
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Or(due, begun)).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Where(notInIDs(skip)).
		Order(clause.OrderByColumn{Column: at}).
		Limit(programAnonymizeBatch), "id")
}

// notInIDs leaves out the programs in skip, the ones that failed a short while
// ago, so they do not take the places of others in a batch. An empty skip
// leaves out nothing.
func notInIDs(skip []*uuid.UUID) clause.Expression {
	if len(skip) == 0 {
		return clause.Expr{SQL: "1 = 1"}
	}
	return clause.Not(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(skip)})
}

// ProgramPeopleCounts are the people of a program by status, stored when
// Anonymize All begins
type ProgramPeopleCounts struct {
	Total, Completed, Exited, Stopped int
}

// storedPeopleRow is the part of a program row that holds the stored counts
type storedPeopleRow struct {
	ID                  uuid.UUID
	AnonymizingAt       *time.Time
	AnonymizedAt        *time.Time
	AnonymizedPeople    int
	AnonymizedCompleted int
	AnonymizedExited    int
	AnonymizedStopped   int
}

// storedPeopleSelect selects the stored counts of programs
func (r *Program) storedPeopleSelect(ctx context.Context) *gorm.DB {
	return r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Select("id", "anonymizing_at", "anonymized_at", "anonymized_people",
			"anonymized_completed", "anonymized_exited", "anonymized_stopped")
}

// begun reports whether Anonymize All began on the program of the row
func (row storedPeopleRow) begun() bool {
	return row.AnonymizingAt != nil || row.AnonymizedAt != nil
}

// storedPeopleCounts returns the counts stored when Anonymize All began as
// status counts. ok is false for a program where it did not begin.
func (r *Program) storedPeopleCounts(ctx context.Context, programID *uuid.UUID) ([]ProgramEnrollmentStatusCount, bool, error) {
	var rows []storedPeopleRow
	if err := r.storedPeopleSelect(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: programID}).
		Limit(1).
		Scan(&rows).Error; err != nil {
		return nil, false, errs.Wrap(err)
	}
	if len(rows) == 0 || !rows[0].begun() {
		return nil, false, nil
	}
	row := rows[0]
	out := []ProgramEnrollmentStatusCount{}
	for _, c := range []ProgramEnrollmentStatusCount{
		{Status: model.ProgramEnrollmentCompleted, Count: row.AnonymizedCompleted},
		{Status: model.ProgramEnrollmentExited, Count: row.AnonymizedExited},
		{Status: model.ProgramEnrollmentStopped, Count: row.AnonymizedStopped},
	} {
		if c.Count > 0 {
			out = append(out, c)
		}
	}
	return out, true, nil
}

// applyStoredPeopleCounts sets the people counts of the list summaries of
// the programs where Anonymize All began from the stored counts
func (r *Program) applyStoredPeopleCounts(ctx context.Context, programIDs []*uuid.UUID, out map[uuid.UUID]*ProgramListSummary) error {
	var rows []storedPeopleRow
	if err := r.storedPeopleSelect(ctx).
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(programIDs)}).
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "anonymizing_at"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "anonymized_at"}, Value: nil},
		)).
		Scan(&rows).Error; err != nil {
		return errs.Wrap(err)
	}
	for _, row := range rows {
		sum, ok := out[row.ID]
		if !ok {
			continue
		}
		sum.Enrolled = 0
		sum.Stopped = int64(row.AnonymizedStopped)
		sum.People = int64(row.AnonymizedPeople - row.AnonymizedExited)
		sum.Total = int64(row.AnonymizedPeople)
	}
	return nil
}

// BeginAnonymize marks the Anonymize All of a program as begun and stores
// the people counts it keeps, in one write. A program where it already began
// keeps its mark and counts, so a run that goes on after a stop does not
// count the enrollments it removed. It reports whether this call began it.
func (r *Program) BeginAnonymize(ctx context.Context, programID *uuid.UUID, counts ProgramPeopleCounts) (bool, error) {
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: programID}).
		Where(clause.Eq{Column: clause.Column{Name: "anonymizing_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_at"}, Value: nil}).
		Updates(map[string]any{
			"anonymizing_at":       time.Now().UTC(),
			"anonymized_people":    counts.Total,
			"anonymized_completed": counts.Completed,
			"anonymized_exited":    counts.Exited,
			"anonymized_stopped":   counts.Stopped,
			"updated_at":           utils.NowRFC3339UTC(),
		})
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	return res.RowsAffected == 1, nil
}

// AssignEnrollmentPseudonyms gives every enrollment of a program that has no
// pseudonym a fresh random one, so each person has one pseudonym in every
// hidden campaign. The rows are found through the program and pseudonym
// index, at most enrollmentPseudonymChunk at a time, and written in one
// transaction per chunk with PauseAfterWrite between two chunks. A written row
// leaves the range, so a call that stops half way can run again. It must not
// be called inside a transaction or while the lifecycle lock is held.
func (r *Program) AssignEnrollmentPseudonyms(ctx context.Context, programID *uuid.UUID) error {
	for {
		var ids []*uuid.UUID
		if err := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
			// a deleted recipient has no sends of its own to link
			Where(clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil}).
			Limit(enrollmentPseudonymChunk).
			Pluck("id", &ids).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(ids) == 0 {
			return nil
		}
		start := time.Now()
		now := utils.NowRFC3339UTC()
		err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, id := range ids {
				if err := tx.Model(&database.ProgramEnrollment{}).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
					Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
					Updates(map[string]any{"anonymized_id": uuid.New().String(), "updated_at": now}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return errs.Wrap(err)
		}
		if len(ids) < enrollmentPseudonymChunk {
			return nil
		}
		if err := PauseAfterWrite(ctx, time.Since(start)); err != nil {
			return err
		}
	}
}

// enrollmentPseudonymOf selects the pseudonym of the enrollment in a program
// of the person of a send row of the outer update
func (r *Program) enrollmentPseudonymOf(programID *uuid.UUID) *gorm.DB {
	return r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.ProgramEnrollment{}).
		Select("?", clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"}).
		Where(programIDEq(clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programID)).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "recipient_id"},
		}}).
		Where(clause.Neq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"}, Value: nil}).
		Limit(1)
}

// LinkSendsToEnrollmentPseudonyms gives every send of a hidden campaign that
// names a person and has no pseudonym the pseudonym of the person's
// enrollment in the program, so a person has the same pseudonym in every
// hidden campaign. A send whose person has no enrollment pseudonym is left as
// it is. The rows are found through the campaign and person index and
// written in paced chunks, a written row stops matching, so a call that stops
// half way can run again. It must not be called inside a transaction or while
// the lifecycle lock is held.
func (r *CampaignRecipient) LinkSendsToEnrollmentPseudonyms(
	ctx context.Context,
	programRepo *Program,
	programID *uuid.UUID,
	campaignID *uuid.UUID,
) error {
	pseudonym := programRepo.enrollmentPseudonymOf(programID)
	find := clause.And(
		clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID},
		clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil},
		clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil},
		clause.Expr{SQL: "EXISTS (?)", Vars: []any{pseudonym}},
	)
	guard := clause.And(notIndexedNeq("recipient_id"), notIndexedEq("anonymized_id"))
	return r.updateCampaignRecipientsPaced(ctx, find, guard, map[string]any{
		"anonymized_id": clause.Expr{SQL: "(?)", Vars: []any{pseudonym}},
		"updated_at":    utils.NowRFC3339UTC(),
	}, false)
}

// LinkRecipientSendsToEnrollmentPseudonyms gives every send of a recipient
// that has no pseudonym the pseudonym of the recipient's enrollment in the
// program of the send's campaign, where that enrollment has one. A recipient
// delete calls it first, so a person whose program is being anonymized keeps
// one pseudonym in every hidden campaign of it. A send of a campaign without
// a program, or of a program where the enrollment has no pseudonym, is left
// as it is. A person has few sends, so this is one small write.
func (r *Program) LinkRecipientSendsToEnrollmentPseudonyms(ctx context.Context, recipientID *uuid.UUID) error {
	programOfCampaign := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.Campaign{}).
		Select("?", clause.Column{Table: database.CAMPAIGN_TABLE, Name: "program_id"}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_TABLE, Name: "id"},
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
		}})
	pseudonym := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.ProgramEnrollment{}).
		Select("?", clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"}).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Expr{SQL: "? = (?)", Vars: []any{
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"},
			programOfCampaign,
		}}).
		Where(clause.Neq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"}, Value: nil}).
		Limit(1)
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
		Where(clause.Expr{SQL: "EXISTS (?)", Vars: []any{pseudonym}}).
		Updates(map[string]any{
			"anonymized_id": clause.Expr{SQL: "(?)", Vars: []any{pseudonym}},
			"updated_at":    utils.NowRFC3339UTC(),
		})
	return errs.Wrap(res.Error)
}

// AssignMissingPseudonyms gives each send of a campaign that names a person
// and has no pseudonym a fresh one and moves the events of that person in the
// campaign onto it, see assignMissingPseudonyms
func (r *CampaignRecipient) AssignMissingPseudonyms(ctx context.Context, campaignID *uuid.UUID) error {
	return r.assignMissingPseudonyms(ctx, campaignID)
}

// scrubbedEventFields are the values an anonymized event keeps in place of
// the submitted data, the IP address, the user agent and the browser metadata
func scrubbedEventFields() map[string]any {
	return map[string]any{
		"user_agent": "anonymized",
		"ip_address": nil,
		"data":       "anonymized",
		"metadata":   "",
		"updated_at": utils.NowRFC3339UTC(),
	}
}

// MoveEventsToPseudonyms moves every event of a hidden campaign that names a
// person onto the pseudonym of that person's send in the campaign and removes
// the submitted data, IP address, user agent and browser metadata. An event
// whose person has no send in the campaign loses the person without a
// pseudonym. The sends must hold their pseudonyms already. A written event no
// longer names a person, so a call that stops half way can run again, see
// updateCampaignEventsPaced.
func (r *Program) MoveEventsToPseudonyms(ctx context.Context, campaignID *uuid.UUID) error {
	pseudonym := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.CampaignRecipient{}).
		Select("?", clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "anonymized_id"}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"},
			clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "campaign_id"},
		}}).
		Where(clause.Expr{SQL: "? = ?", Vars: []any{
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "recipient_id"},
			clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: "recipient_id"},
		}}).
		Where(clause.Neq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "anonymized_id"}, Value: nil}).
		Limit(1)
	fields := scrubbedEventFields()
	// both values are read from the row before the update, so the pseudonym
	// is found by the person the event still names
	fields["anonymized_id"] = clause.Expr{SQL: "(?)", Vars: []any{pseudonym}}
	fields["recipient_id"] = nil
	return r.updateCampaignEventsPaced(ctx, campaignID, notIndexedNeq("recipient_id"), fields)
}

// ScrubCampaignEventData removes the submitted data, IP address, user agent
// and browser metadata of every event of a campaign and keeps who the event
// is about, see updateCampaignEventsPaced
func (r *Program) ScrubCampaignEventData(ctx context.Context, campaignID *uuid.UUID) error {
	return r.updateCampaignEventsPaced(ctx, campaignID, nil, scrubbedEventFields())
}

// updateCampaignEventsPaced sets fields on the events of a campaign that
// match filter, or on all of them when filter is nil. The events are walked
// once in the order of the campaign and event index, by event and row id,
// at most ProgramWriteChunk per write with PauseAfterWrite between two
// writes, so a large campaign does not hold the database for long. filter
// must not read an index, see notIndexedNeq, and is repeated on the write so
// a row changed in between is left as it is. It must not be called inside a
// transaction or while the lifecycle lock is held.
func (r *Program) updateCampaignEventsPaced(
	ctx context.Context,
	campaignID *uuid.UUID,
	filter clause.Expression,
	fields map[string]any,
) error {
	eventColumn := clause.Column{Name: "event_id"}
	rowidColumn := clause.Column{Name: "rowid"}
	started := false
	lastEvent := ""
	var lastRowID int64
	for {
		q := r.DB.WithContext(ctx).
			Model(&database.CampaignEvent{}).
			Select("? AS row_id, ? AS event_id", rowidColumn, eventColumn).
			Where(clause.Eq{Column: clause.Column{Name: "campaign_id"}, Value: campaignID})
		if started {
			q = q.Where(clause.Expr{SQL: "(?, ?) > (?, ?)", Vars: []any{eventColumn, rowidColumn, lastEvent, lastRowID}})
		}
		if filter != nil {
			q = q.Where(filter)
		}
		var rows []struct {
			RowID   int64
			EventID string
		}
		if err := q.Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: eventColumn},
			{Column: rowidColumn},
		}}).Limit(ProgramWriteChunk).Scan(&rows).Error; err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			return nil
		}
		started = true
		lastEvent = rows[len(rows)-1].EventID
		lastRowID = rows[len(rows)-1].RowID
		rowids := make([]int64, len(rows))
		for i, row := range rows {
			rowids[i] = row.RowID
		}
		start := time.Now()
		write := r.DB.WithContext(ctx).
			Model(&database.CampaignEvent{}).
			Where(clause.IN{Column: rowidColumn, Values: int64Values(rowids)})
		if filter != nil {
			write = write.Where(filter)
		}
		if err := write.Updates(fields).Error; err != nil {
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

// SetCampaignDataAnonymizedAt marks a hidden campaign as having had its
// submitted data anonymized
func (r *Program) SetCampaignDataAnonymizedAt(ctx context.Context, campaignID *uuid.UUID) error {
	now := utils.NowRFC3339UTC()
	return errs.Wrap(r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: campaignID}).
		Updates(map[string]any{"data_anonymized_at": now, "updated_at": now}).Error)
}
