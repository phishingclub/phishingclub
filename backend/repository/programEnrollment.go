package repository

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CountEnrollments returns how many people were ever enrolled in a program,
// whatever their status
func (r *Program) CountEnrollments(ctx context.Context, programID *uuid.UUID) (int64, error) {
	var count int64
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Count(&count)
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return count, nil
}

// CountActiveEnrollments returns how many people are active in a program
func (r *Program) CountActiveEnrollments(ctx context.Context, programID *uuid.UUID) (int64, error) {
	var count int64
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}).
		Count(&count)
	if res.Error != nil {
		return 0, errs.Wrap(res.Error)
	}
	return count, nil
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
		Updates(map[string]any{
			"anonymized_id": pseudonym,
			"updated_at":    utils.NowRFC3339UTC(),
		})
	return errs.Wrap(res.Error)
}

// enrollmentSendTimeFields are the send times and round state of an
// enrollment. The send times of an anonymous program are random per person
// and equal the send_at of the person's rows, so they would lead from a send
// back to a person.
var enrollmentSendTimeFields = []string{
	"last_send_at", "last_step_id", "round_start_at", "next_due_at", "completed_at",
}

// ClearEnrollmentPseudonyms removes the pseudonym from every enrollment of a
// program. With sendTimes the send times and round state of every enrollment
// are removed too, see enrollmentSendTimeFields. Each write changes at most
// ProgramWriteChunk rows, found by rowid, with PauseAfterWrite between two
// writes, so a large program does not hold the database for long. A cleared
// row no longer matches, so a call that stops half way can run again and
// clears the rest. It must not be called while the lifecycle lock or a
// transaction is held.
func (r *Program) ClearEnrollmentPseudonyms(
	ctx context.Context,
	programID *uuid.UUID,
	sendTimes bool,
) error {
	byProgram := programIDEq(clause.Column{Name: "program_id"}, programID)
	fields := map[string]any{
		"anonymized_id": nil,
		"updated_at":    utils.NowRFC3339UTC(),
	}
	if sendTimes {
		for _, f := range enrollmentSendTimeFields {
			fields[f] = nil
		}
	}
	// the rows are found through the program and pseudonym index, a cleared
	// row leaves the range so no write reads it again
	if err := r.updateEnrollmentsPaced(ctx, clause.And(
		byProgram,
		clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil},
	), fields); err != nil {
		return err
	}
	if !sendTimes {
		return nil
	}
	if err := PauseBetweenWrites(ctx); err != nil {
		return err
	}
	// a person who never got a pseudonym was never sent to, a few may still
	// hold a planned round
	anyTime := make([]clause.Expression, 0, len(enrollmentSendTimeFields))
	for _, f := range enrollmentSendTimeFields {
		anyTime = append(anyTime, clause.Neq{Column: clause.Column{Name: f}, Value: nil})
	}
	return r.updateEnrollmentsPaced(ctx, clause.And(
		byProgram,
		clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: nil},
		clause.Or(anyTime...),
	), fields)
}

// ReleaseUnlinkedSendsOfRecipient clears the lure code and the message id of
// the program sends of a recipient that no longer carry the recipient link.
// An anonymous program send loses its link after a while but keeps its code
// and message id until the program closes, so they are found by the
// pseudonyms of the recipient's enrollments. It must run before the
// enrollments are removed. A person has one row per round, so this is a
// small write.
func (r *Program) ReleaseUnlinkedSendsOfRecipient(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	pseudonyms := r.DB.Session(&gorm.Session{NewDB: true}).
		Model(&database.ProgramEnrollment{}).
		Select("anonymized_id").
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Where(clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil})
	// the rows are found through the pseudonym index. The recipient column
	// is not read through its index, which holds every unlinked row.
	res := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "anonymized_id"}, pseudonyms}}).
		Where(clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "recipient_id"})}}).
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "lure_code"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "message_id"}, Value: nil},
		)).
		Updates(map[string]any{
			"lure_code":  nil,
			"message_id": nil,
			"updated_at": utils.NowRFC3339UTC(),
		})
	return errs.Wrap(res.Error)
}

