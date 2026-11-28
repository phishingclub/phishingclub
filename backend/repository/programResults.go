package repository

import (
	"context"
	"database/sql"
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

// ProgramEnrollmentRecipientAlias is the alias GORM gives the joined recipient
// of an enrollment, used to sort and search the people of a program.
const ProgramEnrollmentRecipientAlias = "Recipient"

// ProgramEnrollmentSortColumns are the columns the program people table may be
// sorted by, keyed by the name the frontend sends.
var ProgramEnrollmentSortColumns = map[string]string{
	"email":       TableColumn(ProgramEnrollmentRecipientAlias, "email"),
	"name":        TableColumn(ProgramEnrollmentRecipientAlias, "first_name"),
	"status":      TableColumn(database.PROGRAM_ENROLLMENT_TABLE, "status"),
	"next_due_at": TableColumn(database.PROGRAM_ENROLLMENT_TABLE, "next_due_at"),
}

var allowedProgramEnrollmentColumns = func() []string {
	out := make([]string, 0, len(ProgramEnrollmentSortColumns))
	for _, v := range ProgramEnrollmentSortColumns {
		out = append(out, v)
	}
	return out
}()

// ProgramCampaignInfo is one hidden campaign of a program
type ProgramCampaignInfo struct {
	ID                 uuid.UUID
	IsTraining         bool
	CampaignTemplateID *uuid.UUID
}

// ProgramSendRow is one sent message of a program, phishing or training
type ProgramSendRow struct {
	ID           uuid.UUID
	CampaignID   uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	SentAt       time.Time
	// NotableEventID is the most notable event of the send, as on a campaign
	NotableEventID *uuid.UUID
	// Language is the language stamp of the send, empty when it has none and
	// is sent in its template language
	Language string
}

// ProgramEventRow is one event on a program campaign
type ProgramEventRow struct {
	// RowID is the row id of the event, see LatestCampaignEventRowID
	RowID        int64
	CampaignID   uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	EventID      uuid.UUID
	CreatedAt    time.Time
}

// GetCampaignInfos returns every hidden campaign of a program with its kind and
// the template it sends
func (r *Program) GetCampaignInfos(ctx context.Context, programID *uuid.UUID) ([]ProgramCampaignInfo, error) {
	rows := []ProgramCampaignInfo{}
	res := r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("id", "is_training", "campaign_template_id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// notIndexed wraps a column in a unary plus, which keeps the value as it is
// but stops SQLite from reading the predicate through an index on the column.
// It steers the planner to a better index when the statistics are missing.
func notIndexed(column clause.Column) clause.Expr {
	return clause.Expr{SQL: "+?", Vars: []any{column}}
}

// inProgramCampaigns limits rows to the hidden campaigns of a program. With
// indexed false the campaign column is not read through an index, so a
// recipient filter picks the recipient index instead.
func (r *Program) inProgramCampaigns(programID *uuid.UUID, indexed bool) clause.Expr {
	column := clause.Column{Name: "campaign_id"}
	if !indexed {
		return clause.Expr{SQL: "? IN (?)", Vars: []any{notIndexed(column), r.programCampaignIDs(programID)}}
	}
	return clause.Expr{SQL: "? IN (?)", Vars: []any{column, r.programCampaignIDs(programID)}}
}

// selectSentRow selects the columns scanSentRows reads. The sent time is
// read through a unary plus, which keeps the stored text as it is but drops
// the declared column type, so the driver hands over the text instead of
// trying its time formats one by one, and parseStoredTime reads it.
func selectSentRow(db *gorm.DB) *gorm.DB {
	return db.Select("?, ?, ?, ?, ?, ?, ?",
		clause.Column{Name: "id"},
		clause.Column{Name: "campaign_id"},
		clause.Column{Name: "recipient_id"},
		clause.Column{Name: "anonymized_id"},
		notIndexed(clause.Column{Name: "sent_at"}),
		clause.Column{Name: "notable_event_id"},
		clause.Column{Name: "language"},
	)
}

// sentRowsQuery selects the sent messages of a program, of the given
// recipients only when recipientIDs is not nil
func (r *Program) sentRowsQuery(ctx context.Context, programID *uuid.UUID, recipientIDs []*uuid.UUID) *gorm.DB {
	db := selectSentRow(r.DB.WithContext(ctx).Model(&database.CampaignRecipient{})).
		Where(r.inProgramCampaigns(programID, true))
	if recipientIDs == nil {
		return db.Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil})
	}
	// a few people are found through the campaign and recipient index, not
	// through the sent_at index that also holds every other campaign
	return db.
		Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
		Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs)})
}