// UnlinkEnrollmentsOfRecipient removes a deleted recipient from every
// program enrollment while the rows stay, so the person still counts once in
// the people of each program, as a removed person. The recipient and the
// pseudonym go, the status becomes exited and every time and the progress of
// the person are cleared, the enrollment time is moved to the start of its
// month. Nothing on the row then leads to the person or to their sends. A
// person has one enrollment per program, so this is a small write.
func (r *Program) UnlinkEnrollmentsOfRecipient(
	ctx context.Context,
	recipientID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Updates(unlinkedEnrollmentFields())
	return errs.Wrap(res.Error)
}

// unlinkedEnrollmentFields are the values of an enrollment whose person is
// gone, see UnlinkEnrollmentsOfRecipient
func unlinkedEnrollmentFields() map[string]any {
	return map[string]any{
		"recipient_id":     nil,
		"anonymized_id":    nil,
		"status":           model.ProgramEnrollmentExited,
		"created_at":       clause.Expr{SQL: "strftime('%Y-%m-01 00:00:00', ?)", Vars: []any{clause.Column{Name: "created_at"}}},
		"round_start_at":   nil,
		"next_due_at":      nil,
		"exited_at":        nil,
		"completed_at":     nil,
		"missing_since":    nil,
		"last_send_at":     nil,
		"last_step_id":     nil,
		"position":         0,
		"loop_count":       0,
		"advance_failures": 0,
		"updated_at":       utils.NowRFC3339UTC(),
	}
}

// linkedEnrollment matches an enrollment that still names its person. An
// enrollment of a deleted recipient names no one and only counts.
func linkedEnrollment() clause.Expression {
	return clause.Neq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"}, Value: nil}
}

// linkedEnrollmentNotIndexed matches the same rows as linkedEnrollment without
// reading the recipient index, so a lookup by id or by due time keeps its index
func linkedEnrollmentNotIndexed() clause.Expression {
	return clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "recipient_id"})}}
}

// programEnrollmentBatch is how many enrollments one insert or update writes
const programEnrollmentBatch = 200

// ProgramWriteChunk is the most rows one write transaction of the program
// scheduler writes. Each chunk commits on its own, so no write holds the
// database for long and other writers do not wait out their busy timeout.
const ProgramWriteChunk = 500

// WriteChunkPause is how long a write that is split in chunks waits between
// two chunks. A writer that waits on the database lock sleeps up to 100 ms in
// its busy handler between tries, so it rarely wakes inside a gap of a
// millisecond. A short pause lets it take its turn.
var WriteChunkPause = 30 * time.Millisecond

// PauseBetweenWrites waits WriteChunkPause, or returns the context error when
// the context ends first. It must not be called while a transaction or the
// program lifecycle lock is held, as the pause would only make that hold
// longer.
func PauseBetweenWrites(ctx context.Context) error {
	timer := time.NewTimer(WriteChunkPause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errs.Wrap(ctx.Err())
	case <-timer.C:
		return nil
	}
}

// MaxBusyWait is the longest a writer that waits on the database lock sleeps
// between two tries, the last step of the sqlite busy handler
const MaxBusyWait = 100 * time.Millisecond

// PauseAfterWrite waits after one write of a chunked write that took the
// given time, before the next write. A writer that waits on the database lock
// tries again up to MaxBusyWait apart, so a gap shorter than that is often
// missed and the writer waits out many writes. The gap is as long as the
// write took, up to MaxBusyWait, and never shorter than WriteChunkPause. With
// WriteChunkPause zero, as in tests, there is no wait. It returns the context
// error when the context ends first. It must not be called while a
// transaction or the program lifecycle lock is held.
func PauseAfterWrite(ctx context.Context, took time.Duration) error {
	if WriteChunkPause <= 0 {
		return nil
	}
	wait := max(WriteChunkPause, min(took, MaxBusyWait))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errs.Wrap(ctx.Err())
	case <-timer.C:
		return nil
	}
}

// WritePacer spaces out a long run of small writes, such as removing many
// people one at a time. Each person takes a few short writes, so a pause after
// every one would make the run many times longer, while no pause at all lets
// the run take the database lock again right after each write and a writer
// waiting in the busy handler can wait for a second or more. Pause waits
// WriteChunkPause once the writes since the last pause took at least half of
// MaxBusyWait, so other writers get a turn a few times per MaxBusyWait. The
// zero value is ready to use.
type WritePacer struct {
	since time.Time
}