// EachSentRow calls fn with every sent message of a program, read one row at
// a time so a large program is never held in memory twice. When recipientIDs
// is not nil only those recipients' messages are read. The row passed to fn is
// reused, fn must copy what it keeps.
func (r *Program) EachSentRow(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	fn func(*ProgramSendRow) error,
) error {
	if recipientIDs != nil && len(recipientIDs) == 0 {
		return nil
	}
	rows, err := r.sentRowsQuery(ctx, programID, recipientIDs).Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanSentRows(rows, fn)
}

// programSentRowBatch is how many people one read of their sends names
const programSentRowBatch = 500

// EachSentRowOfPeople calls fn with every sent message of a program to the
// given people, matched by recipient id or by pseudonym, read in batches
// through the recipient and pseudonym indexes. A row that matches both ways
// is passed once. The row passed to fn is reused, fn must copy what it keeps.
func (r *Program) EachSentRowOfPeople(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	anonymizedIDs []*uuid.UUID,
	fn func(*ProgramSendRow) error,
) error {
	seen := map[uuid.UUID]bool{}
	once := func(row *ProgramSendRow) error {
		if seen[row.ID] {
			return nil
		}
		seen[row.ID] = true
		return fn(row)
	}
	for _, by := range []struct {
		column string
		ids    []*uuid.UUID
	}{{"recipient_id", recipientIDs}, {"anonymized_id", anonymizedIDs}} {
		for start := 0; start < len(by.ids); start += programSentRowBatch {
			batch := by.ids[start:min(start+programSentRowBatch, len(by.ids))]
			rows, err := selectSentRow(r.DB.WithContext(ctx).Model(&database.CampaignRecipient{})).
				Where(r.inProgramCampaigns(programID, false)).
				Where(clause.Expr{SQL: "? IS NOT NULL", Vars: []any{notIndexed(clause.Column{Name: "sent_at"})}}).
				Where(clause.IN{Column: clause.Column{Name: by.column}, Values: uuidValues(batch)}).
				Rows()
			if err != nil {
				return errs.Wrap(err)
			}
			if err := scanSentRows(rows, once); err != nil {
				return err
			}
		}
	}
	return nil
}

// ProgramPersonReport is a report event of a program send to a person
type ProgramPersonReport struct {
	CampaignID   uuid.UUID
	RecipientID  *uuid.UUID
	AnonymizedID *uuid.UUID
	CreatedAt    time.Time
}

// GetReportsOfPeople returns the events of eventID in the campaigns of a
// program of the given people, found by recipient id or pseudonym, read in
// batches through the person indexes
func (r *Program) GetReportsOfPeople(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	anonymizedIDs []*uuid.UUID,
	eventID *uuid.UUID,
) ([]ProgramPersonReport, error) {
	out := []ProgramPersonReport{}
	for _, by := range []struct {
		column string
		ids    []*uuid.UUID
	}{{"recipient_id", recipientIDs}, {"anonymized_id", anonymizedIDs}} {
		for start := 0; start < len(by.ids); start += programSentRowBatch {
			batch := by.ids[start:min(start+programSentRowBatch, len(by.ids))]
			rows := []ProgramPersonReport{}
			if err := r.DB.WithContext(ctx).
				Model(&database.CampaignEvent{}).
				Select("campaign_id", "recipient_id", "anonymized_id", "created_at").
				Where(r.inProgramCampaigns(programID, false)).
				Where(clause.Eq{Column: clause.Column{Name: "event_id"}, Value: eventID}).
				Where(clause.IN{Column: clause.Column{Name: by.column}, Values: uuidValues(batch)}).
				Scan(&rows).Error; err != nil {
				return nil, errs.Wrap(err)
			}
			out = append(out, rows...)
		}
	}
	return out, nil
}

// ProgramRecipientName is the email and name of a recipient
type ProgramRecipientName struct {
	ID        uuid.UUID
	Email     sql.NullString
	FirstName sql.NullString
	LastName  sql.NullString
}

// GetRecipientNames returns the email and name of the given recipients, read
// in batches and only those columns. A recipient that no longer exists is
// left out.
func (r *Program) GetRecipientNames(ctx context.Context, ids []*uuid.UUID) ([]ProgramRecipientName, error) {
	out := []ProgramRecipientName{}
	for start := 0; start < len(ids); start += programSentRowBatch {
		batch := ids[start:min(start+programSentRowBatch, len(ids))]
		rows := []ProgramRecipientName{}
		if err := r.DB.WithContext(ctx).
			Model(&database.Recipient{}).
			Select("id", "email", "first_name", "last_name").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(batch)}).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ProgramPersonEmail is an enrolled person of a program with the email of
// the recipient
type ProgramPersonEmail struct {
	RecipientID  uuid.UUID
	AnonymizedID *uuid.UUID
	Email        string
}

// GetEnrollmentEmails returns every enrolled person of a program whose
// recipient has an email, with that email, in one query that reads the three
// columns only
func (r *Program) GetEnrollmentEmails(ctx context.Context, programID *uuid.UUID) ([]ProgramPersonEmail, error) {
	rows := []ProgramPersonEmail{}
	err := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("?, ?, ?",
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "anonymized_id"},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "email"},
		).
		Joins("JOIN ? ON ? = ?",
			clause.Table{Name: database.RECIPIENT_TABLE},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
		).
		Where(programIDEq(clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programID)).
		Where(clause.Neq{Column: clause.Column{Table: database.RECIPIENT_TABLE, Name: "email"}, Value: nil}).
		Scan(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// ProgramPersonGroups is an enrolled person of a program with the current
// department and position of the recipient
type ProgramPersonGroups struct {
	RecipientID uuid.UUID
	Department  sql.NullString
	Position    sql.NullString
}

// GetEnrollmentGroups returns every enrolled person of a program whose
// recipient still exists, with the current department and position of the
// recipient, in one query that reads the three columns only
func (r *Program) GetEnrollmentGroups(ctx context.Context, programID *uuid.UUID) ([]ProgramPersonGroups, error) {
	rows := []ProgramPersonGroups{}
	err := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("?, ?, ?",
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "department"},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "position"},
		).
		Joins("JOIN ? ON ? = ?",
			clause.Table{Name: database.RECIPIENT_TABLE},
			clause.Column{Table: database.RECIPIENT_TABLE, Name: "id"},
			clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "recipient_id"},
		).
		Where(programIDEq(clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programID)).
		Scan(&rows).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rows, nil
}

// EachSentRowAfter calls fn with every sent message of a program sent after
// the given stored sent time, see LatestSentAt. The rows are found through the
// sent_at index, so only the newer rows are read. The row passed to fn is
// reused, fn must copy what it keeps.
func (r *Program) EachSentRowAfter(
	ctx context.Context,
	programID *uuid.UUID,
	sentAt string,
	fn func(*ProgramSendRow) error,
) error {
	rows, err := selectSentRow(r.DB.WithContext(ctx).Model(&database.CampaignRecipient{})).
		Where(clause.Gt{Column: clause.Column{Name: "sent_at"}, Value: sentAt}).
		Where(r.inProgramCampaigns(programID, false)).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanSentRows(rows, fn)
}

// CountSentRows counts the sent messages of a program that name a person, the
// rows EachSentRow passes on. The predicates repeat those of the partial
// index idx_campaign_recipients_campaign_sent_at, so the count reads that index
// only and not the table rows.
func (r *Program) CountSentRows(ctx context.Context, programID *uuid.UUID) (int64, error) {
	var count int64
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Where(r.inProgramCampaigns(programID, true)).
		Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "recipient_id"}, Value: nil},
			clause.Neq{Column: clause.Column{Name: "anonymized_id"}, Value: nil},
		)).
		Count(&count).Error
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return count, nil
}