// Pause waits when the writes since the last pause ran long enough, see
// WritePacer. It returns the context error when the context ends first. It
// must not be called while a transaction or the program lifecycle lock is
// held.
func (p *WritePacer) Pause(ctx context.Context) error {
	if p.since.IsZero() {
		p.since = time.Now()
		return nil
	}
	if time.Since(p.since) < MaxBusyWait/2 {
		return nil
	}
	err := PauseBetweenWrites(ctx)
	p.since = time.Now()
	return err
}

// deleteBatch is how many rows one write of a batched delete removes. A row
// of campaign_events or program_enrollments carries a few indexes, so a write
// of this many rows stays short enough that a writer waiting in the busy
// handler finds the pause after it.
const deleteBatch = 250

// deleteBatchSends is how many rows one write of a batched delete of
// campaign_recipients removes. A row of that table carries about twenty
// indexes, so a write of deleteBatch rows takes several times as long.
const deleteBatchSends = 100

// deleteWriteTarget is how long one write of a batched delete aims to take.
// A row costs much less in a large write than in a small one, so a delete of
// many rows in small writes takes many times longer. A write this long is far
// below the busy timeout of other writers, who get a turn in the pause after
// it.
const deleteWriteTarget = 250 * time.Millisecond

// deleteBatchMax bounds how many rows one write of a batched delete removes
const deleteBatchMax = 20000

// deleteInBatches deletes the rows of a table that match where, with a pause
// between writes, see PauseAfterWrite, so no single write holds the database
// for long and other writers get a turn. The first write removes batch rows,
// later writes remove more while a write takes well under deleteWriteTarget
// and fewer when one takes longer, so a delete of many cheap rows does not
// take many times longer than one write would. Each batch is found by where
// and deleted by rowid. It must not be called inside a transaction or while
// the program lifecycle lock is held.
func deleteInBatches(ctx context.Context, db *gorm.DB, table any, where clause.Expression, batch int) error {
	least := batch
	for {
		rowids := db.Session(&gorm.Session{NewDB: true}).
			Model(table).
			Select("rowid").
			Where(where).
			Limit(batch)
		start := time.Now()
		res := db.WithContext(ctx).
			Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Name: "rowid"}, rowids}}).
			Delete(table)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if res.RowsAffected < int64(batch) {
			return nil
		}
		took := time.Since(start)
		switch {
		case took < deleteWriteTarget/2:
			batch = min(batch*2, deleteBatchMax)
		case took > deleteWriteTarget*2:
			batch = max(batch/2, least)
		}
		if err := PauseAfterWrite(ctx, took); err != nil {
			return err
		}
	}
}

// DeleteEnrollmentsByProgramID removes every enrollment of a program in
// batches, see deleteInBatches
func (r *Program) DeleteEnrollmentsByProgramID(ctx context.Context, programID *uuid.UUID) error {
	return deleteInBatches(ctx, r.DB, &database.ProgramEnrollment{},
		programIDEq(clause.Column{Name: "program_id"}, programID), deleteBatch)
}

// InsertEnrollments stores new enrollments in batches. Each ProgramWriteChunk
// rows commit in their own transaction, so a failure keeps the chunks before
// it. The people of the failed chunk have no enrollment, the next membership
// pass enrolls them. A person who already has an enrollment in the program is
// skipped, so a list planned from an older read can be stored as it is.
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
	// one transaction per chunk, so the batches of a chunk share one commit
	for start := 0; start < len(enrollments); start += ProgramWriteChunk {
		chunk := enrollments[start:min(start+ProgramWriteChunk, len(enrollments))]
		if err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).
				CreateInBatches(chunk, programEnrollmentBatch).Error
		}); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// UpdateEnrollmentsFields sets the same columns on many enrollments, in