// scanSentRows calls fn with each sent message row and closes the rows. The
// sent time is read as text, see selectSentRow.
func scanSentRows(rows *sql.Rows, fn func(*ProgramSendRow) error) error {
	defer rows.Close()
	var (
		row                      ProgramSendRow
		recipient, anon, notable uuid.NullUUID
		sentAt                   sql.RawBytes
		language                 sql.NullString
	)
	for rows.Next() {
		if err := rows.Scan(&row.ID, &row.CampaignID, &recipient, &anon, &sentAt, &notable, &language); err != nil {
			return errs.Wrap(err)
		}
		row.Language = language.String
		row.RecipientID = nullUUIDPtr(recipient)
		row.AnonymizedID = nullUUIDPtr(anon)
		row.NotableEventID = nullUUIDPtr(notable)
		row.SentAt = parseStoredTime(sentAt)
		if err := fn(&row); err != nil {
			return err
		}
	}
	return errs.Wrap(rows.Err())
}

// storedTimeForms are the forms the SQLite driver reads a stored time in,
// tried in order when parseStoredTime can not read the text itself
var storedTimeForms = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// parseStoredTime reads a time stored as text, the zero time for null or
// text that is not a time. The forms the application writes, a date and a
// time to the second with an optional fraction and zone, are read directly,
// which is several times faster than time.Parse. Any other text is read in
// the forms of the driver, as a time column read without the unary plus.
func parseStoredTime(b []byte) time.Time {
	if len(b) == 0 {
		return time.Time{}
	}
	if t, ok := parseStoredTimeFast(b); ok {
		return t
	}
	v := strings.TrimSuffix(string(b), "Z")
	for _, form := range storedTimeForms {
		if t, err := time.ParseInLocation(form, v, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseStoredTimeFast reads YYYY-MM-DD HH:MM:SS with a space or a T between
// the date and the time, an optional fraction of up to nine digits and an
// optional zone, Z or +HH:MM or -HH:MM. A time without a zone is UTC. It
// reports false for anything else.
func parseStoredTimeFast(b []byte) (time.Time, bool) {
	if len(b) < 19 || b[4] != '-' || b[7] != '-' || (b[10] != ' ' && b[10] != 'T') || b[13] != ':' || b[16] != ':' {
		return time.Time{}, false
	}
	num := func(from, to int) (int, bool) {
		n := 0
		for _, c := range b[from:to] {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		return n, true
	}
	year, ok1 := num(0, 4)
	month, ok2 := num(5, 7)
	day, ok3 := num(8, 10)
	hour, ok4 := num(11, 13)
	minute, ok5 := num(14, 16)
	sec, ok6 := num(17, 19)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 ||
		month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || sec > 59 {
		return time.Time{}, false
	}
	i := 19
	nanos := 0
	if i < len(b) && b[i] == '.' {
		i++
		digits := 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			if digits == 9 {
				return time.Time{}, false
			}
			nanos = nanos*10 + int(b[i]-'0')
			digits++
			i++
		}
		if digits == 0 {
			return time.Time{}, false
		}
		for ; digits < 9; digits++ {
			nanos *= 10
		}
	}
	offset := 0
	switch {
	case i == len(b):
	case b[i] == 'Z' && i+1 == len(b):
	case (b[i] == '+' || b[i] == '-') && i+6 == len(b) && b[i+3] == ':':
		oh, okh := num(i+1, i+3)
		om, okm := num(i+4, i+6)
		if !okh || !okm || oh > 23 || om > 59 {
			return time.Time{}, false
		}
		offset = oh*3600 + om*60
		if b[i] == '-' {
			offset = -offset
		}
	default:
		return time.Time{}, false
	}
	t := time.Date(year, time.Month(month), day, hour, minute, sec, nanos, time.UTC)
	// a day past the end of its month is not a date, time.Date would move it
	if t.Day() != day {
		return time.Time{}, false
	}
	return t.Add(-time.Duration(offset) * time.Second), true
}

// nullUUIDPtr returns a pointer to a copy of the id, nil when it is null
func nullUUIDPtr(v uuid.NullUUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := v.UUID
	return &id
}

// GetSentRows returns every sent message of a program. When recipientIDs is
// not nil only those recipients' messages are returned.
func (r *Program) GetSentRows(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
) ([]ProgramSendRow, error) {
	out := []ProgramSendRow{}
	err := r.EachSentRow(ctx, programID, recipientIDs, func(row *ProgramSendRow) error {
		out = append(out, *row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EachEventRow calls fn with the given events on every campaign of a program,
// read one row at a time. When recipientIDs is not nil only those recipients'
// events are read. The row passed to fn is reused, fn must copy what it keeps.
func (r *Program) EachEventRow(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	eventIDs []*uuid.UUID,
	fn func(*ProgramEventRow) error,
) error {
	values := uuidValues(eventIDs)
	if len(values) == 0 || (recipientIDs != nil && len(recipientIDs) == 0) {
		return nil
	}
	// a few people are found through the recipient index rather than every
	// event of the program
	db := selectEventRow(r.DB.WithContext(ctx).Model(&database.CampaignEvent{})).
		Where(r.inProgramCampaigns(programID, recipientIDs == nil)).
		Where(clause.IN{Column: clause.Column{Name: "event_id"}, Values: values})
	if recipientIDs != nil {
		db = db.Where(clause.IN{Column: clause.Column{Name: "recipient_id"}, Values: uuidValues(recipientIDs)})
	}
	rows, err := db.Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanEventRows(rows, fn)
}

// EachEventRowAfter calls fn with the given events on every campaign of a
// program added after the event with the given row id. Only the newer rows
// are read, through the row id. The row passed to fn is reused, fn must copy
// what it keeps.
func (r *Program) EachEventRowAfter(
	ctx context.Context,
	programID *uuid.UUID,
	rowID int64,
	eventIDs []*uuid.UUID,
	fn func(*ProgramEventRow) error,
) error {
	values := uuidValues(eventIDs)
	if len(values) == 0 {
		return nil
	}
	rows, err := selectEventRow(r.DB.WithContext(ctx).Model(&database.CampaignEvent{})).
		Where(clause.Gt{Column: clause.Column{Name: "rowid"}, Value: rowID}).
		Where(r.inProgramCampaigns(programID, false)).
		Where(clause.IN{Column: clause.Column{Name: "event_id"}, Values: values}).
		Rows()
	if err != nil {
		return errs.Wrap(err)
	}
	return scanEventRows(rows, fn)
}

// selectEventRow selects the columns scanEventRows reads, the time as text
// as in selectSentRow
func selectEventRow(db *gorm.DB) *gorm.DB {
	return db.Select("?, ?, ?, ?, ?, ?",
		clause.Column{Name: "rowid"},
		clause.Column{Name: "campaign_id"},
		clause.Column{Name: "recipient_id"},
		clause.Column{Name: "anonymized_id"},
		clause.Column{Name: "event_id"},
		notIndexed(clause.Column{Name: "created_at"}),
	)
}

// scanEventRows calls fn with each event row and closes the rows. The time is
// read as text, see selectEventRow.
func scanEventRows(rows *sql.Rows, fn func(*ProgramEventRow) error) error {
	defer rows.Close()
	var (
		row             ProgramEventRow
		recipient, anon uuid.NullUUID
		createdAt       sql.RawBytes
	)
	for rows.Next() {
		if err := rows.Scan(&row.RowID, &row.CampaignID, &recipient, &anon, &row.EventID, &createdAt); err != nil {
			return errs.Wrap(err)
		}
		row.RecipientID = nullUUIDPtr(recipient)
		row.AnonymizedID = nullUUIDPtr(anon)
		row.CreatedAt = parseStoredTime(createdAt)
		if err := fn(&row); err != nil {
			return err
		}
	}
	return errs.Wrap(rows.Err())
}

// GetEventRows returns the given events on every campaign of a program. When
// recipientIDs is not nil only those recipients' events are returned.
func (r *Program) GetEventRows(
	ctx context.Context,
	programID *uuid.UUID,
	recipientIDs []*uuid.UUID,
	eventIDs []*uuid.UUID,
) ([]ProgramEventRow, error) {
	out := []ProgramEventRow{}
	err := r.EachEventRow(ctx, programID, recipientIDs, eventIDs, func(row *ProgramEventRow) error {
		out = append(out, *row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// programPhishingCampaignIDs selects the ids of a program's phishing campaigns
func (r *Program) programPhishingCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		// the program index finds the few campaigns of the program, the index
		// on is_training would walk every phishing campaign
		Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "is_training"}), false}})
}

// programTrainingCampaignIDs selects the ids of the training campaigns of a
// program
func (r *Program) programTrainingCampaignIDs(programID *uuid.UUID) *gorm.DB {
	return r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		// the program index finds the few campaigns of the program
		Where(clause.Expr{SQL: "? = ?", Vars: []any{notIndexed(clause.Column{Name: "is_training"}), true}})
}

// GetLatestPhishingSend returns the latest sent phishing message of a program
// to one person, sent at or before the given time when it is set. The person is
// matched by recipient id or by pseudonym, as an anonymous program row may only
// carry the pseudonym.
func (r *Program) GetLatestPhishingSend(
	ctx context.Context,
	programID *uuid.UUID,
	recipientID *uuid.UUID,
	anonymizedID *uuid.UUID,
	before *time.Time,
) (*model.CampaignRecipient, error) {
	who := []clause.Expression{}
	if recipientID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID})
	}
	if anonymizedID != nil {
		who = append(who, clause.Eq{Column: clause.Column{Name: "anonymized_id"}, Value: anonymizedID})
	}
	if len(who) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	db := r.DB.WithContext(ctx).
		Where("campaign_id IN (?)", r.programPhishingCampaignIDs(programID)).
		Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
		Where(clause.Or(who...))
	if before != nil {
		// sent_at is stored as RFC 3339 text, so compare in that form
		db = db.Where(clause.Lte{Column: clause.Column{Name: "sent_at"}, Value: utils.RFC3339UTC(*before)})
	}
	row := database.CampaignRecipient{}
	res := db.Order(clause.OrderByColumn{Column: clause.Column{Name: "sent_at"}, Desc: true}).
		Limit(1).
		Find(&row)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return ToCampaignRecipient(&row)
}

// ProgramEnrollmentStatusCount is how many enrollments of a program have a status
type ProgramEnrollmentStatusCount struct {
	Status string
	Count  int
}

// GetEnrollmentStatusCounts counts the people of a program by status. Once
// Anonymize All began the enrollments are removed, so the counts stored when
// it began are given, see BeginAnonymize.
func (r *Program) GetEnrollmentStatusCounts(
	ctx context.Context,
	programID *uuid.UUID,
) ([]ProgramEnrollmentStatusCount, error) {
	stored, ok, err := r.storedPeopleCounts(ctx, programID)
	if err != nil {
		return nil, err
	}
	if ok {
		return stored, nil
	}
	return r.CountEnrollmentsByStatus(ctx, programID)
}

// CountEnrollmentsByStatus counts the enrollments of a program by status
func (r *Program) CountEnrollmentsByStatus(
	ctx context.Context,
	programID *uuid.UUID,
) ([]ProgramEnrollmentStatusCount, error) {
	rows := []ProgramEnrollmentStatusCount{}
	res := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("status", "COUNT(*) AS count").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Group("status").
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// GetEnrollmentPage returns enrollments of a program with their recipient,
// searched on email and name and sorted by an allowlisted column. A query
// without a limit returns every match. The count is the number of matches.
func (r *Program) GetEnrollmentPage(
	ctx context.Context,
	programID *uuid.UUID,
	q *vo.QueryArgs,
) ([]*database.ProgramEnrollment, int64, error) {
	base := func() *gorm.DB {
		db := r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Joins(ProgramEnrollmentRecipientAlias).
			Where(programIDEq(clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programID)).
			// a deleted recipient has no row, it is counted in the totals only
			Where(linkedEnrollment())
		if q != nil && strings.TrimSpace(q.Search) != "" {
			search := strings.ReplaceAll(strings.TrimSpace(q.Search), "%", " ")
			search = strings.ReplaceAll(search, "_", " ")
			like := "%" + search + "%"
			db = db.Where(clause.Or(
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "email"}, Value: like},
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "first_name"}, Value: like},
				clause.Like{Column: clause.Column{Table: ProgramEnrollmentRecipientAlias, Name: "last_name"}, Value: like},
			))
		}
		return db
	}
	// without a search every enrollment of the program matches, so the count
	// reads the program index alone and not the recipient of each row. The
	// recipient is a left join, so both count the same rows.
	countDB := base()
	if q == nil || strings.TrimSpace(q.Search) == "" {
		countDB = r.DB.WithContext(ctx).
			Model(&database.ProgramEnrollment{}).
			Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
			Where(linkedEnrollment())
	}
	var count int64
	if err := countDB.Count(&count).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	db := base()
	if q != nil {
		if q.OrderBy != "" {
			var err error
			db, err = WithOrderBy(db, q.OrderBy, q.Desc, allowedProgramEnrollmentColumns...)
			if err != nil {
				return nil, 0, errs.NewValidationError(err)
			}
		}
		db = withOffsetLimit(db, q.Offset, q.Limit)
	}
	// a stable tie break so pages do not overlap
	db = db.Order(clause.OrderByColumn{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "id"}})
	rows := []*database.ProgramEnrollment{}
	if err := db.Find(&rows).Error; err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return rows, count, nil
}

// GetEnrollmentsByRecipientID returns every enrollment of a recipient
func (r *Program) GetEnrollmentsByRecipientID(
	ctx context.Context,
	recipientID *uuid.UUID,
) ([]*database.ProgramEnrollment, error) {
	rows := []*database.ProgramEnrollment{}
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_id"}, Value: recipientID}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// ProgramTemplateInfo is the name and send flags of a template
type ProgramTemplateInfo struct {
	ID         uuid.UUID
	Name       string
	IsUsable   bool
	IsTraining bool
}

// GetTemplateInfos returns the name and flags of the given templates in one
// query per batch. A template that no longer exists is left out.
func (r *Program) GetTemplateInfos(ctx context.Context, ids []*uuid.UUID) (map[uuid.UUID]ProgramTemplateInfo, error) {
	out := map[uuid.UUID]ProgramTemplateInfo{}
	seen := map[uuid.UUID]bool{}
	unique := make([]*uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		unique = append(unique, id)
	}
	for start := 0; start < len(unique); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(unique))
		rows := []ProgramTemplateInfo{}
		if err := r.DB.WithContext(ctx).
			Model(&database.CampaignTemplate{}).
			Select("id", "name", "is_usable", "is_training").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(unique[start:end])}).
			Scan(&rows).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for _, row := range rows {
			out[row.ID] = row
		}
	}
	return out, nil
}