// batches. With statuses only the enrollments in one of them are changed, so a
// change planned from an older read skips a person whose status moved on.
func (r *Program) UpdateEnrollmentsFields(
	ctx context.Context,
	ids []*uuid.UUID,
	fields map[string]any,
	statuses ...string,
) error {
	for k, v := range fields {
		if t, ok := v.(time.Time); ok {
			fields[k] = t.UTC()
		}
	}
	AddUpdatedAt(fields)
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		db := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])})
		if len(statuses) > 0 {
			db = db.Where(enrollmentStatusIn(statuses))
		}
		// a row whose person was deleted is never made active again
		if fields["status"] == model.ProgramEnrollmentActive {
			db = db.Where(linkedEnrollmentNotIndexed())
		}
		res := db.Updates(fields)
		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
	}
	return nil
}

// enrollmentStatusIn matches the enrollments in one of the statuses. It is
// used with an id filter, so the status column is not read through an index
// and the rows are found by id. Without table statistics the status index
// would be picked and every enrollment in the status walked.
func enrollmentStatusIn(statuses []string) clause.Expression {
	values := make([]any, 0, len(statuses))
	for _, st := range statuses {
		values = append(values, st)
	}
	return clause.Expr{SQL: "? IN ?", Vars: []any{notIndexed(clause.Column{Name: "status"}), values}}
}

// GetRecipientsWithStatus returns which of the given people have an
// enrollment in the program in the given status. The ids are read in batches.
func (r *Program) GetRecipientsWithStatus(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	status string,
) ([]*uuid.UUID, error) {
	out := []*uuid.UUID{}
	for start := 0; start < len(recipientIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(recipientIDs))
		ids, err := pluckUUIDs(r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs[start:end])}).
			// found by program and person: the status is not read through an
			// index, or sqlite picks the program and status index and walks
			// every person in the status
			Where(enrollmentStatusIn([]string{status})), "recipient_id")
		if err != nil {
			return nil, err
		}
		out = append(out, ids...)
	}
	return out, nil
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
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
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
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	return &row, nil
}

// HasLiveEnrollmentByPseudonym reports whether a program has an enrollment
// that carries the pseudonym and whose recipient still exists and is not
// disabled by SCIM. Deleting a recipient removes its enrollments, so the
// sends of a deleted person find none. The lookup is served by the program
// and pseudonym index.
func (r *Program) HasLiveEnrollmentByPseudonym(
	ctx context.Context,
	programID *uuid.UUID,
	pseudonym *uuid.UUID,
) (bool, error) {
	var ids []uuid.UUID
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("?", clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "id"}).
		Joins("JOIN ? ON ? = ?",
			clause.Table{Name: database.RECIPIENT_TABLE},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
		).
		Where(programIDEq(clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programID)).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"}, Value: pseudonym}).
		Where(clause.Eq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "deleted_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "scim_soft_deleted_at"}, Value: nil}).
		Limit(1).
		Scan(&ids)
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	return len(ids) > 0, nil
}

// enrollmentPlanColumns are the enrollment columns the membership pass, the
// resume and the replan of an edit read to plan people. The program is known
// to the caller and the position is not planned with.
var enrollmentPlanColumns = []string{
	"id", "recipient_id", "status", "loop_count",
	"round_start_at", "next_due_at", "missing_since", "last_send_at",
	"last_step_id", "anonymized_id", "advance_failures",
}