// LatestCampaignEventRowID returns the row id of the newest campaign event.
// SQLite gives a new row a higher id than every row there is, so a later
// event has a higher id, but the table has no integer key: once the newest
// rows are deleted their ids are given out again, and a new event can have an
// id at or below one read before. CountProgramEventsUpTo finds such an event.
func (r *Program) LatestCampaignEventRowID(ctx context.Context) (int64, error) {
	var latest sql.NullInt64
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select("MAX(?)", clause.Column{Name: "rowid"}).
		Scan(&latest).Error
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return latest.Int64, nil
}

// HasProgramEventsAfter reports whether a program has an event added after
// the event with the given row id. Only the newer rows are read, through the
// row id, so the check stays cheap however many events the program has.
func (r *Program) HasProgramEventsAfter(ctx context.Context, programID *uuid.UUID, rowID int64) (bool, error) {
	var found []int
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Select("1").
		Where(clause.Gt{Column: clause.Column{Name: "rowid"}, Value: rowID}).
		Where(r.inProgramCampaigns(programID, false)).
		Limit(1).
		Scan(&found).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return len(found) > 0, nil
}

// CountProgramEventsUpTo counts the given events on every campaign of a
// program with a row id at or below rowID. The count reads the campaign and
// event index only, which also holds the row id. A count that differs from the
// events read up to that id tells an event was added with a row id given out
// again, or an event was removed.
func (r *Program) CountProgramEventsUpTo(
	ctx context.Context,
	programID *uuid.UUID,
	rowID int64,
	eventIDs []*uuid.UUID,
) (int64, error) {
	values := uuidValues(eventIDs)
	if len(values) == 0 {
		return 0, nil
	}
	var count int64
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignEvent{}).
		Where(r.inProgramCampaigns(programID, true)).
		Where(clause.IN{Column: clause.Column{Name: "event_id"}, Values: values}).
		Where(clause.Lte{Column: clause.Column{Name: "rowid"}, Value: rowID}).
		Count(&count).Error
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return count, nil
}