// GetEnrollmentPlansByProgramID returns all enrollments of a program with only
// the columns the planners use, see enrollmentPlanColumns. The program,
// the position, the timestamps of the row itself and the exit and completion
// times are left empty.
func (r *Program) GetEnrollmentPlansByProgramID(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*database.ProgramEnrollment, error) {
	var rows []*database.ProgramEnrollment
	res := r.DB.WithContext(ctx).
		Select(enrollmentPlanColumns).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Where(linkedEnrollment()).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// EnrollmentPlan is the new round start and next send time of one enrollment
type EnrollmentPlan struct {
	ID           *uuid.UUID
	RoundStartAt time.Time
	NextDueAt    time.Time
}

// PlanEnrollments stores a new round start and next send time per enrollment
// and sets the shared fields on all of them. With statuses only the
// enrollments in one of them are changed. Each ProgramWriteChunk rows are
// written in one transaction that only writes, so many people take few
// commits and no commit holds the database for long. Each row is its own
// update by primary key: sqlite tests the WHEN branches of a CASE one by one
// for every row, which measured slower than one update per row. A failure
// keeps the chunks before it: the people after it keep their old plan, which
// is a valid plan the scheduler sends by, and the caller may plan again.
func (r *Program) PlanEnrollments(
	ctx context.Context,
	plans []EnrollmentPlan,
	shared map[string]any,
	statuses ...string,
) error {
	if len(plans) == 0 {
		return nil
	}
	fields := map[string]any{}
	for k, v := range shared {
		if t, ok := v.(time.Time); ok {
			v = t.UTC()
		}
		fields[k] = v
	}
	AddUpdatedAt(fields)
	for start := 0; start < len(plans); start += ProgramWriteChunk {
		if err := r.planEnrollmentsTx(ctx, plans[start:min(start+ProgramWriteChunk, len(plans))], fields, statuses); err != nil {
			return err
		}
	}
	return nil
}

// planEnrollmentsTx writes one chunk of PlanEnrollments in one transaction
func (r *Program) planEnrollmentsTx(ctx context.Context, plans []EnrollmentPlan, fields map[string]any, statuses []string) error {
	return errs.Wrap(r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, p := range plans {
			update := make(map[string]any, len(fields)+2)
			for k, v := range fields {
				update[k] = v
			}
			// times are stored in UTC so text compares in the due scan hold
			update["round_start_at"] = p.RoundStartAt.UTC()
			update["next_due_at"] = p.NextDueAt.UTC()
			// by table name, so gorm does not copy each value back into a model
			db := tx.Table(database.PROGRAM_ENROLLMENT_TABLE).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: p.ID})
			if len(statuses) > 0 {
				db = db.Where(enrollmentStatusIn(statuses))
			}
			// a plan never applies to a row whose person was deleted, so a plan
			// made from a copy read before the delete can not make it active
			db = db.Where(linkedEnrollmentNotIndexed())
			res := db.Updates(update)
			if res.Error != nil {
				return res.Error
			}
		}
		return nil
	}))
}

// GetGroupMemberIDs returns the ids of the active members of a recipient group:
// the junction members of a static group or the filter matches of a dynamic
// one, without SCIM disabled people. Only ids are read, so a large group costs
// one light query and no list of every member id is bound as parameters.
func (r *Program) GetGroupMemberIDs(
	ctx context.Context,
	groupID *uuid.UUID,
) ([]*uuid.UUID, error) {
	db, err := r.groupMemberQuery(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return pluckUUIDs(db, "id")
}

// groupMemberQuery returns a query on the recipients table limited to the
// active members of a recipient group, the rules of GetGroupMemberIDs
func (r *Program) groupMemberQuery(
	ctx context.Context,
	groupID *uuid.UUID,
) (*gorm.DB, error) {
	var group database.RecipientGroup
	res := r.DB.WithContext(ctx).
		Select("id", "is_dynamic", "filter_field", "filter_value", "company_id").
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: groupID}).
		First(&group)
	if res.Error != nil {
		return nil, res.Error
	}
	recipientsTable := database.RECIPIENT_TABLE
	// SCIM disabled people are left out. Not read through the
	// scim_soft_deleted_at index, which holds nearly every recipient, so the
	// members are found through the group or the company instead
	db := r.DB.WithContext(ctx).
		Model(&database.Recipient{}).
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Table: recipientsTable, Name: "scim_soft_deleted_at"})}})
	if group.IsDynamic {
		if !slices.Contains(model.DynamicGroupAllowedFields, group.FilterField) {
			return nil, errs.Wrap(fmt.Errorf("invalid filter field: %s", group.FilterField))
		}
		db = db.
			Where(clause.Eq{Column: clause.Column{Table: recipientsTable, Name: "deleted_at"}, Value: nil}).
			Where(clause.Eq{Column: clause.Column{Table: recipientsTable, Name: group.FilterField}, Value: group.FilterValue})
		db = whereCompany(db, recipientsTable, group.CompanyID)
	} else {
		// a static group lists its members in the junction table, read through
		// its group index
		members := r.DB.
			Model(&database.RecipientGroupRecipient{}).
			Select("recipient_id").
			Where(clause.Eq{Column: clause.Column{Name: "recipient_group_id"}, Value: groupID})
		db = db.Where(clause.Expr{
			SQL:  "? IN (?)",
			Vars: []any{clause.Column{Table: recipientsTable, Name: "id"}, members},
		})
	}
	return db, nil
}

// programAudienceSortColumns maps the people table sort columns that apply
// to people not yet enrolled onto the recipients table
var programAudienceSortColumns = map[string]string{
	ProgramEnrollmentSortColumns["email"]: TableColumn(database.RECIPIENT_TABLE, "email"),
	ProgramEnrollmentSortColumns["name"]:  TableColumn(database.RECIPIENT_TABLE, "first_name"),
}

var allowedProgramAudienceColumns = func() []string {
	out := make([]string, 0, len(programAudienceSortColumns))
	for _, v := range programAudienceSortColumns {
		out = append(out, v)
	}
	return out
}()

// GetAudiencePage returns the people a program would enroll if it started
// now: the members of its groups by the rules of GetGroupMemberIDs, each person
// once however many groups they are in. It is searched on email and name and
// sorted by the email and name columns of the people table, any other sort is
// left out as every row has the same state. A query without a limit returns
// every match. The count is the number of matches.
func (r *Program) GetAudiencePage(
	ctx context.Context,
	groupIDs []*uuid.UUID,
	q *vo.QueryArgs,
) ([]*database.Recipient, int64, error) {
	rows := []*database.Recipient{}
	members := []clause.Expression{}
	for _, groupID := range groupIDs {
		if groupID == nil {
			continue
		}
		sub, err := r.groupMemberQuery(ctx, groupID)
		if err != nil {
			return nil, 0, errs.Wrap(err)
		}
		members = append(members, clause.Expr{
			SQL:  "? IN (?)",
			Vars: []any{clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}, sub.Select("id")},
		})
	}
	if len(members) == 0 {
		return rows, 0, nil
	}
	base := func() *gorm.DB {
		db := r.DB.WithContext(ctx).
			Model(&database.Recipient{}).
			Where(clause.Or(members...))
		if q != nil && strings.TrimSpace(q.Search) != "" {
			search := strings.ReplaceAll(strings.TrimSpace(q.Search), "%", " ")
			search = strings.ReplaceAll(search, "_", " ")
			like := "%" + search + "%"
			db = db.Where(clause.Or(
				clause.Like{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "email"}, Value: like},
				clause.Like{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "first_name"}, Value: like},
				clause.Like{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "last_name"}, Value: like},
			))
		}
		return db
	}
	var count int64
	if err := base().Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	db := base()
	if q != nil {
		if column, ok := programAudienceSortColumns[q.OrderBy]; ok {
			var err error
			db, err = WithOrderBy(db, column, q.Desc, allowedProgramAudienceColumns...)
			if err != nil {
				return nil, 0, errs.NewValidationError(err)
			}
		}
		db = withOffsetLimit(db, q.Offset, q.Limit)
	}
	// a stable tie break so pages do not overlap
	db = db.Order(clause.OrderByColumn{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"}})
	if err := db.Select("id", "email", "first_name", "last_name").Find(&rows).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return rows, count, nil
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
	// a program past its end date is closed by the scheduler. Until that close
	// works its people are not advanced, so they are not read every tick
	running := r.DB.Model(&database.Program{}).
		Select("id").
		Where(r.runningPrograms(now)).
		Where(clause.Or(
			clause.Eq{Column: clause.Column{Name: "end_at"}, Value: nil},
			clause.Gt{Column: clause.Column{Name: "end_at"}, Value: now.UTC()},
		))
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}).
		Where(clause.Neq{Column: clause.Column{Name: "next_due_at"}, Value: nil}).
		Where(clause.Lte{Column: clause.Column{Name: "next_due_at"}, Value: now.UTC()}).
		// a person missing from the groups waits until they are back or exited.
		// Not read through the missing_since index, which holds every person who
		// is not missing, so the status and due index is read in due order
		Where(clause.Expr{SQL: "? IS NULL", Vars: []any{notIndexed(clause.Column{Name: "missing_since"})}}).
		// a row whose person was deleted has nobody to send to
		Where(linkedEnrollmentNotIndexed()).
		Where("program_id IN (?)", running).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "next_due_at"}}).
		Limit(limit).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}