// LatestSentAt returns the newest sent time of any send as it is stored, empty
// when nothing is sent. The sent time is stored as RFC 3339 in UTC, so the
// stored text orders like the time, to the second. It is read from the
// sent_at index.
func (r *Program) LatestSentAt(ctx context.Context) (string, error) {
	var latest sql.NullString
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("MAX(?)", clause.Column{Name: "sent_at"}).
		Scan(&latest).Error
	if err != nil {
		return "", errs.Wrap(err)
	}
	return latest.String, nil
}

// HasProgramSendsAfter reports whether a send of a program was sent after the
// given stored sent time, see LatestSentAt. Only the rows sent after it are
// read, through the sent_at index, so the check stays cheap however many
// sends the program has.
func (r *Program) HasProgramSendsAfter(ctx context.Context, programID *uuid.UUID, sentAt string) (bool, error) {
	var found []int
	err := r.DB.WithContext(ctx).
		Model(&database.CampaignRecipient{}).
		Select("1").
		Where(clause.Gt{Column: clause.Column{Name: "sent_at"}, Value: sentAt}).
		Where(r.inProgramCampaigns(programID, false)).
		Limit(1).
		Scan(&found).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return len(found) > 0, nil
}

// namesByID reads the id and name of the rows of a table with the given ids,
// in the order of ids. A row that no longer exists is left out.
func (r *Program) namesByID(ctx context.Context, table any, ids []*uuid.UUID) ([]model.ProgramNamedRef, error) {
	out := []model.ProgramNamedRef{}
	values := uuidValues(ids)
	if len(values) == 0 {
		return out, nil
	}
	rows := []struct {
		ID   uuid.UUID
		Name string
	}{}
	if err := r.DB.WithContext(ctx).
		Model(table).
		Select("id", "name").
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: values}).
		Scan(&rows).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	byID := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		byID[row.ID] = row.Name
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == nil || seen[*id] {
			continue
		}
		seen[*id] = true
		if name, ok := byID[*id]; ok {
			out = append(out, model.ProgramNamedRef{ID: id.String(), Name: name})
		}
	}
	return out, nil
}

// GetAllowDenyAllowed reports whether an allow or deny list allows only what
// it holds
func (r *Program) GetAllowDenyAllowed(ctx context.Context, id *uuid.UUID) (bool, error) {
	var allowed []bool
	if err := r.DB.WithContext(ctx).
		Model(&database.AllowDeny{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Limit(1).
		Pluck("allowed", &allowed).Error; err != nil {
		return false, errs.Wrap(err)
	}
	if len(allowed) == 0 {
		return false, errs.Wrap(gorm.ErrRecordNotFound)
	}
	return allowed[0], nil
}

// ProgramNameKind is a kind of row a program refers to by id
type ProgramNameKind int

// the kinds of rows a program names
const (
	ProgramNameAllowDeny ProgramNameKind = iota
	ProgramNamePage
	ProgramNameWebhook
	ProgramNameCompany
)

// GetNames returns the id and name of the rows of one kind with the given ids,
// in the order of ids. A row that no longer exists is left out.
func (r *Program) GetNames(ctx context.Context, kind ProgramNameKind, ids []*uuid.UUID) ([]model.ProgramNamedRef, error) {
	switch kind {
	case ProgramNameAllowDeny:
		return r.namesByID(ctx, &database.AllowDeny{}, ids)
	case ProgramNamePage:
		return r.namesByID(ctx, &database.Page{}, ids)
	case ProgramNameWebhook:
		return r.namesByID(ctx, &database.Webhook{}, ids)
	case ProgramNameCompany:
		return r.namesByID(ctx, &database.Company{}, ids)
	}
	return []model.ProgramNamedRef{}, nil
}
